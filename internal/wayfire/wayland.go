package wayfire

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

// Wayland owns a dedicated Wayland connection for the foreign-toplevel
// protocol. IDs are stable for the lifetime of this connection; the client
// never synthesizes PID or geometry, which this protocol does not expose.
type Wayland struct {
	mu            sync.RWMutex
	views         map[string]*view
	outputs       map[uint32]outputMetadata
	outputObjects map[uint32]uint32
	conn          net.Conn
	manager       uint32
	serial        uint32
	registry      uint32
	state         compositor.Snapshot
	sendFunc      func(uint32, uint16, ...[]byte) error
	callbacks     map[uint32]bool
}

var _ compositor.Client = (*Wayland)(nil)

type view struct {
	id, title, appID string
	outputs          map[string]bool
	activated        bool
}

type outputMetadata struct {
	name, description   string
	x, y, width, height int
	scale               float64
}

const (
	wlDisplayID  = 1
	wlRegistryID = 2
	wlCallbackID = 3
)

func NewWayland() *Wayland {
	return &Wayland{views: map[string]*view{}, outputs: map[uint32]outputMetadata{}, outputObjects: map[uint32]uint32{}, callbacks: map[uint32]bool{}}
}

func waylandSocketFromEnv() (string, error) {
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		display = "wayland-0"
	}
	if filepath.IsAbs(display) {
		return display, nil
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return "", errors.New("XDG_RUNTIME_DIR is unset")
	}
	return filepath.Join(runtime, display), nil
}

// Run connects, binds zwlr_foreign_toplevel_manager_v1, and emits snapshots
// until cancelled or the compositor disconnects.
func (w *Wayland) Run(ctx context.Context, changed func(compositor.Snapshot)) error {
	path, err := waylandSocketFromEnv()
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return fmt.Errorf("connect Wayland display: %w", err)
	}
	w.conn = conn
	defer conn.Close()
	if err := w.initialize(); err != nil {
		return err
	}
	initial := w.currentSnapshot()
	w.mu.Lock()
	w.state = initial
	w.mu.Unlock()
	if changed != nil {
		changed(initial)
	}
	for {
		if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			return err
		}
		obj, op, payload, err := w.readMessage()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			}
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := w.handle(obj, op, payload); err != nil {
			return err
		}
		state := w.currentSnapshot()
		w.mu.Lock()
		w.state = state
		w.mu.Unlock()
		if changed != nil {
			changed(state)
		}
	}
}

func (w *Wayland) initialize() error {
	w.registry = w.nextID()
	if err := w.send(wlDisplayID, 1, argU32(w.registry)); err != nil { // get_registry
		return err
	}
	callback := w.nextID()
	if err := w.send(wlDisplayID, 0, argU32(callback)); err != nil {
		return err
	}
	w.callbacks[callback] = true
	var name uint32
	// Registry globals are delivered before the sync callback.
	for {
		obj, op, p, err := w.readMessage()
		if err != nil {
			return err
		}
		if obj == w.registry && op == 0 {
			global, iface, version, err := parseGlobal(p)
			if err != nil {
				return err
			}
			if iface == "wl_output" {
				id := w.nextID()
				w.outputObjects[id] = global
				w.outputs[global] = outputMetadata{scale: 1}
				if err := w.send(w.registry, 0, argU32(global), argString(iface), argU32(min(version, 4)), argU32(id)); err != nil {
					return err
				}
			}

			if iface == "zwlr_foreign_toplevel_manager_v1" {
				name = global
				w.manager = w.nextID()
				if err := w.send(w.registry, 0, argU32(name), argString(iface), argU32(min(version, 3)), argU32(w.manager)); err != nil {
					return err
				}
			}
		} else if w.callbacks[obj] && op == 0 {
			delete(w.callbacks, obj)
			break
		}
	}
	if w.manager == 0 {
		return errors.New("Wayland compositor does not advertise zwlr_foreign_toplevel_manager_v1")
	}
	return w.roundtrip()
}

func (w *Wayland) roundtrip() error {
	id := w.nextID()
	if err := w.send(wlDisplayID, 0, argU32(id)); err != nil {
		return err
	}
	w.callbacks[id] = true
	for {
		obj, op, payload, err := w.readMessage()
		if err != nil {
			return err
		}
		if w.callbacks[obj] && op == 0 {
			delete(w.callbacks, obj)
			if obj == id {
				return nil
			}
			continue
		}
		if err := w.handle(obj, op, payload); err != nil {
			return err
		}
	}
}

