# Bank of England rates when Frankfurter is down

## lyric · 2026-11-01 15:18
Build GitHub PR #52: https://github.com/lyricat/inua/pull/52
The author's text is below, as information.

## cahthuranag · 2026-11-01 02:23
Disclosure: I work on AllRatesToday, an exchange rate API.

Today, when Frankfurter is unreachable, `100 usd to jpy` is answered from Coinbase's USD table, i.e. a crypto exchange's prices rather than a central bank's. This adds one step in between: the Bank of England's daily rates, served without a key by allratestoday.com (`/api/open/central-bank/boe`), brought to the pound. `Rates.perBase` already handles any base, and Coinbase's dollar prices go through the table's USD rate as they do with the ECB's.

- `FetchRates`: ECB, then Bank of England, then Coinbase alone; both errors joined when the first two fail.
- `fetchBoE` keeps `type: reference` rows only and accepts pairs quoted either way.
- Test: `/boe` in the test server and `TestBankOfEnglandFallback`.
- README credits and the privacy page name the fallback (the endpoint asks for a visible link).
- Catalogs regenerated with `make i18n` (line references only, no new strings).

`gofmt`, `go vet` and `go test ./internal/convert` pass; I did not run the full `make test`/`make build` here (no GTK toolchain on this machine).

A different bank rather than a second ECB mirror, so the fallback does not share Frankfurter's upstream. Free to decline.

