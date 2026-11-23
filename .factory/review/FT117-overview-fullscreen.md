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
