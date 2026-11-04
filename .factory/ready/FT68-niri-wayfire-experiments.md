---
source: issue #61
branch: factory/FT68-niri-wayfire-experiments
round: 1
---
# Validate niri and Wayfire with small integration experiments

## lyric · 2026-11-04 23:04
Build GitHub issue #61: https://github.com/lyricat/inua/issues/61
Parent overview (context only): #58. This is the third sub-issue; #62–#64 stay outside the factory until the one before them is done. The targets are settled: niri and Wayfire, both of them.

This supersedes #61's keep-out-of-factory status for this issue only. Disposable prototypes and documentation: no change to the shell, no abstraction layer, no production refactor.

### Inputs
- The coupling inventory from #59: docs/hyprland-coupling.md.
- The capability comparison from #60 (FT67) when it is done; if FT67 is still open when this builds, take its matrix as pending and say so.
- PR #67 (board item FT65) is a separate Wayfire foundation under review. Read it for what it learned about Wayfire's IPC and the foreign-toplevel protocol, but do not depend on it, merge it, or fix it here.

### Scope
For each of niri and Wayfire, a narrow vertical slice as a throwaway program under a clearly marked experiment directory (for example experiments/compositor/niri and experiments/compositor/wayfire, excluded from the normal build): enumerate windows and workspaces, observe changes, switch workspaces and focus a window. Probe startup synchronisation, identifiers, event ordering, disconnect and reconnect, and errors. Record niri's scrolling layout and workspace semantics, and which Wayfire capabilities need protocols, IPC or plugins. An operation that is unavailable is recorded as unavailable, never simulated.

### Environment
The build machine has neither niri nor Wayfire, and the factory cannot install system packages or run downloaded binaries. Write each experiment so the owner can run it in a nested session on a machine that has the compositor (the Wayfire evidence in PR #67 came from a Debian machine). Where a compositor is not available to the build, write the instructions and the prototype, check it against the compositor's protocol and IPC documentation and sources, and list every observation that was not made in a real session as untested. Do not move this item to backlog for the missing compositors.

### Deliverables / acceptance criteria
- Separate reproducible experiment instructions for niri and Wayfire, with the compositor versions used or targeted.
- Evidence for each action and event in each slice, and an explicit list of unsupported and untested behaviour.
- A comparison of both targets with Hyprland's semantics and the concrete constraints they put on an interface design.
- A go/no-go recommendation for each target, and any revision to the baseline proposed by #60.
- No claim that the prototypes are compositor support.
- Linked from docs/hyprland-coupling.md.

## factory · plan · 2026-11-05 03:35
Behaviour: Separate opt-in disposable probes enumerate windows/workspaces, watch changes, focus native window IDs and switch native workspaces. Subscribe before non-atomic snapshots; distinguish empty, unavailable, malformed and denied results. Invalidate state on disconnect, reconnect watcher generations without replaying actions, and distinguish acknowledgment from observed effects.
Scope: experiments/compositor and the coupling-inventory link only; version-targeted nested instructions, action/event evidence, unsupported/untested behavior, Hyprland comparison and go/no-go for both targets. No production support, abstraction layer or shell changes; no PR67 merging or fixes.
Decisions: Python standard library and fake Unix sockets; absent compositors are expected, so every real-session observation remains UNTESTED. FT67 is ready but unshipped and remains a pending proposal. Protocol claims use versioned sources, not PR67 runtime claims.
Checking: Fake-socket regression coverage for framing, identifiers, errors, empty/null states, timeouts, startup/event ordering, reconnects and action checks; run experiment tests, make test in throwaway HOME/XDG with isolated buses, diff and documentation-link checks. No live compositor or UI checks.

## factory · 2026-11-05 03:35
Recovered the interrupted build's uncommitted experiment drafts and inventory link, preserving a backup in the build directory; Codex completed them. Commit ba74a605, pushed factory/FT68-niri-wayfire-experiments. Only 11 planned experiment/documentation files changed; no forbidden paths or unrelated commits.
Built separate niri and Wayfire probes, reproducible nested-session instructions, EVIDENCE.md with protocol sources and test limits, native identity/event/error semantics, Hyprland comparison and baseline revision recommendations. Both targets are GO for further nested validation and NO-GO for claiming production support. PR67 was context only; FT67 remains pending.
Checks: Codex experiment suite passed 17 tests; make test passed Python 15 tests, go vet and Go race tests (103 packages; 53 top-level and 6 subtests skipped, reasons in EVIDENCE.md). Manager independently reran both experiment tests and make test with isolated HOME/XDG/buses: both exit 0. git diff --check passed; working tree clean.
Installs: none. All real niri/Wayfire sessions, nested-session instructions and visual observations remain UNTESTED; no UI changes. Source checks and fake sockets do not prove real compositor interoperability.
Moved new/ → review/. Owner should inspect EVIDENCE.md, per-target run instructions and the go/no-go limits after review.

## factory · review · 2026-11-05 03:55
head: ba74a6055961010af0b61cabb688189d224420ed
verdict: pass

Validation: Codex reviewed all 11 changed files in origin/master...HEAD against the owner's request and plan; no confirmed P0–P3 findings. No merge conflicts. Experiment tests: 17 passed. Required make test exited 0: Python session tests 15 passed, go vet passed, Go race tests passed across 103 packages; 53 top-level tests and 6 subtests skipped for missing dependencies, intentionally isolated tools and conditional cases. Diff formatting and internal documentation links passed. Tests used throwaway HOME/XDG, isolated buses and short temporary paths. Manager verified unchanged HEAD, clean worktree and git diff --check. No code edits or commits. Installs: none.
Limits: Real niri/Wayfire sessions, nested-session instructions and visual behavior remain UNTESTED, as explicitly permitted by the owner's experiment scope. These probes do not establish production support. FT67 remains pending and PR67 is context only.
Moved review/ → ready/. Owner should inspect EVIDENCE.md, both targets' run instructions and the go/no-go limitations before shipping.
