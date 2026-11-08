---
branch: factory/FT83-pass-fido2-security-key
round: 2
---
# Pass: unlock the gpg key with a FIDO2 security key

## factory · 2026-11-08 13:42
Added to backlog at the owner's request in chat: "i'd like to add yubikey support in pass light app by using fido2 tools. any idea?", then "add to factory backlog, and implement it".

Proposal discussed in chat and accepted:
- FIDO2 cannot decrypt OpenPGP, so the key unlocks the gpg key's passphrase. inua-pinentry, which gets the keygrip from gpg-agent, answers GETPIN for an enrolled key by asking for the security key's PIN, running `fido2-assert -G -h` (touch), and unsealing the passphrase stored sealed (AES-GCM) under the hmac-secret.
- Enrolment from the Pass app: `fido2-cred -M -h` makes the credential; the passphrase is typed once, checked, sealed and stored per keygrip. Several security keys may be enrolled; the typed passphrase keeps working as the way back in.
- The FIDO2 PIN is required (user verification), asked in inua's dialog; the tools read it from stdin when no tty is available.
- fido2-tools (libfido2) is an optional runtime dependency; without it the feature is hidden.

## factory · plan · 2026-11-08 13:53
**Behaviour**
1. Pass, File → Security keys opens a dialog. It lists each key named in the store's `.gpg-id` whose secret parts are on this computer (not stubs or card keys), the security keys enrolled for it (device name, date, Remove), and "Add a security key".
2. Without libfido2's tools the dialog says to install them; with no such key it says so; no device plugged in at Add gives "plug one in".
3. Add: the key's passphrase and the security key's PIN. gpg-agent checks the passphrase against each of the key's keygrips (`PASSWD --verify`, loopback; ignores the cache). The security key is checked for a PIN and hmac-secret, makes a non-resident credential (touch), and gives the hmac-secret for a random salt (touch). The passphrase is sealed (HKDF-SHA256 → AES-256-GCM, bound to fingerprint and credential) into `$XDG_DATA_HOME/inua/security-keys.json` (0600). Status lines say what it waits for; errors show under the form.
4. inua-pinentry, for a GETPIN with a keygrip that has enrolments (not a new passphrase), finds a plugged security key that knows one of the credentials (silent `up=false` probe), then asks in the dialog for its PIN: title "Unlock with Security Key", hint to touch, buttons Unlock / Type Passphrase / Cancel. The hmac-secret opens the passphrase, which goes to gpg-agent.
5. A wrong PIN re-asks with tries left; a missed touch re-asks; a blocked PIN, read failure or bad seal falls back to the passphrase with the reason. If gpg-agent asks again in the same connection (the kept passphrase is out of date), the passphrase is asked with a note to re-add the security key.

**Scope**: new `internal/securitykey`; `Request.SecurityKey` in `internal/pinentry`; inua-pinentry wiring; the shell dialog's variant; the Pass dialog; docs (`docs/security-key.md`, `docs/apps.md`). Left out: the OpenPGP-applet route (keys on the card), a touch-waiting indicator after the PIN, choosing among several plugged devices at enrolment (the first is used).

**Decisions**: the PIN is always required (user verification), and a key without a PIN is refused, so the security key alone never unlocks. The libfido2 tools are driven as processes (no cgo; build stays `CGO_ENABLED=0`), each in its own session so they read the PIN from stdin; their parameters come on fd 3. The PIN question is not sent to the web terminal's pages, since the key is touched at the computer. Wrapping the passphrase (rather than making the secret the passphrase) leaves gpg's keyring untouched and allows a spare key.

**Checking**: unit tests for parsing, sealing and the store; fake fido2 tools for enrol/unlock (wrong PIN, timeout, Type Passphrase, cancel, re-ask, no tools); real gpg-agent in a throwaway home for `Verify` (right, wrong while cached, escaped, no passphrase). By hand: enrol with a real YubiKey in a nested session, then `pass show` an entry.

## factory · 2026-11-08 13:53
Built at the owner's request in chat ("add to factory backlog, and implement it"); moved from backlog/ to review/.

Built (d36cfec9, one commit): as the plan above. Files: `internal/securitykey/{fido2,store,gpg,enrol,unlock}.go` and tests, `internal/lightui/pass_seckey.go`, `ui/pass.blp`/`.ui` (`seckey_dialog`), `internal/ui/askpass.go`, `internal/pinentry/pinentry.go`, `cmd/inua-pinentry/main.go`, `internal/asker/asker.go`, `internal/term/host/secrets.go`, docs, catalogs.

Tests: `make test` passed with a throwaway HOME (Python tests, go vet, Go race tests). The new tests ran, none skipped, with the real gpg-agent: it refused a wrong passphrase right after the right one was cached, so `PASSWD --verify` checks the passphrase given.

Hardware: on the owner's YubiKey 5 (fw 5.8), without PIN or touch only: `fido2-token -L`/`-I` parse (PIN set, hmac-secret, 8 retries) and the silent probe through fd 3 and setsid answered NO_CREDENTIALS in 0.1 s. Making a credential and reading the hmac-secret with the PIN on stdin were not run on hardware (they need the PIN and a touch).

