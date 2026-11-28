// Package gtkx keeps puregotk's sharp edges out of UI code.
//
// Rules it encodes (see spike/gtk4-layershell/README.md for how they were
// found in the toolkit spike):
//
//   - Ownership. puregotk's constructors and getters (NewLabel, GetObject,
//     GetFirstChild, ...) return a reference that Go owns, and nothing ever
//     releases it automatically. Widgets built in Go are handed to a parent
//     with Append, which drops that reference. Objects UI code keeps for a
//     while are tracked in a Scope and released together.
//   - Callbacks. purego creates one native trampoline per Go function
//     pointer, at most 2000, never freed. Handlers must be long-lived
//     variables, shared between widgets, never a closure per widget.
//   - Threads. GTK is used only from the GLib main loop thread; goroutines
//     hand work over with Post or Call.
package gtkx

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/lyricat/inua/internal/i18n"
)

// init keeps the main goroutine on the process's main thread, which Go
// otherwise moves between threads whenever it waits. AppKit takes only the
// main thread: GtkApplication's start on another, setting the menu bar,
// ends the program on macOS (issue #48).
func init() {
	runtime.LockOSThread()
}

// --- main loop ---

var (
	queueMu sync.Mutex
	queue   []func()
	// drain is the single idle callback; see the package comment.
	drain glib.SourceFunc = func(uintptr) bool {
		queueMu.Lock()
		q := queue
		queue = nil
		queueMu.Unlock()
		for _, f := range q {
			f()
		}
		return false // the idle source ends; Post adds a new one
	}
)

// Post runs f on the main loop thread, asynchronously and in order.
func Post(f func()) {
	queueMu.Lock()
	first := len(queue) == 0
	queue = append(queue, f)
	queueMu.Unlock()
	if first {
		glib.IdleAdd(&drain, 0)
	}
}

// Call waits for f on the main loop. A call that has not started by its
// deadline is skipped; a callback already running cannot be interrupted.
func Call[T any](timeout time.Duration, f func() T) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ch := make(chan T, 1)
	Post(func() {
		if ctx.Err() == nil {
			ch <- f()
		}
	})
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		var zero T
		return zero, fmt.Errorf("gtkx: main loop did not respond within %v", timeout)
	}
}

// --- ownership ---

// Append adds a widget that Go just created to box and drops Go's reference,
// leaving box as the only owner.
func Append(box *gtk.Box, w *gtk.Widget) {
	box.Append(w)
	w.Unref()
}

// Clear removes all children of box.
func Clear(box *gtk.Box) {
	for c := box.GetFirstChild(); c != nil; c = box.GetFirstChild() {
		box.Remove(c)
		c.Unref() // GetFirstChild returned a new reference
	}
}

// Scope collects references UI code holds for the lifetime of something,
// typically a window, and releases them together.
type Scope struct{ refs []*gobject.Object }

// Track records a reference to release later.
func (s *Scope) Track(o gobject.Ptr) {
	s.refs = append(s.refs, &gobject.Object{Ptr: o.GoPointer()})
}

// Release drops every tracked reference.
func (s *Scope) Release() {
	for _, o := range s.refs {
		o.Unref()
	}
	s.refs = nil
}

// --- markup ---

// Builder instantiates a GtkBuilder (.ui) definition. Objects fetched with Get
// are tracked in the given Scope.
type Builder struct {
	b     *gtk.Builder
	scope *Scope
}

// Load parses a .ui definition. Close the Builder after fetching objects.
func Load(ui string, scope *Scope) (*Builder, error) {
	b := gtk.NewBuilder()
	if _, err := b.AddFromString(i18n.UI(ui), -1); err != nil { // its marked strings in the language in use
		b.Unref()
		return nil, err
	}
	return &Builder{b: b, scope: scope}, nil
}

// Get casts the object with the given id into dst and tracks the reference.
// It panics on a missing id: the markup and the code disagree.
func (b *Builder) Get(id string, dst gobject.Ptr) {
	o := b.b.GetObject(id)
	if o == nil {
		panic("gtkx: no object " + id + " in .ui definition")
	}
	o.Cast(dst)
	b.scope.Track(dst)
}

// Take casts the object with the given id into dst without tracking it: the
// caller owns the reference, typically to hand it to a parent with Append.
func (b *Builder) Take(id string, dst gobject.Ptr) {
	o := b.b.GetObject(id)
	if o == nil {
		panic("gtkx: no object " + id + " in .ui definition")
	}
	o.Cast(dst)
}

// Close drops the builder. Objects survive through their parents, the
// window list (for toplevels) or references tracked by Get.
func (b *Builder) Close() { b.b.Unref() }

// --- signals puregotk does not bind ---

// ConnectChanged connects GtkEditable::changed. handler must be a long-lived
// variable (see the package comment).
func ConnectChanged(editable gobject.Ptr, handler *func(uintptr, uintptr)) {
	gobject.SignalConnect(editable.GoPointer(), "changed", glib.NewCallback(handler))
}

// ConnectItemsChanged connects GListModel::items-changed(position, removed,
// added). handler must be a long-lived variable (see the package comment).
func ConnectItemsChanged(model gobject.Ptr, handler *func(uintptr, uint32, uint32, uint32, uintptr)) {
	gobject.SignalConnect(model.GoPointer(), "items-changed", glib.NewCallback(handler))
}

// ConnectNotify connects GObject::notify for one property. handler must be a
// long-lived variable (see the package comment); it gets the object, the
// GParamSpec and user data.
func ConnectNotify(obj gobject.Ptr, property string, handler *func(uintptr, uintptr, uintptr)) {
	gobject.SignalConnect(obj.GoPointer(), "notify::"+property, glib.NewCallback(handler))
}

// SetStringSetting sets a string property of the default GtkSettings,
// such as "gtk-icon-theme-name".
func SetStringSetting(name, value string) {
	var v gobject.Value
	v.Init(gobject.TypeStringVal)
	defer v.Unset()
	v.SetString(value)
	gtk.SettingsGetDefault().SetProperty(name, &v)
}
