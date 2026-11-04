package hypr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/lyricat/inua/internal/compositor"
)

func init() {
	compositor.Register("hyprland", func() (compositor.Backend, error) {
		b, err := Open()
		if err != nil {
			return nil, err
		}
		return b, nil
	})
}

// Backend is Hyprland as a compositor.Backend: the store's snapshots, the
// dispatchers behind the contract's actions, in Lua on Hyprland 0.55 and
// later (probed at each connection), classic before. Client stays
// reachable for what only Hyprland has.
type Backend struct {
	Client *Client
	// FreshWorkspace is where NewWorkspace numbers from when no empty
	// workspace can be recycled: 1, or 11 while the number keys go to
	// workspaces 1 to 10 directly.
	FreshWorkspace int
	// OnEvent gets every raw event, for the shell's Hyprland-only parts
	// (the overview's custom events, screencast, config reloads).
	OnEvent func(Event)

	store *Store
	mu    sync.Mutex
	lua   bool
	up    bool
}

// Open is the backend of the Hyprland this process runs under;
// compositor.ErrNoCompositor without one.
func Open() (*Backend, error) {
	paths, err := PathsFromEnv()
	if err != nil {
		return nil, compositor.ErrNoCompositor
	}
	return New(NewClient(paths)), nil
}

// New is a backend on a client, for tests and tools that found the sockets
// themselves.
func New(c *Client) *Backend {
	return &Backend{Client: c, FreshWorkspace: 1}
}

func (b *Backend) Name() string { return "hyprland" }

// Lua reports whether Hyprland runs Lua (0.55 and later), known once
// connected: the overview needs it, and the actions use its dispatchers.
func (b *Backend) Lua() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lua
}

// Run keeps the store running until ctx ends. Capabilities are probed and
// reported at each connection, before the connection is.
func (b *Backend) Run(ctx context.Context, h compositor.Handler) error {
	b.mu.Lock()
	if b.store == nil {
		b.store = NewStore(b.Client, backendHandler{b, h})
	}
	st := b.store
	b.mu.Unlock()
	return st.Run(ctx)
}

func (b *Backend) Refresh() {
	b.mu.Lock()
	st := b.store
	b.mu.Unlock()
	if st != nil {
		st.Refresh()
	}
}

type backendHandler struct {
	b *Backend
	h compositor.Handler
}

func (bh backendHandler) OnSnapshot(s compositor.Snapshot) { bh.h.OnSnapshot(s) }
func (bh backendHandler) OnEvent(e Event) {
	if bh.b.OnEvent != nil {
		bh.b.OnEvent(e)
	}
}
func (bh backendHandler) OnConnection(up bool, err error) {
	b := bh.b
	lua := false
	if up {
		lua = b.probeLua()
	}
	b.mu.Lock()
	changed := b.lua != lua || b.up != up
	b.lua, b.up = lua, up
	b.mu.Unlock()
	if changed {
		bh.h.OnCapabilities(b.Capabilities())
	}
	bh.h.OnConnection(up, err)
}

// probeLua asks whether Hyprland runs Lua: 0.53 answers "unknown request"
// to eval.
func (b *Backend) probeLua() bool {
	ctx, cancel := context.WithTimeout(context.Background(), b.Client.Timeout)
	defer cancel()
	reply, err := b.Client.Request(ctx, "eval _G.inuaOverviewProbe=true")
	ok := err == nil && strings.TrimSpace(string(reply)) == "ok"
	if ok != b.Lua() {
		log.Printf("hyprland: runs Lua: %v (the overview is on with it)", ok)
	}
	return ok
}

