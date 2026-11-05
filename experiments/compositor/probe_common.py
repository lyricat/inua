"""Disposable experiment helpers. Nothing here is a shell/runtime API."""

import argparse
import hashlib
import json
import math
from pathlib import Path
import select
import socket
import struct
import time


class ProbeError(Exception):
    def __init__(self, message, category="malformed"):
        super().__init__(message)
        self.category = category


def failure_fields(error):
    if isinstance(error, ProbeError):
        category = error.category
    elif isinstance(error, PermissionError):
        category = "denied"
    elif isinstance(error, TimeoutError):
        category = "timeout"
    elif isinstance(error, (FileNotFoundError, ConnectionRefusedError)):
        category = "unavailable"
    elif isinstance(error, (EOFError, ConnectionError)):
        category = "disconnected"
    else:
        category = "os-error"
    return dict(category=category, error_type=type(error).__name__, reason=str(error))


def require(condition, message, category="malformed"):
    if not condition:
        raise ProbeError(message, category)


def integer(value, minimum=0, maximum=2**64 - 1):
    return type(value) is int and minimum <= value <= maximum


def records(value, key="id"):
    require(isinstance(value, list), "expected an array, not an unavailable/null state")
    seen = set()
    for item in value:
        require(isinstance(item, dict) and integer(item.get(key)), "invalid native ID record")
        require(item[key] not in seen, "duplicate native ID")
        seen.add(item[key])
    return value


class Journal:
    def __init__(self):
        self.start = time.monotonic()
        self.seq = 0

    def __call__(self, kind, **fields):
        self.seq += 1
        print(json.dumps(dict(seq=self.seq, elapsed=round(time.monotonic() - self.start, 6),
                              kind=kind, **fields), ensure_ascii=False), flush=True)


