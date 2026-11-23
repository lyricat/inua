---
source: pr #104
branch: factory/FT116-files-system-mounts
round: 0
---
# fix(files): hide system configuration mounts from sidebar

## factory · 2026-11-23 15:10
Owner requested moving the open non-draft PRs to review/; this is PR #104.
Source: https://github.com/lyricat/inua/pull/104
PR head: 89689b9076071ff313f2ac667e3d3b8fd315a12d

Imported the PR head unchanged to the factory branch above for review. No code review or tests performed during intake; the UI has not been checked by eye here.

PR description (author-reported behavior and verification, not independently verified):

> Sandbox bind mounts under /etc and /.host-etc are listed as devices in Files because their backing filesystem is not pseudo. Exclude those system paths from the sidebar without changing mount lookup or hiding user mounts. Verified with `go test ./internal/apps/files -run ^TestMountinfo$`. No live UI check performed.
