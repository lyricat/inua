---
branch: factory/FT83-pass-fido2-security-key
round: 1
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
