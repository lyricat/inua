---
branch: factory/FT62-term-drag-sessions
round: 1
---
# Terminal: drag and drop sessions

## lyric · 2026-11-04 22:24
In inua-term I want to drag and drop sessions to reorder them, as workspaces can be dragged already.

## factory · plan · 2026-11-05 00:35
Behaviour: Drag a column 2 session header to reorder the whole session, including its pane rows, within the current workspace. Match workspace dragging: snapshot, animated gap, immediate optimistic drop; keep selected session/pane and running processes. Cancel or outside drops restore the order. Empty and single-session lists are safe; reject stale, foreign and cross-workspace drags. Persist through the host and broadcast to clients; reconcile failed requests with authoritative state. First run stays unchanged.

Scope: Session ordering in model/host/client, GTK header drag handling, behavioural regression tests and terminal documentation. No cross-workspace moves, pane-drag changes or context menus.

Decisions: Headers are handles, whole session blocks move; reuse workspace interaction conventions without broad refactoring. Search disables reordering.

Checking: Model moves in both directions and to endpoints, no-op/invalid requests, preserved focus and pane layout, host validation and persistence; run make test. Manually check varying-height sessions, cancellation, search, concurrent changes, client synchronization and restart persistence in an isolated nested session. UI is not checked by eye in this factory.

## factory · 2026-11-05 00:35
Built and pushed 4739b7dc (feat(term): reorder sessions by dragging headers).
Implemented session-header dragging with snapshot/gap animation, in-workspace ordering, disabled dragging during search, and stale/foreign drag rejection. Model and host preserve session/pane identity, broadcast and persist order; failed requests restore authoritative state. Updated terminal/design documentation and translation catalogs.
Installs: none.
Checks: coder tests and independent manager make test both passed under throwaway HOME (Python session tests, go vet ./..., go test -race ./...). make i18n and git diff --check passed. No protected paths changed.
Not checked: UI was not inspected by eye; nested-session manual checks listed above remain for the owner. No live windows or services were manipulated.
Moved new/ → review/ for review; not merged into master.

## factory · review · 2026-11-05 00:59
head: 4739b7dce32c74a543dcdd3a92ed35553d5c051b
verdict: pass

Validation: Codex reviewed all 17 files in origin/master...HEAD against the request and plan; no verified introduced P0–P3 defects found. No merge conflicts. After correcting isolated HOME, temporary-directory and Unix socket path-length compatibility issues, make test passed (Python session tests, go vet, Go race tests); git diff --check passed. Worktree unchanged; no code commits. Installs: none.
Limits: UI was not checked by eye. Real drag animations, scrolling and concurrent interactions remain for manual verification. Terminal-engine-dependent tests were skipped by existing test rules because the engine was unavailable.
Moved review/ → ready/; not merged into master. No linked issues or PRs.
