---
source: pr #98
branch: factory/FT109-overview-preserve-selection
round: 1
---
# fix(overview): preserve selection until the pointer moves

## factory · 2026-11-15 20:27
Imported https://github.com/lyricat/inua/pull/98 directly into review/ at the owner's request.
PR head: 2d22df24291ffda91072fa7b6ff7c4745a16daa3. Preserved on the dedicated factory branch; the PR's source branch is named master.

Scope reported by the PR: opening overview under a stationary pointer must preserve the initial selection. Record the opening pointer position and enable hover selection only after the pointer changes position, including motion within the same window preview and coordinate comparisons across monitors.

PR-author validation (not independently rerun during import): go test ./internal/ui ./internal/shell, make i18n and git diff --check reported passing. Author reports a live two-monitor Hyprland interaction check confirmed by the user; no screenshot or nested-session validation provided. This is author-reported evidence, not a factory UI check.

Pending factory review against current master and make test. This import performs no code review, tests, merge or release.

## factory · review · 2026-11-15 20:38
head: 2d22df24291ffda91072fa7b6ff7c4745a16daa3
verdict: pass
notes:
- Claude Code reviewed origin/master...HEAD against the imported PR request; no qualifying findings. Opening pointer capture, movement gating, same-preview motion, event ordering and cross-monitor coordinates were traced.
- make test passed with throwaway HOME and isolated session environment: 18 Python session tests, go vet ./..., and CGO_ENABLED=1 go test -race ./... (117 packages passed). Earlier command setup errors were corrected before the successful full run.
- Catalog extraction in a scratch copy matches committed catalogs; changed Go files are gofmt-clean. No installs or tracked-file changes.
- UI was not visually or interactively checked; PR-author live two-monitor validation remains unverified. Synthetic compositor events, cross-monitor keyboard focus and entering from outside overview were not exercised. No automated test covers the new pointer gating.

## factory · 2026-11-15 21:15
Shipped to master by the interrupted ship run; completing board bookkeeping after owner-requested fresh Claude Code review.
Merge: d04821ce7cf1fa80f78f6fc30134d2cfb260a2c4
Release: v0.1.40 (master c103e6af70910e604a357f13e3fdb7a75c00ee67); remote master and tag verified.
Claude Code independently reviewed combined range 66672412..c103e6af, all 16 changed files, and found no P0/P1. Isolated make test passed (Python session tests, go vet, race-enabled Go tests, 117 packages); gofmt and diff checks clean. No installs, edits or commits by reviewer.
Linked PR #98 is already merged.
UI not checked visually or interactively; no deployment or restart. Factory branches retained (clean not configured).
