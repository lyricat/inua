#!/usr/bin/env python3
"""Disposable Wayfire 0.10.0 IPC experiment. No foreign-toplevel ID conversion."""

from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from probe_common import (Journal, ProbeError, Wire, confirm, failure_fields, integer, metadata,
                          parser, records, require, watch)

PREFIX = "window-rules/"
EVENTS = ["view-mapped", "view-unmapped", "view-set-output", "view-geometry-changed",
          "view-wset-changed", "view-focused", "view-title-changed", "view-app-id-changed",
          "view-workspace-changed", "output-gain-focus", "output-wset-changed",
          "wset-workspace-changed", "view-minimized", "view-fullscreen", "view-sticky"]
READ_METHODS = [PREFIX + m for m in ("list-views", "list-outputs", "list-wsets", "get-focused-view")]


class Wayfire:
    def __init__(self, args, emit):
        self.args = args
        self.emit = emit
        self.wire = None
        self.methods = set()

    @staticmethod
    def reply(value):
        if isinstance(value, dict) and "error" in value:
            require(isinstance(value["error"], str), "invalid Wayfire error string")
            raise ProbeError("Wayfire error: " + value["error"], "rejected")
        return value

    def request(self, method, data=None):
        if self.wire is None:
            self.wire = Wire(self.args.nested_socket, "native-u32", self.args.timeout, self.emit, "request")
        self.wire.send({"method": method, "data": {} if data is None else data})
        return self.reply(self.wire.read())

    def close(self):
        self.methods.clear()
        if self.wire is not None:
            self.wire.close()
            self.wire = None

    def need(self, method):
        if method not in self.methods:
            raise ProbeError("unavailable IPC method: " + method +
                             " (requires ipc/ipc-rules; workspace switch also requires vswitch)",
                             "unavailable")

    def inspect(self):
        self.methods = set()
        value = self.request("list-methods")
        require(isinstance(value, dict) and isinstance(value.get("methods"), list) and
                all(isinstance(m, str) for m in value["methods"]), "invalid method list")
        self.methods = set(value["methods"])
        for method in READ_METHODS:
            self.need(method)
        for method in (PREFIX + "events/watch", PREFIX + "focus-view", "vswitch/set-workspace"):
            self.emit("availability", method=method, available=method in self.methods)
        for method, data in (("wayfire/configuration", {}),
                             ("wayfire/get-config-option", {"option": "core/plugins"})):
            self.need(method)
            result = self.request(method, data)
            require(isinstance(result, dict), "invalid configuration reply")
            if method.endswith("get-config-option"):
                require(result.get("result") == "ok" and isinstance(result.get("value"), str),
                        "invalid core/plugins reply")
            else:
                require("api-version" in result and "build-commit" in result,
                        "missing build configuration fields")
            self.emit("server-configuration", method=method, value=result,
                      declared_version=self.args.version, target="0.10.0")

    def snapshot(self):
        views = records(self.request(PREFIX + "list-views"))
        outputs = records(self.request(PREFIX + "list-outputs"))
        wsets = records(self.request(PREFIX + "list-wsets"), "index")
        for view in views:
            require(integer(view.get("wset-index"), -1), "invalid signed wset-index")
            require(type(view.get("mapped")) is bool and isinstance(view.get("role"), str),
                    "invalid view role/mapped fields")
        for item in outputs + wsets:
            ws = item.get("workspace")
            require(isinstance(ws, dict) and integer(ws.get("grid_width"), 1) and
                    integer(ws.get("grid_height"), 1) and integer(ws.get("x")) and
                    integer(ws.get("y")), "invalid workspace grid")
            require(ws["x"] < ws["grid_width"] and ws["y"] < ws["grid_height"],
                    "active workspace outside reported grid")
        focused = self.request(PREFIX + "get-focused-view")
        require(isinstance(focused, dict) and focused.get("result") == "ok" and "info" in focused,
                "invalid focused-view reply")
        if focused["info"] is not None:
            records([focused["info"]])
        return {"views": views, "outputs": outputs, "wsets": wsets, "focused_view": focused["info"]}

    def subscribe(self):
        self.need(PREFIX + "events/watch")
        wire = Wire(self.args.nested_socket, "native-u32", self.args.timeout, self.emit, "events")
        try:
            wire.send({"method": PREFIX + "events/watch", "data": {"events": EVENTS}})
            value = self.reply(wire.read())
            require(isinstance(value, dict) and value.get("result") == "ok", "invalid watch acknowledgement")
            return wire
        except BaseException:
            wire.close()
            raise

    @staticmethod
    def check_event(event):
        Wayfire.reply(event)
        require(isinstance(event, dict) and isinstance(event.get("event"), str), "invalid Wayfire event")
        name = event["event"]
        require(name in EVENTS, "unexpected event: " + name)
        if name.startswith("view-"):
            require("view" in event, "missing event view")
            if event["view"] is not None:
                records([event["view"]])
            else:
                require(name == "view-focused", "unexpected null event view")
        if name == "view-workspace-changed":
            for key in ("from", "to"):
                point = event.get(key)
                require(isinstance(point, dict) and integer(point.get("x"), -2**31, 2**31 - 1) and
                        integer(point.get("y"), -2**31, 2**31 - 1), "invalid workspace event coordinates")

    def act(self, args):
        state = self.snapshot()
        self.emit("action-preflight", atomic=False, state=state)
        if args.command == "focus":
            require(integer(args.id, 0, 2**32 - 1), "view ID must fit the native uint32 lookup", "invalid-target")
            view = next((v for v in state["views"] if v["id"] == args.id), None)
            require(view is not None, "unknown IPC view ID; foreign-toplevel handles are not IPC IDs", "invalid-target")
            require(view["role"] == "toplevel" and view["mapped"], "view is not a mapped toplevel", "invalid-target")
            method, data = PREFIX + "focus-view", {"id": args.id}
            predicate = lambda s: s["focused_view"] is not None and s["focused_view"]["id"] == args.id
        else:
            require(integer(args.output, 0, 2**31 - 1) and integer(args.x, 0, 2**31 - 1) and
                    integer(args.y, 0, 2**31 - 1), "invalid output ID or grid coordinates", "invalid-target")
            output = next((o for o in state["outputs"] if o["id"] == args.output), None)
            require(output is not None, "unknown output ID", "invalid-target")
            ws = output["workspace"]
            require(args.x < ws["grid_width"] and args.y < ws["grid_height"], "coordinates outside grid", "invalid-target")
            method, data = "vswitch/set-workspace", {"output-id": args.output, "x": args.x, "y": args.y}
            predicate = lambda s: any(o["id"] == args.output and o["workspace"]["x"] == args.x and
                                      o["workspace"]["y"] == args.y for o in s["outputs"])
        self.need(method)
        self.emit("action-attempt", method=method, payload=data, retry=False)
        value = self.request(method, data)
        require(isinstance(value, dict) and value.get("result") == "ok", "invalid action acknowledgement")
        self.emit("action-acknowledged", completion_proven=False)
        confirm(self, predicate, args.timeout, self.emit)


def main():
    p = parser("Wayfire")
    sub = p.add_subparsers(dest="command", required=True)
    sub.add_parser("snapshot")
    sub.add_parser("watch")
    sub.add_parser("focus").add_argument("id", type=int, help="IPC view ID, not a Wayland object ID")
    switch = sub.add_parser("switch")
    switch.add_argument("output", type=int)
    switch.add_argument("x", type=int)
    switch.add_argument("y", type=int)
    args = p.parse_args()
    emit = Journal()
    probe = None
    try:
        metadata(args, "wayfire", emit)
        probe = Wayfire(args, emit)
        if args.command == "watch":
            watch(probe, args, emit)
        else:
            probe.inspect()
            if args.command == "snapshot":
                emit("snapshot", atomic=False, state=probe.snapshot())
            else:
                probe.act(args)
        return 0
    except (OSError, EOFError, ProbeError) as error:
        emit("failed", state=None, **failure_fields(error), mutations_replayed=False)
        return 1
    except KeyboardInterrupt:
        emit("interrupted", state=None, mutations_replayed=False)
        return 130
    finally:
        if probe is not None:
            probe.close()


if __name__ == "__main__":
    sys.exit(main())
