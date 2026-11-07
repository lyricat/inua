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
