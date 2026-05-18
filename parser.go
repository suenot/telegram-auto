// Package parser implements the w_popularity telegram adapter.
//
// Status: STUB. Returns shared.ErrNotImplemented.
//
// Strategy:
//   primary:  MTProto via gotd/td or t.me/<channel> preview HTML
//   fallback: camoufox
package parser

import (
	"context"
	"time"

	shared "github.com/suenot/w-popularity-shared"
)

// Config controls runtime behaviour. Add platform-specific fields here.
type Config struct {
	// Token, cookie, or API key — fill in per implementation.
	Credential string
	// HTTPTimeout caps every outbound call.
	HTTPTimeout time.Duration
	// CamoufoxURL is set when falling back to browser-based scraping.
	CamoufoxURL string
}

// New constructs a stubbed parser. Real impl is pending.
func New(cfg Config) *TelegramParser { return &TelegramParser{cfg: cfg} }

type TelegramParser struct{ cfg Config }

func (p *TelegramParser) Platform() shared.Platform { return shared.PlatformTelegram }

func (p *TelegramParser) FetchChannel(ctx context.Context, handle string) (shared.ChannelSnapshot, error) {
	return shared.ChannelSnapshot{}, shared.ErrNotImplemented
}

func (p *TelegramParser) FetchRecentPosts(ctx context.Context, handle string, since time.Time) ([]shared.PostSnapshot, error) {
	return nil, shared.ErrNotImplemented
}