// Capabilities: everything, the key observer and previews with Lua. Before
// the first connection the Lua-dependent ones are Unavailable.
func (b *Backend) Capabilities() compositor.Capabilities {
	b.mu.Lock()
	lua, up := b.lua, b.up
	b.mu.Unlock()
	withLua := compositor.Unavailable
	switch {
	case lua:
		withLua = compositor.Supported
	case up:
		withLua = compositor.Unsupported
	}
	return compositor.Capabilities{
		compositor.CapGeometry: compositor.Supported, compositor.CapMoveWithoutFollow: compositor.Supported,
		compositor.CapWorkspaces: compositor.Supported, compositor.CapQuietFocus: compositor.Supported,
		compositor.CapPreviews: compositor.Supported, compositor.CapKeystrokes: compositor.Supported,
		compositor.CapBinds: compositor.Supported, compositor.CapKeyObserver: withLua,
		compositor.CapDisplays: compositor.Supported, compositor.CapVirtualOutput: compositor.Supported,
		compositor.CapPointer: compositor.Supported, compositor.CapPlacement: compositor.Supported,
		compositor.CapModes: compositor.Supported, compositor.CapRules: compositor.Supported,
	}
}

// Dispatch runs the Lua form where Hyprland runs Lua and lua is given,
// else the classic one.
func (b *Backend) Dispatch(ctx context.Context, classic, lua string) error {
	if b.Lua() && lua != "" {
		return b.Client.LuaDispatch(ctx, lua)
	}
	return b.Client.Dispatch(ctx, classic)
}

func window(id string) string { return "address:" + id }

// luaWorkspace is a workspace as a Lua dispatcher takes it: a number for
// a numeric id (a quoted number would name a new workspace), else a name.
func luaWorkspace(id string) string {
	if _, err := strconv.Atoi(id); err == nil {
		return id
	}
	return strconv.Quote(id)
}

func (b *Backend) FocusWindow(ctx context.Context, id string) error {
	return b.Dispatch(ctx, "focuswindow "+window(id), fmt.Sprintf(`hl.dsp.focus({window=%q})`, window(id)))
}

func (b *Backend) CloseWindow(ctx context.Context, id string) error {
	return b.Dispatch(ctx, "closewindow "+window(id), fmt.Sprintf(`hl.dsp.window.close({window=%q})`, window(id)))
}

// KillWindow ends a window's client at once, without asking it: the
// overview's kill mode.
func (b *Backend) KillWindow(ctx context.Context, id string) error {
	return b.Dispatch(ctx, "killwindow "+window(id), fmt.Sprintf(`hl.dsp.window.kill({window=%q})`, window(id)))
}

func (b *Backend) FocusWorkspace(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("hyprland: no workspace")
	}
	return b.Dispatch(ctx, "workspace "+id, "hl.dsp.focus({workspace="+luaWorkspace(id)+"})")
}

func (b *Backend) FocusOutput(ctx context.Context, name string) error {
	return b.Dispatch(ctx, "focusmonitor "+name, fmt.Sprintf(`hl.dsp.focus({monitor=%q})`, name))
}

func (b *Backend) MoveWindow(ctx context.Context, id, workspace string, follow bool) error {
	if workspace == "" {
		return errors.New("hyprland: no workspace")
	}
	classic := "movetoworkspace " + workspace + "," + window(id)
	lua := fmt.Sprintf(`hl.dsp.window.move({window=%q,workspace=%s})`, window(id), luaWorkspace(workspace))
	if !follow {
		classic = "movetoworkspacesilent " + workspace + "," + window(id)
		lua = fmt.Sprintf(`hl.dsp.window.move({window=%q,workspace=%s,follow=false})`, window(id), luaWorkspace(workspace))
	}
	return b.Dispatch(ctx, classic, lua)
}

// NewWorkspace is the number a window dropped on New gets: an empty
// workspace recycled, else a fresh number (compositor.NextNumbered).
func (b *Backend) NewWorkspace(snap compositor.Snapshot, output string, unavailable func(string) bool) (compositor.Workspace, bool) {
	return compositor.NextNumbered(snap, output, unavailable, b.FreshWorkspace)
}

func (b *Backend) MoveWorkspace(ctx context.Context, id, output string) error {
	return b.Dispatch(ctx, "moveworkspacetomonitor "+id+" "+output,
		fmt.Sprintf(`hl.dsp.workspace.move({workspace=%q,monitor=%q})`, id, output))
}

