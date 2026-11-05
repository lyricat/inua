package wayfire

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

func init() {
	compositor.Register("wayfire", func() (compositor.Backend, error) {
		b, err := Open()
		if err != nil {
			return nil, err
		}
		return b, nil
	})
}

// Backend is Wayfire as a compositor.Backend. The state is read again
// after each event, bursts merged, as the Hyprland store does.
type Backend struct {
	Client   *Client
	Debounce time.Duration

	MinBackoff, MaxBackoff time.Duration

	refreshReq chan struct{}
	mu         sync.Mutex
	st         state
	snap       compositor.Snapshot
	methods    map[string]bool // nil until probed
	up         bool
}

// Open is the backend of the Wayfire this process runs under;
// compositor.ErrNoCompositor without one (or with its ipc plugin off,
// which leaves WAYFIRE_SOCKET unset).
func Open() (*Backend, error) {
	socket, err := SocketFromEnv()
	if err != nil {
		return nil, err
	}
	return New(NewClient(socket)), nil
}

func New(c *Client) *Backend {
	return &Backend{
		Client: c, Debounce: 15 * time.Millisecond,
		MinBackoff: 250 * time.Millisecond, MaxBackoff: 5 * time.Second,
		refreshReq: make(chan struct{}, 1),
	}
}

func (b *Backend) Name() string { return "wayfire" }

func (b *Backend) Refresh() {
	select {
	case b.refreshReq <- struct{}{}:
	default:
	}
}

// has reports whether a method's plugin is loaded, as last probed.
func (b *Backend) has(method string) bool { return b.loaded()[method] }

// loaded is the methods of the loaded plugins as last probed, probing now
// when nothing has yet (a one-shot command, a light app placing its
// window); nil while Wayfire cannot be asked.
func (b *Backend) loaded() map[string]bool {
	b.mu.Lock()
	m := b.methods
	b.mu.Unlock()
	if m != nil {
		return m
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.Client.Timeout)
	defer cancel()
	probed, err := b.Client.Methods(ctx)
	if err != nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.methods == nil {
		b.methods = probed
	}
	return b.methods
}

// Capabilities follow the plugins: a capability whose methods are
// missing is Unavailable (the plugin can be loaded), as is everything
// while Wayfire cannot be asked. Wayfire has no previews, keystrokes
// to a window, key observer, modes or pointer moves for a shell; the
// binds, displays and virtual output it has by IPC are not wired yet.
func (b *Backend) Capabilities() compositor.Capabilities {
	m := b.loaded()
	state := func(methods ...string) compositor.State {
		for _, name := range methods {
			if !m[name] {
				return compositor.Unavailable
			}
		}
		return compositor.Supported
	}
	return compositor.Capabilities{
		compositor.CapGeometry:          state("window-rules/list-views"),
		compositor.CapQuietFocus:        state("window-rules/focus-view"),
		compositor.CapMoveWithoutFollow: state("vswitch/send-view"),
		compositor.CapWorkspaces:        state("vswitch/set-workspace", "vswitch/send-view"),
		compositor.CapPlacement:         state("window-rules/configure-view", "vswitch/send-view"),
		compositor.CapOnDemandFocus:     state("window-rules/list-views"),
		compositor.CapKeyboard:          state("wayfire/set-config-options"),
		compositor.CapTouchpad:          state("wayfire/set-config-options"),
		compositor.CapBinds:             state("command/register-binding"),
		compositor.CapRules:             compositor.Supported,
	}
}

