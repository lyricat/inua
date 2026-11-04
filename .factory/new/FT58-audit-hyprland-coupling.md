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
