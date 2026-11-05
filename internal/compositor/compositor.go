// Package compositor is the contract between the shell and the compositor
// it runs on: the state the shell reads (outputs, workspaces, windows), the
// few actions every backend has, the capabilities a backend may add and
// the state each is in. Backends (internal/hypr, …) implement Backend and
// register themselves; nothing here imports a backend, and nothing here
// imports GTK. docs/compositor-contracts.md is the design.
package compositor

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
)

// Rect is a window's place in global logical pixels.
type Rect struct{ X, Y, W, H int }

// Output is a monitor as the compositor names and places it. Name equals
// the connector name GDK reports for the same monitor.
type Output struct {
	Name        string
	Description string
	X, Y        int // logical position in the global space
	Width       int // logical size after scale and transform
	Height      int
	Scale       float64
	Transform   int     // wl_output transform; odd values turn the mode's size
	RefreshRate float64 // Hz, 0 unknown
	Focused     bool
	// ActiveWorkspace is the workspace shown on it; SpecialWorkspace the
	// one shown over it (Hyprland's special workspaces), "" elsewhere.
	ActiveWorkspace, SpecialWorkspace string
}

// Workspace is one the user can go to. ID is the backend's and opaque;
// Order sorts an output's workspaces and is stable while nothing moves.
type Workspace struct {
	ID            string
	Name          string
	Output        string
	Order         int
	Active        bool // shown on its output
	Focused       bool // active on the focused output
	Urgent        bool
	HasFullscreen bool
}

// Window is a mapped toplevel. ID is the backend's and opaque. FocusRank
// orders windows by when they were focused: 1 the most recent, larger
// older, 0 unknown. Geometry is nil when the backend has none
// (CapGeometry).
type Window struct {
	ID        string
	PID       int
	Title     string
	AppID     string // Hyprland's class, niri's app_id
	Workspace string // workspace id, "" when unknown
	Output    string
	Focused   bool
	Urgent    bool
	Floating  bool
	FocusRank int
	Geometry  *Rect
}

// Snapshot is a consistent view of the compositor's state. Outputs are by
// name, workspaces by output then Order, windows in the backend's order.
type Snapshot struct {
	Outputs       []Output
	Workspaces    []Workspace
	Windows       []Window
	FocusedWindow string // window id, "" if none
	FocusedOutput string // output name, "" if none
}

// Output finds an output by name.
func (s Snapshot) Output(name string) (Output, bool) {
	i := slices.IndexFunc(s.Outputs, func(o Output) bool { return o.Name == name })
	if i < 0 {
		return Output{}, false
	}
	return s.Outputs[i], true
}

// Workspace finds a workspace by id.
func (s Snapshot) Workspace(id string) (Workspace, bool) {
	i := slices.IndexFunc(s.Workspaces, func(w Workspace) bool { return w.ID == id })
	if i < 0 {
		return Workspace{}, false
	}
	return s.Workspaces[i], true
}

// Window finds a window by id.
func (s Snapshot) Window(id string) (Window, bool) {
	i := slices.IndexFunc(s.Windows, func(w Window) bool { return w.ID == id })
	if i < 0 {
		return Window{}, false
	}
	return s.Windows[i], true
}

// ActiveWorkspace is the workspace shown on an output, "" when the output
// is unknown.
func (s Snapshot) ActiveWorkspace(output string) string {
	o, _ := s.Output(output)
	return o.ActiveWorkspace
}

// Handler receives a backend's output. Calls come from the backend's
// goroutine, one at a time; implementations must not block for long.
type Handler interface {
	// OnSnapshot is called with the state when it differs from the last.
	OnSnapshot(Snapshot)
	// OnConnection reports the control channel going up (err == nil) or
	// down. The first snapshot of a session comes before up.
	OnConnection(connected bool, err error)
	// OnCapabilities is called when a probe changes a capability's state.
	OnCapabilities(Capabilities)
}

