"""inua workshop: write an inua light app in Python.

inua starts the app with this module on PYTHONPATH; nothing to install.
The app describes its window with `ui` and reacts to events with `app`:

    from inua import app, ui

    @app.view
    def view(state):
        return ui.page(ui.entry("name", placeholder="Your name"),
                       ui.text("Hello, " + state.get("name", "")))

    app.run()

After each event the view is drawn again. The protocol underneath is
JSON-RPC 2.0, a message a line, on stdin and stdout; write logs to stderr.
See docs/workshop.md in inua.
"""

import json
import sys
import traceback

PROTOCOL = 1


class InuaError(Exception):
    """An error inua answered a call with; code is JSON-RPC's."""

    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


class Event:
    """What the user did: type (click, change, submit, timer), the id of
    what they did it to, its value, and every input's value in state. A
    topic followed with app.watch is type "desktop", its id the topic and
    its value what changed."""

    def __init__(self, params):
        self.type = params.get("type")
        self.id = params.get("id")
        self.value = params.get("value")
        self.state = params.get("state") or {}

    def __repr__(self):
        return "Event(%r, %r, %r)" % (self.type, self.id, self.value)


class _Store:
    """The app's own data, kept by inua: app.store["key"] = value."""

    def __init__(self, app):
        self._app = app

    def get(self, key, default=None):
        v = self._app.call("store.get", key=key)
        return default if v is None else v

    def __getitem__(self, key):
        return self.get(key)

    def __setitem__(self, key, value):
        self._app.call("store.set", key=key, value=value)

    def __delitem__(self, key):
        self._app.call("store.set", key=key, value=None)


class App:
    def __init__(self):
        self.state = {}  # every input's value by id, as the last event had it
        self.info = {}  # what initialize said: language, theme, app, dir
        self.store = _Store(self)
        self._view = None
        self._init = []
        self._handlers = []  # (id, type, function)
        self._watch = []  # topics to follow from the start
        self._next = 0
        self._pending = []  # messages read while waiting for an answer
        self._running = False

    # --- decorators ---

    def view(self, fn):
        """The function that returns the window's tree from the state."""
        self._view = fn
        return fn

    def on(self, id, type=None):
        """A handler for events of the element id (of any type, or of one:
        "click", "change", "submit", "timer"); it gets the Event. An id
        ending in * matches every id it begins: "delete:*"."""

        def deco(fn):
            self._handlers.append((id, type, fn))
            return fn

        return deco

    def init(self, fn):
        """A function to run once inua said hello, before the first view."""
        self._init.append(fn)
        return fn

    def watch(self, topic):
        """A handler for changes on the desktop, while the window is open:
        "window" (the focused window), "clipboard" (a new copy),
        "notification" (each new one), "system" (Wi-Fi, volume, battery…),
        "media" (what plays). Each needs its use in the manifest. The
        handler gets an Event whose value is what changed."""

        def deco(fn):
            self._handlers.append((topic, "desktop", fn))
            if topic not in self._watch:
                self._watch.append(topic)
            return fn

        return deco

    def subscribe(self, *topics):
        """Follow topics (see watch) from now on; handle them with
        @app.on(topic, "desktop")."""
        self.call("events.subscribe", topics=list(topics))

    # --- talking to inua ---

    def _send(self, msg):
        msg["jsonrpc"] = "2.0"
        sys.stdout.write(json.dumps(msg, ensure_ascii=False, separators=(",", ":")) + "\n")
        sys.stdout.flush()

    def _read(self):
        if self._pending:
            return self._pending.pop(0)
        while True:
            line = sys.stdin.readline()
            if not line:
                return None
            line = line.strip()
            if line:
                return json.loads(line)

    def call(self, method, **params):
        """Ask inua to do something (notify, clipboard.copy, store.get…)
        and wait for its answer; an error raises InuaError."""
        self._next += 1
        mid = self._next
        self._send({"id": mid, "method": method, "params": params})
        while True:
            line = sys.stdin.readline()
            if not line:
                raise InuaError(-32000, "inua went away")
            line = line.strip()
            if not line:
                continue
            msg = json.loads(line)
            if msg.get("id") == mid and "method" not in msg:
                if "error" in msg:
                    e = msg["error"]
                    raise InuaError(e.get("code", -32000), e.get("message", ""))
                return msg.get("result")
            self._pending.append(msg)  # an event meanwhile: after this handler

    def notify(self, method, **params):
        self._send({"method": method, "params": params})

    def set(self, id, value):
        """Set an input's value, as clearing an entry after a submit."""
        self.state[id] = value
        self.notify("state.set", values={id: value})

    def timer(self, id, seconds):
        """A "timer" event for id every so many seconds; 0 stops it."""
        self.call("timer.set", id=id, seconds=seconds)

    def render(self):
        """Draw the view again; done after every event anyway."""
        if self._view is None:
            return
        try:
            root = self._view(self.state)
        except Exception:
            traceback.print_exc()
            root = ui.page(ui.empty("Something went wrong", text=traceback.format_exc().strip().splitlines()[-1], icon="dialog-warning-symbolic"))
        self.notify("render", root=root)

    # --- the loop ---

    def run(self):
        """Answer inua until the window closes."""
        while True:
            msg = self._read()
            if msg is None:
                return
            method = msg.get("method")
            params = msg.get("params") or {}
            if method == "initialize":
                self.info = params
                self._send({"id": msg["id"], "result": {"protocol": PROTOCOL}})
                if self._watch:
                    self._safely(self.subscribe, *self._watch)
                for fn in self._init:
                    self._safely(fn)
                self.render()
            elif method == "shutdown":
                self._send({"id": msg["id"], "result": None})
                return
            elif method == "event":
                ev = Event(params)
                self.state.update(ev.state)
                for id, type, fn in list(self._handlers):
                    hit = id == ev.id or (id.endswith("*") and str(ev.id).startswith(id[:-1]))
                    if hit and (type is None or type == ev.type):
                        self._safely(fn, ev)
                self.render()
            elif "id" in msg and method:
                self._send({"id": msg["id"], "error": {"code": -32601, "message": "no such method: " + method}})

    def _safely(self, fn, *args):
        try:
            fn(*args)
        except Exception:
            traceback.print_exc()