// Run follows Wayfire until ctx ends, reconnecting with backoff.
func (b *Backend) Run(ctx context.Context, h compositor.Handler) error {
	backoff := b.MinBackoff
	for {
		start := time.Now()
		err := b.session(ctx, h)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		b.mu.Lock()
		b.up = false
		b.mu.Unlock()
		h.OnConnection(false, err)
		if time.Since(start) > b.MaxBackoff {
			backoff = b.MinBackoff
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
		backoff = min(backoff*2, b.MaxBackoff)
	}
}

// probe reads the loaded plugins' methods and reports the capabilities
// when they changed.
func (b *Backend) probe(ctx context.Context, h compositor.Handler) error {
	m, err := b.Client.Methods(ctx)
	if err != nil {
		return err
	}
	b.mu.Lock()
	changed := !reflect.DeepEqual(m, b.methods)
	b.methods = m
	b.mu.Unlock()
	if changed {
		h.OnCapabilities(b.Capabilities())
	}
	return nil
}

func (b *Backend) session(ctx context.Context, h compositor.Handler) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The events first, then the state: a change racing the read is an
	// event, and causes another read.
	events := make(chan string, 256)
	up := make(chan struct{})
	errc := make(chan error, 1)
	go func() { errc <- watch(ctx, b.Client.Socket, func() { close(up) }, events) }()
	select {
	case <-up:
	case err := <-errc:
		return err
	}
	if err := b.probe(ctx, h); err != nil {
		return err
	}
	if err := b.refresh(ctx, h); err != nil {
		return err
	}
	b.mu.Lock()
	b.up = true
	b.mu.Unlock()
	h.OnConnection(true, nil)

	var pending <-chan time.Time
	for {
		select {
		case ev := <-events:
			if ev == "plugin-activation-state-changed" || ev == "keyboard-modifier-state-changed" {
				continue // an animation's start and end, a key: no state of ours
			}
			if pending == nil {
				pending = time.After(b.Debounce)
			}
		case <-pending:
			pending = nil
			if err := b.refresh(ctx, h); err != nil {
				return err
			}
		case <-b.refreshReq:
			pending = nil
			if err := b.probe(ctx, h); err != nil {
				return err
			}
			if err := b.refresh(ctx, h); err != nil {
				return err
			}
		case err := <-errc:
			return err
		}
	}
}

func (b *Backend) refresh(ctx context.Context, h compositor.Handler) error {
	st, err := b.Client.read(ctx)
	if err != nil {
		return err
	}
	snap := st.snapshot()
	b.mu.Lock()
	changed := !reflect.DeepEqual(snap, b.snap)
	b.st, b.snap = st, snap
	b.mu.Unlock()
	if changed {
		h.OnSnapshot(snap)
	}
	return nil
}

// Snapshot reads the state once.
func (b *Backend) Snapshot(ctx context.Context) (compositor.Snapshot, error) {
	st, err := b.current(ctx)
	if err != nil {
		return compositor.Snapshot{}, err
	}
	return st.snapshot(), nil
}

// current is the state now: an action needs the cell an output shows and
// where a view is as they are, not as the last event left them.
func (b *Backend) current(ctx context.Context) (state, error) {
	return b.Client.read(ctx)
}

func viewID(id string) (int, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return 0, fmt.Errorf("wayfire: view id %q: %w", id, err)
	}
	return n, nil
}

func (b *Backend) call(ctx context.Context, method string, data map[string]any) error {
	_, err := b.Client.Call(ctx, method, data)
	return err
}

// FocusWindow focuses a view; Wayfire shows its cell when it is on
// another one.
func (b *Backend) FocusWindow(ctx context.Context, id string) error {
	n, err := viewID(id)
	if err != nil {
		return err
	}
	return b.call(ctx, "window-rules/focus-view", map[string]any{"id": n})
}

// FocusWindowQuietly is FocusWindow: focus-view leaves the pointer alone.
func (b *Backend) FocusWindowQuietly(ctx context.Context, id string) error {
	return b.FocusWindow(ctx, id)
}

func (b *Backend) CloseWindow(ctx context.Context, id string) error {
	n, err := viewID(id)
	if err != nil {
		return err
	}
	return b.call(ctx, "window-rules/close-view", map[string]any{"id": n})
}

// outputOf is the output that shows a workspace set.
func (st state) outputOf(wset int) (output, bool) {
	for _, o := range st.outputs {
		if o.WSet == wset {
			return o, true
		}
	}
	return output{}, false
}

func (b *Backend) FocusWorkspace(ctx context.Context, id string) error {
	wset, x, y, err := parseWorkspace(id)
	if err != nil {
		return err
	}
	st, err := b.current(ctx)
	if err != nil {
		return err
	}
	o, ok := st.outputOf(wset)
	if !ok {
		return fmt.Errorf("wayfire: no output shows workspace set %d", wset)
	}
	return b.call(ctx, "vswitch/set-workspace", map[string]any{"x": x, "y": y, "output-id": o.ID})
}

// MoveWindow sends a view to a cell (vswitch/send-view does not follow);
// with follow the output goes there too and the view is focused. A cell
// of another output's set takes the view to that set first.
func (b *Backend) MoveWindow(ctx context.Context, id, workspace string, follow bool) error {
	n, err := viewID(id)
	if err != nil {
		return err
	}
	wset, x, y, err := parseWorkspace(workspace)
	if err != nil {
		return err
	}
	st, err := b.current(ctx)
	if err != nil {
		return err
	}
	for _, v := range st.views {
		if v.ID == n && v.WSet != wset {
			if !b.has("wsets/send-view-to-wset") {
				return fmt.Errorf("%w: moving a window to another output needs Wayfire's wsets plugin", compositor.ErrUnavailable)
			}
			if err := b.call(ctx, "wsets/send-view-to-wset", map[string]any{"view-id": n, "wset-index": wset}); err != nil {
				return err
			}
		}
	}
	if err := b.call(ctx, "vswitch/send-view", map[string]any{"view-id": n, "x": x, "y": y}); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	return b.FocusWindow(ctx, id)
}

