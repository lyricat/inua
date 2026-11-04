---
source: issue #61
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
