// Package parser implements the w_popularity telegram adapter.
//
// Strategy: scrape the public preview at https://t.me/s/<handle>. That
// endpoint exposes subscriber counts and the last ~20 posts inline for
// public CHANNELS. User handles (and private/missing channels) have no
// preview and return ErrNotFound.
package parser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	shared "github.com/suenot/w-popularity-shared"
	"golang.org/x/net/html"
)

// Config controls runtime behaviour.
type Config struct {
	// Credential is unused for the public preview scraper; kept for
	// symmetry with sibling parsers.
	Credential string
	// HTTPTimeout caps every outbound call. Default: 15s.
	HTTPTimeout time.Duration
	// CamoufoxURL is unused; reserved for future browser fallback.
	CamoufoxURL string
	// BaseURL overrides https://t.me. Used in tests.
	BaseURL string
	// HTTPClient overrides the default HTTP client. Used in tests.
	HTTPClient *http.Client
	// UserAgent overrides the request UA. Default: Mozilla/5.0.
	UserAgent string
}

// New constructs a TelegramParser.
func New(cfg Config) *TelegramParser {
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 15 * time.Second
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://t.me"
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.HTTPTimeout}
	}
	return &TelegramParser{cfg: cfg}
}

// TelegramParser scrapes the public t.me/s/ preview.
type TelegramParser struct{ cfg Config }

// Platform implements shared.Parser.
func (p *TelegramParser) Platform() shared.Platform { return shared.PlatformTelegram }

// FetchChannel returns a snapshot for the given public channel handle.
// Users (and private/missing channels) return shared.ErrNotFound.
func (p *TelegramParser) FetchChannel(ctx context.Context, handle string) (shared.ChannelSnapshot, error) {
	doc, finalURL, err := p.fetchPreview(ctx, handle)
	if err != nil {
		return shared.ChannelSnapshot{}, err
	}
	if !hasPreviewMarker(doc) {
		return shared.ChannelSnapshot{}, fmt.Errorf("%w: %q is not a public channel (no t.me/s preview at %s)", shared.ErrNotFound, handle, finalURL)
	}

	counters := extractCounters(doc)
	title := extractTitle(doc)

	snap := shared.ChannelSnapshot{
		Platform:  shared.PlatformTelegram,
		Handle:    handle,
		URL:       fmt.Sprintf("https://t.me/%s", handle),
		FetchedAt: time.Now().UTC(),
		Followers: counters["subscribers"],
		Raw:       map[string]interface{}{},
	}
	// Posts count is rarely surfaced directly. Approximate as sum of media counters.
	if pc, ok := counters["posts"]; ok {
		snap.PostsCount = pc
	} else {
		snap.PostsCount = counters["photos"] + counters["videos"] + counters["files"] + counters["links"]
	}
	if title != "" {
		snap.Raw["title"] = title
	}
	for k, v := range counters {
		snap.Raw[k] = v
	}
	return snap, nil
}

// FetchRecentPosts parses the embedded posts in the /s/ preview.
// Posts older than `since` are filtered out. Newest-first.
func (p *TelegramParser) FetchRecentPosts(ctx context.Context, handle string, since time.Time) ([]shared.PostSnapshot, error) {
	doc, finalURL, err := p.fetchPreview(ctx, handle)
	if err != nil {
		return nil, err
	}
	if !hasPreviewMarker(doc) {
		return nil, fmt.Errorf("%w: %q is not a public channel (no t.me/s preview at %s)", shared.ErrNotFound, handle, finalURL)
	}

	now := time.Now().UTC()
	var out []shared.PostSnapshot
	for _, msg := range extractMessages(doc) {
		if msg.publishedAt.Before(since) {
			continue
		}
		postID := msg.postID
		if slash := strings.LastIndex(postID, "/"); slash >= 0 {
			postID = postID[slash+1:]
		}
		out = append(out, shared.PostSnapshot{
			Platform:      shared.PlatformTelegram,
			ChannelHandle: handle,
			PostID:        postID,
			URL:           fmt.Sprintf("https://t.me/%s/%s", handle, postID),
			Kind:          shared.PostKindPost,
			PublishedAt:   msg.publishedAt,
			FetchedAt:     now,
			Views:         msg.views,
		})
	}
	// Reverse to newest-first (Telegram renders oldest→newest in the /s/ preview).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// fetchPreview GETs /s/<handle> and returns the parsed document and the
// final URL after redirects.
func (p *TelegramParser) fetchPreview(ctx context.Context, handle string) (*html.Node, string, error) {
	if handle == "" {
		return nil, "", fmt.Errorf("%w: empty handle", shared.ErrNotFound)
	}
	u := fmt.Sprintf("%s/s/%s", strings.TrimRight(p.cfg.BaseURL, "/"), url.PathEscape(handle))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, u, fmt.Errorf("%w: build request: %v", shared.ErrTransient, err)
	}
	req.Header.Set("User-Agent", p.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		// context cancellation surfaces as is so callers can detect it.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, u, err
		}
		return nil, u, fmt.Errorf("%w: http: %v", shared.ErrTransient, err)
	}
	defer resp.Body.Close()

	finalURL := u
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, finalURL, fmt.Errorf("%w: http 404 for %s", shared.ErrNotFound, finalURL)
	case resp.StatusCode >= 500:
		return nil, finalURL, fmt.Errorf("%w: http %d for %s", shared.ErrTransient, resp.StatusCode, finalURL)
	case resp.StatusCode >= 400:
		return nil, finalURL, fmt.Errorf("%w: http %d for %s", shared.ErrTransient, resp.StatusCode, finalURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		return nil, finalURL, fmt.Errorf("%w: read body: %v", shared.ErrTransient, err)
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, finalURL, fmt.Errorf("%w: parse html: %v", shared.ErrTransient, err)
	}
	return doc, finalURL, nil
}

