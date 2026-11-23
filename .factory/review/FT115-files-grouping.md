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
