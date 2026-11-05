package clipboard

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/compositor"
	_ "github.com/lyricat/inua/internal/compositors" // the backends Detect knows
)

// Selection is where the selection being copied now comes from: the
// program that copied it, and the window focused, with sensitive (the
// programs listed as sensitive) and the types a password manager marks
// its secrets with.
func Selection(sensitive []string) Source {
	from := FindSource(focusedWindow(), sensitive)
	if MarkedSensitive(offeredTypes()) {
		from.Sensitive = true // a password manager's secret
	}
	return from
}

// offeredTypes are the types the clipboard's owner offers now; none when
// wl-paste cannot say in time.
func offeredTypes() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "wl-paste", "--list-types").Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// focusedWindow is the window focused now, as the compositor names it;
// none without one.
func focusedWindow() Window {
	var w Window
	b, err := compositor.Detect()
	if err != nil {
		return w
	}
	sn, ok := b.(compositor.Snapshotter)
	if !ok {
		return w
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond) // a copy is not held up for long
	defer cancel()
	snap, err := sn.Snapshot(ctx)
	if err != nil {
		return w
	}
	if f, ok := snap.Window(snap.FocusedWindow); ok {
		w.Class, w.PID = f.AppID, f.PID
	}
	return w
}
