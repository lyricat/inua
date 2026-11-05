// Package layershell binds gtk4-layer-shell (libgtk4-layer-shell, Load)
// with purego, no cgo.
//
// gtk4-layer-shell works by wrapping libwayland-client calls, so it must be
// loaded before libwayland-client. puregotk dlopens GTK (and with it
// libwayland-client) during package initialization, before main runs, so
// loading the library from Go is always too late. Instead, call
// EnsurePreloaded first thing in main: it re-executes the process once with
// the library in LD_PRELOAD, before any window exists.
package layershell

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// Loader is the dlopen/dlsym pair of the purego variant the program already
// links (github.com/ebitengine/purego, or puregotk's fork). A binary can
// contain only one of them, so this package imports neither:
//
//	layershell.Loader{
//		Open: func(name string) (uintptr, error) {
//			return purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
//		},
//		Register: purego.RegisterLibFunc,
//	}
type Loader struct {
	Open     func(name string) (uintptr, error)
	Register func(fptr any, lib uintptr, name string)
}

const (
	soname    = "libgtk4-layer-shell.so.0"
	markerEnv = "INUA_LAYERSHELL_PRELOAD"
	origEnv   = "INUA_ORIG_LD_PRELOAD"
)

type Layer int32

const (
	LayerBackground Layer = 0
	LayerBottom     Layer = 1
	LayerTop        Layer = 2
	LayerOverlay    Layer = 3
)

type Edge int32

const (
	EdgeLeft   Edge = 0
	EdgeRight  Edge = 1
	EdgeTop    Edge = 2
	EdgeBottom Edge = 3
)

type KeyboardMode int32

const (
	KeyboardNone      KeyboardMode = 0
	KeyboardExclusive KeyboardMode = 1
	KeyboardOnDemand  KeyboardMode = 2
)

