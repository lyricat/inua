---
source: pr #77
branch: factory/FT76-style-animations
round: 1
---
# feat(style): a setting for animations, in inua's own surfaces

## factory · 2026-11-07 09:51
Imported https://github.com/lyricat/inua/pull/77 directly into review at the owner's request.

Original PR description:

## What

A new setting, `[style] animations` (on by default), turns motion in inua's own surfaces on or off:

- GTK's animation setting and a CSS provider that removes transitions/animations when off, for the shell and its apps;
- Settings → Appearance, an "Animations" switch;
- the setting is saved with the rest, and applies immediately.

Applying the same setting to the compositor (Hyprland, niri, Wayfire) is the next PR in this series; this one only covers inua's own surfaces.

## Verification

- `go build ./...`
- `go test ./internal/config`
- `make generate`, `make i18n`


## factory · 2026-11-07 09:51
Status: review (pending; no review performed by this move).
Source branch: iamcheyan/inua:feat/style-animations
head: 0923b10a08f333161e53ef231e9095e5368cefdc
The factory branch preserves the exact PR head. Verification above is reported by the PR author, not rerun during this status change.

## factory · review · 2026-11-07 10:32
head: 18b32c4f8a67be8435afa8e7c4217cd6757b37af
verdict: pass
notes:
- [P2] internal/ui/apply.go:193 (also internal/appui/app.go:316): GTK 4.22 rejects the CSS declarations containing `!important` with `Junk at end of value`, leaving the parsed provider empty. Remove `!important` and rely on provider priority. Example: parsing the current disable-animation CSS yields an empty provider; removing these suffixes preserves the rules.
- [P2] internal/config/config.go:186: The pointer-valued animation setting breaks Style equality across config reads and prevents the wallpaper reload path. Compare semantic values or use a default-true bool. Example: repeated reads of identical config compare equal on base but unequal on HEAD; with a non-image wallpaper and generation disabled, changing the theme field color then reloading skips loadWallpaper and retains the old background (probe and call-chain verified).
- [P2] internal/ui/apply.go:183: Disabling animations does not disable the dock's custom gap motion. Make the dock use its target margins immediately and stop any active gap animation when disabled. Example: dragging a pinned app with animations disabled still calls onDockDropMotion -> setGap(..., false) -> stepGap, advancing margins by 30% every 16 ms; this bypasses GTK/CSS (call-chain verified, not visually exercised).
- [P3] ui/settings.blp:321: The description promises compositor animation control although only in-process GTK/CSS is updated. Limit the description and config comments/translations to inua surfaces. Example: turning this off sends no animation option to compositor backends, so workspace/window compositor animations remain unchanged.

Merged origin/master into the item branch and resolved translation conflicts, preserving both sides, at the reviewed head; pushed that branch. No protected paths changed.
Codex reviewed origin/master...HEAD against base e97ebcc, all 16 changed files and relevant callers. No P0/P1 findings; pass follows the factory threshold, not an assertion that the P2/P3 notes are fixed.
Validation: full make test passed on HEAD and base (18 Python tests, go vet, Go race tests: 112 packages passed and 34 without tests). An initial isolated-environment setup failed; corrected setup and complete rerun passed. Generated Blueprint and seven translation files matched. Conflict resolution also ran make generate, make i18n and make test successfully.
Tests used a throwaway HOME and isolated environment. UI was not visually checked; Broadway tests skipped under the session constraints, and terminal-engine tests were limited by missing libinua-vt.so. No installs. Review made no code changes. Moved review/ -> ready/ for owner inspection; not merged into master.

## factory · 2026-11-07 16:03
Shipped at the owner's request after verifying the latest reviewed head. Merged into master at 9ba64f4c; release v0.1.27 points to 8db50f34. Linked PR #77: merged automatically by GitHub.
Validation: make generate and make i18n passed with clean generated output; full make test passed first run with throwaway HOME/runtime and uncached Go tests (18 Python tests, go vet, 113 race-tested Go packages). FT76 catalog conflicts resolved preserving both sides. Existing P2/P3 notes remain unchanged. No installs. UI was not visually checked or exercised on the desktop.
Moved ready/ → done/.
