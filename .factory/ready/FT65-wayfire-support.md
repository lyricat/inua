---
source: issue #68
branch: feat/compositor-58
round: 2
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

## factory · 2026-11-05 17:02
Replaced at the owner's request (2026-11-05): this item is no longer built from PR #67. The owner asked for Wayfire support to be done on the #58 branch instead and for that work to take this item's place. The header now names branch `feat/compositor-58` (head 21d0c12f5bf39b126cb80bd17de232034da002ae) and the tracking issue #68 as the source; PR #67 is left open and untouched, and the two reviews above are of its head, not of this branch.

What the branch has for this item: the Wayfire backend of the compositor contract (`internal/wayfire`, IPC only), the session (`scripts/inua-session`, one "Inua" entry, the compositor chosen in Settings), the sample `config/wayfire/wayfire.ini`, inua's keyboard, touchpad, shortcut and display settings applied over Wayfire's IPC, the Displays page (displays read through wlr-output-management, `internal/wlroutput`), and the launcher's window commands through the backend. The two P1 findings on PR #67 do not apply: there is no foreign-toplevel client, and view ids are the IPC's own.

The branch is the whole of #58's work, not Wayfire alone: it also holds the contract (#62), Hyprland behind it (#63), the niri backend and the settings layer (#64). Only #68 is linked here by the owner.

Tests: `make test` passed on the head before the merge of master (18 session tests, go vet, 111 Go packages with the race detector, throwaway HOME); after the merge, which changed only `scripts/inua-session` and its test, the session tests and the build passed.

Checked by running, headless in throwaway containers: Wayfire 0.11.0 (Arch) and Wayfire 0.9.0 (Debian 13, gtk4-layer-shell 1.0.4): the shell starts, workspaces, dock, launcher (window list, fullscreen, centre, move to workspace), overview, control center, notifications, clipboard paste, a screenshot, the Displays page and display rules (mode, scale, position, mirror, off, and back), the login path through `inua-session` in a fresh home. `docs/compositor-experiments.md` has the table.

Not checked: real monitors and GPU output, a display plugged in while the shell runs, a login through a real display manager, Wayfire 0.10, the overview's pointer drag, dictation, area and window screenshots, screen sharing. On Debian 13 the lock screen is not available (its gtk4-layer-shell has no session lock) and the shell says so.

## factory · review · 2026-11-05 18:32
head: 21d0c12f5bf39b126cb80bd17de232034da002ae
verdict: pass
notes:
- [P2] internal/wayfire/config.go:217: resetting input settings sends no replacement for empty overrides, leaving previous values active. Fake IPC: original ctrl:nocaps / 0 / adaptive, overridden caps:escape / 0.7 / flat, then reset still leaves overridden values. Preserve and restore original options.
- [P2] internal/desk/tools.go:184: generic comma rejection blocks valid Wayfire workspace IDs. Listed ID 1:1,0 works directly through the backend but switch_workspace and move_window return "want a workspace". Match exact snapshot IDs before backend-specific validation, retaining injection protection.
- [P2] internal/niri/snapshot.go:273: logical output dimensions are scaled again by screenshot consumers. A 3840x2160 output at scale 2 becomes 1920x1080 in the snapshot, then logicalSize returns 960x540 and captureScale 4 instead of 2. Unify dimension conventions and apply scaling/rotation once.
- [P2] internal/ui/overview_manage.go:408: mixed workspace name/ID lookup selects the wrong niri workspace. With (ID,name) pairs (2,1), (4,2), (6,3), clicking the second card background passes name 2 and resolves ID 2 rather than 4. Carry IDs consistently and match IDs only.

Validation: Codex reviewed all 116 changed files in origin/master...HEAD; no P0/P1 findings. make test exited 0 with 18 Python session tests, go vet and 111 Go packages passing race tests (35 packages have no tests), but TestNewIsShortUnderALongTempDir and TestNewKeepsAShortTempDir were explicitly skipped to avoid their hardcoded temporary-directory use. This is not an unmodified full-suite pass; unavailable-tool tests may also skip. Initial isolated runs failed in unchanged disk/upgrade/vm/vmui packages due to temporary-path launcher setup; adjusted launcher passed, base suite not rerun. Fake IPC and extracted unchanged UI functions reproduced all four findings. No installations, tracked changes or commits. No merge conflicts with origin/master.

Limits: no visual UI, real compositor, monitor/GPU or login integration checks. Tests used throwaway HOME. Existing VM test helpers created and cleaned temporary sockets outside the run directory in two attempts; this execution constraint deviation is recorded. Removed the prior review worktree during recovery; it was clean. Passed under the factory's no-P0/P1 rule and moved to ready; owner should consider the four P2 defects and test exclusions before shipping. Only issue #68 is linked; superseded PR #67 remains untouched.
