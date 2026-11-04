"""Fake Unix servers only. Run in a throwaway HOME/XDG; no session discovery."""

import copy
import importlib.util
import json
import os
from pathlib import Path
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from probe_common import ProbeError, Wire, failure_fields, records, watch

ROOT = Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / name / "probe.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


niri = load("niri")
wayfire = load("wayfire")


def encode(value, framing):
    data = json.dumps(value, ensure_ascii=False).encode()
    return struct.pack("=I", len(data)) + data if framing == "native-u32" else data + b"\n"


class FakeServer:
    def __init__(self, case, framing, handle):
        self.tmp = tempfile.TemporaryDirectory(prefix="ipc-")
        self.path = str(Path(self.tmp.name) / "s")
        self.framing = framing
        self.handle = handle
        self.requests = []
        self.errors = []
        self.stop = threading.Event()
        self.threads = []
        self.listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.listener.bind(self.path)
        self.listener.listen()
        self.listener.settimeout(0.05)
        self.thread = threading.Thread(target=self.accept)
        self.thread.start()
        case.addCleanup(self.close)

    def accept(self):
        while not self.stop.is_set():
            try:
                conn, _ = self.listener.accept()
            except socket.timeout:
                continue
            thread = threading.Thread(target=self.serve, args=(conn,))
            self.threads.append(thread)
            thread.start()

    def serve(self, conn):
        try:
            with conn, conn.makefile("rb") as reader:
                conn.settimeout(2)
                while not self.stop.is_set():
                    if self.framing == "lines":
                        data = reader.readline()
                        if not data:
                            return
                    else:
                        header = reader.read(4)
                        if not header:
                            return
                        length = struct.unpack("=I", header)[0]
                        data = reader.read(length)
                    request = json.loads(data)
                    self.requests.append(request)
                    if self.handle(self, conn, request) is False:
                        return
        except (BrokenPipeError, ConnectionResetError):
            pass  # A rejected frame closes the probe side immediately.
        except BaseException as error:
            self.errors.append(error)

    def send(self, conn, value):
        conn.sendall(encode(value, self.framing))

    def close(self):
        self.stop.set()
        self.thread.join(2)
        self.listener.close()
        for thread in self.threads:
            thread.join(2)
            if thread.is_alive():
                self.errors.append(AssertionError("fake server worker leaked"))
        self.tmp.cleanup()
        if self.errors:
            raise self.errors[0]


