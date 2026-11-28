---
source: pr #108
branch: factory/FT120-overview-apps-pages
round: 0
---
# feat(overview): paginated app browsing and compact workspace mode

## factory · 2026-11-29 00:02
Imported open, non-draft PR directly into review at the owner’s request.

PR: https://github.com/lyricat/inua/pull/108
Original branch: `feat/overview-apps-pages` (fork).
Head: `8f5d8ebf5a60d8b6b50ef94f8bc4d05b2c024e2b`
Factory branch mirrors the PR head; no factory build or review has run. Tests have not been run during intake.

PR description (untrusted source text):
> ## Summary
> 
> This PR contains only the latest Overview changes, split into three focused commits:
> 
> 1. **Paginated apps and compact mode** — full Overview puts installed applications below workspace previews; Super+Tab keeps a compact workspace chooser. Application pages support scroll/dot navigation, click-to-launch and search. `inua call overview open compact` opens the compact mode explicitly.
> 2. **App-to-workspace drops** — launch an application after focusing an existing or new workspace. Dock payloads use the same tagged application format while preserving Dock reordering. Window placement remains subject to application reuse and compositor rules.
> 3. **Visual presentation** — smaller search field, workspace emphasis and page indicators, with full/compact screenshots. Removes an unsupported GTK CSS `max-width` declaration found during verification.
> 
> Based directly on upstream master `88a99286`. Does not include the fork's global-menu fixes, title-border preference, or the unrelated launcher icon helper change.
> 
> ## Verification
> 
> - `make i18n` (regenerated for the Go commit slices)
> - `CGO_ENABLED=0 go build ./cmd/inua-shell`
> - `go test ./internal/ui ./internal/shell` with the GTK library environment
> - Throwaway GTK/Xvfb scenario exercised full and compact rendering, application page changes, search hiding/restoring the application grid, and the app drop launch path. The fixture compositor recorded output/workspace focus and a real harmless command wrote its launch marker. The fixture is not committed.
> 
> The launch-path check invokes the drop routing directly; physical cross-monitor drag-and-drop and placement of already-running single-instance applications were not exercised. Live preview capture is unchanged. The isolated fixture emitted an application-startup warning and an unavailable XCompose-path warning; screenshots and the scenario still completed.
> 
> ## Screenshots
> 
> ### Full Overview
> 
> ![Full Overview with application pages](https://raw.githubusercontent.com/iamcheyan/inua/79ca7ae18779db542326efb97c4334265eea4157/docs/evidence/overview-app-pages/full.png)
> 
> ### Compact workspace chooser
> 
> ![Compact Overview](https://raw.githubusercontent.com/iamcheyan/inua/79ca7ae18779db542326efb97c4334265eea4157/docs/evidence/overview-app-pages/compact.png)
