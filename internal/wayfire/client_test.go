package wayfire

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

func fakeSocket(t *testing.T, response string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wayfire.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var header [4]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return
		}
		n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16 | int(header[3])<<24
		request := make([]byte, n)
		_, _ = io.ReadFull(conn, request)
		payload := []byte(response)
		frame := []byte{byte(len(payload)), byte(len(payload) >> 8), byte(len(payload) >> 16), byte(len(payload) >> 24)}
		_, _ = conn.Write(append(frame, payload...))
	}()
	return path
}

// fakeSockets serves one response per connection, in order.
func fakeSockets(t *testing.T, responses ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wayfire.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for _, response := range responses {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var header [4]byte
			if _, err := io.ReadFull(conn, header[:]); err != nil {
				conn.Close()
				return
			}
			n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16 | int(header[3])<<24
			_, _ = io.ReadFull(conn, make([]byte, n))
			payload := []byte(response)
			frame := []byte{byte(len(payload)), byte(len(payload) >> 8), byte(len(payload) >> 16), byte(len(payload) >> 24)}
			_, _ = conn.Write(append(frame, payload...))
			conn.Close()
		}
	}()
	return path
}

func TestRequestAndMethods(t *testing.T) {
	path := fakeSocket(t, `{"methods":["list-views","list-outputs"]}`)
	c := &Client{Socket: path, Timeout: time.Second}
	methods, err := c.Methods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 || methods[0] != "list-views" || methods[1] != "list-outputs" {
		t.Fatalf("methods = %#v", methods)
	}
	caps := c.Capabilities()
	assertUnavailableCapabilities(t, caps)
	if caps.Reasons["IPCMethodList"] != "" {
		t.Fatalf("method-list diagnostic remained unavailable: %+v", caps.Reasons)
	}
}

func TestMalformedReply(t *testing.T) {
	c := &Client{Socket: fakeSocket(t, "not-json"), Timeout: time.Second}
	_, err := c.Request(context.Background(), "list-methods", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "malformed JSON") {
		t.Fatalf("error = %v", err)
	}
}

func TestIPCErrorReply(t *testing.T) {
	c := &Client{Socket: fakeSocket(t, `{"error":"No such method found!"}`), Timeout: time.Second}
	_, err := c.Request(context.Background(), "list-views", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "No such method") {
		t.Fatalf("error = %v", err)
	}
}

func TestTruncatedReplyFrame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wayfire.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var header [4]byte
		if _, err := io.ReadFull(conn, header[:]); err == nil {
			_, _ = conn.Write([]byte{5, 0, 0, 0, '{'})
		}
	}()
	c := &Client{Socket: path, Timeout: time.Second}
	_, err = c.Request(context.Background(), "list-methods", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "response body") {
		t.Fatalf("error = %v", err)
	}
}

func TestOversizedReplyFrame(t *testing.T) {
	var header [4]byte
	for i := range header {
		header[i] = 0xff
	}
	if _, err := readFrame(strings.NewReader(string(header[:]))); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %v", err)
	}
}

func TestClosedSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
	c := &Client{Socket: path, Timeout: time.Second}
	_, err = c.Request(context.Background(), "list-methods", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected closed socket error")
	}
}

func TestMapState(t *testing.T) {
	snapshot := MapState(
		[]OutputState{{ID: 4, Name: "HEADLESS-1", Description: "Virtual output", Width: 1440, Height: 900, Scale: 1, Focused: true}},
		[]ViewState{{Handle: "view-1", Title: "Example", AppID: "example.app", Output: "HEADLESS-1", WorkspaceID: 4, Activated: true}},
		4, 3, 3,
	)
	if len(snapshot.Monitors) != 1 || snapshot.FocusedMonitor != "HEADLESS-1" {
		t.Fatalf("monitors/focus = %#v %q", snapshot.Monitors, snapshot.FocusedMonitor)
	}
	if len(snapshot.Workspaces) != 9 || !snapshot.Workspaces[4].Active {
		t.Fatalf("workspaces = %#v", snapshot.Workspaces)
	}
	if len(snapshot.Toplevels) != 1 || snapshot.Toplevels[0].Title != "Example" || snapshot.Toplevels[0].Class != "example.app" || snapshot.ActiveToplevel != "view-1" {
		t.Fatalf("toplevels = %#v active=%q", snapshot.Toplevels, snapshot.ActiveToplevel)
	}
	if snapshot.Toplevels[0].PID != 0 || snapshot.Toplevels[0].Monitor != "HEADLESS-1" {
		t.Fatalf("unexpected inferred toplevel fields: %+v", snapshot.Toplevels[0])
	}
}

