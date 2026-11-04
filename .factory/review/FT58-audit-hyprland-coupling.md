---
source: issue #59
branch: factory/FT58-audit-hyprland-coupling
round: 1
---
# Audit existing Hyprland coupling by user-facing feature

## lyric · 2026-11-03 14:23
Build GitHub issue #59: https://github.com/lyricat/inua/issues/59
Parent overview (context only): #58.

Move #59 into factory/new for the initial investigation. This supersedes #59's earlier planning-only / keep-out-of-factory status for this issue only. Issues #60–#64 remain outside factory.

### Objective
Establish an evidence-backed inventory before choosing interfaces or promising portability. This task is investigation and documentation only: do not implement an abstraction layer, migrate Hyprland, add another compositor, or change runtime behavior.

### Scope
Trace applicable user-facing features (windows, workspaces, outputs, Overview, focus, shortcuts, previews, Dock avoidance, and session startup/configuration) to their information, actions, events, and lifecycle requirements.
Classify standard Wayland protocols and required versions, Hyprland-specific IPC/extensions, implicit geometry/workspace/event assumptions, and startup/configuration dependencies. Include source paths and symbols; searching for `hypr` alone is insufficient.

### Deliverables / acceptance criteria
- A repository document containing a feature → code location → dependency and semantics → alternative → limitation/fallback matrix.
- Distinguish confirmed findings, hypotheses, and unknowns; do not present untested alternative compositor capabilities as verified.
- Identify compositor-internal integration needs and likely migration risks.
- Identify existing regression tests and missing coverage.
- No runtime code changes or premature interface design; further implementation requires separate approval.

### Dependencies
None; this is the starting investigation.

## factory · plan · 2026-11-04 11:38
Behaviour: Trace user-facing windows, workspaces, outputs, Overview, focus, shortcuts, previews, Dock avoidance and session startup from initial/empty state through actions, events, disconnection and error handling.

Scope: Documentation matrix mapping features to paths/symbols, dependencies and semantics, alternatives and fallback limitations; include protocol versions, compositor-internal requirements, migration risks and regression coverage. No runtime implementation or abstraction design; issues #60–64 excluded.

Decisions: Separate source-confirmed findings from hypotheses and unknowns; do not claim alternative compositor compatibility without testing. Use docs/hyprland-coupling.md and link from architecture documentation.

Checking: Check source references and protocol XML, inspect coverage against acceptance criteria, run make test in a throwaway HOME with isolated session directories. No new automated tests for documentation-only changes.

## factory · 2026-11-04 11:38
Built and pushed 52837fa (with recovered task commit 8a03af4). Added the feature audit and architecture link; clarified Overview output changes, shortcut-recording failure boundaries and window-move semantics. Codex checked 22 features and 134 local references, cited symbols and protocol XML. Only two documentation files changed; protected paths untouched. git diff --check passed.

Recovery: Reused the stopped build worktree and its task commit. Preserved untracked audit artifacts in a stash; read-only module-cache leftovers that stash could not remove were moved to a recovery directory outside the worktree.

Installs: No unpinned tools or dependency-manifest changes; retry installed/downloaded nothing.

Tests: Full make test still fails after retry and independent manager verification (exit 2). Python 15 tests and go vet pass. Codex ran the same full command on isolated master 4ad7126 and reproduced the same failing tests: clipboard, apps/disk, upgrade, vm and vmui. The short temporary-path alias avoids Unix socket length errors but causes path-validation failures; clipboard depends on host process enumeration. Example failure: `source_test.go:49: firefox: got start-hyprland`. No unrelated runtime fixes were made. Review must account for the failing required test.

Not checked: UI, real compositor sessions and alternative desktop compatibility were not exercised. No UI or runtime code changed.

Moved new/ → review/ for review, not approval to merge.