// Backend is what every compositor backend has: the state, delivered as
// snapshots, and the actions the baseline needs. Errors are the
// compositor's, wrapped; nil means the request was accepted, and the
// resulting state comes as a snapshot.
type Backend interface {
	// Name is the compositor's: "hyprland", "niri", "wayfire".
	Name() string
	// Run connects, delivers the full state, follows changes and
	// reconnects with backoff until ctx ends.
	Run(ctx context.Context, h Handler) error
	// Refresh asks for a re-read of the state.
	Refresh()
	Capabilities() Capabilities

	FocusWindow(ctx context.Context, id string) error
	CloseWindow(ctx context.Context, id string) error
	FocusWorkspace(ctx context.Context, id string) error
	// MoveWindow puts a window on a workspace; follow moves the focus with
	// it. follow=false is ErrUnsupported without CapMoveWithoutFollow.
	MoveWindow(ctx context.Context, id, workspace string, follow bool) error
}

// Capability names an optional part of a backend; State says whether it
// is there.
type Capability string

const (
	CapGeometry          Capability = "geometry"            // Window.Geometry is set
	CapMoveWithoutFollow Capability = "move-without-follow" // MoveWindow(follow=false)
	CapWorkspaces        Capability = "workspaces"          // WorkspaceMover
	CapQuietFocus        Capability = "quiet-focus"         // QuietFocuser
	CapPreviews          Capability = "previews"            // live window images
	CapKeystrokes        Capability = "keystrokes"          // Keystroker
	CapBinds             Capability = "binds"               // shortcuts read and written
	CapKeyObserver       Capability = "key-observer"        // Super tap, Super+Tab, slots
	CapDisplays          Capability = "displays"            // display settings
	CapVirtualOutput     Capability = "virtual-output"      // VirtualOutputs
	CapPointer           Capability = "pointer"             // Pointer
	CapPlacement         Capability = "placement"           // Placer
	CapModes             Capability = "modes"               // Moder
	CapRules             Capability = "rules"               // compositor styling rules
	// CapConfig: inua's keyboard, touchpad and shortcut settings reach
	// the compositor (a config inua writes and the compositor loads).
	CapConfig Capability = "config"
	// CapOnDemandFocus: a layer surface asking for the keyboard on demand
	// gets it when it appears (Hyprland). Without it the surface is
	// focused only by a click (niri), so a surface that must take keys at
	// once asks for the keyboard exclusively.
	CapOnDemandFocus Capability = "on-demand-focus"
)

// State is where a capability stands. A capability absent from a
// Capabilities map is Unsupported.
type State int

const (
	Unsupported State = iota // the compositor cannot do it
	Unavailable              // it should, but is not answering now
	Supported
)

func (s State) String() string {
	switch s {
	case Supported:
		return "supported"
	case Unavailable:
		return "unavailable"
	}
	return "unsupported"
}

// MarshalText writes a state by its name, for dumps.
func (s State) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

type Capabilities map[Capability]State

// Has reports whether c is Supported.
func (cs Capabilities) Has(c Capability) bool { return cs[c] == Supported }

// Clone copies the map so a handler can keep it.
func (cs Capabilities) Clone() Capabilities {
	out := make(Capabilities, len(cs))
	for k, v := range cs {
		out[k] = v
	}
	return out
}

var (
	// ErrUnsupported: the compositor cannot do this.
	ErrUnsupported = errors.New("compositor: not supported")
	// ErrUnavailable: the backend is not connected, or the part is not
	// answering now.
	ErrUnavailable = errors.New("compositor: not available now")
	// ErrNoCompositor: Detect found no backend for this session.
	ErrNoCompositor = errors.New("compositor: no known compositor in this session")
)

// Snapshotter reads the state once, for a program that does not run a
// backend: a light app placing its window, the assistant's tools.
type Snapshotter interface {
	Snapshot(ctx context.Context) (Snapshot, error)
}

// OutputFocuser focuses an output, for an action that must happen on it.
type OutputFocuser interface {
	FocusOutput(ctx context.Context, name string) error
}

// WorkspaceMover is CapWorkspaces: naming a new workspace and moving one.
type WorkspaceMover interface {
	// NewWorkspace names the workspace a window moved to "new" on output
	// would land on, skipping ids unavailable says are taken (reserved,
	// in flight). false when there is none.
	NewWorkspace(snap Snapshot, output string, unavailable func(id string) bool) (Workspace, bool)
	MoveWorkspace(ctx context.Context, id, output string) error
}