func (w *Wayland) handle(obj uint32, op uint16, p []byte) error {
	if obj == 1 && op == 0 {
		return fmt.Errorf("Wayland protocol error: %x", p)
	}
	if obj == w.manager && op == 0 { // toplevel(new_id)
		if len(p) < 4 {
			return errors.New("short foreign-toplevel new handle")
		}
		id := binary.NativeEndian.Uint32(p[:4])
		key := fmt.Sprintf("wf:%d", id)
		w.mu.Lock()
		w.views[key] = &view{id: key, outputs: map[string]bool{}}
		w.mu.Unlock()
		return nil
	}
	if outputGlobal, ok := w.outputObjects[obj]; ok {
		meta := w.outputs[outputGlobal]
		switch op {
		case 0:
			if len(p) >= 8 {
				meta.height = int(binary.NativeEndian.Uint32(p[:4]))
				meta.width = int(binary.NativeEndian.Uint32(p[4:8]))
			}
		case 2:
			if len(p) >= 4 {
				meta.scale = float64(binary.NativeEndian.Uint32(p[:4]))
			}
		case 3:
			meta.name = parseString(p)
		case 4:
			meta.description = parseString(p)
		}
		w.outputs[outputGlobal] = meta
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	v := w.findView(obj)
	if v == nil {
		return nil
	}
	switch op {
	case 0: // title
		v.title = parseString(p)
	case 1: // app_id
		v.appID = parseString(p)
	case 2: // output_enter
		if len(p) >= 4 {
			v.outputs[fmt.Sprintf("output:%d", binary.NativeEndian.Uint32(p[:4]))] = true
		}
	case 3: // output_leave
		if len(p) >= 4 {
			delete(v.outputs, fmt.Sprintf("output:%d", binary.NativeEndian.Uint32(p[:4])))
		}
	case 4: // state
		v.activated = stateActivated(p)
	case 5: // done
	case 6: // closed
		delete(w.views, v.id)
		_ = w.send(obj, 7) // zwlr_foreign_toplevel_handle_v1.destroy
	case 7: // parent
	default:
	}
	return nil
}

func (w *Wayland) findView(handle uint32) *view {
	key := fmt.Sprintf("wf:%d", handle)
	return w.views[key]
}

func (w *Wayland) Snapshot(context.Context) (compositor.Snapshot, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.state.Toplevels == nil {
		return compositor.Snapshot{}, errors.New("Wayfire foreign-toplevel state has not arrived yet")
	}
	return w.state, nil
}

func (w *Wayland) Events(context.Context) (<-chan compositor.Event, error) {
	return nil, errors.New("Wayfire events are delivered through the Run callback, not a channel")
}

func (w *Wayland) Capabilities() compositor.Capabilities {
	return compositor.Capabilities{
		StateQuery: true, StateEvents: false, FocusWindow: false, MoveWindow: false,
		ResizeWindow: false, CloseWindow: true, SwitchWorkspace: false, CreateWorkspace: false,
		ShortcutListing: false, WindowCapture: false, SessionLock: false,
		Reasons: map[string]string{
			"StateEvents":     "state snapshots are delivered by Run callback",
			"FocusWindow":     "foreign-toplevel activation needs a wl_seat and input serial",
			"MoveWindow":      "foreign-toplevel protocol does not provide workspace assignment",
			"ResizeWindow":    "foreign-toplevel protocol does not provide geometry control",
			"SwitchWorkspace": "ext_workspace is not advertised by the measured compositor",
			"CreateWorkspace": "Wayfire workspaces are a fixed grid configured at startup",
			"ShortcutListing": "Wayfire shortcut enumeration is not implemented",
			"WindowCapture":   "only output screencopy is advertised; per-window capture is unavailable",
			"SessionLock":     "ext_session_lock is advertised, but lock-surface binding is not implemented",
		},
	}
}

func (w *Wayland) FocusWindow(context.Context, string) error {
	return errors.New("foreign-toplevel activation requires a wl_seat and input serial")
}

func (w *Wayland) MoveWindowToWorkspace(context.Context, string, int) error {
	return errors.New("foreign-toplevel protocol does not provide workspace assignment")
}

func (w *Wayland) CloseWindow(_ context.Context, address string) error {
	w.mu.RLock()
	_, ok := w.views[address]
	w.mu.RUnlock()
	if !ok {
		return fmt.Errorf("Wayfire toplevel %q does not exist", address)
	}
	var handle uint32
	if _, err := fmt.Sscanf(address, "wf:%d", &handle); err != nil || handle == 0 {
		return fmt.Errorf("invalid Wayfire toplevel id %q", address)
	}
	return w.send(handle, 5) // zwlr_foreign_toplevel_handle_v1.close
}

func (w *Wayland) SwitchWorkspace(context.Context, int) error {
	return errors.New("ext_workspace is not advertised by the measured compositor")
}

func (w *Wayland) currentSnapshot() compositor.Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := compositor.Snapshot{}
	for global, meta := range w.outputs {
		if meta.name == "" {
			continue
		}
		out.Monitors = append(out.Monitors, compositor.Monitor{ID: int(global), Name: meta.name, Description: meta.description, Width: meta.width, Height: meta.height, Scale: meta.scale})
	}
	for _, v := range w.views {
		monitor := ""
		for name := range v.outputs {
			if meta, ok := w.outputs[uint32(outputID(name))]; ok {
				monitor = meta.name
				break
			}
		}
		out.Toplevels = append(out.Toplevels, compositor.Toplevel{Address: v.id, Title: v.title, Class: v.appID, Monitor: monitor, Activated: v.activated})
		if v.activated {
			out.ActiveToplevel = v.id
		}
	}
	return out
}