Not checked: the UI was not looked at (Pass dialog, shell PIN dialog). Installs: none; libfido2's tools are an optional runtime dependency, already present here.

## factory · review · 2026-11-08 14:06
head: d36cfec97a58c5b98a170ccde02ca21e577b51ba
verdict: pass
notes:
- [P2] internal/securitykey/gpg.go:68: GnuPG field 15 contains the card serial directly, not a `>` prefix; exclude card-backed secret keys using the field semantics. Example: primary `#` and subkey serial `D2760001240100000006000012340000` still include `GRIP-CARD` as enrollable local key material.
- [P2] internal/securitykey/store.go:167: Validate nonce length before AEAD.Open so corrupt enrollment data returns an error instead of crashing pinentry. Example: `"nonce":null` followed by a successful fake-token assertion panics with `crypto/cipher: incorrect nonce length given to GCM` instead of falling back to the passphrase.
- [P2] internal/term/host/secrets.go:34: Disabling the web PIN prompt must also disable its waiting channel or honor cancellation. Example: a security-key request with a missing desktop socket and a 30 ms context remains blocked after 200 ms; the ordinary passphrase request immediately returns `failed`.
- [P2] internal/ui/askpass.go:308: Return the fixed protocol value for Type Passphrase, not a translated label. Example: Chinese returns `不行` instead of `not ok`, producing Result=failed and NotOK=false and leaving the full Ask call blocked past cancellation.

Codex reviewed all 24 changed files in origin/master...HEAD (base e9b45a9), including the security review reference. No P0/P1 findings: moved to ready/ under the factory review threshold; the four P2 defects above remain for owner consideration before shipping.
Validation: make test passed (exit 0): Python 18/18, go vet, race tests across 114 packages; 34 packages had no tests. Eight existing securitykey tests passed with no skips; four isolated overlay probes confirmed the findings. git diff --check passed; review made no source changes or commits. Installs: none.
Not checked: UI not visually inspected; real security-key PIN/touch flow not tested. Fake tools and libfido2 source inspection do not establish real-hardware compatibility. No linked GitHub issue or PR in the item.

## factory · 2026-11-08 14:13
Moved from ready/ back to review/ at the owner's request in chat: "move it to review". No code changes; head d36cfec97a58c5b98a170ccde02ca21e577b51ba as reviewed above.

## factory · 2026-11-08 14:16
Fix round 2 at the owner's request in chat ("build it"), for the four P2 notes of the 14:06 review. One commit, 1b293194; head 1b293194d316d783c47bd4dff1520fb80dd3986d.

Fixed:
- [P2] gpg.go:68: field 15 counts as a local secret only when empty or "+"; a stub ("#") or a card serial (no ">" prefix) is excluded. The test listing now uses a real card serial.
- [P2] store.go:167: `open` returns ErrSealed for a nonce of the wrong length before AEAD.Open; the pinentry falls back to the passphrase. Test: a nil nonce.
- [P2] secrets.go:34: fixed at the waiting side, `passask.Ask`: the pages channel exists only when the pages are asked, so a question they do not show returns as soon as the dialog fails. New `TestAskNotForPagesFailsWithTheDialog` (missing socket, returns Failed at once).
- [P2] askpass.go:308: the third button answers with the literal protocol value "not ok" (it was `i18n.T("not ok")`), so Type Passphrase works in translations. This also fixes the same path for Confirm's third button, which was affected before FT83.

Tests: `make generate` no change; `make i18n` updated the catalogs (one msgid gone). `make test` passed with a throwaway HOME.
Not checked: the UI was not looked at; the real PIN-and-touch flow was not run. Installs: none.

## factory · review · 2026-11-08 14:24
head: 1b293194d316d783c47bd4dff1520fb80dd3986d
verdict: pass
notes:
- [P2] internal/securitykey/unlock.go:110: Re-enrolling the same security key after changing the GPG passphrase still selects its oldest enrollment. Enrol appends, while find returns the first matching credential. Example: one device knows credential-1 and credential-2 sealing old and new passphrases; an isolated overlay probe returned "old passphrase" instead of "new passphrase". Prefer the newest matching enrollment or replace the obsolete enrollment while preserving spare keys.

Codex reviewed all 26 changed files in origin/master...HEAD (base e9b45a9), including the security review reference. The previous four P2 findings were verified fixed. No P0/P1 findings: moved to ready/ under the factory threshold; the new P2 above remains for owner consideration before shipping.
Validation: make test ultimately passed (exit 0): Python 18/18, go vet, 115 Go packages passed. First attempt failed because test isolation restricted an AppArmor query; after correcting the isolation mount, the test passed. Tests used a throwaway HOME. Regression probes covered five malformed nonce lengths and seven translated protocol values; GTK handler verification was static. git diff --check passed; no source changes or commits. No installs reported.
Not checked: UI not visually inspected; real hardware PIN/touch flow not tested. No linked GitHub issue or PR in the item.
