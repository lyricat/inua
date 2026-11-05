#!/usr/bin/env python3
"""Disposable niri 26.04 / niri-ipc 26.4.0 experiment. Python standard library only."""

from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from probe_common import (Journal, ProbeError, Wire, confirm, failure_fields, integer, metadata,
                          parser, records, require, watch)


class Niri:
    def __init__(self, args, emit):
        self.args = args
        self.emit = emit
        self.wire = None

    def request(self, request, response):
        if self.wire is None:
            self.wire = Wire(self.args.nested_socket, "lines", self.args.timeout, self.emit, "request")
        self.wire.send(request)
        return self.reply(self.wire.read(), response)

    def close(self):
        if self.wire is not None:
            self.wire.close()
            self.wire = None

    @staticmethod
    def reply(value, response):
        require(isinstance(value, dict) and len(value) == 1, "invalid niri reply envelope")
        if "Err" in value:
            require(isinstance(value["Err"], str), "invalid niri error string")
            raise ProbeError("niri error: " + value["Err"], "rejected")
        require("Ok" in value, "missing niri Ok/Err")
        value = value["Ok"]
        if response == "Handled":
            require(value == "Handled", "expected Handled acknowledgement")
            return None
        require(isinstance(value, dict) and set(value) == {response},
                "unexpected niri response variant: " + str(value))
        return value[response]

    def inspect(self):
        version = self.request("Version", "Version")
        require(isinstance(version, str) and version, "invalid Version")
        self.emit("server-version", value=version, target="26.04 / niri-ipc 26.4.0",
                  declared=self.args.version)

    def snapshot(self):
        # Separate requests are explicitly NOT a consistent cross-entity transaction.
        workspaces = records(self.request("Workspaces", "Workspaces"))
        windows = records(self.request("Windows", "Windows"))
        for window in windows:
            require(type(window.get("is_focused")) is bool, "invalid window focus flag")
            require("workspace_id" in window and (window["workspace_id"] is None or
                    integer(window["workspace_id"])), "invalid window workspace ID")
        for ws in workspaces:
            require(type(ws.get("is_active")) is bool and type(ws.get("is_focused")) is bool,
                    "invalid workspace active/focus flags")
            require(integer(ws.get("idx"), 1, 255), "invalid workspace index")
        return {"workspaces": workspaces, "windows": windows}

    def subscribe(self):
        wire = Wire(self.args.nested_socket, "lines", self.args.timeout, self.emit, "events")
        try:
            wire.send("EventStream")
            self.reply(wire.read(), "Handled")
            return wire
        except BaseException:
            wire.close()
            raise

    @staticmethod
    def check_event(event):
        require(isinstance(event, dict) and len(event) == 1, "invalid niri event envelope")
        name, payload = next(iter(event.items()))
        require(isinstance(payload, dict) and name not in ("Ok", "Err"), "invalid niri event payload")
        if name in ("WindowsChanged", "WorkspacesChanged"):
            records(payload.get("windows" if name == "WindowsChanged" else "workspaces"))
        elif name == "WindowOpenedOrChanged":
            records([payload.get("window")])
        elif name in ("WindowClosed", "WindowFocusChanged", "WorkspaceActivated"):
            require("id" in payload and (integer(payload["id"]) or
                    (name == "WindowFocusChanged" and payload["id"] is None)), "invalid event ID")
        # Other variants are raw observations, never interpreted as state updates.

    def act(self, args):
        state = self.snapshot()
        self.emit("action-preflight", atomic=False, state=state)
        require(integer(args.id), "ID must be an unsigned 64-bit native ID", "invalid-target")
        if args.command == "focus":
            require(any(w["id"] == args.id for w in state["windows"]), "unknown window ID", "invalid-target")
            action = {"FocusWindow": {"id": args.id}}
            predicate = lambda s: any(w["id"] == args.id and w["is_focused"] for w in s["windows"])
        else:
            require(any(w["id"] == args.id for w in state["workspaces"]), "unknown workspace ID (not idx)", "invalid-target")
            action = {"FocusWorkspace": {"reference": {"Id": args.id}}}
            predicate = lambda s: any(w["id"] == args.id and w["is_focused"] for w in s["workspaces"])
        self.emit("action-attempt", payload=action, retry=False)
        self.request({"Action": action}, "Handled")
        self.emit("action-acknowledged", completion_proven=False)
        confirm(self, predicate, args.timeout, self.emit)


def main():
    p = parser("niri")
    sub = p.add_subparsers(dest="command", required=True)
    sub.add_parser("snapshot")
    sub.add_parser("watch")
    for command in ("focus", "switch"):
        sub.add_parser(command).add_argument("id", type=int, help="native window/workspace ID; never idx")
    args = p.parse_args()
    emit = Journal()
    probe = None
    try:
        metadata(args, "niri", emit)
        probe = Niri(args, emit)
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
