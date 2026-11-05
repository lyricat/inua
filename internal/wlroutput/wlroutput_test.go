package wlroutput

import (
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
	"time"
)

// compositor plays the server's side on a pipe: the registry with its
// globals, then, once the manager is bound, the events given.
func compositor(t *testing.T, globals map[string]uint32, events func(w io.Writer)) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	client.SetDeadline(time.Now().Add(5 * time.Second))
	go func() {
		for range 2 { // get_registry, sync
			if _, _, _, err := recv(server); err != nil {
				return
			}
		}
		name := uint32(1)
		for iface, version := range globals {
			send(server, registry, 0, u32(name), str(iface), u32(version))
			name++
		}
		send(server, callback, 0, u32(0))
		if events == nil {
			return
		}
		obj, op, p, err := recv(server) // bind
		if err != nil || obj != registry || op != 0 {
			t.Errorf("bind: object %d opcode %d: %v", obj, op, err)
			return
		}
		p.u32()
		if iface, version, id := p.str(), p.u32(), p.u32(); iface != interfaceName || version != 2 || id != manager {
			t.Errorf("bound %q version %d as %d", iface, version, id)
		}
		events(server)
	}()
	return client
}

// What a compositor sends for a 4K display at 150% and one that is off,
// in the protocol's own order.
func TestHeads(t *testing.T) {
	const head1, mode1, mode2, head2, mode3 = 0xff000000, 0xff000001, 0xff000002, 0xff000003, 0xff000004
	conn := compositor(t, map[string]uint32{"wl_compositor": 6, interfaceName: 4}, func(w io.Writer) {
		send(w, manager, 0, u32(head1))
		send(w, head1, 0, str("DP-1"))
		send(w, head1, 1, str("Dell Inc. DELL U2720QM GZF09H3 (DP-1)"))
		send(w, head1, 2, u32(600), u32(340)) // physical size, not kept
		send(w, head1, 3, u32(mode1))
		send(w, mode1, 0, u32(3840), u32(2160))
		send(w, mode1, 1, u32(59997))
		send(w, mode1, 2)
		send(w, head1, 3, u32(mode2))
		send(w, mode2, 0, u32(1920), u32(1080))
		send(w, mode2, 1, u32(60000))
		send(w, head1, 4, u32(1))
		send(w, head1, 5, u32(mode1))
		send(w, head1, 6, u32(0xfffff880), u32(0)) // -1920, 0
		send(w, head1, 7, u32(1))
		send(w, head1, 8, u32(384)) // 1.5 as wl_fixed
		send(w, head1, 10, str("Dell Inc."))
		send(w, head1, 11, str("DELL U2720QM"))
		send(w, head1, 12, str("GZF09H3"))
		send(w, manager, 0, u32(head2))
		send(w, head2, 0, str("eDP-1"))
		send(w, head2, 3, u32(mode3))
		send(w, mode3, 0, u32(2256), u32(1504))
		send(w, mode3, 1, u32(59999))
		send(w, head2, 4, u32(0))
		send(w, manager, 1, u32(7)) // done
	})
	got, err := read(conn)
	if err != nil {
		t.Fatal(err)
	}
	want := []Head{
		{Name: "DP-1", Description: "Dell Inc. DELL U2720QM GZF09H3 (DP-1)", Make: "Dell Inc.", Model: "DELL U2720QM", Enabled: true,
			Modes:   []Mode{{3840, 2160, 59997, true}, {1920, 1080, 60000, false}},
			Current: &Mode{3840, 2160, 59997, true}, X: -1920, Y: 0, Scale: 1.5, Transform: 1},
		{Name: "eDP-1", Modes: []Mode{{2256, 1504, 59999, false}}, Scale: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("heads\n got %+v\nwant %+v", got, want)
	}
}

func TestNoManager(t *testing.T) {
	conn := compositor(t, map[string]uint32{"wl_compositor": 6}, nil)
	if _, err := read(conn); !errors.Is(err, ErrNoManager) {
		t.Fatalf("a compositor without the protocol: %v", err)
	}
}

// A protocol error ends the read with what the compositor said.
func TestProtocolError(t *testing.T) {
	conn := compositor(t, map[string]uint32{interfaceName: 4}, func(w io.Writer) {
		send(w, 1, 0, u32(manager), u32(3), str("no such request"))
	})
	if _, err := read(conn); err == nil || errors.Is(err, ErrNoManager) {
		t.Fatalf("a protocol error: %v", err)
	}
}
