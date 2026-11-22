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

## factory · review · 2026-11-22 17:07
head: 1306869af213e8be10cca0c5452b7694b6d0afe1
verdict: pass
notes:
- [P3] internal/ui/launcher.go:1158: when the selected theme has neither an alias nor either generic fallback, setIcon returns without replacing the existing image. Example: app_icons="hicolor" with the switch enabled; GTK lookup probes found both generic names absent and only 4 of 31 aliases present, so a reused bar image can retain the previous app icon. Clear the image or provide an explicit last-resort placeholder; gridIcon and Files' chooser also need an empty-result policy.
- [P3] internal/desktop/entry.go:72: the new GTK-free alias lookup has no regression test despite AGENTS.md requiring tests for logic changes. Add table coverage for io.github.lyricat.Inua.Term.Quick, an unknown inua ID and a non-inua name; also cover the new app_icons_inua config round trip.

Validation: Claude Code reviewed origin/master...HEAD. make test passed (18 Python session tests, go vet and go test -race ./...). make generate and make i18n passed in an isolated copy with no generated-file drift. No unpinned installs; worktree unchanged. Branch merges cleanly with origin/master. No P0/P1 findings.

Limits: UI was not checked by eye or in a nested session. Papirus and Numix-Circle were unavailable for icon probes. Lookup probes do not verify drawing quality or live redraw. Theme inheritance can select installed GNOME app icons from hicolor; this is recorded as behavior, not a confirmed defect. Chooser config-read latency was not measured.
