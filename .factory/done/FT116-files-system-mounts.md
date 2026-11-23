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

## factory · review · 2026-11-23 15:38
head: 89689b9076071ff313f2ac667e3d3b8fd315a12d
verdict: pass
notes:
- Claude Code reviewed origin/master...HEAD; no findings. System configuration mounts are filtered only from UserMounts; mount lookup and user mounts remain unchanged.
- make test passed: 18 Python session tests, go vet ./..., and CGO_ENABLED=1 go test -race ./... (117 packages passed, 32 without tests). Tests used an isolated HOME and caches.
- No unpinned tools installed; only pinned Go modules restored. Worktree unchanged.
- UI was not checked by eye. Other sandbox bind-mount paths and the separate UDisks2 volume list were not exercised.

## factory · 2026-11-23 19:28
Shipped into master: ac2f21cd1d25a2588e5102a697d7955f6cbcafe7. Release: v0.1.43; master and annotated tag pushed atomically.
Latest passing Claude review matched the branch head before merge. make generate and make i18n passed after each merge; final regeneration left no diff. Targeted package race tests and final make test passed (18 Python tests, go vet and Go race tests).
PR #104 already marked MERGED after push; no close needed. Branch retained (clean not configured). No new installs.
Existing P2/P3 findings remain unchanged. UI not checked by eye; no nested compositor session, deployment or restart.