// QuietFocuser is CapQuietFocus: a focus that does not move the pointer.
type QuietFocuser interface {
	FocusWindowQuietly(ctx context.Context, id string) error
}

// Keystroker is CapKeystrokes: a shortcut sent to one window.
type Keystroker interface {
	// SendKey sends a shortcut such as "CTRL SHIFT V" to the window,
	// focusing it first where the compositor needs that.
	SendKey(ctx context.Context, window, mods, key string) error
}

// Pointer is CapPointer.
type Pointer interface {
	CursorPos(ctx context.Context) (x, y int, err error)
	MoveCursor(ctx context.Context, x, y int) error
}

// Moder is CapModes: a key mode (submap) that takes the keys.
type Moder interface {
	// EnterMode enters the named mode; "" leaves it.
	EnterMode(ctx context.Context, name string) error
}

// Placer is CapPlacement: placing an app's own window by its process.
type Placer interface {
	BringHere(ctx context.Context, pid int) error
	WindowOf(ctx context.Context, pid int) (Rect, error)
	PlaceWindow(ctx context.Context, pid int, r Rect) error
	// RoomRight is the space to the right of the window, and the gap kept
	// between windows.
	RoomRight(ctx context.Context, pid int) (Rect, int, error)
	// Tile tiles a floating window; it reports whether it was floating.
	Tile(ctx context.Context, pid int) (bool, error)
}

// VirtualOutputs is CapVirtualOutput.
type VirtualOutputs interface {
	SyncVirtualScreen(ctx context.Context, mode string) (changed bool, err error)
}

// NextNumbered is NewWorkspace for compositors whose workspaces are
// named by positive integers (Hyprland, Sway): an empty workspace on the
// output, else the lowest empty one anywhere, else the first number from
// fresh on that no workspace has and unavailable does not refuse. A
// workspace counts as empty when no window is on it and it is not shown.
func NextNumbered(snap Snapshot, output string, unavailable func(id string) bool, fresh int) (Workspace, bool) {
	if unavailable == nil {
		unavailable = func(string) bool { return false }
	}
	occupied := map[string]bool{}
	for _, w := range snap.Windows {
		occupied[w.Workspace] = true
	}
	for _, o := range snap.Outputs {
		occupied[o.ActiveWorkspace] = true
	}
	taken := map[int]bool{}
	var prefer, recycle *Workspace
	for i := range snap.Workspaces {
		ws := &snap.Workspaces[i]
		n, err := strconv.Atoi(ws.ID)
		if err != nil || n <= 0 {
			continue
		}
		taken[n] = true
		if occupied[ws.ID] || ws.Active || unavailable(ws.ID) {
			continue
		}
		if output != "" && ws.Output == output && prefer == nil {
			prefer = ws
		}
		if recycle == nil || n < recycle.Order {
			recycle = ws
		}
	}
	if prefer != nil {
		return *prefer, true
	}
	if recycle != nil {
		return *recycle, true
	}
	n := max(fresh, 1)
	for taken[n] || unavailable(strconv.Itoa(n)) {
		n++
	}
	id := strconv.Itoa(n)
	return Workspace{ID: id, Name: id, Output: output, Order: n}, true
}

var (
	regMu    sync.Mutex
	backends []registered
)

type registered struct {
	name string
	open func() (Backend, error)
}

// Register adds a backend opener. open returns ErrNoCompositor when the
// session is not that compositor's. Backends register in init; the
// programs that need a compositor import them.
func Register(name string, open func() (Backend, error)) {
	regMu.Lock()
	defer regMu.Unlock()
	backends = append(backends, registered{name, open})
}

// Detect opens the backend of the compositor this process runs under, in
// registration order; ErrNoCompositor when none matches. Another error
// from an opener is returned as is: the compositor is there but cannot be
// reached.
func Detect() (Backend, error) {
	regMu.Lock()
	regs := slices.Clone(backends)
	regMu.Unlock()
	for _, r := range regs {
		b, err := r.open()
		if errors.Is(err, ErrNoCompositor) {
			continue
		}
		return b, err
	}
	return nil, ErrNoCompositor
}