// hasPreviewMarker checks for `tgme_channel_info_counter` — the
// canonical signal that we actually rendered a public-channel preview.
func hasPreviewMarker(doc *html.Node) bool {
	found := false
	walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && hasClass(n, "tgme_channel_info_counter") {
			found = true
			return false
		}
		return true
	})
	return found
}

// extractCounters returns a map like {"subscribers": 11400000, "photos": 98, ...}.
func extractCounters(doc *html.Node) map[string]int64 {
	out := map[string]int64{}
	walk(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || !hasClass(n, "tgme_channel_info_counter") {
			return true
		}
		var rawVal, rawType string
		walk(n, func(c *html.Node) bool {
			if c.Type != html.ElementNode {
				return true
			}
			if hasClass(c, "counter_value") {
				rawVal = nodeText(c)
			}
			if hasClass(c, "counter_type") {
				rawType = strings.ToLower(strings.TrimSpace(nodeText(c)))
			}
			return true
		})
		v, ok := parseShortNum(rawVal)
		if ok && rawType != "" {
			out[rawType] = v
		}
		return true
	})
	return out
}

// extractTitle pulls the channel's human-readable title.
func extractTitle(doc *html.Node) string {
	var title string
	walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && hasClass(n, "tgme_channel_info_header_title") {
			title = strings.TrimSpace(nodeText(n))
			return false
		}
		return true
	})
	return title
}

// messageInfo is the minimal data we keep per widget message.
type messageInfo struct {
	postID      string
	publishedAt time.Time
	views       int64
}

// extractMessages walks the tree pulling out each tgme_widget_message.
func extractMessages(doc *html.Node) []messageInfo {
	var out []messageInfo
	walk(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || !hasClass(n, "tgme_widget_message") {
			return true
		}
		dp := attr(n, "data-post")
		if dp == "" {
			return true
		}
		var msg messageInfo
		msg.postID = dp
		walk(n, func(c *html.Node) bool {
			if c.Type != html.ElementNode {
				return true
			}
			if c.Data == "time" {
				if dt := attr(c, "datetime"); dt != "" && msg.publishedAt.IsZero() {
					if t, err := time.Parse(time.RFC3339, dt); err == nil {
						msg.publishedAt = t.UTC()
					}
				}
			}
			if hasClass(c, "tgme_widget_message_views") && msg.views == 0 {
				if v, ok := parseShortNum(nodeText(c)); ok {
					msg.views = v
				}
			}
			return true
		})
		out = append(out, msg)
		return true
	})
	return out
}

// parseShortNum converts Telegram's compact counters ("1.2K", "5M",
// "2.1B", "1,234", "98") to int64. Returns false if it can't parse.
func parseShortNum(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")

	mult := int64(1)
	if n := len(s); n > 0 {
		switch last := s[n-1]; last {
		case 'K', 'k':
			mult = 1_000
			s = s[:n-1]
		case 'M', 'm':
			mult = 1_000_000
			s = s[:n-1]
		case 'B', 'b':
			mult = 1_000_000_000
			s = s[:n-1]
		}
	}
	if s == "" {
		return 0, false
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i * mult, true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f * float64(mult)), true
	}
	return 0, false
}

// --- html helpers ---

// walk depth-first visits every node. Return false from fn to stop
// descending into the current subtree.
func walk(n *html.Node, fn func(*html.Node) bool) {
	if n == nil {
		return
	}
	if !fn(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func hasClass(n *html.Node, want string) bool {
	if n == nil {
		return false
	}
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == want {
					return true
				}
			}
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	walk(n, func(c *html.Node) bool {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		return true
	})
	return b.String()
}