def _node(type, children=None, **props):
    n = {"type": type}
    for k, v in props.items():
        if v is not None and v is not False:
            n[k] = v
    if children:
        n["children"] = [c for c in children if c]
    return n


class ui:
    """The building blocks; each returns a node for the view's tree."""

    @staticmethod
    def page(*children, scroll=True):
        """The window's content; a bar among the children sits at the bottom."""
        n = _node("page", children)
        if not scroll:
            n["scroll"] = False
        return n

    @staticmethod
    def columns(*children):
        """Children side by side; give one a width to fix it."""
        return _node("columns", children)

    @staticmethod
    def scroll(*children, width=None, style=None):
        """A scrolling area; style "sidebar" draws it as the light apps'
        sidebar, "panel" as a panel at the right (give it a width, in
        columns)."""
        return _node("scroll", children, width=width, style=style)

    @staticmethod
    def box(*children, horizontal=False, spacing=None, width=None):
        return _node("box", children, horizontal=horizontal, spacing=spacing, width=width)

    @staticmethod
    def section(title, *children, meta=None):
        return _node("section", children, title=title, meta=meta)

    @staticmethod
    def row(title, sub=None, id=None, icon=None, selected=False, end=()):
        """A title over a dim line; with an id, a click on it is an event;
        end are widgets at its end (buttons, a switch)."""
        return _node("row", list(end), title=title, sub=sub, id=id, icon=icon, selected=selected)

    @staticmethod
    def text(text, style=None):
        """style: heading, title, sub, mono, hero (a big number), readout
        (a small mono value) or kicker (a small capital label)."""
        return _node("text", None, text=text, style=style)

    @staticmethod
    def facts(items):
        """Names and values: [("Host", "example.com"), …]."""
        return _node("facts", None, items=[[str(k), str(v)] for k, v in items])

    @staticmethod
    def button(id, label=None, icon=None, style=None, tooltip=None):
        """style: primary, danger or plain."""
        return _node("button", None, id=id, label=label, icon=icon, style=style, sub=tooltip)

    @staticmethod
    def entry(id, placeholder=None, value=None, password=False):
        return _node("entry", None, id=id, placeholder=placeholder, value=value, password=password)

    @staticmethod
    def search(id, placeholder=None, value=None):
        return _node("search", None, id=id, placeholder=placeholder, value=value)

    @staticmethod
    def textarea(id, value=None, placeholder=None):
        return _node("textarea", None, id=id, value=value, placeholder=placeholder)

    @staticmethod
    def switch(id, value=False):
        return {"type": "switch", "id": id, "value": bool(value)}

    @staticmethod
    def dropdown(id, options, value=None):
        return _node("dropdown", None, id=id, options=list(options), value=value)

    @staticmethod
    def bar(*children):
        """The actions at the bottom of the page."""
        return _node("bar", children)

    @staticmethod
    def empty(title, text=None, icon=None):
        return _node("empty", None, title=title, text=text, icon=icon)

    @staticmethod
    def tabs(id, *tabs, value=None):
        """Tabs above their pages; value is the shown tab's id (the first
        when none). Choosing one is a change event with its id."""
        return _node("tabs", tabs, id=id, value=value)

    @staticmethod
    def tab(id, title, *children):
        """A tab of ui.tabs: its title and its page's content."""
        return _node("tab", children, id=id, title=title)

    @staticmethod
    def segmented(id, options, value=None):
        """A choice of a few options as joined buttons; a change event
        with the option."""
        return _node("segmented", None, id=id, options=list(options), value=value)

    @staticmethod
    def slider(id, min=0, max=100, step=1, value=None):
        """A number from min to max; change events as it moves."""
        return _node("slider", None, id=id, min=min, max=max, step=step, value=min if value is None else value)

    @staticmethod
    def progress(value, text=None):
        """How far something is, from 0 to 1."""
        return _node("progress", None, value=float(value), text=text)

    @staticmethod
    def spinner(text=None):
        """Something is going on."""
        return _node("spinner", None, text=text)

    @staticmethod
    def chart(points, height=80, min=None, max=None):
        """A line over a soft fill; min and max fix its range, else it
        follows the points."""
        return _node("chart", None, points=[float(p) for p in points], height=height, min=min, max=max)

    @staticmethod
    def tiles(*tiles):
        """Tiles that wrap, as the control center's."""
        return _node("tiles", tiles)

    @staticmethod
    def tile(id, title, sub=None, icon=None, selected=False):
        """A tile of ui.tiles; a click is an event with its id."""
        return _node("tile", None, id=id, title=title, sub=sub, icon=icon, selected=selected)

    @staticmethod
    def image(src=None, icon=None, width=None, height=None, style=None):
        """An image file, or an icon by name; style "app" shows an icon as the dock does."""
        return _node("image", None, src=src, icon=icon, width=width, height=height, style=style)


app = App()
