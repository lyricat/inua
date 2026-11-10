// inua workshop: write an inua light app in JavaScript (Node or Bun).
//
// inua starts the app with this file's folder on NODE_PATH, so
// require("inua") finds it; ES modules import it by its path:
//   const { app, ui } = await import(process.env.INUA_WORKSHOP_LIB + "/node/inua.mjs");
//
//   const { app, ui } = require("inua");
//   app.view(state => ui.page(ui.entry("name", { placeholder: "Your name" }),
//                             ui.text("Hello, " + (state.name || "world"))));
//   app.on("add", async ev => { await app.call("notify", { title: "Hi" }); });
//   app.run();
//
// After each event the view is drawn again. The protocol underneath is
// JSON-RPC 2.0, a message a line, on stdin and stdout; log to stderr
// (console.error). See docs/workshop.md in inua.
"use strict";

const readline = require("node:readline");

const PROTOCOL = 1;

class InuaError extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}

class App {
  constructor() {
    this.state = {}; // every input's value by id, as the last event had it
    this.info = {}; // what initialize said: language, theme, app, dir
    this._view = null;
    this._init = [];
    this._handlers = []; // [id, type, fn]
    this._watch = []; // topics to follow from the start
    this._next = 0;
    this._waiting = new Map(); // id → {resolve, reject}
    this._queue = []; // messages to handle, one at a time
    this._busy = false;
    this.store = {
      get: async (key, def = null) => {
        const v = await this.call("store.get", { key });
        return v === null || v === undefined ? def : v;
      },
      set: (key, value) => this.call("store.set", { key, value }),
      delete: key => this.call("store.set", { key, value: null }),
    };
  }

  /** The function that returns the window's tree from the state. */
  view(fn) {
    this._view = fn;
    return this;
  }

  /** A handler for events of id (of any type, or of one: "click",
   * "change", "submit", "timer"); an id ending in * matches every id it
   * begins. The handler may be async. */
  on(id, type, fn) {
    if (typeof type === "function") [type, fn] = [null, type];
    this._handlers.push([id, type, fn]);
    return this;
  }

  /** A function to run once inua said hello, before the first view. */
  init(fn) {
    this._init.push(fn);
    return this;
  }

  /** A handler for changes on the desktop while the window is open:
   * "window" (the focused window), "clipboard" (a new copy),
   * "notification" (each new one), "system" (Wi-Fi, volume, battery…),
   * "media" (what plays). Each needs its use in the manifest; the
   * handler gets an event whose value is what changed. */
  watch(topic, fn) {
    this._handlers.push([topic, "desktop", fn]);
    if (!this._watch.includes(topic)) this._watch.push(topic);
    return this;
  }

  /** Follow topics (see watch) from now on; handle them with
   * app.on(topic, "desktop", fn). */
  subscribe(...topics) {
    return this.call("events.subscribe", { topics });
  }

  _send(msg) {
    msg.jsonrpc = "2.0";
    process.stdout.write(JSON.stringify(msg) + "\n");
  }

  /** Ask inua to do something (notify, clipboard.copy, store.get…);
   * resolves with its answer, rejects with an InuaError. */
  call(method, params = {}) {
    const id = ++this._next;
    return new Promise((resolve, reject) => {
      this._waiting.set(id, { resolve, reject });
      this._send({ id, method, params });
    });
  }

  notify(method, params = {}) {
    this._send({ method, params });
  }

  /** Set an input's value, as clearing an entry after a submit. */
  set(id, value) {
    this.state[id] = value;
    this.notify("state.set", { values: { [id]: value } });
  }

  /** A "timer" event for id every so many seconds; 0 stops it. */
  timer(id, seconds) {
    return this.call("timer.set", { id, seconds });
  }

  /** Draw the view again; done after every event anyway. */
  async render() {
    if (!this._view) return;
    let root;
    try {
      root = await this._view(this.state);
    } catch (e) {
      console.error(e);
      root = ui.page(ui.empty("Something went wrong", { text: String(e && e.message || e), icon: "dialog-warning-symbolic" }));
    }
    this.notify("render", { root });
  }

  /** Answer inua until the window closes. */
  run() {
    const rl = readline.createInterface({ input: process.stdin, terminal: false });
    rl.on("line", line => {
      line = line.trim();
      if (!line) return;
      let msg;
      try {
        msg = JSON.parse(line);
      } catch (e) {
        console.error("inua: not JSON:", line);
        return;
      }
      if (msg.method === undefined && msg.id !== undefined) { // an answer
        const w = this._waiting.get(msg.id);
        if (!w) return;
        this._waiting.delete(msg.id);
        if (msg.error) w.reject(new InuaError(msg.error.code, msg.error.message));
        else w.resolve(msg.result === undefined ? null : msg.result);
        return;
      }
      this._queue.push(msg);
      this._pump();
    });
    rl.on("close", () => process.exit(0));
  }

  async _pump() {
    if (this._busy) return;
    this._busy = true;
    while (this._queue.length) {
      const msg = this._queue.shift();
      try {
        await this._handle(msg);
      } catch (e) {
        console.error(e);
      }
    }
    this._busy = false;
  }

