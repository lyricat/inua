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

## factory · review · 2026-11-04 11:50
head: 52837fa04ea09321be8fcb1f2cfa300ad603720e
verdict: fix
fix:
- [P1] Makefile:102: Required make test fails on both HEAD and origin/master (5 packages, 54 failures). This is a pre-existing/environment-related test gate failure, not a regression introduced by this documentation change. Resolve the test environment or baseline failures and obtain a passing full make test before approval. Example: source_test.go:49: firefox: got start-hyprland.
notes:
- [P2] docs/hyprland-coupling.md:37: Clipboard and dictation are grouped without documenting the existing clipboard fallback. With client == nil, pasteClipboard invokes wtype to send Ctrl+V to the currently focused window (internal/ui/clipboard.go:629); dictation requires Hyprland. Document these separately, the wtype dependency and the lack of address-targeted focus in that fallback.

Validation: Codex reviewed origin/master...HEAD, the complete documentation diff, source references and protocol XML. Full make test on HEAD and baseline with throwaway HOME and isolated session directories exited 2; Python 15 tests and go vet passed. Both race runs had 5 failing packages / 54 failures and 46 skips. Targeted race rerun of those packages with a real TMPDIR left only clipboard TestFindSource failing; it does not replace the full required test. git diff --check passed; worktree clean. No code changes or commits, no unpinned installs or manifest changes. No merge conflicts with master.

Limits: UI, real compositor sessions and alternative desktop compatibility were not exercised. Item stays in review/ pending owner decision; no merge approval.

## lyric · 2026-11-04 13:15
The test failures from the last round are resolved on master: TestFindSource is fixed (89e121e), and full `make test` passes with TMPDIR unset and a throwaway HOME.

Merge origin/master into this branch first, so it has that fix.

Run `make test` with TMPDIR unset and do not use a short temporary-path alias. If socket path errors return, report them; do not work around them.

Fix the review's P2: in docs/hyprland-coupling.md, document clipboard and dictation separately, including the `wtype` fallback in pasteClipboard and its lack of address-targeted focus.
