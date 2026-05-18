# w-popularity-parser-telegram

`telegram` parser for [w_popularity](https://github.com/suenot/w-popularity).

**Status:** stub. `FetchChannel` and `FetchRecentPosts` return `shared.ErrNotImplemented`.

## Strategy

- **Primary:** MTProto via gotd/td or t.me/<channel> preview HTML
- **Fallback:** camoufox

## Usage

```go
import parser "github.com/suenot/w-popularity-parser-telegram"

p := parser.New(parser.Config{Credential: os.Getenv("CRED")})
snap, err := p.FetchChannel(ctx, handle)
```

## License

MIT
