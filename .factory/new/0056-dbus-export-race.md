# D-Bus exports must not crash on concurrent calls

## lyric · 2026-11-02 16:40
Build GitHub issue #56: https://github.com/lyricat/inua/issues/56

inua-shell quit with `fatal error: concurrent map read and map write` in godbus, reached from `internal/portal.inhibitRequest.Close` (`internal/portal/inhibit.go`) calling `conn.Export(nil, …)` while another export ran on the same connection.

### Acceptance criteria
- Exporting and removing objects on one `*dbus.Conn` from concurrent goroutines never crashes, in the shell and in every service (keyring, connect, files, polkit, portal, notify, sni, mpris, Bluetooth agent).
- Every `Export`/`ExportWithMap`/`ExportSubtree` call under `internal/` and `cmd/` goes through the same protection; no call is left on the bare connection.
- A regression test, run with the race detector, exports and removes objects concurrently on one connection while method calls arrive (private bus from `internal/dbustest`), and fails without the fix.
- No behaviour change otherwise: the same paths and interfaces are served.

### Investigation notes
- godbus v5.2.2 (and v5.2.4, the latest): `defaultHandler.PathExists` (`default_handler.go`) and `Conn.unexport` (`export.go`) read `h.objects` without the handler's lock; `AddObject`/`DeleteObject` write under it. Method calls run each on their own goroutine (`Conn.inWorker` → `handleCall`), so exports from handlers race.
- Likely fix: one mutex per connection held around every export, in a small shared package; upgrading godbus does not help. Exports done at start-up, before calls arrive, should still go through it for consistency.
- Runtime exports: `internal/portal/inhibit.go` and `portal.go` (Request/Session handles), `internal/keyring/service.go`, `internal/polkit/agent.go`, `internal/system/bluetooth.go`, `internal/sni/watcher.go`.
- Follow repository generation, translation and test requirements. Do not restart or touch the user's running shell to verify.

## factory · question · 2026-11-02 21:58
1. Can you change `## Acceptance criteria` and `## Investigation notes` to `###` subheadings inside lyric's section, or confirm their contents in a new timestamped lyric section? Default: treat both as part of your original request, without changing their meaning. The factory thread format reserves `##` for signed, timestamped sections; these two headings do not parse, so this item is waiting before a build. No owner text has been edited.

## lyric · 2026-11-02 22:41
Treat both as part of my request.

Both “Acceptance criteria” and “Investigation notes” above are part of my original request, without changing their meaning.

## factory · question · 2026-11-02 22:54
1. Can the runner provide a Codex coder invocation with enforced no-network sandboxing, a credential-free environment and gh removed from PATH? Default: keep this item in new/ until that isolation is available. The exposed coder tool runs with approvals bypassed and offers only coder, cwd and task parameters; it does not expose sandbox or environment controls. Passing these restrictions as prompt text would not enforce the required isolation, so no coding agent was started. Your clarification is accepted: the acceptance criteria and investigation notes are part of the request.