func TestWindowsUseIPCViewIDs(t *testing.T) {
	path := fakeSockets(t,
		`[{"id":12,"pid":123,"title":"Terminal","app-id":"kitty","output-name":"HDMI-A-1","activated":true,"wset-index":1},{"id":7,"pid":124,"title":"Files","app-id":"files","output-name":"HDMI-A-2","activated":false,"wset-index":2},{"id":3,"pid":125,"title":"Layer","app-id":"inua","output-name":"HDMI-A-1","activated":false,"wset-index":-1}]`,
		`{"result":"ok","info":{"id":11,"name":"HDMI-A-2"}}`,
		`[{"id":1,"name":"HDMI-A-1","geometry":{"x":0,"y":0,"width":1280,"height":1024},"workspace":{"x":0,"y":0,"grid_width":3,"grid_height":3}},{"id":11,"name":"HDMI-A-2","geometry":{"x":1280,"y":0,"width":1920,"height":1080},"workspace":{"x":0,"y":0,"grid_width":3,"grid_height":3}}]`,
		`{"result":"ok","value":"1.000000"}`,
		`{"result":"ok","value":"2.000000"}`,
	)
	c := &Client{Socket: path, Timeout: time.Second}
	snapshot, err := c.Windows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveToplevel != "wf:12" || len(snapshot.Toplevels) != 3 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	first := snapshot.Toplevels[0]
	if first.Address != "wf:12" || first.Class != "kitty" || first.PID != 123 || first.Monitor != "HDMI-A-1" || !first.Activated {
		t.Fatalf("first window = %+v", first)
	}
	if snapshot.FocusedMonitor != "HDMI-A-2" || len(snapshot.Monitors) != 2 || snapshot.Monitors[1].Scale != 2 {
		t.Fatalf("monitors = %#v focused=%q", snapshot.Monitors, snapshot.FocusedMonitor)
	}

	path = fakeSocket(t, `[{"id":12,"title":"Terminal","app-id":"kitty","activated":false},{"id":7,"title":"Files","app-id":"files","activated":true}]`)
	c = &Client{Socket: path, Timeout: time.Second}
	if app, err := c.FocusedAppID(context.Background()); err != nil || app != "files" {
		t.Fatalf("FocusedAppID = %q, %v", app, err)
	}
}

func TestMethodEnumerationFailureIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sock")
	c := &Client{Socket: path, Timeout: time.Second}
	if _, err := c.Methods(context.Background()); err == nil {
		t.Fatal("expected method enumeration failure")
	}
	if got := c.Capabilities().Reasons["Connection"]; got == "" {
		t.Fatal("connection failure was not retained in capability status")
	}
}

func TestCapabilitiesRemainExplicitlyUnavailable(t *testing.T) {
	c := &Client{}
	caps := c.Capabilities()
	if caps.Reasons["IPCMethodList"] == "" {
		t.Fatalf("missing method-list diagnostic before enumeration: %+v", caps.Reasons)
	}
	assertUnavailableCapabilities(t, caps)
	c.mu.Lock()
	c.methods = map[string]bool{"some/unrelated-method": true}
	c.mu.Unlock()
	caps = c.Capabilities()
	assertUnavailableCapabilities(t, caps)
	if caps.Reasons["IPCMethodList"] != "" {
		t.Fatalf("method-list diagnostic remained unavailable after enumeration: %+v", caps.Reasons)
	}
}

func assertUnavailableCapabilities(t *testing.T, caps compositor.Capabilities) {
	t.Helper()
	if caps.StateQuery || caps.StateEvents || caps.FocusWindow || caps.MoveWindow || caps.ResizeWindow || caps.CloseWindow || caps.SwitchWorkspace || caps.CreateWorkspace || caps.ShortcutListing || caps.WindowCapture || caps.SessionLock {
		t.Fatalf("unimplemented capability reported as supported: %+v", caps)
	}
	if caps.Reasons["StateQuery"] == "" || caps.Reasons["WindowCapture"] == "" {
		t.Fatalf("missing capability explanation: %+v", caps.Reasons)
	}
}

func TestSnapshotMappingSeam(t *testing.T) {
	c := &Client{}
	if _, err := c.Snapshot(context.Background()); err == nil {
		t.Fatal("expected unavailable snapshot")
	}
	c.SetSnapshot(compositor.Snapshot{Monitors: []compositor.Monitor{{Name: "output"}}})
	got, err := c.Snapshot(context.Background())
	if err != nil || len(got.Monitors) != 1 || got.Monitors[0].Name != "output" {
		t.Fatalf("snapshot = %+v, error = %v", got, err)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("WAYFIRE_SOCKET", "/tmp/wf-test.sock")
	c, err := FromEnv()
	if err != nil || c.Socket != "/tmp/wf-test.sock" {
		t.Fatalf("client=%+v err=%v", c, err)
	}
	t.Setenv("WAYFIRE_SOCKET", "")
	t.Setenv("_WAYFIRE_SOCKET", "/tmp/wf-private.sock")
	c, err = FromEnv()
	if err != nil || c.Socket != "/tmp/wf-private.sock" {
		t.Fatalf("private env socket client=%+v err=%v", c, err)
	}
	t.Setenv("_WAYFIRE_SOCKET", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected unset socket error")
	}
	_ = os.Unsetenv("WAYFIRE_SOCKET")
}
