package parser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shared "github.com/suenot/w-popularity-shared"
)

// fakeChannelHTML mimics the minimum structure t.me/s/<handle> emits for
// a public channel: header title, channel_info_counter blocks, and two
// widget messages with datetime+views.
const fakeChannelHTML = `<!DOCTYPE html>
<html><body>
<div class="tgme_channel_info">
  <div class="tgme_channel_info_header">
    <div class="tgme_channel_info_header_title"><span>Test Channel</span></div>
    <div class="tgme_channel_info_header_username"><a href="/test">@test</a></div>
  </div>
  <div class="tgme_channel_info_counters">
    <div class="tgme_channel_info_counter"><span class="counter_value">11.4M</span> <span class="counter_type">subscribers</span></div>
    <div class="tgme_channel_info_counter"><span class="counter_value">98</span> <span class="counter_type">photos</span></div>
    <div class="tgme_channel_info_counter"><span class="counter_value">1.2K</span> <span class="counter_type">videos</span></div>
    <div class="tgme_channel_info_counter"><span class="counter_value">186</span> <span class="counter_type">links</span></div>
  </div>
</div>
<section class="tgme_channel_history">
  <div class="tgme_widget_message_wrap">
    <div class="tgme_widget_message" data-post="test/100">
      <div class="tgme_widget_message_bubble">
        <div class="tgme_widget_message_footer">
          <a class="tgme_widget_message_date" href="https://t.me/test/100"><time datetime="2026-05-01T10:00:00+00:00">10:00</time></a>
          <span class="tgme_widget_message_views">1.2K</span>
        </div>
      </div>
    </div>
  </div>
  <div class="tgme_widget_message_wrap">
    <div class="tgme_widget_message" data-post="test/101">
      <div class="tgme_widget_message_bubble">
        <div class="tgme_widget_message_footer">
          <a class="tgme_widget_message_date" href="https://t.me/test/101"><time datetime="2026-05-02T11:00:00+00:00">11:00</time></a>
          <span class="tgme_widget_message_views">5M</span>
        </div>
      </div>
    </div>
  </div>
</section>
</body></html>`

// fakeNoPreviewHTML mimics what t.me returns for a user handle after
// 302 to /<handle> — no tgme_channel_info_counter anywhere.
const fakeNoPreviewHTML = `<!DOCTYPE html>
<html><body>
<div class="tgme_page">
  <div class="tgme_page_title"><span>Some User</span></div>
  <div class="tgme_page_extra">@someuser</div>
  <a class="tgme_action_button_new shine" href="tg://resolve?domain=someuser">SEND MESSAGE</a>
</div>
</body></html>`

func newTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
}

func TestPlatform(t *testing.T) {
	if p := New(Config{}); p.Platform() != shared.PlatformTelegram {
		t.Fatalf("platform mismatch: %s", p.Platform())
	}
}

func TestFetchChannel_HappyPath(t *testing.T) {
	srv := newTestServer(t, fakeChannelHTML)
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	snap, err := p.FetchChannel(context.Background(), "test")
	if err != nil {
		t.Fatalf("FetchChannel: %v", err)
	}
	if snap.Platform != shared.PlatformTelegram {
		t.Errorf("platform = %s", snap.Platform)
	}
	if snap.Handle != "test" {
		t.Errorf("handle = %s", snap.Handle)
	}
	if snap.URL != "https://t.me/test" {
		t.Errorf("url = %s", snap.URL)
	}
	if snap.Followers != 11_400_000 {
		t.Errorf("followers = %d; want 11400000", snap.Followers)
	}
	// photos(98) + videos(1200) + links(186) = 1484
	if snap.PostsCount != 1484 {
		t.Errorf("postsCount = %d; want 1484", snap.PostsCount)
	}
	if got, _ := snap.Raw["title"].(string); got != "Test Channel" {
		t.Errorf("title raw = %q", got)
	}
}

func TestFetchRecentPosts_HappyPath(t *testing.T) {
	srv := newTestServer(t, fakeChannelHTML)
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	posts, err := p.FetchRecentPosts(context.Background(), "test", time.Time{})
	if err != nil {
		t.Fatalf("FetchRecentPosts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("got %d posts; want 2", len(posts))
	}
	// Newest-first: post 101 first.
	if posts[0].PostID != "101" {
		t.Errorf("posts[0].PostID = %s; want 101", posts[0].PostID)
	}
	if posts[0].Views != 5_000_000 {
		t.Errorf("posts[0].Views = %d; want 5000000", posts[0].Views)
	}
	if posts[0].URL != "https://t.me/test/101" {
		t.Errorf("posts[0].URL = %s", posts[0].URL)
	}
	if posts[0].Kind != shared.PostKindPost {
		t.Errorf("posts[0].Kind = %s", posts[0].Kind)
	}
	want := time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC)
	if !posts[0].PublishedAt.Equal(want) {
		t.Errorf("posts[0].PublishedAt = %s; want %s", posts[0].PublishedAt, want)
	}
	if posts[1].PostID != "100" {
		t.Errorf("posts[1].PostID = %s; want 100", posts[1].PostID)
	}
	if posts[1].Views != 1200 {
		t.Errorf("posts[1].Views = %d; want 1200", posts[1].Views)
	}
}

func TestFetchRecentPosts_SinceFilter(t *testing.T) {
	srv := newTestServer(t, fakeChannelHTML)
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	since := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	posts, err := p.FetchRecentPosts(context.Background(), "test", since)
	if err != nil {
		t.Fatalf("FetchRecentPosts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d posts; want 1 (after %s)", len(posts), since)
	}
	if posts[0].PostID != "101" {
		t.Errorf("posts[0].PostID = %s; want 101", posts[0].PostID)
	}
}

func TestFetchChannel_NoPreviewReturnsNotFound(t *testing.T) {
	srv := newTestServer(t, fakeNoPreviewHTML)
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.FetchChannel(context.Background(), "someuser")
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "not a public channel") {
		t.Errorf("expected hint in error, got: %v", err)
	}
}

func TestFetchRecentPosts_NoPreviewReturnsNotFound(t *testing.T) {
	srv := newTestServer(t, fakeNoPreviewHTML)
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.FetchRecentPosts(context.Background(), "someuser", time.Time{})
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestParseShortNum(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"1.2K", 1_200, true},
		{"5M", 5_000_000, true},
		{"2.1B", 2_100_000_000, true},
		{"1234", 1234, true},
		{"1,234", 1234, true},
		{"11.4M", 11_400_000, true},
		{"  98 ", 98, true},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, c := range cases {
		got, ok := parseShortNum(c.in)
		if ok != c.ok {
			t.Errorf("parseShortNum(%q) ok = %v; want %v", c.in, ok, c.ok)
			continue
		}
		if got != c.want {
			t.Errorf("parseShortNum(%q) = %d; want %d", c.in, got, c.want)
		}
	}
}

func TestFetchChannel_HTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.FetchChannel(context.Background(), "x")
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFetchChannel_HTTP5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL})
	_, err := p.FetchChannel(context.Background(), "x")
	if !errors.Is(err, shared.ErrTransient) {
		t.Fatalf("want ErrTransient, got %v", err)
	}
}
