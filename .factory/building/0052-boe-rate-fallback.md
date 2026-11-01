---
branch: factory/52-boe-rate-fallback
round: 0
---
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


## factory · plan · 2026-11-01 15:20
**Behaviour**
1. The launcher answers "100 usd to jpy" from the ECB's rates (Frankfurter), as now.
2. When Frankfurter fails, it asks allratestoday.com for the Bank of England's daily reference rates and answers from those, brought to one pound; the answer's detail gives their date.
3. When both fail, Coinbase's dollar rates answer alone, as now; the error, when all fail, names both rate services.
4. Cryptocurrencies still come from Coinbase, through the table's dollar rate.

**Scope**
PR #52's commit as it is (internal/convert, README credits, website/privacy.html), with the catalogs regenerated on today's master. Nothing else in the launcher.

**Decisions**
- The PR's author keeps authorship: the commit is taken over by cherry-pick.
- Rates from the fallback are cached as long as the ECB's (RatesMaxAge); Frankfurter is not retried sooner. Simple, and the Bank of England's change once a day too.
- Currencies the Bank of England does not list fall to Coinbase's table, as the ECB's do now.
- Using a commercial service's open endpoint (the author works there) is the owner's call at review; noted, not decided here.

**Checking**
- The PR's TestBankOfEnglandFallback, and the whole make test.
- The live endpoint's shape was read once (2026-10-29 rates, GBP base, "reference" rows), matching the parser.
- Not checked: the launcher by eye with Frankfurter blocked.