  async _handle(msg) {
    const p = msg.params || {};
    switch (msg.method) {
      case "initialize":
        this.info = p;
        this._send({ id: msg.id, result: { protocol: PROTOCOL } });
        if (this._watch.length) await this._safely(() => this.subscribe(...this._watch));
        for (const fn of this._init) await this._safely(fn);
        await this.render();
        return;
      case "shutdown":
        this._send({ id: msg.id, result: null });
        process.exit(0);
        return;
      case "event": {
        const ev = { type: p.type, id: p.id, value: p.value, state: p.state || {} };
        Object.assign(this.state, ev.state);
        for (const [id, type, fn] of this._handlers.slice()) {
          const hit = id === ev.id || (id.endsWith("*") && String(ev.id).startsWith(id.slice(0, -1)));
          if (hit && (!type || type === ev.type)) await this._safely(fn, ev);
        }
        await this.render();
        return;
      }
      default:
        if (msg.id !== undefined) this._send({ id: msg.id, error: { code: -32601, message: "no such method: " + msg.method } });
    }
  }

  async _safely(fn, ...args) {
    try {
      await fn(...args);
    } catch (e) {
      console.error(e);
    }
  }
}

// --- the building blocks ---

const isOpts = a => a && typeof a === "object" && !Array.isArray(a) && a.type === undefined;

function node(type, props, children) {
  const n = { type };
  for (const [k, v] of Object.entries(props || {})) {
    if (v !== undefined && v !== null && v !== false) n[k] = v;
  }
  if (children) {
    const kids = children.flat(Infinity).filter(Boolean);
    if (kids.length) n.children = kids;
  }
  return n;
}

// A container takes its options first, if any, then its children (arrays
// of children are flattened, falsy ones left out).
function container(type, args, ...known) {
  let opts = {};
  if (isOpts(args[0])) opts = args.shift();
  const props = {};
  for (const k of known) props[k] = opts[k];
  return node(type, props, args);
}

const ui = {
  /** The window's content; a bar among the children sits at the bottom.
   * ui.page({ scroll: false }, …) for columns. */
  page(...args) {
    const opts = isOpts(args[0]) ? args[0] : {};
    const n = container("page", args);
    if (opts.scroll === false) n.scroll = false;
    return n;
  },
  columns: (...args) => container("columns", args),
  /** A scrolling area: ui.scroll({ width: 200, style: "sidebar" }, …);
   * style "sidebar" is the light apps' sidebar, "panel" a panel at the right. */
  scroll: (...args) => container("scroll", args, "width", "style"),
  box: (...args) => container("box", args, "horizontal", "spacing", "width"),
  section(title, ...args) {
    const n = container("section", args, "meta");
    n.title = title;
    return n;
  },
  /** A title over a dim line; with an id, a click on it is an event; end
   * are widgets at its end (buttons, a switch). */
  row: (title, { sub, id, icon, selected, end = [] } = {}) => node("row", { title, sub, id, icon, selected }, end),
  /** style: heading, title, sub, mono, hero (a big number), readout (a
   * small mono value) or kicker (a small capital label). */
  text: (text, { style } = {}) => node("text", { text, style }),
  facts: items => node("facts", { items: items.map(([k, v]) => [String(k), String(v)]) }),
  /** style: primary, danger or plain. */
  button(id, label, opts = {}) {
    if (isOpts(label)) [label, opts] = [undefined, label];
    return node("button", { id, label, icon: opts.icon, style: opts.style, sub: opts.tooltip });
  },
  entry: (id, { placeholder, value, password } = {}) => node("entry", { id, placeholder, value, password }),
  search: (id, { placeholder, value } = {}) => node("search", { id, placeholder, value }),
  textarea: (id, { value, placeholder } = {}) => node("textarea", { id, value, placeholder }),
  switch: (id, value = false) => ({ type: "switch", id, value: Boolean(value) }),
  dropdown: (id, options, { value } = {}) => node("dropdown", { id, options: [...options], value }),
  /** The actions at the bottom of the page. */
  bar: (...children) => node("bar", {}, children),
  empty: (title, { text, icon } = {}) => node("empty", { title, text, icon }),
  /** Tabs above their pages: ui.tabs(id, { value }, ui.tab(…), …); value
   * is the shown tab's id (the first when none). Choosing one is a change
   * event with its id. */
  tabs: (id, ...args) => {
    const n = container("tabs", args, "value");
    n.id = id;
    return n;
  },
  /** A tab of ui.tabs: its title and its page's content. */
  tab: (id, title, ...children) => node("tab", { id, title }, children),
  /** A choice of a few options as joined buttons; a change event with the
   * option. */
  segmented: (id, options, { value } = {}) => node("segmented", { id, options: [...options], value }),
  /** A number from min to max; change events as it moves. */
  slider: (id, { min = 0, max = 100, step = 1, value } = {}) => ({ type: "slider", id, min, max, step, value: value ?? min }),
  /** How far something is, from 0 to 1. */
  progress: (value, { text } = {}) => node("progress", { value: Number(value), text }),
  /** Something is going on. */
  spinner: ({ text } = {}) => node("spinner", { text }),
  /** A line over a soft fill; min and max fix its range. */
  chart: (points, { height = 80, min, max } = {}) => node("chart", { points: points.map(Number), height, min, max }),
  /** Tiles that wrap, as the control center's. */
  tiles: (...tiles) => node("tiles", {}, tiles),
  /** A tile of ui.tiles; a click is an event with its id. */
  tile: (id, title, { sub, icon, selected } = {}) => node("tile", { id, title, sub, icon, selected }),
  /** An image file, or an icon by name; style "app" shows an icon as the dock does. */
  image: ({ src, icon, width, height, style } = {}) => node("image", { src, icon, width, height, style }),
};

const app = new App();

module.exports = { app, ui, InuaError, App };
