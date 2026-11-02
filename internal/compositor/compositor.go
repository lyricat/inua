package compositor

import "context"

// Client is the compositor-facing interface used by Inua's shell UI.
type Client interface {
	// Capabilities reports operations the connected compositor implements.
	Capabilities() Capabilities
	// Snapshot returns the latest consistent view of outputs/workspaces/toplevels.
	Snapshot(ctx context.Context) (Snapshot, error)
	// Events returns a channel of compositor events; closing it stops the subscription.
	Events(ctx context.Context) (<-chan Event, error)
	// FocusWindow focuses the given toplevel address.
	FocusWindow(ctx context.Context, address string) error
	// MoveWindowToWorkspace moves a toplevel to the given workspace.
	MoveWindowToWorkspace(ctx context.Context, address string, workspace int) error
	// CloseWindow closes the given toplevel.
	CloseWindow(ctx context.Context, address string) error
	// SwitchWorkspace switches to the given workspace.
	SwitchWorkspace(ctx context.Context, id int) error
}

// Snapshot is a compositor-independent view of state.
type Snapshot struct {
	Monitors       []Monitor
	Workspaces     []Workspace
	Toplevels      []Toplevel
	ActiveToplevel string
	FocusedMonitor string
}

type Monitor struct {
	ID                int
	Name              string
	Description       string
	X, Y              int
	Width, Height     int
	Scale             float64
	Focused           bool
	ActiveWorkspaceID int
}

type Workspace struct {
	ID            int
	Name          string
	Monitor       string
	Active        bool
	Focused       bool
	HasFullscreen bool
	Urgent        bool
}

type Toplevel struct {
	Address            string
	PID                int
	Title              string
	Class              string
	WorkspaceID        int
	Monitor            string
	WorkspacePositionX int
	WorkspacePositionY int
	WorkspaceGridWidth int
	GeometryX          int
	GeometryY          int
	GeometryWidth      int
	GeometryHeight     int
	Activated          bool
}

// Capabilities lets the UI expose unavailable actions instead of silently
// rendering empty or nonfunctional controls.
type Capabilities struct {
	StateQuery      bool
	StateEvents     bool
	FocusWindow     bool
	MoveWindow      bool
	ResizeWindow    bool
	CloseWindow     bool
	SwitchWorkspace bool
	CreateWorkspace bool
	ShortcutListing bool
	WindowCapture   bool
	SessionLock     bool
	Reasons         map[string]string
}

type Event struct {
	Kind string
	Data map[string]any
}
