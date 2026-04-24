# Everyday Services

## Quick Example

```
User: "What's AAPL trading at?"
  -> Agent calls finance.Quote(ctx, "AAPL")
  -> AlphaVantageProvider:
       Rate limiter: 25 req/day, 18 remaining -- OK
       GET https://www.alphavantage.co/query?function=GLOBAL_QUOTE&symbol=AAPL&apikey=...
  -> Quote{Symbol: "AAPL", Price: 178.50, Change: +2.30, ChangePercent: 1.3, Currency: "USD"}
```

## Finance: Stock Quotes

The `finance` package provides real-time stock/crypto price data.

### Provider: Alpha Vantage

```go
provider := finance.NewAlphaVantageProvider("your-api-key")
quote, err := provider.Quote(ctx, "AAPL")
// quote.Price, quote.Change, quote.ChangePercent, quote.Currency, quote.Timestamp
```

- **API**: Alpha Vantage `GLOBAL_QUOTE` endpoint
- **Rate limit**: 25 requests/day (free tier), enforced client-side with a token-bucket limiter
- **Timeout**: 15 seconds per request
- **Currency**: USD (returned by the API)

## How Services Use the Proxy

All external API calls go through the **Resource Access Proxy** (see `docs/security/SECURITY-MODEL.md`):

1. **Capability check** -- Agent needs `data.finance.read` (or `tool.web.fetch` for HTTP)
2. **Credential resolve** -- API key looked up from vault by domain
3. **Token refresh** -- Automatic for OAuth2 credentials nearing expiry
4. **Execute** -- HTTP request with injected auth
5. **Audit** -- Every access logged

## BYOK (Bring Your Own Keys)

Each service requires the user's own API key. Store via vault:

```bash
# Keys are encrypted at rest in the vault
# No keys ship with AI Butler -- you provide your own
```

## Capability Defaults

From `capability.MessagingDefaults()`:
- `data.finance.read` -- granted with `AuditSummary`
- `tool.web.fetch` -- granted with rate limit (10/min) and `AuditFull`

## Current scope & roadmap

- **Today (v0.1):** Finance (Alpha Vantage), Provider interface for extensibility
- **Planned:** Weather, maps, news providers (same Provider pattern)

## Source Files

- `internal/finance/finance.go` -- Quote, Provider interface, AlphaVantageProvider, rateLimiter
- `internal/proxy/proxy.go` -- Resource Access Proxy (capability + credential + execute + audit)
- `internal/capability/defaults.go` -- Default capability grants for services
