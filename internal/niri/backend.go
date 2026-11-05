package niri

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

func init() {
	compositor.Register("niri", func() (compositor.Backend, error) {
		b, err := Open()
		if err != nil {
			return nil, err
		}
		return b, nil
	})
}

// Backend is niri as a compositor.Backend.
type Backend struct {
	Client   *Client
	Debounce time.Duration // between the first event of a burst and the snapshot

	MinBackoff, MaxBackoff time.Duration

	refreshReq chan struct{}
	mu         sync.Mutex
	snap       compositor.Snapshot
	up         bool
}

// Open is the backend of the niri this process runs under;
// compositor.ErrNoCompositor without one.
func Open() (*Backend, error) {
	socket, err := SocketFromEnv()
	if err != nil {
		return nil, err
	}
	return New(NewClient(socket)), nil
}

// New is a backend on a client.
func New(c *Client) *Backend {
	return &Backend{
		Client: c, Debounce: 15 * time.Millisecond,
		MinBackoff: 250 * time.Millisecond, MaxBackoff: 5 * time.Second,
		refreshReq: make(chan struct{}, 1),
	}
}

func (b *Backend) Name() string { return "niri" }

func (b *Backend) Refresh() {
	select {
	case b.refreshReq <- struct{}{}:
	default:
	}
}

// Capabilities: what niri's IPC and protocols give, the same before and
// after connecting. Geometry is not among them: niri 26.04 places only
// floating windows (tile_pos_in_workspace_view is null for a tiled one),
// so a window has a Geometry only when it floats. Keystrokes, previews,
// binds, the key observer, the pointer, display settings and virtual
// outputs are not there either (docs/compositor-capabilities.md).
func (b *Backend) Capabilities() compositor.Capabilities {
	return compositor.Capabilities{
		compositor.CapMoveWithoutFollow: compositor.Supported,
		compositor.CapWorkspaces:        compositor.Supported, compositor.CapQuietFocus: compositor.Supported,
		compositor.CapPlacement: compositor.Supported, compositor.CapRules: compositor.Supported,
	}
}

// Run follows the event stream until ctx ends, reconnecting with backoff.
func (b *Backend) Run(ctx context.Context, h compositor.Handler) error {
	h.OnCapabilities(b.Capabilities())
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

// session is one connection: the stream's first events are the state, so
// the first snapshot comes before the connection is reported up. Outputs
// have no event of their own; they are read when the workspaces change.
func (b *Backend) session(ctx context.Context, h compositor.Handler) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	events := make(chan map[string]json.RawMessage, 256)
	up := make(chan struct{})
	errc := make(chan error, 1)
	go func() { errc <- readEvents(ctx, b.Client.Socket, func() { close(up) }, events) }()
	select {
	case <-up:
	case err := <-errc:
		return err
	}

	st := newState()
	if err := b.readOutputs(ctx, st); err != nil {
		return err
	}
	publish := func() {
		snap := st.snapshot()
		b.mu.Lock()
		changed := !reflect.DeepEqual(snap, b.snap)
		b.snap = snap
		b.mu.Unlock()
		if changed {
			h.OnSnapshot(snap)
		}
	}
	connected := false
	var pending <-chan time.Time
	for {
		select {
		case ev := <-events:
			changed := st.apply(ev)
			if _, ok := ev["WorkspacesChanged"]; ok {
				if err := b.readOutputs(ctx, st); err != nil {
					return err
				}
			}
			if !st.ready {
				continue
			}
			if !connected {
				publish()
				connected = true
				b.mu.Lock()
				b.up = true
				b.mu.Unlock()
				h.OnConnection(true, nil)
				continue
			}
			if changed && pending == nil {
				pending = time.After(b.Debounce)
			}
		case <-pending:
			pending = nil
			publish()
		case <-b.refreshReq:
			pending = nil
			if err := b.readOutputs(ctx, st); err != nil {
				return err
			}
			if st.ready {
				publish()
			}
		case err := <-errc:
			return err
		}
	}
}

func (b *Backend) readOutputs(ctx context.Context, st *state) error {
	outs, err := query[map[string]output](ctx, b.Client, "Outputs")
	if err != nil {
		return err
	}
	st.outputs = outs
	return nil
}

// Snapshot reads the state once.
func (b *Backend) Snapshot(ctx context.Context) (compositor.Snapshot, error) {
	st := newState()
	if err := b.readOutputs(ctx, st); err != nil {
		return compositor.Snapshot{}, err
	}
	wss, err := query[[]workspace](ctx, b.Client, "Workspaces")
	if err != nil {
		return compositor.Snapshot{}, err
	}
	wins, err := query[[]window](ctx, b.Client, "Windows")
	if err != nil {
		return compositor.Snapshot{}, err
	}
	st.workspaces = wss
	for _, w := range wins {
		st.windows[w.ID] = w
	}
	return st.snapshot(), nil
}