// FocusWindowQuietly focuses the window with cursor:no_warps turned on for
// the focus only, and back as it was: in one Lua script where Hyprland
// runs Lua, else in one batch of classic commands.
func (b *Backend) FocusWindowQuietly(ctx context.Context, id string) error {
	c := b.Client
	if b.Lua() {
		script := fmt.Sprintf(`local noWarps = hl.get_config("cursor.no_warps"); hl.config({cursor={no_warps=true}}); local ok, err = pcall(function() hl.dispatch(hl.dsp.focus({window=%q})) end); hl.config({cursor={no_warps=noWarps}}); if not ok then error(err) end`, window(id))
		reply, err := c.Request(ctx, "eval "+script)
		if err == nil && strings.TrimSpace(string(reply)) != "ok" {
			err = fmt.Errorf("eval: %s", strings.TrimSpace(string(reply)))
		}
		return err
	}
	focus := "focuswindow " + window(id)
	reply, err := c.Request(ctx, "j/getoption cursor:no_warps")
	if err == nil && strings.Contains(string(reply), `"int": 0`) {
		// One batch, so the setting is back before anything else runs.
		_, err = c.Request(ctx, "[[BATCH]]keyword cursor:no_warps 1;dispatch "+focus+";keyword cursor:no_warps 0")
		return err
	}
	return c.Dispatch(ctx, focus)
}

// SendKey focuses the window quietly and sends it the shortcut, by
// address so a window under the pointer cannot take it.
func (b *Backend) SendKey(ctx context.Context, id, mods, key string) error {
	if err := b.FocusWindowQuietly(ctx, id); err != nil {
		return err
	}
	return b.Dispatch(ctx, fmt.Sprintf("sendshortcut %s, %s, %s", mods, key, window(id)),
		fmt.Sprintf(`hl.dsp.send_shortcut({ mods = %q, key = %q, window = %q })`, mods, key, window(id)))
}

func (b *Backend) CursorPos(ctx context.Context) (int, int, error) {
	reply, err := b.Client.Request(ctx, "j/cursorpos")
	if err != nil {
		return 0, 0, err
	}
	var p struct{ X, Y float64 }
	if err := jsonUnmarshal(reply, &p); err != nil {
		return 0, 0, err
	}
	return int(p.X), int(p.Y), nil
}

func (b *Backend) MoveCursor(ctx context.Context, x, y int) error {
	return b.Dispatch(ctx, fmt.Sprintf("movecursor %d %d", x, y), fmt.Sprintf("hl.dsp.cursor.move({ x = %d, y = %d })", x, y))
}

// EnterMode enters a submap; "" leaves it (reset).
func (b *Backend) EnterMode(ctx context.Context, name string) error {
	if name == "" {
		name = "reset"
	}
	return b.Dispatch(ctx, "submap "+name, fmt.Sprintf("hl.dsp.submap(%q)", name))
}

func (b *Backend) BringHere(ctx context.Context, pid int) error { return b.Client.BringHere(ctx, pid) }
func (b *Backend) WindowOf(ctx context.Context, pid int) (compositor.Rect, error) {
	return b.Client.WindowOf(ctx, pid)
}
func (b *Backend) PlaceWindow(ctx context.Context, pid int, r compositor.Rect) error {
	return b.Client.PlaceWindow(ctx, pid, r)
}
func (b *Backend) RoomRight(ctx context.Context, pid int) (compositor.Rect, int, error) {
	return b.Client.RoomRight(ctx, pid)
}
func (b *Backend) Tile(ctx context.Context, pid int) (bool, error) { return b.Client.Tile(ctx, pid) }
func (b *Backend) SyncVirtualScreen(ctx context.Context, mode string) (bool, error) {
	return b.Client.SyncVirtualScreen(ctx, mode)
}

// Snapshot reads the state once.
func (b *Backend) Snapshot(ctx context.Context) (compositor.Snapshot, error) {
	q, err := b.Client.Query(ctx)
	if err != nil {
		return compositor.Snapshot{}, err
	}
	return BuildSnapshot(q, nil)
}

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
