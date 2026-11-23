---
source: pr #105
branch: factory/FT117-overview-fullscreen
round: 0
---
# fix(overview): remove dock space below workspace previews

## factory · 2026-11-23 15:10
Owner requested moving the open non-draft PRs to review/; this is PR #105.
Source: https://github.com/lyricat/inua/pull/105
PR head: a9868ed207104178a651361f6f734a9931c3adaf

Imported the PR head unchanged to the factory branch above for review. No code review or tests performed during intake; the UI has not been checked by eye here.

PR description (author-reported behavior and verification, not independently verified):

> ## Summary
> 
> - Remove the dock footprint from Overview's card layout so the workspace previews use the full monitor area.
> - Keep the dock as an overlay without shrinking or offsetting Overview cards.
> - Remove the obsolete inset helper and test; update the compositor coupling note.
> 
> ## Verification
> 
> - `make i18n`
> - `CGO_ENABLED=0 go test ./internal/ui -run 'Test(Overview|DockWantedAndExclusive)'`
> - Verified in the current Hyprland session after building the merged shell: Overview opens with live workspace surfaces and no Dock reservation in its layout.

## factory · review · 2026-11-23 15:55
head: a9868ed207104178a651361f6f734a9931c3adaf
verdict: fix
fix:
- [P1] internal/ui/overview.go:558: moved translatable strings leave po/inua.pot and all six catalogs stale. Example: "No workspaces" is now on line 558, while the template still references line 563. The isolated extractor reproduces the changed references; CI runs make i18n followed by git diff --exit-code, so its catalog check fails. Run make i18n and commit the template and catalogs.
notes:
- [P2] internal/ui/overview.go:700: per-card dock cropping changes the preview coordinate mapping, but overviewDropTarget still stores only Scale and overview_manage.go:262 does not restore the cropped origin or independent vertical scale. Example: on a 1920x1080 monitor with a reserved 72px left dock and an 800px-wide card, x=400 (zero drag offset) maps to 924, rather than the displayed monitor coordinate 996. Preserve the mapped rectangle and invert both axes when dropping floating windows in their current workspace.

Claude Code reviewed origin/master...HEAD. make test passed: 18 Python session tests, go vet ./..., and CGO_ENABLED=1 go test -race ./... (117 packages with tests). Changed Go files passed gofmt. No installs or manifest changes; worktree remains clean. The branch merges cleanly with the fetched master. UI was not checked by eye. Live preview distortion, mapping refresh during dock-setting changes, and non-bottom-dock coverage remain unverified risks, not additional confirmed findings.

Round 0 is below max_rounds 3; moved review/ → new/ for a fix round. Documentation-only suggestion omitted because it did not establish an observable defect.
