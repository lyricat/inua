package wayfire

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
)

func TestWaylandClientCapabilitiesAndClose(t *testing.T) {
	w := NewWayland()
	w.views["wf:12"] = &view{id: "wf:12", outputs: map[string]bool{}}
	var object uint32
	var opcode uint16
	w.sendFunc = func(obj uint32, op uint16, _ ...[]byte) error { object, opcode = obj, op; return nil }
	caps := w.Capabilities()
	if !caps.StateQuery || !caps.CloseWindow || caps.FocusWindow || caps.MoveWindow || caps.ResizeWindow || caps.SwitchWorkspace || caps.WindowCapture {
		t.Fatalf("capabilities = %+v", caps)
	}
	if err := w.CloseWindow(t.Context(), "wf:12"); err != nil {
		t.Fatal(err)
	}
	if object != 12 || opcode != 5 {
		t.Fatalf("close request=(%d,%d), want (12,5)", object, opcode)
	}
	if err := w.CloseWindow(t.Context(), "wf:99"); err == nil {
		t.Fatal("expected missing handle error")
	}
}

func TestForeignToplevelEventStateMapping(t *testing.T) {
	w := NewWayland()
	w.manager = 4
	var sentObject uint32
	var sentOpcode uint16
	w.sendFunc = func(object uint32, opcode uint16, _ ...[]byte) error {
		sentObject, sentOpcode = object, opcode
		return nil
	}
	if err := w.handle(w.manager, 0, argU32(9)); err != nil {
		t.Fatal(err)
	}
	handle := uint32(9)
	if err := w.handle(handle, 0, waylandString("Window title")); err != nil {
		t.Fatal(err)
	}
	if err := w.handle(handle, 1, waylandString("example.app")); err != nil {
		t.Fatal(err)
	}
	state := make([]byte, 8)
	binary.NativeEndian.PutUint32(state, 1)
	binary.NativeEndian.PutUint32(state[4:], 2)
	if err := w.handle(handle, 4, state); err != nil {
		t.Fatal(err)
	}
	snapshot := w.currentSnapshot()
	if len(snapshot.Toplevels) != 1 {
		t.Fatalf("toplevel count = %d", len(snapshot.Toplevels))
	}
	window := snapshot.Toplevels[0]
	if window.Address != "wf:9" || window.Title != "Window title" || window.Class != "example.app" || !window.Activated || snapshot.ActiveToplevel != "wf:9" {
		t.Fatalf("window = %+v snapshot = %+v", window, snapshot)
	}
	if err := w.handle(handle, 6, nil); err != nil {
		t.Fatal(err)
	}
	if len(w.currentSnapshot().Toplevels) != 0 || sentObject != handle || sentOpcode != 7 {
		t.Fatalf("close mapping did not remove/destroy handle: sent=(%d,%d)", sentObject, sentOpcode)
	}
}

func TestRoundtripForwardsPendingEventPayload(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	w := NewWayland()
	w.conn = client
	w.manager = 4

	serverErr := make(chan error, 1)
	go func() {
		var request [12]byte
		if _, err := io.ReadFull(server, request[:]); err != nil {
			serverErr <- err
			return
		}
		// A toplevel event is pending when roundtrip sends its sync request.
		if _, err := server.Write(testWaylandEvent(4, 0, argU32(9))); err != nil {
			serverErr <- err
			return
		}
		if _, err := server.Write(testWaylandEvent(2, 0, nil)); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	if err := w.roundtrip(); err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	if got := w.currentSnapshot().Toplevels; len(got) != 1 || got[0].Address != "wf:9" {
		t.Fatalf("toplevels after roundtrip = %+v, want handle wf:9", got)
	}
}

func testWaylandEvent(object uint32, opcode uint16, payload []byte) []byte {
	message := make([]byte, 8+len(payload))
	binary.NativeEndian.PutUint32(message, object)
	binary.NativeEndian.PutUint32(message[4:], uint32(len(message))<<16|uint32(opcode))
	copy(message[8:], payload)
	return message
}

func waylandString(s string) []byte { return argString(s) }
