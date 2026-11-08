# Pass: unlock the gpg key with a FIDO2 security key

## factory · 2026-11-08 13:42
Added to backlog at the owner's request in chat: "i'd like to add yubikey support in pass light app by using fido2 tools. any idea?", then "add to factory backlog, and implement it".

Proposal discussed in chat and accepted:
- FIDO2 cannot decrypt OpenPGP, so the key unlocks the gpg key's passphrase. inua-pinentry, which gets the keygrip from gpg-agent, answers GETPIN for an enrolled key by asking for the security key's PIN, running `fido2-assert -G -h` (touch), and unsealing the passphrase stored sealed (AES-GCM) under the hmac-secret.
- Enrolment from the Pass app: `fido2-cred -M -h` makes the credential; the passphrase is typed once, checked, sealed and stored per keygrip. Several security keys may be enrolled; the typed passphrase keeps working as the way back in.
- The FIDO2 PIN is required (user verification), asked in inua's dialog; the tools read it from stdin when no tty is available.
- fido2-tools (libfido2) is an optional runtime dependency; without it the feature is hidden.