class Fixture:
    def __init__(self, target):
        self.target = target
        self.streams = 0
        self.disconnect_first = False
        self.ignore_action = False
        self.reject_action = False
        self.drop_action = False
        self.empty = False
        self.missing = set()
        self.window = {"id": 2**53 + 3 if target == "niri" else 17,
                       "workspace_id": 901, "is_focused": False,
                       "wset-index": -1, "role": "toplevel", "mapped": True,
                       "title": "试验窗口"}
        self.ws = {"id": 901, "idx": 2, "output": "nested", "is_active": False, "is_focused": False}
        self.output = {"id": 4, "wset-index": 27,
                       "workspace": {"x": 0, "y": 0, "grid_width": 3, "grid_height": 2}}
        self.wset = {"index": 27, "output-id": 4, "workspace": copy.deepcopy(self.output["workspace"])}
        self.focused = None
        self.methods = wayfire.READ_METHODS + [wayfire.PREFIX + "events/watch", wayfire.PREFIX + "focus-view",
                                               "vswitch/set-workspace", "wayfire/configuration",
                                               "wayfire/get-config-option"]

    def handle(self, server, conn, request):
        if self.target == "niri":
            if request == "EventStream":
                self.streams += 1
                server.send(conn, {"Ok": "Handled"})
                # These are intentionally coalesced, and workspaces precede windows.
                conn.sendall(encode({"WorkspacesChanged": {"workspaces": [] if self.empty else [self.ws]}}, "lines") +
                             encode({"WindowsChanged": {"windows": [] if self.empty else [self.window]}}, "lines"))
                if self.disconnect_first and self.streams == 1:
                    self.window["id"] += 1
                    return False
                server.stop.wait(2)
                return
            if isinstance(request, dict) and "Action" in request:
                if self.drop_action:
                    return False
                if self.reject_action:
                    server.send(conn, {"Err": "test rejection"})
                    return
                if not self.ignore_action:
                    if "FocusWindow" in request["Action"]:
                        self.window["is_focused"] = True
                    else:
                        self.ws["is_focused"] = True
                server.send(conn, {"Ok": "Handled"})
                return
            values = {"Version": "niri 26.04", "Windows": [] if self.empty else [self.window],
                      "Workspaces": [] if self.empty else [self.ws]}
            server.send(conn, {"Ok": {request: values[request]}})
            return
        method, data = request["method"], request["data"]
        if method == "window-rules/events/watch":
            self.streams += 1
            server.send(conn, {"result": "ok"})
            server.send(conn, {"event": "view-focused", "view": None})
            if self.disconnect_first and self.streams == 1:
                self.window["id"] += 1
                return False
            server.stop.wait(2)
            return
        if method in ("window-rules/focus-view", "vswitch/set-workspace"):
            if self.drop_action:
                return False
            if self.reject_action:
                server.send(conn, {"error": "test rejection"})
                return
            if not self.ignore_action:
                if method.endswith("focus-view"):
                    self.focused = self.window
                else:
                    self.output["workspace"].update(x=data["x"], y=data["y"])
            server.send(conn, {"result": "ok"})
            return
        values = {"list-methods": {"methods": [m for m in self.methods if m not in self.missing]},
                  "wayfire/configuration": {"api-version": 1, "build-commit": "fixture"},
                  "wayfire/get-config-option": {"result": "ok", "value": "ipc ipc-rules vswitch"},
                  "window-rules/list-views": [] if self.empty else [self.window],
                  "window-rules/list-outputs": [] if self.empty else [self.output],
                  "window-rules/list-wsets": [] if self.empty else [self.wset],
                  "window-rules/get-focused-view": {"result": "ok", "info": self.focused}}
        server.send(conn, values[method])