class Wire:
    """Bounded JSON framing with one deadline per entire message, including fragments."""

    def __init__(self, path, framing, timeout, emit, channel):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(timeout)
        try:
            self.sock.connect(path)
        except BaseException:
            self.sock.close()
            raise
        self.framing = framing
        self.timeout = timeout
        self.emit = emit
        self.channel = channel
        self.buffer = bytearray()

    def close(self):
        self.sock.close()

    def send(self, request):
        data = json.dumps(request, ensure_ascii=False, separators=(",", ":"),
                          allow_nan=False).encode("utf-8")
        # Wayfire's inbound limit includes the four-byte header.
        require(len(data) <= (1 << 20) - 4, "request too large")
        frame = (struct.pack("=I", len(data)) + data if self.framing == "native-u32"
                 else data + b"\n")
        self.emit("request", channel=self.channel, payload=request)
        self.sock.settimeout(self.timeout)
        self.sock.sendall(frame)

    def ready(self, timeout):
        return bool(self.buffer) or bool(select.select([self.sock], [], [], timeout)[0])

    def read(self):
        deadline = time.monotonic() + self.timeout
        while True:
            if self.framing == "native-u32":
                if len(self.buffer) >= 4:
                    length = struct.unpack("=I", self.buffer[:4])[0]
                    require(0 < length <= 1 << 20, "invalid frame length")
                    end = 4 + length
                    if len(self.buffer) >= end:
                        data = bytes(self.buffer[4:end])
                        del self.buffer[:end]
                        break
            else:
                end = self.buffer.find(b"\n")
                require(end <= 1 << 20 and (end >= 0 or len(self.buffer) <= 1 << 20),
                        "line too large")
                if end >= 0:
                    data = bytes(self.buffer[:end])
                    del self.buffer[:end + 1]
                    break
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("incomplete reply/frame timed out")
            self.sock.settimeout(remaining)
            data = self.sock.recv(65536)
            if not data:
                raise EOFError("IPC disconnected (possibly a truncated frame)")
            self.buffer.extend(data)
        try:
            value = json.loads(data.decode("utf-8"), parse_constant=self._bad_constant,
                               object_pairs_hook=self._unique_object)
        except (ValueError, UnicodeError, RecursionError) as error:
            raise ProbeError("malformed JSON reply: " + str(error)) from error
        self.emit("reply", channel=self.channel, payload=value)
        return value

    @staticmethod
    def _bad_constant(value):
        raise ValueError("invalid JSON constant: " + value)

    @staticmethod
    def _unique_object(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise ValueError("duplicate JSON key: " + key)
            value[key] = item
        return value


def parser(target):
    p = argparse.ArgumentParser(description=f"Disposable {target} IPC experiment; nested session only")
    p.add_argument("--nested-socket", required=True, help="explicit IPC socket; never read from environment")
    p.add_argument("--version", required=True, help="operator-recorded compositor/package version")
    p.add_argument("--config", required=True, type=Path, help="explicit nested config to hash, not modify")
    p.add_argument("--timeout", type=float, default=3, help="request/frame and action observation timeout")
    p.add_argument("--seconds", type=float, default=30, help="watch duration")
    p.add_argument("--reconnects", type=int, default=2, help="watch-only retries at the selected socket")
    return p


def metadata(args, target, emit):
    require(args.version.strip(), "version must not be empty")
    require(math.isfinite(args.timeout) and args.timeout > 0, "timeout must be positive and finite")
    require(math.isfinite(args.seconds) and args.seconds > 0, "seconds must be positive and finite")
    require(args.reconnects >= 0, "reconnects must be nonnegative")
    data = args.config.read_bytes()
    emit("run", target=target, declared_version=args.version,
         config_sha256=hashlib.sha256(data).hexdigest(), config_bytes=len(data),
         socket_selection="explicit nested socket", real_session_verdict="UNTESTED by this code alone")


def watch(probe, args, emit):
    """Log native events; periodically reread, never merge unordered deltas into a model."""
    end = time.monotonic() + args.seconds
    attempts = 0
    generation = 0
    valid = False
    while time.monotonic() < end:
        stream = None
        try:
            generation += 1
            emit("epoch-begin", generation=generation, state=None)
            probe.inspect()
            stream = probe.subscribe()
            emit("subscribed", generation=generation)
            emit("snapshot-begin", generation=generation)
            snapshot = probe.snapshot()
            emit("snapshot", generation=generation, atomic=False, state=snapshot)
            valid = True
            next_snapshot = time.monotonic() + 1
            while time.monotonic() < end:
                if stream.ready(min(0.1, max(0, end - time.monotonic()))):
                    event = stream.read()
                    probe.check_event(event)
                    emit("event", generation=generation, payload=event)
                if time.monotonic() >= next_snapshot:
                    emit("snapshot-begin", generation=generation)
                    snapshot = probe.snapshot()
                    emit("snapshot", generation=generation, atomic=False, state=snapshot)
                    next_snapshot = time.monotonic() + 1
        except (OSError, EOFError, ProbeError) as error:
            snapshot = None
            valid = False
            emit("invalidated", generation=generation, state=None, **failure_fields(error),
                 mutations_replayed=False)
            # Malformed replies/missing plugins are not transient transport failures.
            if isinstance(error, (ProbeError, PermissionError)) or attempts >= args.reconnects:
                raise
            attempts += 1
            time.sleep(min(0.25, max(0, end - time.monotonic())))
        finally:
            if stream is not None:
                stream.close()
            probe.close()
    require(valid, "watch ended disconnected; no current snapshot", "disconnected")
    emit("watch-ended", generation=generation, retries=attempts)


def confirm(probe, predicate, timeout, emit):
    end = time.monotonic() + timeout
    while True:
        state = probe.snapshot()
        emit("action-observation", atomic=False, state=state)
        if predicate(state):
            emit("action-observed", causality_proven=False)
            return
        if time.monotonic() >= end:
            raise ProbeError("action acknowledged but target state not observed; outcome unconfirmed, not retried",
                             "unconfirmed")
        time.sleep(0.1)
