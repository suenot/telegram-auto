# telegram-auto

`telegram` parser for [w_popularity](https://github.com/suenot/w-popularity).

Scrapes the public preview at `https://t.me/s/<handle>` — no auth, no
API token, no MTProto. The same HTML response carries both the channel
counters and the last ~20 posts inline.

## Caveats

- Works only for **public channels**. User handles (`t.me/<user>`) have
  no `/s/` preview — Telegram redirects them to a plain "Send Message"
  page, so `FetchChannel` and `FetchRecentPosts` return
  `shared.ErrNotFound` with a hint that the handle is not a public
  channel.
- Private channels and missing handles likewise return `ErrNotFound`.
- The preview embeds the most recent ~20 messages only. For deeper
  history you'd need MTProto or `?before=` pagination (not implemented).
- Reactions and comments are not exposed in the preview; those fields
  stay at zero in `PostSnapshot`.
- Telegram's compact counters (`1.2K`, `5M`, `2.1B`) are parsed by
  multiplying the float prefix. There is some loss of precision for
  large numbers (Telegram itself rounds).

## Usage

```go
import parser "github.com/suenot/telegram-auto"

p := parser.New(parser.Config{HTTPTimeout: 15 * time.Second})
snap, err := p.FetchChannel(ctx, "durov")
posts, err := p.FetchRecentPosts(ctx, "durov", time.Now().Add(-24*time.Hour))
```

## Strategy

- **Primary:** `GET https://t.me/s/<handle>` HTML scrape (this module).
- **Fallback (not implemented):** MTProto via `gotd/td` for private
  channels or deep history; Camoufox for anti-bot scenarios.

## License

MIT
