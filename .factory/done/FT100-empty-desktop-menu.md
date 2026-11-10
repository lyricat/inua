---
source: pr #96
branch: pr/empty-desktop-menu
round: 2
---
# feat(bar): a designed system menu on an empty workspace

## factory · 2026-11-10 08:36
Imported GitHub PR #96 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/96
Upstream head at import: fc7eed8a54f21d0e8a43e3c19eb0c4a4e43e3c45
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## Lyric · 2026-11-10 09:53
先把 FT92~FT100移动到 backlog


## factory · 2026-11-10 20:37
Owner requested moving all current backlog items to new: “把 backlog 这些放到 new，注意他们的分支也在 fork 上”。
Moved backlog → new; no build or review performed in this update.
Verified source branch: `iamcheyan/inua:pr/empty-desktop-menu` (fork), head `fc7eed8a54f21d0e8a43e3c19eb0c4a4e43e3c45`.
The existing branch header names the fork branch, not an origin branch. For the next build, fetch `refs/pull/96/head` from origin to obtain the PR head; continue from that code rather than assuming `origin/pr/empty-desktop-menu` exists or starting again from master. Preserve the existing PR work and branch identity; do not push to the contributor's fork in this board update.


## factory · 2026-11-10 21:01
Owner corrected the requested destination: “诶不对，他们应该放到 review”。
Moved new → review at the owner’s request; no build, review or tests performed in this update.
Review the existing fork PR implementation using the source PR head (refs/pull/<PR>/head); the branch header is a fork branch, not necessarily an origin branch. The preceding instruction to build is superseded by this review request.


## factory · plan · 2026-11-10 21:13
Build started before the concurrent owner correction was read; there was no prior plan or review fix list.
Behaviour: system menus on genuinely empty focused workspaces, normal app menus on occupied workspaces including unknown PIDs; settings route to translated pages; power actions reuse existing capability and confirmation handling.
Scope: preserve PR #96, merge current master, fix regressions and cover menu logic; exclude unrelated per-monitor menu features.
Decisions: retain branch identity, use existing shell actions, no live desktop or power operations.
Checking: empty/focused/special workspace cases, settings entries and translations, power availability, existing asynchronous menu guards; make i18n and make test. UI not checked visually.

## factory · 2026-11-10 21:13
Concurrent update: the owner moved this item new → review at 21:01 while Claude Code was building. This was discovered on the required board pull after the code push. The item remains in review, respecting the corrected destination; no further implementation was started after discovering it.
The completed work was pushed to origin/pr/empty-desktop-menu, not to the contributor fork. Reviewers must distinguish that built branch from the original PR #96 fork head; the built head is bf2bb23a (merge 421e968e, original PR fc7eed8a). No merge into master or release occurred.
Built: integrated current master; detected truly empty desktops rather than missing PIDs; preserved app-menu handling; translated and completed settings entries; refreshed hibernate availability; routed destructive session/power actions through the existing Control Center confirmation. Added model regression tests and docs/appmenus.md; regenerated catalogs.
Decisions: About appears in both Settings and Help; Screenshot opens its tool; Control Center opens rather than toggles. Existing untranslated settings labels remain as on master.
Installs: none; no manifest or lockfile changes.
Tests: Claude Code make test passed; manager independently reran make test with throwaway HOME/TMPDIR and it passed (Python tests, go vet, Go race tests). Protected paths unchanged; worktree clean.
Not checked: visual UI, nested session, live logind detection and actual power actions. Owner should inspect menu appearance and confirmation flow.
Push initially failed because the credential helper needed a repository path; the single retry with command-local credential.useHttpPath=true succeeded.


## factory · review · 2026-11-10 21:57
head: bf2bb23aa83d5bc049b9cba2e04527d6ebecb263
verdict: pass
notes:
- Claude Code reviewed origin/master...HEAD (merge-base 3478c014), 14 files, against the request and build plan. No confirmed P0/P1/P2/P3 defects.
- Validation: isolated make test passed (18 Python tests, go vet, Go race tests). The initial run failed because the reviewer unset XDG_RUN2026-11-10 21:57_DIR; rerun with a private 0700 runtime directory passed. This was a test setup error, not a branch defect. make i18n on a throwaway tracked-files copy produced no changes. Worktree clean; no installs or commits.
- Residual UI risk: the reviewer estimated that the Settings submenu may exceed a short screen's height, but did not verify GTK placement/clipping. Its proposed P3 is retained as an unconfirmed risk, not a finding.
- UI not visually checked; nested session, real power/session/lock/screenshot actions, live hibernate capability changes and pinned-window compositor behavior were not exercised. Menu lock/screenshot use built-in actions rather than custom Control Center command overrides.
- Moved review → ready. No merge into master or release. Review applies to the origin built branch, not the unchanged fork PR head.


## factory · 2026-11-10 23:35
Shipped to master; merge commit: a14c78d47f13c934b64549f8fc84a283a9446df2. Release: v0.1.33, final master 7dd6decf22e8e33fa026768cc2b673508dc59078; master and annotated tag pushed atomically.
PR #96 already merged Contributor forks were not modified.
Claude Code resolved integration conflicts and performed a fresh read-only combined-diff review (same persistent Claude session, not an independent reviewer): no P0/P1 findings. make test passed after every merge, on final combined review, and on independent manager rerun: 18 Python tests, go vet, Go race tests. make generate and make i18n current; protected paths unchanged. No installs or test failures this run.
Conflict resolution preserves empty-workspace menu alongside FT99 per-monitor published menus. Power confirmation behavior inspected; not exercised live.
Tests used isolated HOME/TMPDIR/private XDG_RUNTIME_DIR and private D-Bus. UI not visually checked; no real multi-monitor/nested desktop, power actions or live settings exercised. Per-bar wiring lacks direct automated GDK monitor coverage.
ready/ → done/.