class ProbeTests(unittest.TestCase):
    def probe(self, target):
        fixture = Fixture(target)
        server = FakeServer(self, "lines" if target == "niri" else "native-u32", fixture.handle)
        args = SimpleNamespace(nested_socket=server.path, timeout=0.15, seconds=0.5, reconnects=2,
                               version="fixture", command="focus", id=fixture.window["id"], output=4, x=1, y=1)
        log = []
        emit = lambda kind, **fields: log.append(dict(kind=kind, **copy.deepcopy(fields)))
        probe = niri.Niri(args, emit) if target == "niri" else wayfire.Wayfire(args, emit)
        self.addCleanup(probe.close)
        return fixture, server, args, log, emit, probe

    def test_fragmented_and_coalesced_unicode_frames(self):
        for framing in ("lines", "native-u32"):
            with self.subTest(framing=framing):
                def handle(server, conn, request):
                    data = encode({"title": "窗口"}, framing) + encode([], framing)
                    for byte in data:
                        conn.sendall(bytes([byte]))
                server = FakeServer(self, framing, handle)
                wire = Wire(server.path, framing, 1, lambda *a, **k: None, "test")
                try:
                    wire.send("hello")
                    self.assertEqual(wire.read(), {"title": "窗口"})
                    self.assertEqual(wire.read(), [])
                finally:
                    wire.close()

    def test_bad_frames_and_eof(self):
        cases = [("lines", b"{bad}\n", ProbeError), ("lines", b"", EOFError),
                 ("lines", b"{}", EOFError), ("lines", b"NaN\n", ProbeError),
                 ("lines", b'{"Ok":[],"Ok":null}\n', ProbeError),
                 ("lines", b"\xff\n", ProbeError), ("lines", b"x" * ((1 << 20) + 1), ProbeError),
                 ("native-u32", struct.pack("=I", 0), ProbeError),
                 ("native-u32", struct.pack("=I", (1 << 20) + 1), ProbeError),
                 ("native-u32", b"\x04\x00", EOFError),
                 ("native-u32", struct.pack("=I", 5) + b"{}", EOFError),
                 ("native-u32", struct.pack("=I", 3) + b"bad", ProbeError)]
        for framing, data, error in cases:
            with self.subTest(framing=framing, data=data[:20]):
                def handle(server, conn, request):
                    conn.sendall(data)
                    return False
                server = FakeServer(self, framing, handle)
                wire = Wire(server.path, framing, 1, lambda *a, **k: None, "test")
                try:
                    wire.send("test")
                    with self.assertRaises(error):
                        wire.read()
                finally:
                    wire.close()

    def test_partial_frame_times_out(self):
        for framing in ("lines", "native-u32"):
            with self.subTest(framing=framing):
                def handle(server, conn, request):
                    conn.sendall(b'{' if framing == "lines" else struct.pack("=I", 2) + b'{')
                    server.stop.wait(2)
                server = FakeServer(self, framing, handle)
                wire = Wire(server.path, framing, 0.05, lambda *a, **k: None, "test")
                try:
                    wire.send("test")
                    with self.assertRaises(TimeoutError):
                        wire.read()
                finally:
                    wire.close()

    def test_empty_is_valid_but_null_or_duplicate_ids_are_not(self):
        for target in ("niri", "wayfire"):
            fixture, _, _, _, _, probe = self.probe(target)
            fixture.empty = True
            probe.inspect()
            state = probe.snapshot()
            self.assertEqual(state["windows" if target == "niri" else "views"], [])
        for value in (None, {}, [{"id": True}], [{"id": 1}, {"id": 1}]):
            with self.assertRaises(ProbeError):
                records(value)

    def test_native_action_ids_and_grid(self):
        for target in ("niri", "wayfire"):
            fixture, server, args, log, _, probe = self.probe(target)
            probe.inspect()
            probe.act(args)
            self.assertEqual(log[-1]["kind"], "action-observed")
            args.command = "switch"
            args.id = fixture.ws["id"]
            probe.act(args)
            if target == "niri":
                self.assertIn({"Action": {"FocusWindow": {"id": 2**53 + 3}}}, server.requests)
                self.assertIn({"Action": {"FocusWorkspace": {"reference": {"Id": 901}}}}, server.requests)
            else:
                self.assertIn({"method": "window-rules/focus-view", "data": {"id": 17}}, server.requests)
                self.assertIn({"method": "vswitch/set-workspace", "data": {"output-id": 4, "x": 1, "y": 1}}, server.requests)
                self.assertEqual(probe.snapshot()["views"][0]["wset-index"], -1)

    def test_unknown_ids_and_invalid_grid_never_mutate(self):
        for target in ("niri", "wayfire"):
            _, _, args, log, _, probe = self.probe(target)
            probe.inspect()
            for value in (-1, True, 2**64, 999):
                args.id = value
                with self.assertRaises(ProbeError) as caught:
                    probe.act(args)
                self.assertEqual(caught.exception.category, "invalid-target")
            args.command, args.id, args.x = "switch", 2, 3  # idx is not ID; width is not valid x.
            with self.assertRaises(ProbeError):
                probe.act(args)
            args.output, args.x = 999, 0
            with self.assertRaises(ProbeError):
                probe.act(args)
            self.assertFalse(any(e["kind"] == "action-attempt" for e in log))

    def test_missing_plugins_and_error_envelopes(self):
        fixture, _, _, _, _, probe = self.probe("wayfire")
        fixture.missing.add("window-rules/list-views")
        with self.assertRaisesRegex(ProbeError, "unavailable IPC method"):
            probe.inspect()
        for value in ({"Err": "denied"}, {"Err": None}, {"Ok": {}}, {}, {"Ok": "Handled", "Err": "bad"}):
            with self.assertRaises(ProbeError):
                niri.Niri.reply(value, "Windows")
        for value in ({"error": "no such method"}, {"error": ""}, {"error": None}):
            with self.assertRaises(ProbeError):
                wayfire.Wayfire.reply(value)

    def test_unavailable_actions_and_watch_do_not_fake_success(self):
        fixture, _, args, log, _, probe = self.probe("wayfire")
        fixture.missing.update({"window-rules/focus-view", "window-rules/events/watch", "vswitch/set-workspace"})
        probe.inspect()
        with self.assertRaises(ProbeError):
            probe.subscribe()
        with self.assertRaises(ProbeError):
            probe.act(args)
        args.command = "switch"
        with self.assertRaises(ProbeError):
            probe.act(args)
        self.assertFalse(any(e["kind"] == "action-attempt" for e in log))

    def test_actions_reject_unconfirmed_error_and_disconnect_without_replay(self):
        for target in ("niri", "wayfire"):
            for command, flag in ((c, f) for c in ("focus", "switch")
                                  for f in ("ignore_action", "reject_action", "drop_action")):
                with self.subTest(target=target, command=command, flag=flag):
                    fixture, _, args, log, _, probe = self.probe(target)
                    args.command = command
                    if command == "switch":
                        args.id = fixture.ws["id"]
                    setattr(fixture, flag, True)
                    probe.inspect()
                    with self.assertRaises((ProbeError, EOFError)):
                        probe.act(args)
                    self.assertEqual(sum(e["kind"] == "action-attempt" for e in log), 1)
                    self.assertFalse(any(e["kind"] == "action-observed" for e in log))

    def test_disconnect_invalidates_and_reconnect_resnapshots(self):
        for target in ("niri", "wayfire"):
            fixture, _, args, log, emit, probe = self.probe(target)
            fixture.disconnect_first = True
            watch(probe, args, emit)
            invalid = next(i for i, e in enumerate(log) if e["kind"] == "invalidated")
            self.assertIsNone(log[invalid]["state"])
            after = [e for e in log[invalid + 1:] if e["kind"] == "snapshot"]
            self.assertEqual(after[0]["generation"], 2)
            key = "windows" if target == "niri" else "views"
            self.assertEqual(after[0]["state"][key][0]["id"], fixture.window["id"])
            self.assertFalse(any(e["kind"] == "action-attempt" for e in log))
            self.assertEqual(fixture.streams, 2)
            self.assertEqual([e["generation"] for e in log if e["kind"] == "epoch-begin"], [1, 2])
            inspect_kind = "server-version" if target == "niri" else "availability"
            self.assertTrue(any(e["kind"] == inspect_kind for e in log[invalid + 1:]))
            self.assertIsNone(probe.wire)
            if target == "wayfire":
                self.assertFalse(probe.methods)

    def test_subscription_precedes_nonatomic_snapshot_for_both_targets(self):
        for target in ("niri", "wayfire"):
            _, server, args, log, emit, probe = self.probe(target)
            args.seconds = 0.1
            watch(probe, args, emit)
            requests = server.requests if target == "niri" else [r["method"] for r in server.requests]
            subscription = "EventStream" if target == "niri" else "window-rules/events/watch"
            query = "Workspaces" if target == "niri" else "window-rules/list-views"
            self.assertLess(requests.index(subscription), requests.index(query))
            kinds = [e["kind"] for e in log]
            self.assertLess(kinds.index("subscribed"), kinds.index("snapshot-begin"))
            self.assertLess(kinds.index("snapshot"), kinds.index("event"))
            self.assertFalse(next(e for e in log if e["kind"] == "snapshot")["atomic"])

    def test_permission_denial_stops_without_retry_or_snapshot(self):
        # Deterministic even when tests run as root; no real socket is contacted.
        for target in ("niri", "wayfire"):
            _, _, args, log, emit, probe = self.probe(target)
            with patch.object(socket.socket, "connect", side_effect=PermissionError(13, "denied")) as connect:
                with self.assertRaises(PermissionError):
                    watch(probe, args, emit)
                self.assertEqual(connect.call_count, 1)
            self.assertEqual(log[-1]["category"], "denied")
            self.assertIsNone(log[-1]["state"])
            self.assertFalse(any(e["kind"] == "snapshot" for e in log))

    def test_failure_categories_keep_server_errors_and_null_distinct(self):
        for error, category in ((FileNotFoundError(), "unavailable"),
                                (TimeoutError(), "timeout"), (EOFError(), "disconnected")):
            self.assertEqual(failure_fields(error)["category"], category)
        for target in ("niri", "wayfire"):
            for reply, category in (({"Err": "permission denied"} if target == "niri" else
                                     {"error": "permission denied"}, "rejected"),
                                    ({"Ok": {"Workspaces": None}} if target == "niri" else None, "malformed")):
                server = FakeServer(self, "lines" if target == "niri" else "native-u32",
                                    lambda s, c, r, value=reply: s.send(c, value))
                args = SimpleNamespace(nested_socket=server.path, timeout=0.15)
                probe = (niri.Niri if target == "niri" else wayfire.Wayfire)(args, lambda *a, **k: None)
                try:
                    with self.assertRaises(ProbeError) as caught:
                        probe.snapshot()
                    self.assertEqual(failure_fields(caught.exception)["category"], category)
                finally:
                    probe.close()

    def test_idle_watch_outlives_request_timeout(self):
        for target in ("niri", "wayfire"):
            _, _, args, log, emit, probe = self.probe(target)
            args.seconds = 0.35
            watch(probe, args, emit)
            self.assertFalse(any(e["kind"] == "invalidated" for e in log))
            self.assertEqual(log[-1]["kind"], "watch-ended")

    def test_event_shapes_and_order_are_preserved(self):
        niri.Niri.check_event({"WindowFocusChanged": {"id": None}})
        wayfire.Wayfire.check_event({"event": "view-focused", "view": None})
        for check, event in [(niri.Niri.check_event, {"WindowsChanged": {"windows": None}}),
                             (niri.Niri.check_event, {"WindowClosed": {}}),
                             (wayfire.Wayfire.check_event, {"result": "ok"}),
                             (wayfire.Wayfire.check_event, {"event": "view-workspace-changed", "view": {"id": 1}})]:
            with self.assertRaises(ProbeError):
                check(event)
        _, _, args, log, emit, probe = self.probe("niri")
        args.seconds = 0.1
        watch(probe, args, emit)
        names = [next(iter(e["payload"])) for e in log if e["kind"] == "event"]
        self.assertEqual(names[:2], ["WorkspacesChanged", "WindowsChanged"])

    def test_reconnect_budget_and_malformed_snapshot_fail_visibly(self):
        for target in ("niri", "wayfire"):
            fixture, _, args, log, emit, probe = self.probe(target)
            fixture.disconnect_first = True
            args.reconnects = 0
            with self.assertRaises(EOFError):
                watch(probe, args, emit)
            self.assertEqual(log[-1]["kind"], "invalidated")
            self.assertIsNone(log[-1]["state"])
            self.assertEqual(fixture.streams, 1)
        for target, reply in (("niri", {"Err": "permission denied"}),
                              ("niri", {"Ok": {"Workspaces": None}}),
                              ("wayfire", {"error": "plugin unloaded"}),
                              ("wayfire", None)):
            framing = "lines" if target == "niri" else "native-u32"
            server = FakeServer(self, framing, lambda s, c, r: s.send(c, reply))
            args = SimpleNamespace(nested_socket=server.path, timeout=0.1)
            cls = niri.Niri if target == "niri" else wayfire.Wayfire
            probe = cls(args, lambda *a, **k: None)
            try:
                with self.assertRaises(ProbeError):
                    probe.snapshot()
            finally:
                probe.close()

    def test_cli_requires_explicit_socket_and_reports_missing_socket(self):
        with tempfile.TemporaryDirectory(prefix="cli-") as temp:
            config = Path(temp) / "config"
            config.write_text("# synthetic config\n")
            for target in ("niri", "wayfire"):
                script = str(ROOT / target / "probe.py")
                result = subprocess.run([sys.executable, script, "snapshot"], capture_output=True, text=True)
                self.assertEqual(result.returncode, 2)
                self.assertIn("--nested-socket", result.stderr)
                result = subprocess.run([sys.executable, script, "--nested-socket", str(Path(temp)/"missing"),
                                         "--version", "fixture", "--config", str(config), "snapshot"],
                                        capture_output=True, text=True)
                self.assertEqual(result.returncode, 1)
                entries = [json.loads(line) for line in result.stdout.splitlines()]
                self.assertEqual(entries[0]["kind"], "run")
                self.assertEqual(entries[-1]["kind"], "failed")
                self.assertIsNone(entries[-1]["state"])


if __name__ == "__main__":
    unittest.main()
