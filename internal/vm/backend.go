package vm

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// Backend runs one machine: what the window and the command line drive, and
// what the guide's three backends implement (QEMU, Dockur, Waydroid).
type Backend interface {
	// Install makes what the machine needs before it can run: its disk, the
	// image it is pulled from, its container or its project. It never starts
	// the guest, and it never touches what is already there.
	Install(ctx context.Context, m Machine) error
	// State is what the guest is doing now.
	State(ctx context.Context, m Machine) (State, error)
	// Start brings the guest up. It returns before the guest is ready: a
	// first install is tens of minutes, and the caller shows progress.
	Start(ctx context.Context, m Machine) error
	// Stop shuts the guest down, keeping everything it has: its disk, its
	// password and its definition.
	Stop(ctx context.Context, m Machine) error
	// Display is how to see a running machine. A backend that has no viewer
	// of its own answers Kind DisplayNone.
	Display(ctx context.Context, m Machine) (Display, error)
	// Remove takes the machine's container or process apart. With keepDisk it
	// leaves the guest's disk and its password alone; without it, the guest's
	// own files go too, and that is the caller's second, confirmed step.
	Remove(ctx context.Context, m Machine, keepDisk bool) error
}

// Display is how to see a running machine: the viewer to open, and the web
// console when the backend has one.
type Display struct {
	Kind    DisplayKind // spice | gtk | rdp | web | serial | none
	Address string      // a socket path, or 127.0.0.1:port
	Console string      // the web console's URL, when there is one
}

// Seamless is implemented by the backends that can show a program running in
// the guest as a window of its own on this desktop.
type Seamless interface {
	// Apps are the programs the guest offers.
	Apps(ctx context.Context, m Machine) ([]GuestApp, error)
	// Launch starts one of them on this desktop.
	Launch(ctx context.Context, m Machine, app GuestApp) error
}

// GuestApp is a program found in a guest, for seamless mode: its name, its
// icon (a file inua copied out of the guest), and the guest's own way to run
// it.
type GuestApp struct {
	ID      string
	Name    string
	Icon    string
	Command string
}

// Progress is implemented by the backends that can say what a starting guest
// is doing: a Windows container's own log lines, a guest's serial output.
// It is not part of Backend: a backend that cannot say anything simply does
// not implement it, and the window shows no progress line.
type Progress interface {
	RecentOutput(ctx context.Context, m Machine) string
}

// Prepared is implemented by the backends that can say whether the machine's
// own things - its disk, its container, its project - are already there. It is
// what tells the window "nothing made yet" from "installed, and off".
type Prepared interface {
	Prepared(m Machine) bool
}

// FreePort is the first port at or above from that this host has free, on the
// loopback interface: what a machine publishes is only ever reached from here.
func FreePort(from int) (int, error) {
	for port := from; port < from+200 && port <= 65535; port++ {
		ln, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port at or above %d", from)
}

// ErrNotThisBackend is what a backend answers for a machine it does not run.
var ErrNotThisBackend = errors.New("the machine is not this backend's")
