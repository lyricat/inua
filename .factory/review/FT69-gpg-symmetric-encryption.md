---
branch: factory/FT69-gpg-symmetric-encryption
round: 1
---
# GPG: symmetric encryption and decryption

## lyric · 2026-11-05 09:51
The GPG app should support symmetric encryption and decryption. The UI/UX should make sense.

## factory · plan · 2026-11-05 21:34
**Behaviour**

- Encrypt: the head of [TO] gets a switch, **Public keys** | **Passphrase**. Public keys is the composer as it was. Passphrase replaces the recipients with two fields, the passphrase and the same again (each with the eye to show it), and a line saying that no key is needed to open it and that the passphrase should travel apart from the message. The head reads "Anyone who has the passphrase". [FROM] is not shown: nothing is signed, and the user opens it with the passphrase as anyone does.
- Enter in the first field goes to the second; Enter there encrypts. Encrypt with no passphrase says "Type a passphrase first." and focuses the field; with two that differ, "The two passphrases differ." and focuses the second. Nothing runs until both hold.
- Text comes out armored, files as `NAME.gpg` beside the original, as with keys; nothing is overwritten. The result page says "For anyone who has the passphrase". One passphrase covers every file chosen.
- The fields are emptied once something was encrypted, and by Clear. "Encrypt to…" on the Keys tab switches back to Public keys.
- Decrypt: nothing new to do. A pasted message or a file encrypted with a passphrase is decrypted as any other; gpg asks for the passphrase in its dialog (inua's, "Passphrase Required"), and gpg-agent remembers it for that message as it does a key's. The result says "Encrypted with a passphrase" where it said to whom ("Encrypted to X, and with a passphrase" for a message that has both).
- A wrong passphrase says "That passphrase does not open it. Decrypt asks again." and a closed dialog "Cancelled. Decrypt asks again."; no result section opens, no file is written, the message or file stays where it was, and the Decrypt button asks again.

**Scope**

- `internal/apps/gpg`: `Options.Passphrase` (`gpg --symmetric`), given to gpg on its own descriptor; `Result.Passphrase`; `ErrPassphrase` and `ErrCanceled` from gpg's status lines.
- `internal/lightui/gpg.go`, `ui/gpg.blp`: the switch, the two fields, the messages above.
- Docs (`docs/apps.md`, `docs/architecture.md`), the catalogs with the new text in all six languages.
- Left out on purpose: signing a passphrase message; a message both to keys and with a passphrase (gpg can make one, the composer does not offer it); a strength meter or a generated passphrase; choosing the cipher (gpg's default, AES-256 here); the shell's passphrase dialog, which already words such a request sensibly.

**Decisions**

- The passphrase to encrypt with is typed in the composer, not in gpg's dialog. gpg's dialog would ask twice for each file, and files could end up under different passphrases; in the window it is checked before anything runs and is one for all. The window already holds the plain text, so it holds nothing more sensitive than before.
- It reaches gpg through `--pinentry-mode loopback --passphrase-fd`, a pipe: never in the arguments, which any program can read, and never in a file.
- To decrypt, gpg's dialog asks: the app cannot know a message wants a passphrase before gpg reads it, and this keeps "decrypted as soon as it is pasted".
- Passphrase and keys exclude each other in `Options`: with one passphrase given to gpg, signing would try it on the user's key.
- The switch is in [TO] because it answers the same question, who can open it. The segment is "Public keys", not "Keys": that message already exists in the catalogs as keyboard shortcuts.

**Checking**

- Tests with real gpg in a throwaway home: a text and a file encrypted with a passphrase and decrypted; a wrong passphrase is `ErrPassphrase` and writes no file; options that mix a passphrase with keys are refused. gpg's status lines for a message opened, to a key as well, a wrong passphrase, a closed dialog.
- By hand: the composer in both modes, the two messages for a missing and a differing passphrase, the result, decrypting with a wrong and then the right passphrase.

## factory · 2026-11-05 21:36
Built on the owner's word in a session ("implement FT69, and move to review status"), straight from `backlog/`; branch `factory/FT69-gpg-symmetric-encryption`, one commit, `6f9967df feat(gpg): encrypt with a passphrase, to no key`, on `origin/master` 648db7a8.

**What was built**: the plan above, as written.

**Found on the way**: a decryption that failed still opened the [DECRYPTED] section, empty, under the error (and a failed text encryption its result page): the window checked gpg's output for nil, which it never is. Seen with a wrong passphrase, true before for any failure; fixed in the same commit, since a wrong passphrase is now an ordinary case.

**Installs**: none.

**Tests**: `make test` passes (throwaway HOME): the session tests, `go vet`, 106 packages with the race detector. `make generate` and `make i18n` leave the tree as committed.

**Checked by eye**, headless: the built app on GTK's Broadway with a throwaway HOME and keyring, a private bus and a scripted pinentry, driven in headless Chrome. Seen: both modes of the composer and the switch between them (From comes back with Public keys); "Type a passphrase first." and "The two passphrases differ." with the focus moved; Enter from the first field to the second and Enter to encrypt; the armored result and its head; the clipboard offer on Decrypt; a wrong passphrase (the line in the bar, no result section); the right one after pressing Decrypt ("Encrypted with a passphrase", the text).

**Not checked**: on the real desktop, in Hyprland; inua's own passphrase dialog asking for a message's passphrase (the scripted pinentry stood in for it; gpg's request was read: no key, "Please enter the passphrase for decryption."); files in the window (the chooser was not driven; the same calls are covered by the package tests); the composer with keys in the keyring; the six translations on screen, which are mine and not reviewed by a native speaker; a gpg older than 2.4.8.

## factory · review · 2026-11-05 22:29
head: 6f9967dfcb3b6dbc0ca1b84cc25a4638d21c30ae
verdict: fix
fix:
- [P1] internal/sockdir/sockdir_test.go:19: Required `make test` did not pass in the review sandbox: TestNewIsShortUnderALongTempDir falls back to /tmp and receives permission denied. This is an environment validation blocker, not an FT69 regression; the baseline code is identical. Provide an authorized private writable /tmp and rerun the full test command before promotion. A follow-up attempt with bwrap failed before tests (missing /proc/sys/kernel/overflowuid); namespace creation was also denied.
notes:
- [P2] internal/i18n/locales/ko.po:18253: The Korean explanation translates passphrase as ciphertext (암호문), saying that ciphertext alone suffices to open the message. Use a correct passphrase term, such as 암호 구문, in the new guidance.
- Reviewed origin/master (648db7a8)...HEAD with Codex: all 14 changed files, including cryptographic paths. No merge conflicts with base.
- Validation: 15 Python session tests and go vet passed. Initial race run passed 99 packages and failed 7; six of those passed after environment adjustments, but the socket test remained blocked. Full make test success is not established. All 8 GPG package tests passed; isolated real GPG 2.4.8 probes covered cancellation, wrong-passphrase retry, cached decryption and mixed recipients. Generated translations and Blueprint output matched the commit in temporary copies.
- Installs: none. Project working tree remained clean. No visual UI check; real desktop pinentry interaction and older GPG versions remain unverified.
- Item remains in review/ for the owner to decide. No linked GitHub issue or PR was specified.