func outputID(name string) uint64 {
	var id uint64
	_, _ = fmt.Sscanf(name, "output:%d", &id)
	return id
}

func (w *Wayland) nextID() uint32 {
	if w.serial < 2 {
		w.serial = 2
	}
	id := w.serial
	w.serial++
	return id
}

func (w *Wayland) send(object uint32, opcode uint16, args ...[]byte) error {
	if w.sendFunc != nil {
		return w.sendFunc(object, opcode, args...)
	}
	var payload []byte
	for _, arg := range args {
		payload = append(payload, arg...)
	}
	size := 8 + len(payload)
	if size%4 != 0 {
		return errors.New("unaligned Wayland request")
	}
	message := make([]byte, size)
	binary.NativeEndian.PutUint32(message, object)
	binary.NativeEndian.PutUint32(message[4:], uint32(size)<<16|uint32(opcode))
	copy(message[8:], payload)
	_, err := w.conn.Write(message)
	return err
}

func (w *Wayland) readMessage() (uint32, uint16, []byte, error) {
	var header [8]byte
	if _, err := io.ReadFull(w.conn, header[:]); err != nil {
		return 0, 0, nil, err
	}
	word := binary.NativeEndian.Uint32(header[4:])
	size := int(word >> 16)
	if size < 8 || size > 1<<20 || size%4 != 0 {
		return 0, 0, nil, errors.New("invalid Wayland event size")
	}
	p := make([]byte, size-8)
	if _, err := io.ReadFull(w.conn, p); err != nil {
		return 0, 0, nil, err
	}
	return binary.NativeEndian.Uint32(header[:]), uint16(word), p, nil
}

func argU32(n uint32) []byte { p := make([]byte, 4); binary.NativeEndian.PutUint32(p, n); return p }
func argString(s string) []byte {
	n := len(s) + 1
	padded := (n + 3) &^ 3
	p := make([]byte, 4+padded)
	binary.NativeEndian.PutUint32(p, uint32(n))
	copy(p[4:], s)
	return p
}
func parseGlobal(p []byte) (uint32, string, uint32, error) {
	if len(p) < 12 {
		return 0, "", 0, errors.New("short registry global")
	}
	name := binary.NativeEndian.Uint32(p)
	n := int(binary.NativeEndian.Uint32(p[4:]))
	if n < 1 || 8+n > len(p) {
		return 0, "", 0, errors.New("invalid registry interface")
	}
	iface := string(p[8 : 8+n-1])
	off := 8 + ((n + 3) &^ 3)
	if off+4 > len(p) {
		return 0, "", 0, errors.New("short registry version")
	}
	return name, iface, binary.NativeEndian.Uint32(p[off:]), nil
}
func parseString(p []byte) string {
	if len(p) < 4 {
		return ""
	}
	n := int(binary.NativeEndian.Uint32(p))
	if n < 1 || n > len(p)-4 {
		return ""
	}
	return string(p[4 : 4+n-1])
}
func stateActivated(p []byte) bool {
	if len(p) < 4 {
		return false
	}
	n := int(binary.NativeEndian.Uint32(p))
	off := 4
	for i := 0; i < n && off+4 <= len(p); i++ {
		state := binary.NativeEndian.Uint32(p[off:])
		if state == 2 {
			return true
		}
		off += 4
	}
	return false
}
