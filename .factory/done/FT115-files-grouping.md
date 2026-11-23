---
source: pr #103
branch: factory/FT115-files-grouping
round: 0
---
# feat(files): group folder contents without changing views

## factory · 2026-11-23 15:10
Owner requested moving the open non-draft PRs to review/; this is PR #103.
Source: https://github.com/lyricat/inua/pull/103
PR head: df589b49179a6fdb62a197689acb4d0a69977ecf

Imported the PR head unchanged to the factory branch above for review. No code review or tests performed during intake; the UI has not been checked by eye here.

PR description (author-reported behavior and verification, not independently verified):

> ## Summary
> 
> - Add folder background menu actions to group by type, modification date, or size, and to turn grouping off.
> - Keep the active list, grid, or column view. Grid groups have their own heading and start on a new row. Sorting still applies within each group, and the grouping preference is saved per folder.
> - Keep grid headings and spacer cells out of file operations and selection counts.
> 
> ## Verification
> 
> - `make generate`, `make i18n`, and `CGO_ENABLED=0 go build ./cmd/inua-files` on the upstream-based candidate.
> - `go test ./internal/apps/files/... ./internal/filesui` with the host's `/storage` mount hidden so its real Trash does not affect the tests.
> - In an isolated GTK/Xvfb session, selected **Group by date modified** in the grid and **Group by type** in the list; verified both layouts stayed in their original mode and the settings persisted. On the development build, also checked that Ctrl+A in a grouped grid selected the five files/folders, not the headings or spacers. The upstream-based candidate was not smoke-tested on Wayland.
> 
> ## Screenshots
> 
> | Grid · date modified | List · type |
> | --- | --- |
> | ![Grid grouped by modification date](https://raw.githubusercontent.com/iamcheyan/inua/4ad29d47efdc0f4458fb8eb56e72321b383c68a1/docs/evidence/files-grouping/grid-by-modified-date.png) | ![List grouped by file type](https://raw.githubusercontent.com/iamcheyan/inua/4ad29d47efdc0f4458fb8eb56e72321b383c68a1/docs/evidence/files-grouping/list-by-type.png) |
> 

## factory · review · 2026-11-23 15:30
head: df589b49179a6fdb62a197689acb4d0a69977ecf
verdict: pass
notes:
- [P2] internal/filesui/groupgrid.go:117: grouped-grid heading/spacer cells refuse focus, blocking keyboard navigation into later groups. In a three-column GTK Broadway probe with Today containing a.txt/b.txt and Older containing c.txt/d.txt/e.txt, ChildFocus(DOWN) moved a.txt to b.txt, then repeated DOWN/RIGHT returned false and stayed on b.txt; the ungrouped control navigated normally. Skip non-file slots explicitly during keyboard movement and add regression coverage. Physical arrow-key input was not exercised.

Validation: Claude Code reviewed origin/master...HEAD (702ac2f9..df589b49), all 24 changed files. make test passed (18 Python tests, go vet, Go race tests including GTK Broadway tests). gofmt clean; isolated i18n regeneration matched committed catalogs. Grouped selection returned only the five files despite twelve selected model slots. No new installs. No P0/P1 findings; factory policy therefore moves this item to ready/ with the P2 noted.

Not checked: UI by eye, nested Hyprland/Wayland, narrow-column clipping and selected-list heading appearance. Resize scroll behavior was inconclusive. Date groups are recomputed on folder load, not automatically at midnight. A style-only P3 suggestion was excluded from actionable findings.

Execution exception: the coder reported that the repository socket helper fell back to /tmp for its long TMPDIR, and that it deleted two scratch socket directories there, violating factory temporary-file rules. The manager performed no /tmp deletion. Future tests need a short temporary socket path inside the run directory. The coder also used a read-only config symlink in an isolated translation-check copy. Worktree remained clean; no source changes or commits were made during review.

## factory · 2026-11-23 19:28
Shipped into master: 66c86383d3336beeeb71adc85486e13c4cea35a1. Release: v0.1.43; master and annotated tag pushed atomically.
Latest passing Claude review matched the branch head before merge. make generate and make i18n passed after each merge; final regeneration left no diff. Targeted package race tests and final make test passed (18 Python tests, go vet and Go race tests).
PR #103 closed after push (GitHub had not marked it merged). Branch retained (clean not configured). No new installs.
Existing P2/P3 findings remain unchanged. UI not checked by eye; no nested compositor session, deployment or restart.
