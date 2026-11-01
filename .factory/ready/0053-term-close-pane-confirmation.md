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

## factory · 2026-11-01
The owner explicitly authorized implementation, superseding the register-only note. Building on branch `factory/0053-term-close-pane-confirmation` in a separate worktree. Item 0047 is unchanged.

## factory · 2026-11-01 · review
Implemented on `factory/0053-term-close-pane-confirmation`; stop at review,
without merging. The original checkout also shows this item in review.

Changes:
- Host `close` requests inspect the current PTY foreground group and the
  current root process arguments. Only a recognized idle interactive shell
  or an exited pane closes without confirmation. Unknown inspection state,
  direct programs, shell command invocations and exec replacements require
  consent. Working/Blocked agents retain their warnings.
- Ordinary repeated requests never grant consent. The UI ignores close
  requests while checking or while any sheet is open. Cancel/Escape sends
  no confirmed request; only the Close button sends explicit confirmation.
  Both the shortcut and command palette dispatch through this path.
- Documented the host API and its limits: background jobs in separate
  process groups are not detected, and process state can change after inspection.

Verification:
- Added tests first; their initial run failed on the missing decision and
  confirmation APIs. The implemented tests pass.
- Regression coverage: process-state decisions and inspection failures,
  direct programs, foreground children, current exec replacements, repeated
  unconfirmed requests, program survival without consent, explicit confirmation,
  idle shell direct close and Working/Blocked agent protection.
- Terminal packages and termui pass with the race detector; Hyprland config
  tests pass with a temporary runtime directory. Terminal binary builds.
- Final `make test` used a throwaway HOME and runtime directory. Python's
  15 tests and `go vet ./...` pass. The race suite fails only at
  `TestArchiveRoundTrip/7z`: `this needs the 7z program (7zip)`.
  The initial run also lacked XDG_RUNTIME_DIR; that failure was resolved
  by using a temporary runtime directory for the final run.
- `make i18n` passes; catalogs updated. `git diff --check` passes.
- `make generate` fails: `blueprint-compiler: not found` (exit 127).
  No Blueprint layout was changed.
- Nested-session attempt stops with `run this from a Wayland session (e.g. GNOME)`.
  Not visually verified: shortcut and palette confirmation with a foreground
  program; Cancel/Escape preserving it; repeated shortcuts and unrelated open
  sheets; idle-shell direct close; Working/Blocked agent wording; focus changes
  while the inspection is pending. No live terminal, shell or user session
  was stopped, restarted or sent input.

Review remains open with the environment limitations above. The item has not
entered ready and has not been merged. Item 0047 is unchanged.

## factory · 2026-11-01 20:23
Owner decision in Console: “这两问题不构成问题，可以进入下一阶段”.
The owner considers both reported review findings non-blocking: new-client /
old-host close-confirmation compatibility, and foreground children sharing
the shell process group when job control is disabled. No implementation
changes are requested for these findings.

Move to ready (awaiting owner merge) on the owner's explicit acceptance,
not on a claim that all automated or manual verification passed. The
previously recorded full-suite 7z dependency failure and unverified Wayland
interaction remain outstanding; no tests were rerun for this status change.
Implementation branch: `factory/0053-term-close-pane-confirmation`, reviewed
commit: `88be4a3`. No implementation files changed and no merge performed.
