---
source: pr #95
branch: pr/appmenu-per-monitor
round: 1
---
# fix(appmenu): keep each monitor on its own window menus

## factory · 2026-11-10 08:36
Imported GitHub PR #95 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/95
Upstream head at import: 8122c416da7004bd6abd7f0f263fc9b7465ba4eb
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## Lyric · 2026-11-10 09:53
先把 FT92~FT100移动到 backlog


## factory · 2026-11-10 20:37
Owner requested moving all current backlog items to new: “把 backlog 这些放到 new，注意他们的分支也在 fork 上”。
Moved backlog → new; no build or review performed in this update.
Verified source branch: `iamcheyan/inua:pr/appmenu-per-monitor` (fork), head `8122c416da7004bd6abd7f0f263fc9b7465ba4eb`.
The existing branch header names the fork branch, not an origin branch. For the next build, fetch `refs/pull/95/head` from origin to obtain the PR head; continue from that code rather than assuming `origin/pr/appmenu-per-monitor` exists or starting again from master. Preserve the existing PR work and branch identity; do not push to the contributor's fork in this board update.


## factory · 2026-11-10 21:01
Owner corrected the requested destination: “诶不对，他们应该放到 review”。
Moved new → review at the owner’s request; no build, review or tests performed in this update.
Review the existing fork PR implementation using the source PR head (refs/pull/<PR>/head); the branch header is a fork branch, not necessarily an origin branch. The preceding instruction to build is superseded by this review request.


## factory · 2026-11-10 22:01
Review blocked: the recorded branch `pr/appmenu-per-monitor` does not exist on origin (lyricat/inua). Verified GitHub PR #95 is open with fork head `iamcheyan/inua:pr/appmenu-per-monitor` at `8122c416da7004bd6abd7f0f263fc9b7465ba4eb`. The owner requested review of the fork implementation, but the factory review procedure requires the recorded origin branch and does not authorize replacing an existing branch header or pushing to the contributor fork. No replacement branch was created. Owner must authorize branch setup repair before factory review can proceed.

FT99 is the oldest unreviewed item; FT89 and FT91 already have reviews for their current heads. No Claude Code invocation, tests, installs or review verdict; item remains in review/. No code changes, merge or release.

## factory · 2026-11-10 22:08
Owner clarification from Lyric: “就按照 fork 分支来 review 不就好了”；requested recording it here: “你在 ft99 下注明一下”。
Review the existing implementation from `iamcheyan/inua:pr/appmenu-per-monitor`, GitHub PR #95. Fetch `refs/pull/95/head` from `origin` and review that fetched commit against the current base, recording the actual full commit SHA in the review's `head:` field. Do not assume that `origin/pr/appmenu-per-monitor` exists; the branch header identifies the fork branch.
Correction to the preceding blocked report: absence of a same-named branch on origin is not itself a blocker for reviewing the fork PR. No factory branch configuration repair or push to the contributor's fork is needed for this read-only review. This clarification supersedes the preceding report's request for branch setup authorization.
Item remains in review/. This update records the review source only; no code review, tests, approval, merge or code-branch push was performed.


## factory · review · 2026-11-10 22:56
head: 8122c416da7004bd6abd7f0f263fc9b7465ba4eb
verdict: pass
notes:
- [P2] internal/ui/appmenu.go:46-48: Registrar changes clear every bar's remembered window; the next search clears its registration even for the same window. Example: a Kate submenu is open on monitor 1 and another app registers a menu; all bars briefly lose menus and the open submenu closes. Preserve the registration during a same-window refresh with a per-bar stale flag.
- [P2] internal/ui/appmenu.go:144-145: clearAppMenu and renderAppMenu (line 208) unconditionally close the global popover. Example: monitor 1 has an open menu and monitor 2 receives a top-level LayoutUpdated or loses its window; monitor 1's menu closes and pending submenu requests are invalidated. Track popover ownership and close/invalidate only for the affected bar.

Read-only Claude Code review of fork PR #95, fetched via refs/pull/95/head per the owner's clarification. Scope: origin/master...HEAD, merge-base bf807607, 9 files; current master 3478c014a6032277b3e6a8f089a67a4979d05b01. Findings verified by code-path inspection, not live UI reproduction. Omitted dead-code/comment-only feedback as nonfunctional.

Validation: initial make test failed because the review environment omitted XDG_RUN2026-11-10 22:56_DIR. After setting a private runtime directory, the manager reran the complete make test successfully (18 Python session tests, go vet, Go race tests), with throwaway HOME and TMPDIR. No remaining required-test failure. No installs or tracked code changes. Catalog references inspected; make i18n regeneration was not verified. No visual UI or live multi-monitor checks.

Integration limitation: the fetched commit conflicts with current master in internal/ui/appmenu.go, internal/ui/bar.go and generated catalogs. Following the explicitly requested read-only fork review, no merge/conflict resolution or code-branch push was performed. Pass means no P0/P1 review findings, not merge readiness; integration and testing of the merged result remain required before shipment. Moved review/ → ready/ under the factory review threshold.
