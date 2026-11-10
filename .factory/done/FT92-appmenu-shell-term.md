---
source: pr #76
branch: feat/appmenu-shell-term
round: 1
---
# feat(appmenu): the terminal and settings publish menus for the bar

## factory · 2026-11-10 08:36
Imported GitHub PR #76 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/76
Upstream head at import: 1fa21f459391aa59ad46da329d065af38aaa4a75
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## Lyric · 2026-11-10 09:53
先把 FT92~FT100移动到 backlog


## factory · 2026-11-10 20:37
Owner requested moving all current backlog items to new: “把 backlog 这些放到 new，注意他们的分支也在 fork 上”。
Moved backlog → new; no build or review performed in this update.
Verified source branch: `iamcheyan/inua:feat/appmenu-shell-term` (fork), head `3786e78c234612d288d90ca0ddf1e8cb89696679`.
The existing branch header names the fork branch, not an origin branch. For the next build, fetch `refs/pull/76/head` from origin to obtain the PR head; continue from that code rather than assuming `origin/feat/appmenu-shell-term` exists or starting again from master. Preserve the existing PR work and branch identity; do not push to the contributor's fork in this board update.


## factory · 2026-11-10 21:01
Owner corrected the requested destination: “诶不对，他们应该放到 review”。
Moved new → review at the owner’s request; no build, review or tests performed in this update.
Review the existing fork PR implementation using the source PR head (refs/pull/<PR>/head); the branch header is a fork branch, not necessarily an origin branch. The preceding instruction to build is superseded by this review request.


## factory · review · 2026-11-10 21:44
head: 36e3f62fe5f42dbd1c5aaaab7be5112ad10d376e
verdict: pass
notes:
- [P3] internal/ui/settings_menu.go:33: Settings publishes a process-wide menu also discovered for other shell windows. With Settings open and an About window focused, File → Close closes Settings rather than About. Guard actions by active Settings window or publish only while active. Confirmed by code tracing; not exercised visually.
- [P3] internal/termui/menubar.go:99: Terminal → Quit bypasses closeWindow and its SetUI save. Resize or fold a column, then quit from the new Linux menu: latest UI state is not saved. Route quit through window close. Confirmed by code tracing; not exercised visually.

Reviewed origin/master...HEAD with Claude Code. Fetched PR #76 fork head 3786e78c, merged master 3478c014, resolved generated catalog conflicts preserving master translations, regenerated catalogs and pushed 36e3f62f to origin/feat/appmenu-shell-term. The contributor fork was not changed; this verdict covers the origin branch, not the original PR head.
Validation: merge coder, manager and separate review coder each ran make test successfully with throwaway HOME. Review reports 18 Python tests, go vet, 117 Go packages with race checks; private-bus discovery test ran. Catalog regeneration is byte-identical; worktree clean. No installs. No P0/P1 findings; moved review → ready, not merged or released.
Residual risks: UI not visually checked, no nested session or macOS validation; focus-dependent terminal actions need interactive verification. Independent publisher name counters can collide if both publishers are used in one process, but no current binary does so; excluded as a speculative finding.


## factory · 2026-11-10 23:35
Shipped to master; merge commit: d1a8bdf1c411409a5ccdc4ae0eaeccd1b5942f0c. Release: v0.1.33, final master 7dd6decf22e8e33fa026768cc2b673508dc59078; master and annotated tag pushed atomically.
PR #76 already merged Contributor forks were not modified.
Claude Code resolved integration conflicts and performed a fresh read-only combined-diff review (same persistent Claude session, not an independent reviewer): no P0/P1 findings. make test passed after every merge, on final combined review, and on independent manager rerun: 18 Python tests, go vet, Go race tests. make generate and make i18n current; protected paths unchanged. No installs or test failures this run.
Remaining P3: Settings menu can target Settings while another shell window is focused; terminal menu Quit does not save latest UI layout.
Tests used isolated HOME/TMPDIR/private XDG_RUNTIME_DIR and private D-Bus. UI not visually checked; no real multi-monitor/nested desktop, power actions or live settings exercised. Per-bar wiring lacks direct automated GDK monitor coverage.
ready/ → done/.
