---
source: pr #102
branch: factory/FT114-themed-app-icons
round: 0
---
# feat: replace inua app icons with selected theme

## factory · 2026-11-22 16:37
Owner requested adding non-draft PR #102 directly to review/.
Source: https://github.com/lyricat/inua/pull/102
PR head: 1306869af213e8be10cca0c5452b7694b6d0afe1

Imported the PR head unchanged to the factory branch above for review. No code review or tests performed during intake; the UI has not been checked by eye here.

PR description (author-reported behavior and verification, not independently verified):

> ## Summary
> 
> - Add an Appearance switch that replaces inua programs' app-style icons with icons from the selected app icon theme.
> - Resolve inua app IDs to standard theme icon names; use the selected theme's generic app icon when no matching app icon exists. Enabled mode does not fall back to inua's artwork.
> - Apply the same aliases in Files' Open With chooser. Line icons remain controlled by their existing setting.
> 
> ## Verification
> 
> - `make generate`
> - `make i18n`
> - `CGO_ENABLED=0 go build -buildvcs=false -o bin/inua-shell ./cmd/inua-shell`
> - `CGO_ENABLED=0 go build -buildvcs=false -o bin/inua-files ./cmd/inua-files`
> 
> The builds ran in an isolated worktree without Git metadata, hence `-buildvcs=false`. I did not manually toggle the updated setting in a nested session.
