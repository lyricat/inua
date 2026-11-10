# Unlocking a gpg key with a security key

A FIDO2 security key (a YubiKey, a SoloKey, a Nitrokey 3 …) can stand in
for the passphrase of the gpg key a password store encrypts to: inua's
pinentry asks for the security key's PIN, the key is touched, and gpg-agent
gets the passphrase. The passphrase keeps working; this adds a way in, it
takes none away.

Code: `internal/securitykey` (no GTK), the Pass app's dialog in
`internal/lightui/pass_seckey.go`, the Settings page in
`internal/ui/settings_securitykeys.go`, and `cmd/inua-pinentry`.

## How

FIDO2 cannot decrypt OpenPGP, and moving the key onto the security key
(its OpenPGP applet) is another matter. What a FIDO2 key can do is give a
secret: with the `hmac-secret` extension, a credential and a salt always
give the same 32 bytes, from that key alone, after its PIN and a touch.

- **Enrolling** (Pass, **File → Security keys**, **Add a security key**):
  the passphrase is typed and gpg-agent checks it opens the key's secret
  parts (`PASSWD --verify` over the agent's socket, loopback; the check
  ignores a passphrase the agent remembers). The security key makes a
  credential (`fido2-cred -M -h -v`, a touch), then gives its secret for a
  random salt (`fido2-assert -G -h -v`, a touch). The passphrase is sealed
  with AES-256-GCM under a key drawn from that secret (HKDF-SHA256), tied
  to the gpg key's fingerprint and the credential, and kept in
  `$XDG_DATA_HOME/inua/security-keys.json` (mode 600) with the
  credential's ID, the salt and the keygrips it opens. The credential is
  not resident: nothing is written to the security key.
- **Unlocking**: when gpg-agent asks inua-pinentry for the passphrase of
  an enrolled keygrip, and a security key plugged in knows one of its
  credentials (asked without a PIN or a touch), the dialog asks for that
  security key's PIN. The key blinks; touched, it gives the secret, which
  opens the passphrase, which goes to gpg-agent. **Type Passphrase**
  asks for the passphrase as before.
- A wrong PIN is asked for again, with the tries left; a security key not
  touched in time too. A blocked PIN, a security key that cannot be read,
  or a passphrase that no longer opens the key (it was changed) falls
  back to typing it, saying why.
- Several security keys can be enrolled for one gpg key, a spare among
  them. **Remove** forgets an enrolment.
- **Settings → Security Keys** shows each key plugged in: its PIN and
  the tries left, its firmware, and the gpg keys it unlocks (the
  enrolments whose credential it knows, asked without a PIN or a touch).
  It sets the PIN (`fido2-token -S`) or changes it (`-C`); the
  hmac-secret does not depend on the PIN, so a changed PIN unlocks as
  before.

The PIN is always asked (user verification): a security key alone does
not unlock, as anyone could pick it up. Enrolling one without a PIN set is
refused (`fido2-token -S` sets one). The security key's PIN question is
not sent to the web terminal's pages: the key is touched at the computer.

## Requirements

libfido2's tools (`fido2-token`, `fido2-cred`, `fido2-assert`; Arch:
`libfido2`, Debian: `fido2-tools`), and access to the security key's
hidraw device (as for a browser's WebAuthn: the usual udev rules). Without
the tools the dialog says so and the pinentry asks as before. The tools
read the PIN from stdin when there is no terminal, so they are run in a
session of their own; the rest of their input comes on file descriptor 3.

## What it protects against

The sealed passphrase on disk opens with the security key and its PIN,
nothing else; a copy of the file and the gpg keyring without the security
key gives no more than the keyring gave before. Someone who has the
security key and knows its PIN unlocks the gpg key as the passphrase
would. Changing the gpg key's passphrase leaves the kept one out of date:
the pinentry says so, and the security key is added again.