func parseID(id string) (uint64, error) {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("niri: id %q: %w", id, err)
	}
	return n, nil
}

func (b *Backend) FocusWindow(ctx context.Context, id string) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	return b.Client.Action(ctx, "FocusWindow", map[string]any{"id": n})
}

// FocusWindowQuietly is FocusWindow: niri moves the pointer only when
// warp-mouse-to-focus is set.
func (b *Backend) FocusWindowQuietly(ctx context.Context, id string) error {
	return b.FocusWindow(ctx, id)
}

func (b *Backend) CloseWindow(ctx context.Context, id string) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	return b.Client.Action(ctx, "CloseWindow", map[string]any{"id": n})
}

func (b *Backend) FocusWorkspace(ctx context.Context, id string) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	return b.Client.Action(ctx, "FocusWorkspace", map[string]any{"reference": map[string]any{"Id": n}})
}

func (b *Backend) FocusOutput(ctx context.Context, name string) error {
	return b.Client.Action(ctx, "FocusMonitor", map[string]any{"output": name})
}

func (b *Backend) MoveWindow(ctx context.Context, id, workspace string, follow bool) error {
	w, err := parseID(id)
	if err != nil {
		return err
	}
	ws, err := parseID(workspace)
	if err != nil {
		return err
	}
	return b.Client.Action(ctx, "MoveWindowToWorkspace", map[string]any{
		"window_id": w, "reference": map[string]any{"Id": ws}, "focus": follow})
}

// NewWorkspace is the output's trailing empty workspace, which niri keeps
// at the end of every output; none when it is reserved or absent.
func (b *Backend) NewWorkspace(snap compositor.Snapshot, output string, unavailable func(string) bool) (compositor.Workspace, bool) {
	occupied := map[string]bool{}
	for _, w := range snap.Windows {
		occupied[w.Workspace] = true
	}
	var best *compositor.Workspace
	for i := range snap.Workspaces {
		ws := &snap.Workspaces[i]
		if ws.Output != output || occupied[ws.ID] || unavailable != nil && unavailable(ws.ID) {
			continue
		}
		if best == nil || ws.Order > best.Order {
			best = ws
		}
	}
	if best == nil {
		return compositor.Workspace{}, false
	}
	return *best, true
}

func (b *Backend) MoveWorkspace(ctx context.Context, id, output string) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	return b.Client.Action(ctx, "MoveWorkspaceToMonitor", map[string]any{"output": output, "reference": map[string]any{"Id": n}})
}

// windowOf is process pid's window in the state now.
func (b *Backend) windowOf(ctx context.Context, pid int) (compositor.Window, compositor.Snapshot, error) {
	snap, err := b.Snapshot(ctx)
	if err != nil {
		return compositor.Window{}, snap, err
	}
	for _, w := range snap.Windows {
		if w.PID == pid {
			return w, snap, nil
		}
	}
	return compositor.Window{}, snap, fmt.Errorf("no window of process %d", pid)
}

// BringHere moves process pid's window to the focused workspace and
// focuses it.
func (b *Backend) BringHere(ctx context.Context, pid int) error {
	w, snap, err := b.windowOf(ctx, pid)
	if err != nil {
		return err
	}
	focused := ""
	for _, ws := range snap.Workspaces {
		if ws.Focused {
			focused = ws.ID
		}
	}
	if focused != "" && w.Workspace != focused {
		return b.MoveWindow(ctx, w.ID, focused, true)
	}
	return b.FocusWindow(ctx, w.ID)
}

func (b *Backend) WindowOf(ctx context.Context, pid int) (compositor.Rect, error) {
	w, _, err := b.windowOf(ctx, pid)
	if err != nil {
		return compositor.Rect{}, err
	}
	if w.Geometry == nil {
		return compositor.Rect{}, errors.New("niri: the window is not in view")
	}
	return *w.Geometry, nil
}

// PlaceWindow and RoomRight need a floating window's absolute place and
// the reserved space of an output, which niri does not give.
func (b *Backend) PlaceWindow(context.Context, int, compositor.Rect) error {
	return compositor.ErrUnsupported
}
func (b *Backend) RoomRight(context.Context, int) (compositor.Rect, int, error) {
	return compositor.Rect{}, 0, compositor.ErrUnsupported
}

// Tile puts a floating window back in the layout.
func (b *Backend) Tile(ctx context.Context, pid int) (bool, error) {
	w, _, err := b.windowOf(ctx, pid)
	if err != nil || !w.Floating {
		return false, err
	}
	n, _ := parseID(w.ID)
	return true, b.Client.Action(ctx, "ToggleWindowFloating", map[string]any{"id": n})
}