// EnsurePreloaded re-executes the current process with gtk4-layer-shell in
// LD_PRELOAD unless that already happened. On return the process runs with
// the library preloaded, and LD_PRELOAD is restored to its original value so
// that child processes (launched apps) do not inherit it.
func EnsurePreloaded() error {
	if os.Getenv(markerEnv) == "1" {
		os.Unsetenv(markerEnv)
		if orig, ok := os.LookupEnv(origEnv); ok {
			os.Unsetenv(origEnv)
			if orig == "" {
				os.Unsetenv("LD_PRELOAD")
			} else {
				os.Setenv("LD_PRELOAD", orig)
			}
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	orig := os.Getenv("LD_PRELOAD")
	preload := soname
	if orig != "" {
		preload = soname + ":" + orig
	}
	env := append(os.Environ(), markerEnv+"=1", origEnv+"="+orig, "LD_PRELOAD="+preload)
	return syscall.Exec(exe, os.Args, env)
}

var (
	isSupported        func() int32
	getMajor, getMinor func() uint32
	initForWindow      func(window uintptr)
	setNamespace       func(window uintptr, ns string)
	setLayer           func(window uintptr, layer Layer)
	setMonitor         func(window uintptr, monitor uintptr)
	setAnchor          func(window uintptr, edge Edge, anchor int32)
	setMargin          func(window uintptr, edge Edge, margin int32)
	setExclusiveZone   func(window uintptr, zone int32)
	autoExclusiveZone  func(window uintptr)
	setKeyboardMode    func(window uintptr, mode KeyboardMode)
	lockSupported      func() int32
	lockNew            func() uintptr
	lockLock           func(lock uintptr) int32
	lockUnlock         func(lock uintptr)
	lockIsLocked       func(lock uintptr) int32
	lockAssign         func(lock, window, monitor uintptr)
	loaded             bool
)

// Load binds gtk4-layer-shell. It fails if the library is missing or was not
// preloaded (in which case it would silently do nothing).
func Load(l Loader) error {
	if !isPreloaded() {
		return fmt.Errorf("layershell: %s is not preloaded (call EnsurePreloaded first in main)", soname)
	}
	return load(l, soname, "libgtk4-layer-shell0")
}

func load(l Loader, name, pkg string) error {
	lib, err := l.Open(name)
	if err != nil {
		return fmt.Errorf("layershell: %w (install %s)", err, pkg)
	}
	l.Register(&isSupported, lib, "gtk_layer_is_supported")
	l.Register(&getMajor, lib, "gtk_layer_get_major_version")
	l.Register(&getMinor, lib, "gtk_layer_get_minor_version")
	l.Register(&initForWindow, lib, "gtk_layer_init_for_window")
	l.Register(&setNamespace, lib, "gtk_layer_set_namespace")
	l.Register(&setLayer, lib, "gtk_layer_set_layer")
	l.Register(&setMonitor, lib, "gtk_layer_set_monitor")
	l.Register(&setAnchor, lib, "gtk_layer_set_anchor")
	l.Register(&setMargin, lib, "gtk_layer_set_margin")
	l.Register(&setExclusiveZone, lib, "gtk_layer_set_exclusive_zone")
	l.Register(&autoExclusiveZone, lib, "gtk_layer_auto_exclusive_zone_enable")
	l.Register(&setKeyboardMode, lib, "gtk_layer_set_keyboard_mode")
	loaded = true
	// The session lock came with 1.1; Debian 13 has 1.0, where binding
	// its functions would stop the program. Without them LockSupported
	// is false and nothing else is called.
	if AtLeast(1, 1) {
		l.Register(&lockSupported, lib, "gtk_session_lock_is_supported")
		l.Register(&lockNew, lib, "gtk_session_lock_instance_new")
		l.Register(&lockLock, lib, "gtk_session_lock_instance_lock")
		l.Register(&lockUnlock, lib, "gtk_session_lock_instance_unlock")
		l.Register(&lockIsLocked, lib, "gtk_session_lock_instance_is_locked")
		l.Register(&lockAssign, lib, "gtk_session_lock_instance_assign_window_to_monitor")
	}
	return nil
}

// isPreloaded checks /proc/self/maps for the library.
func isPreloaded() bool {
	maps, err := os.ReadFile("/proc/self/maps")
	return err == nil && strings.Contains(string(maps), "libgtk4-layer-shell.so")
}

// Version returns "major.minor" of the loaded library.
func Version() string {
	if !loaded {
		return ""
	}
	return fmt.Sprintf("%d.%d", getMajor(), getMinor())
}

// AtLeast reports whether the loaded library is version major.minor or
// later.
func AtLeast(major, minor uint32) bool {
	if !loaded {
		return false
	}
	ma, mi := getMajor(), getMinor()
	return ma > major || ma == major && mi >= minor
}

// Supported reports whether the compositor speaks wlr-layer-shell. Call it
// after GTK is initialized (e.g. in the application's activate handler).
func Supported() bool { return loaded && isSupported() != 0 }

// The functions below take a GtkWindow*. InitForWindow must be called before
// the window is realized.

func InitForWindow(window uintptr)                   { initForWindow(window) }
func SetNamespace(window uintptr, ns string)         { setNamespace(window, ns) }
func SetLayer(window uintptr, l Layer)               { setLayer(window, l) }
func SetMonitor(window uintptr, monitor uintptr)     { setMonitor(window, monitor) }
func SetMargin(window uintptr, e Edge, margin int32) { setMargin(window, e, margin) }
func SetExclusiveZone(window uintptr, zone int32)    { setExclusiveZone(window, zone) }
func AutoExclusiveZone(window uintptr)               { autoExclusiveZone(window) }
func SetKeyboardMode(window uintptr, m KeyboardMode) { setKeyboardMode(window, m) }

func SetAnchor(window uintptr, e Edge, anchor bool) {
	v := int32(0)
	if anchor {
		v = 1
	}
	setAnchor(window, e, v)
}

// Session lock (ext-session-lock-v1), since 1.1. A lock instance is a
// GObject with the signals "locked", "failed" and "unlocked", and since
// 1.2 "monitor" (GdkMonitor*): on it, assign a new, unrealized window to
// that monitor. Before 1.2, assign one to each monitor right after Lock.
// The library destroys the windows when the lock ends.

// LockSupported reports whether the compositor supports session locks.
// It is false with a library older than 1.1, which has no session lock.
func LockSupported() bool { return loaded && lockSupported != nil && lockSupported() != 0 }

// NewLock makes a session lock instance (a new reference).
func NewLock() uintptr { return lockNew() }

// Lock starts locking; false is an immediate failure.
func Lock(lock uintptr) bool { return lockLock(lock) != 0 }

// Unlock ends the lock this instance holds.
func Unlock(lock uintptr) { lockUnlock(lock) }

// IsLocked reports whether the instance holds the lock.
func IsLocked(lock uintptr) bool { return lockIsLocked(lock) != 0 }

// AssignLockWindow makes window the lock surface on monitor.
func AssignLockWindow(lock, window, monitor uintptr) { lockAssign(lock, window, monitor) }
