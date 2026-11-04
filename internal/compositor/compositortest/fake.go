// Package compositortest is a fake compositor backend for tests: the test
// pushes snapshots and reads the actions back.
package compositortest

import (
	"context"
	"fmt"
	"sync"

	"github.com/lyricat/inua/internal/compositor"
)

// Fake implements compositor.Backend and WorkspaceMover. Actions append
// one line each to Actions, such as "focus-window 0x1".
type Fake struct {
	Caps    compositor.Capabilities
	Fresh   int // NewWorkspace numbers from here (NextNumbered)
	Fail    error
	mu      sync.Mutex
	actions []string
	h       compositor.Handler
	refresh chan struct{}
}

func New() *Fake {
	return &Fake{
		Caps: compositor.Capabilities{
			compositor.CapWorkspaces: compositor.Supported, compositor.CapMoveWithoutFollow: compositor.Supported,
		},
		Fresh:   1,
		refresh: make(chan struct{}, 1),
	}
}

func (f *Fake) Name() string { return "fake" }

// Run keeps the handler and reports the connection up until ctx ends.
func (f *Fake) Run(ctx context.Context, h compositor.Handler) error {
	f.mu.Lock()
	f.h = h
	f.mu.Unlock()
	h.OnCapabilities(f.Caps.Clone())
	h.OnConnection(true, nil)
	<-ctx.Done()
	return ctx.Err()
}

// Push delivers a snapshot as the compositor would.
func (f *Fake) Push(s compositor.Snapshot) {
	f.mu.Lock()
	h := f.h
	f.mu.Unlock()
	if h != nil {
		h.OnSnapshot(s)
	}
}

func (f *Fake) Refresh() {
	select {
	case f.refresh <- struct{}{}:
	default:
	}
}

// Refreshed reports whether Refresh was called since the last check.
func (f *Fake) Refreshed() bool {
	select {
	case <-f.refresh:
		return true
	default:
		return false
	}
}

func (f *Fake) Capabilities() compositor.Capabilities { return f.Caps.Clone() }

func (f *Fake) record(format string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, fmt.Sprintf(format, args...))
	return f.Fail
}

// Actions are the actions so far, oldest first.
func (f *Fake) Actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.actions...)
}

func (f *Fake) FocusWindow(_ context.Context, id string) error {
	return f.record("focus-window %s", id)
}
func (f *Fake) CloseWindow(_ context.Context, id string) error {
	return f.record("close-window %s", id)
}
func (f *Fake) FocusWorkspace(_ context.Context, id string) error {
	return f.record("focus-workspace %s", id)
}
func (f *Fake) MoveWindow(_ context.Context, id, ws string, follow bool) error {
	if !follow && !f.Caps.Has(compositor.CapMoveWithoutFollow) {
		return compositor.ErrUnsupported
	}
	return f.record("move-window %s %s follow=%v", id, ws, follow)
}
func (f *Fake) NewWorkspace(snap compositor.Snapshot, output string, unavailable func(string) bool) (compositor.Workspace, bool) {
	return compositor.NextNumbered(snap, output, unavailable, f.Fresh)
}
func (f *Fake) MoveWorkspace(_ context.Context, id, output string) error {
	return f.record("move-workspace %s %s", id, output)
}