// NewWorkspace is a cell of the output's grid with no window on it: the
// grid is fixed, so "new" is the first empty cell not shown and not
// taken.
func (b *Backend) NewWorkspace(snap compositor.Snapshot, output string, unavailable func(string) bool) (compositor.Workspace, bool) {
	occupied := map[string]bool{}
	for _, w := range snap.Windows {
		occupied[w.Workspace] = true
	}
	for _, ws := range snap.Workspaces { // by output, then order
		if ws.Output != output || ws.Active || occupied[ws.ID] || unavailable != nil && unavailable(ws.ID) {
			continue
		}
		return ws, true
	}
	return compositor.Workspace{}, false
}

// MoveWorkspace: a cell belongs to its output's set and cannot be taken
// to another output; one already there needs nothing.
func (b *Backend) MoveWorkspace(ctx context.Context, id, output string) error {
	wset, _, _, err := parseWorkspace(id)
	if err != nil {
		return err
	}
	st, err := b.current(ctx)
	if err != nil {
		return err
	}
	if o, ok := st.outputOf(wset); ok && o.Name == output {
		return nil
	}
	return compositor.ErrUnsupported
}

// viewOf is process pid's window and its output.
func (st state) viewOf(pid int) (view, output, error) {
	for _, v := range st.views {
		if v.PID != pid || !isWindow(v) {
			continue
		}
		for _, o := range st.outputs {
			if o.ID == v.OutputID {
				return v, o, nil
			}
		}
	}
	return view{}, output{}, fmt.Errorf("no window of process %d", pid)
}

// BringHere sends process pid's window to the cell its output shows and
// focuses it.
func (b *Backend) BringHere(ctx context.Context, pid int) error {
	st, err := b.current(ctx)
	if err != nil {
		return err
	}
	v, o, err := st.viewOf(pid)
	if err != nil {
		return err
	}
	if x, y := cellOf(v, o); x != o.Workspace.X || y != o.Workspace.Y {
		if err := b.call(ctx, "vswitch/send-view", map[string]any{"view-id": v.ID, "x": o.Workspace.X, "y": o.Workspace.Y}); err != nil {
			return err
		}
	}
	return b.call(ctx, "window-rules/focus-view", map[string]any{"id": v.ID})
}

func (b *Backend) WindowOf(ctx context.Context, pid int) (compositor.Rect, error) {
	st, err := b.current(ctx)
	if err != nil {
		return compositor.Rect{}, err
	}
	v, o, err := st.viewOf(pid)
	if err != nil {
		return compositor.Rect{}, err
	}
	return compositor.Rect{X: int(o.Geometry.X + v.Geometry.X), Y: int(o.Geometry.Y + v.Geometry.Y), W: int(v.Geometry.Width), H: int(v.Geometry.Height)}, nil
}

func (b *Backend) PlaceWindow(ctx context.Context, pid int, r compositor.Rect) error {
	st, err := b.current(ctx)
	if err != nil {
		return err
	}
	v, o, err := st.viewOf(pid)
	if err != nil {
		return err
	}
	return b.call(ctx, "window-rules/configure-view", map[string]any{"id": v.ID, "geometry": map[string]any{
		"x": r.X - int(o.Geometry.X), "y": r.Y - int(o.Geometry.Y), "width": r.W, "height": r.H}})
}

// RoomRight is the window and how far it may grow to the right within
// its output's work area; Wayfire keeps no gap between windows.
func (b *Backend) RoomRight(ctx context.Context, pid int) (compositor.Rect, int, error) {
	st, err := b.current(ctx)
	if err != nil {
		return compositor.Rect{}, 0, err
	}
	v, o, err := st.viewOf(pid)
	if err != nil {
		return compositor.Rect{}, 0, err
	}
	r := compositor.Rect{X: int(o.Geometry.X + v.Geometry.X), Y: int(o.Geometry.Y + v.Geometry.Y), W: int(v.Geometry.Width), H: int(v.Geometry.Height)}
	room := int(o.Workarea.X+o.Workarea.Width) - int(v.Geometry.X+v.Geometry.Width)
	return r, max(room, 0), nil
}

// Tile: Wayfire's windows float unless a tiling plugin holds them; there
// is nothing to undo.
func (b *Backend) Tile(context.Context, int) (bool, error) { return false, nil }
