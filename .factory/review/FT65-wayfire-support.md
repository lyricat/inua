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

## factory · review · 2026-11-05 11:43
head: b05e52527410209fcdfd3e54831e7dcda62cffc5
verdict: fix
fix:
- [P1] internal/wayfire/wayland.go:196: initial roundtrip replaces existing-window toplevel(new_id) payload with nil, reproducing "short foreign-toplevel new handle" and terminating window monitoring. Forward the original payload.
- [P1] internal/ui/wayfire.go:104: foreign-toplevel object IDs are used as IPC view IDs; object 0xff000000 sends id=4278190080, so Dock/launcher focus and close target no valid view. Use protocol-native operations or a verified ID mapping.
notes:
- [P2] internal/ui/shell.go:797: closing a Hyprland window while the launcher is open leaves stale rows because snapshot updates no longer call syncLauncher. Restore synchronization.
- [P2] internal/wayfire/wayland.go:219: valid wl_output mode/scale/name events produce an empty monitor list due to incorrect opcodes and fields. Decode the protocol correctly.
- [P2] internal/wayfire/wayland.go:324: close sends opcode 3 (unset_minimized), not 5; destruction at line 261 sends 0 (set_maximized), not 7. Correct requests and tests asserting the wrong values.
- [P2] internal/wayfire/client.go:583: subscriptions retain the ordinary request deadline and stop after two seconds by default; fake connection reproduced a 50ms timeout. Manage subscription deadlines/cancellation independently and surface failures.
- [P2] internal/wayfire/client.go:448: maximum workspace ID is used as grid width; workspace 4 in a 3x3 grid sends (3,0), not (0,1). Preserve actual grid width.
- [P2] internal/wayfire/client.go:43: uint64 wset-index rejects a panel record with -1 and fails the whole refresh. Decode signed values and handle unassigned records explicitly.
- [P2] config/session/wayfire.ini:26: bindings_* = key : command does not register command-plugin shortcuts. Use paired binding_* and command_* settings.
- [P2] config/session/wayfire.ini:23: shell instance is wayfire but commands address default, so shortcuts cannot control this shell even after binding syntax is fixed. Use one instance name.
- [P3] docs/wayfire-debian-validation.md:25: anonymize local usernames, absolute paths and process identifiers in committed evidence.

Validation: Codex reviewed all 20 changed files in origin/master...HEAD. Final make test passed (16 Python session tests, go vet, all Go race tests); base comparison passed too. Seven fake-connection reproductions confirmed protocol defects. Earlier missing desktop.Wayfire, session test and premature shell startup failures are fixed; clipboard.TestFindSource passed on both HEAD and base. An initial test attempt hit temporary socket path length limits; shorter test paths passed without source edits. No installations, tracked changes or commits. PR head reconfirmed unchanged before recording; no merge conflicts with origin/master. Fork PR fetched via pull/67/head.

Limits: no real or nested compositor/UI verification, lockscreen or actual compositor integration checks. Screenshot hashes match but do not establish runtime correctness of this head. Tests used throwaway HOME. One existing base-test helper created and cleaned a socket in the system temporary directory, outside the requested run directory; this constraint deviation is recorded rather than hidden.
