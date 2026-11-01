# Confirm before closing a terminal pane with a running program

## owner · 2026-11-01
When the current pane has a program running, closing the pane through a shortcut or command must require confirmation.

Register this request in factory/new only. Do not implement it yet.

## Acceptance criteria
- Closing a pane with a running program asks for confirmation, not just when a recognized agent is Working or Blocked.
- Cancel leaves the pane and program running; only explicit confirmation closes it.
- Repeated shortcuts or an already open sheet must not bypass confirmation.
- An idle shell can still close directly.
- Shortcut and command entry points use consistent protection. Preserve the existing useful warnings for active agents.
- Add regression coverage for the process-state decision and repeated close requests; check the interaction in a nested session and report any unverified cases.

## Investigation notes
- `internal/termui/keys.go`: `closeFocused` currently confirms only Working/Blocked agents; its `a.sheet != nil` branch closes directly.
- `internal/term/host/client.go`: `ClosePane`; inspect host request handling and all callers before choosing where to enforce confirmation.
- `internal/term/host/agents.go` checks PTY foreground processes periodically. Prefer current state at close time over relying only on this cached agent state.
- `internal/term/host/remember.go`: `running`; `internal/term/pty/pty.go`: `Foreground` and `Pid` are existing building blocks. Account for directly launched programs and failed process inspection; define background-job limitations explicitly.
- Follow repository generation, translation and test requirements. Do not manipulate the user's running terminal/session for verification.

No implementation is retained: preliminary changes were reverted at the owner's request. This task has not entered building or review.
