// Package wlroutput reads the displays of a wlroots compositor (Wayfire,
// Sway) through wlr-output-management: each head with its modes, the one
// it shows, its place and its scale, on or off. It only reads; a
// compositor's settings are changed its own way. The protocol is spoken
// directly, on a connection of its own that is closed after the first
// complete state.
package wlroutput

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// ErrNoManager: the compositor does not have the protocol.
var ErrNoManager = errors.New("wlroutput: the compositor has no wlr-output-management")

// Mode is one a head can show. Refresh is in mHz, as the protocol has it;
// 0 when the head names none (a headless one).
type Mode struct {
	Width, Height int
	Refresh       int
	Preferred     bool
}

// Head is a display. Current is nil while it is off; X, Y and Scale are
// its place in the layout and its scale while it is on.
type Head struct {
	Name, Description, Make, Model string
	Enabled                        bool
	Modes                          []Mode
	Current                        *Mode
	X, Y                           int
	Scale                          float64
	Transform                      int
}

const interfaceName = "zwlr_output_manager_v1"

// Heads asks the compositor of this session ($WAYLAND_DISPLAY).
func Heads(ctx context.Context) ([]Head, error) {
	path, err := socketPath()
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("wlroutput: connect Wayland: %w", err)
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Second)
	}
	conn.SetDeadline(deadline)
	return read(conn)
}

func socketPath() (string, error) {
	name := os.Getenv("WAYLAND_DISPLAY")
	if name == "" {
		name = "wayland-0"
	}
	if filepath.IsAbs(name) {
		return name, nil
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return "", errors.New("wlroutput: XDG_RUNTIME_DIR is unset")
	}
	return filepath.Join(runtime, name), nil
}

// The client's objects: wl_display is 1; the compositor numbers the heads
// and modes it makes itself.
const (
	registry = 2
	callback = 3
	manager  = 4
)

type head struct {
	Head
	modes   []uint32
	current uint32
}

// read speaks the protocol on rw until the manager says its state is
// complete.
func read(rw io.ReadWriter) ([]Head, error) {
	if err := send(rw, 1, 1, u32(registry)); err != nil { // get_registry
		return nil, err
	}
	if err := send(rw, 1, 0, u32(callback)); err != nil { // sync
		return nil, err
	}
	var name, version uint32
	for listed := false; !listed; {
		obj, op, p, err := recv(rw)
		if err != nil {
			return nil, fmt.Errorf("wlroutput: the Wayland registry: %w", err)
		}
		switch {
		case obj == 1 && op == 0:
			return nil, protocolError(p)
		case obj == registry && op == 0: // global(name, interface, version)
			n := p.u32()
			if iface := p.str(); iface == interfaceName {
				name, version = n, p.u32()
			}
		case obj == callback && op == 0:
			listed = true
		}
	}
	if version == 0 {
		return nil, ErrNoManager
	}
	// make, model and the serial number came with version 2.
	if err := send(rw, registry, 0, u32(name), str(interfaceName), u32(min(version, 2)), u32(manager)); err != nil {
		return nil, err
	}

	heads := map[uint32]*head{}
	modes := map[uint32]*Mode{}
	var order []uint32
	for {
		obj, op, p, err := recv(rw)
		if err != nil {
			return nil, fmt.Errorf("wlroutput: the displays: %w", err)
		}
		switch {
		case obj == 1 && op == 0:
			return nil, protocolError(p)
		case obj == manager && op == 0: // head
			id := p.u32()
			heads[id] = &head{Head: Head{Scale: 1}}
			order = append(order, id)
		case obj == manager && op == 1: // done
			out := make([]Head, 0, len(order))
			for _, id := range order {
				h := heads[id]
				for _, m := range h.modes {
					h.Modes = append(h.Modes, *modes[m])
				}
				if m := modes[h.current]; m != nil && h.Enabled {
					current := *m
					h.Current = &current
				}
				out = append(out, h.Head)
			}
			return out, nil
		case obj == manager && op == 2: // finished
			return nil, errors.New("wlroutput: the compositor ended the listing")
		case heads[obj] != nil:
			h := heads[obj]
			switch op {
			case 0:
				h.Name = p.str()
			case 1:
				h.Description = p.str()
			case 3: // mode
				id := p.u32()
				modes[id] = &Mode{}
				h.modes = append(h.modes, id)
			case 4:
				h.Enabled = p.u32() != 0
			case 5:
				h.current = p.u32()
			case 6:
				h.X, h.Y = int(int32(p.u32())), int(int32(p.u32()))
			case 7:
				h.Transform = int(p.u32())
			case 8: // wl_fixed
				h.Scale = float64(int32(p.u32())) / 256
			case 10:
				h.Make = p.str()
			case 11:
				h.Model = p.str()
			}
		case modes[obj] != nil:
			m := modes[obj]
			switch op {
			case 0:
				m.Width, m.Height = int(int32(p.u32())), int(int32(p.u32()))
			case 1:
				m.Refresh = int(int32(p.u32()))
			case 2:
				m.Preferred = true
			}
		}
	}
}

func protocolError(p args) error {
	obj, code := p.u32(), p.u32()
	return fmt.Errorf("wlroutput: Wayland protocol error %d on object %d: %s", code, obj, p.str())
}

// args is a message's arguments, read in order; a short message gives
// zeros.
type args []byte

func (a *args) u32() uint32 {
	if len(*a) < 4 {
		*a = nil
		return 0
	}
	v := binary.NativeEndian.Uint32(*a)
	*a = (*a)[4:]
	return v
}

func (a *args) str() string {
	n := int(a.u32())
	padded := (n + 3) &^ 3
	if n < 1 || padded > len(*a) {
		*a = nil
		return ""
	}
	s := string((*a)[:n-1]) // without the NUL
	*a = (*a)[padded:]
	return s
}

func u32(v uint32) []byte { return binary.NativeEndian.AppendUint32(nil, v) }

func str(s string) []byte {
	b := u32(uint32(len(s) + 1))
	b = append(b, s...)
	return append(b, make([]byte, 4-len(s)%4)...) // the NUL and the padding
}

func send(w io.Writer, obj uint32, op uint16, parts ...[]byte) error {
	msg := make([]byte, 8)
	for _, p := range parts {
		msg = append(msg, p...)
	}
	binary.NativeEndian.PutUint32(msg, obj)
	binary.NativeEndian.PutUint32(msg[4:], uint32(len(msg))<<16|uint32(op))
	_, err := w.Write(msg)
	return err
}

func recv(r io.Reader) (obj uint32, op uint16, p args, err error) {
	hdr := make([]byte, 8)
	if _, err = io.ReadFull(r, hdr); err != nil {
		return 0, 0, nil, err
	}
	obj = binary.NativeEndian.Uint32(hdr)
	word := binary.NativeEndian.Uint32(hdr[4:])
	size := int(word >> 16)
	if size < 8 {
		return 0, 0, nil, fmt.Errorf("wlroutput: a message of %d bytes", size)
	}
	p = make([]byte, size-8)
	_, err = io.ReadFull(r, p)
	return obj, uint16(word), p, err
}
