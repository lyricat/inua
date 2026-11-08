---
branch: factory/FT84-settings-security-keys-page
round: 1
---
# Settings: a Security keys page for the FIDO2 key itself

## factory · 2026-11-08 14:25
Added to backlog at the owner's request in chat: "is there a manage page for yubikey?", then "add to backlog".

Proposal from chat: Pass's Security keys dialog (FT83) manages only inua's enrolments. A Settings page would manage the security key plugged in:
- Its name and firmware, whether a PIN is set, and the PIN tries left (`fido2-token -I`).
- Set or change the PIN (`fido2-token -S` / `-C`), asked in inua's dialog.
- The gpg keys it unlocks (FT83's enrolments), with a way to Pass to add or remove.

Left out on purpose: listing resident credentials (passkeys; needs the PIN) and reset (wipes every passkey on the key).

Depends on FT83 (`internal/securitykey`).

## factory · plan · 2026-11-08 14:43
**Behaviour**
1. Settings → Devices → Security Keys (index icon: Phosphor `usb` as `security-key-symbolic`; aliases yubikey, fido2, security-keys).
2. Shown: reads off the main thread each key from `fido2-token -L`, its `-I` (PIN set, tries left, minimum PIN length, firmware), and the FT83 enrolments whose credential it knows (silent `up=false` probe), named by the gpg key's user ID.
3. A section per key: PIN row (Set · N tries left / Not set / Blocked) with Set PIN or Change PIN; "Unlocks <user ID>" rows with the date added, or "Unlocks no gpg key"; Open Pass; Firmware.
4. Set/Change PIN opens Current (when set), New, New again, Cancel, Save under the PIN row. Checked first: current typed, length from the key's minimum to 63 bytes, one line, the two match. Then `fido2-token -S` or `-C` with the PINs on stdin in the order the tool asks. A wrong current PIN says the tries left; policy and blocked errors have their own words. Success re-reads and notes that the gpg keys still unlock.
5. No tools: install note. No key: "Plug in a security key." While shown it lists keys every 3 s and re-reads when the set changes (not while a form for a key still plugged in is open).

**Scope**: `internal/securitykey` (Info.MinPIN/Firmware, ErrPINPolicy, CheckPIN, SetPIN, ChangePIN, Holds), `internal/ui/settings_securitykeys.go`, `ui/settings.blp` page, index/aliases, icon, docs. Left out as agreed: passkey listing and reset.

**Decisions**: built on FT83's branch, since it needs `internal/securitykey`; ship FT83 first. The page is in the Devices group. PIN fields sit on the page rather than in the password dialog, as gpg.blp's passphrase rows do. Polling (3 s, `fido2-token -L` only) instead of udev events, stopped when the page is out of sight.

**Checking**: tests for parsing (firmware, minimum length), SetPIN/ChangePIN stdin order and errors with a fake `fido2-token`, CheckPIN (rune length, bytes, newline), Holds. By hand: the page with a YubiKey plugged and unplugged, a PIN change.

## factory · 2026-11-08 14:43
Built at the owner's request in chat ("implement it now"); moved from backlog/ to review/.

Built (38d6d9aa, on top of FT83's 1b293194): as the plan above.

Tests: `make generate`, `make i18n` (catalogs updated), `make test` passed with a throwaway HOME; the new securitykey tests ran.

Not checked: the UI was not looked at. Set/Change PIN was not run on hardware: it would change the owner's PIN. The order of PINs on stdin follows libfido2's tools (`-S`: new, again; `-C`: current, new, again), checked only against the fake tool. Installs: none.

## factory · 2026-11-08 14:57
Merged into master at the owner's request in chat ("merge to master, commit"), from review/ directly: merge commit cca5859c. FT83 went first, as FT84 is built on it. After the merge: make generate and make i18n changed nothing; make test passed. No linked issues or PRs to close.


## factory · review · 2026-11-08 14:59
head: 38d6d9aa6c22c270ead2728305ce1dc685518be9
verdict: fix
fix:
- [P1] internal/ui/settings_securitykeys.go:451: A pending ChangePIN callback survives closing and reopening Settings on another page. Initialization clears the PIN widgets without advancing gen; the callback guard accepts the stale operation and ErrPIN reaches the nil current widget (lines 471–472), crashing the shell. Invalidate operations on close/rebuild and verify the operation and form before touching widgets. An isolated callback probe reproduced the panic.
notes:
- [P2] internal/securitykey/unlock.go:110: Re-enrolling the same device after changing the GPG passphrase appends a new record, but find selects the first recognized old credential and returns the old passphrase. Prefer the latest valid enrollment or replace the superseded one. A fake-device probe reproduced old rather than new passphrase selection. Inherited from FT83.
- The item was concurrently shipped and moved to done/ while review ran; left in done/ as required. This review records the reviewed branch head, not a new review of the shipped master.
- Validation: make test passed with throwaway HOME (18 Python tests, go vet, Go race tests). make generate and make i18n in an isolated copy matched committed outputs. A later /proc-short-path confinement experiment failed identically on HEAD and base (53 failures); no branch-only failure. Initial tests used temporary /tmp paths through sockdir fallback, subsequently cleaned by tests.
- Reviewed all 35 changed files in origin/master...HEAD as fetched at review start, including FT83. No installs; no code edits or commits. UI not visually checked; real keys, hardware and PINs not accessed. Independent security-review reference was unavailable to the coder.
