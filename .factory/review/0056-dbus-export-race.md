---
branch: factory/0056-dbus-export-race
round: 0
---
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

## factory · plan · 2026-11-03 08:22
**Behaviour:** Serialize exports and removals per D-Bus connection, including startup and method-handler exports, while preserving paths, interfaces and errors. Dispatch and method execution must not hold the export lock; handlers can themselves export objects.

**Scope:** Add shared export protection, migrate all connection exports under internal/ and cmd/ (including tests), protect property exports, and add private-bus race regression coverage. No UI or live-session changes.

**Decisions:** Reference-count active/waiting export operations so idle connections are not permanently retained. Cover export variants and audit source for unprotected calls. Dependency patches were not part of the initial implementation plan.

**Checking:** Run make i18n and make test; demonstrate that the concurrency regression fails without protection. Check subtree and mapping semantics as well as concurrent method traffic.

## factory · question · 2026-11-03 08:22
The build and one verification retry stopped before pushing code. Local branch factory/0056-dbus-export-race holds commit 3ad1146 with export serialization, migrated call sites, property protection and regression tests. The item remains in new/.

Codex reports the negative control reproduces PathExists/unexport races without serialization; the protected export concurrency test passes. make i18n, 15 Python tests and go vet passed. Factory independently ran make test: it fails with races in godbus defaultHandler.LookupObject during subtree dispatch. Export-only serialization cannot protect that internal reader. The retry confirmed that LookupObject traverses the interface map without its object lock, while AddInterface/DeleteInterface write under that lock; introspectPath also reads the objects map without locking. Existing regression tests remain intact. No UI changed and no real desktop session was checked.

1. May we vendor godbus v5.2.2 under third_party and use go.mod replace for minimal internal locking fixes, retaining the shared export protection and regression tests? Recommended default: yes, so subtree dispatch and introspection can be protected as well as export operations.

## lyric · 2026-11-03 08:40
1. No. Do not vendor, replace or patch godbus.

Keep the export serialization, the migrated call sites, the property protection and the source audit: they fix the crash in #56. Subtree exports are set up only before the bus name is owned and removed only after it is released, as the keyring does; they never change while calls arrive. Change the subtree part of the regression test to that pattern instead of exporting and removing subtrees during calls, and keep the concurrent export test as it is. Treat the subtree and introspection reads inside godbus as out of scope here; they are reported upstream in https://github.com/godbus/dbus/pull/460 and come with a later godbus release.

## factory · plan · 2026-11-03 08:45
**Behaviour:** Serialize exports and removals per connection without changing served paths, interfaces or errors. Subtree tests export before acquiring the service name, keep interfaces stable while calls arrive, drain replies and release the name before removal.

**Scope:** Retain shared export serialization, migrated call sites, property protection, source audit and the unchanged concurrent export regression. Adjust subtree semantics coverage only; no UI or dependency changes.

**Decisions:** Follow the owner's clarification: do not vendor, replace or patch godbus. Its internal subtree/introspection races outside this lifecycle remain out of scope pending an upstream release.

**Checking:** Run make i18n and make test (Python tests, go vet, Go race tests), plus repeated focused dbusexport race coverage.

## factory · 2026-11-03 08:45
Resumed the existing build after the owner's answer. Codex committed f5139f1 on top of 3ad1146; both were verified and pushed to factory/0056-dbus-export-race. Subtree coverage now follows service-name ownership and checks removal and the surviving interface. The concurrent export regression remains unchanged.

Factory independently ran make i18n and make test successfully with a throwaway HOME; tracked files remained clean and no forbidden paths changed. Codex also reports ten passing focused race-test runs. No godbus dependency changes. No UI changed; the real desktop session was not checked or touched. Moved to review/ for merge-result verification in the next pass.
