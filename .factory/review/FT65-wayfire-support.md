---
source: pr #67
branch: feat/wayfire-compositor-abstraction
round: 1
---
# Wayfire support (compositor abstraction, IPC client, session)

## factory · 2026-11-04 22:46
Added directly to review at the owner's request: 把 pr 67 加到 review status

PR: https://github.com/lyricat/inua/pull/67
Base: master
Head at intake: 854dad6e59e755274b98cb4d01104a3056813c9d

PR description: Self-contained foundation for Wayfire support: compositor abstraction, Wayfire IPC/foreign-toplevel client, session script, sample config, runtime evidence.

The PR author reports passing tests; this intake did not run tests or perform a code review. Review is pending, including verification of the runtime evidence and UI limitations.

## factory · review · 2026-11-04 23:00
head: 854dad6e59e755274b98cb4d01104a3056813c9d
verdict: fix
fix:
- [P1] internal/ui/shell.go:520: desktop.Wayfire is undefined; shell compilation, go vet and Go tests fail. Implement the compositor detector.
- [P1] scripts/test-session.py:108: make test fails because the new test expects generated configuration while the session script exits on missing configuration. Align the startup contract and test; base session tests pass.
- [P1] scripts/inua-wayfire-session:23: shell starts before Wayfire provides a Wayland display; on a fresh login shell exits and Wayfire never starts. Launch shell through compositor autostart.
- [P1] internal/wayfire/wayland.go:196: initial roundtrip drops message payload; an existing toplevel(new_id) triggers short foreign-toplevel new handle and ends monitoring. Forward the payload.
- [P1] internal/ui/wayfire.go:104: foreign-toplevel object IDs are passed as Wayfire IPC view IDs for focus/close. Object 0xff000000 becomes view 4278190080, which is not its compositor view ID. Use protocol-native actions or a verified mapping.
- [P1] internal/clipboard: required race-test validation also fails TestFindSource on base; pre-existing, not introduced by this PR. Resolve the baseline test failure before claiming the required suite passes.
notes:
- [P2] internal/ui/shell.go:797: removing syncLauncher after snapshot updates leaves stale Hyprland window rows, e.g. after window close; restore view synchronization.
- [P2] internal/wayfire/wayland.go:229: wl_output opcodes/fields are decoded incorrectly; valid name/scale/mode events produce an empty monitor list. Follow the protocol event definitions.
- [P2] internal/wayfire/wayland.go:324: close uses opcode 3 (unset_minimized), not 5; handle destruction at line 261 uses 0, not 7. Correct both request opcodes.
- [P2] internal/wayfire/client.go:583: long-lived event subscriptions inherit the request deadline and silently stop after two seconds by default. Manage subscription deadlines/cancellation separately; fake socket reproduced timeout at 50ms.
- [P2] internal/wayfire/client.go:448: maximum workspace index is mistaken for grid width; workspace 4 in a 3x3 grid maps to (3,0), not (0,1). Preserve actual grid width for switching/moving.
- [P2] internal/wayfire/client.go:43: uint64 wset-index rejects valid -1 values and fails the whole state refresh. Decode signed values and handle them explicitly.
- [P2] config/session/wayfire.ini:26: bindings_* = key : command is not supported; use paired binding_* and command_* settings.
- [P3] docs/wayfire-debian-validation.md:25: remove local usernames, absolute paths and process identifiers from committed runtime evidence.

Validation: reviewed all 19 changed files in origin/master...HEAD with Codex. make test fails the new Python session test (base: 15 pass). CGO_ENABLED=0 go vet ./... and CGO_ENABLED=1 go test -race ./... fail undefined desktop.Wayfire; TestFindSource also fails on base. Seven isolated Wayfire reproductions confirmed defects. Tests used throwaway HOME. No unpinned installs or manifest changes; pinned Go dependencies restored only. No tracked code changes or commits.

Limits: screenshot dimensions/hashes match, but runtime evidence cannot validate this non-compiling HEAD and references handlers absent from it. No real or nested compositor session or live UI visual verification; lockscreen, permission boundaries and real compositor integration remain unchecked. PR #67 is from a fork, so its head was fetched via refs/pull/67/head rather than a nonexistent origin branch. No merge conflicts with origin/master.

## lyric · 2026-11-05 09:42
Tracking issue for this item: https://github.com/lyricat/inua/issues/68, a sub-issue of the overview #58 (a pull request cannot be one).
Closes #68
