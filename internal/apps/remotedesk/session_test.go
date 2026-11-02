package remotedesk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/hypr"
)

// overSSH is a sharing run from a shell outside the session: no display
// in its environment, and Hyprland running at runtime.
func overSSH(t *testing.T, s *Sharing, runtime string) {
	s.Getenv = func(k string) string { return map[string]string{"PATH": "/usr/bin"}[k] }
	s.Instance = func() (hypr.Instance, error) {
		return hypr.Instance{Signature: "abc_1800000000_1", Runtime: runtime, PID: os.Getpid(), Wayland: "wayland-1"}, nil
	}
}

func systemdRun(calls []string) string {
	for _, c := range calls {
		if strings.HasPrefix(c, "systemd-run") {
			return c
		}
	}
	return ""
}

func TestSharingOverSSHTakesTheSessionsDisplay(t *testing.T) {
	s, _, calls := newSharing(t)
	overSSH(t, s, "/run/user/1000")
	if st := s.Status(t.Context()); st.Need != NeedNothing {
		t.Errorf("a session is running: need = %v", st.Need)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	run := systemdRun(*calls)
	for _, want := range []string{"--setenv=WAYLAND_DISPLAY=wayland-1", "--setenv=XDG_RUNTIME_DIR=/run/user/1000",
		"--setenv=HYPRLAND_INSTANCE_SIGNATURE=abc_1800000000_1", "--setenv=XDG_SESSION_TYPE=wayland",
		"--setenv=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus"} {
		if !strings.Contains(run, want) {
			t.Errorf("no %q in %q", want, run)
		}
	}
}

func TestSharingWithoutASessionIsRefused(t *testing.T) {
	s, _, calls := newSharing(t)
	overSSH(t, s, "/run/user/1000")
	s.Instance = func() (hypr.Instance, error) { return hypr.Instance{}, hypr.ErrNoInstance }
	if st := s.Status(t.Context()); st.Need != NeedDisplay {
		t.Errorf("no session: need = %v, want NeedDisplay", st.Need)
	}
	if err := s.SetOn(t.Context(), true); !errors.Is(err, ErrNoSession) {
		t.Fatalf("SetOn = %v, want ErrNoSession", err)
	}
	if run := systemdRun(*calls); run != "" {
		t.Errorf("Sunshine started with nothing to capture: %q", run)
	}
}

func TestTheVirtualScreenSetting(t *testing.T) {
	s, _, _ := newSharing(t)
	if v := s.Virtual(); v != "" {
		t.Errorf("by default: %q, want off", v)
	}
	if err := s.SetVirtual("2560x1440@60"); err != nil {
		t.Fatal(err)
	}
	if v := s.Virtual(); v != "2560x1440@60" {
		t.Errorf("Virtual = %q", v)
	}
	if err := s.SetVirtual("big"); err == nil {
		t.Error("a mode that is not one was kept")
	}
	if err := s.SetVirtual(""); err != nil || s.Virtual() != "" {
		t.Errorf("off: %v, %q", err, s.Virtual())
	}
}

// fakeHypr answers Hyprland's request socket for the instance at
// runtime, and records what it was asked.
type fakeHypr struct {
	mu       sync.Mutex
	monitors string
	asked    []string
}

func serveHypr(t *testing.T, runtime, sig string, f *fakeHypr) {
	dir := filepath.Join(runtime, "hypr", sig)
	os.MkdirAll(dir, 0o700)
	ln, err := net.Listen("unix", filepath.Join(dir, ".socket.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 4096)
			n, _ := conn.Read(buf)
			cmd := string(buf[:n])
			f.mu.Lock()
			f.asked = append(f.asked, cmd)
			reply := "ok"
			if cmd == "j/monitors all" {
				reply = f.monitors
			}
			f.mu.Unlock()
			io.WriteString(conn, reply)
			conn.Close()
		}
	}()
}

func TestSyncVirtualMakesTheScreenAndRestartsSunshine(t *testing.T) {
	// A short path: a unix socket's must fit in 108 bytes.
	runtime, err := os.MkdirTemp("", "hy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtime) })
	f := &fakeHypr{monitors: `[]`}
	serveHypr(t, runtime, "abc_1800000000_1", f)

	s, _, calls := newSharing(t)
	overSSH(t, s, runtime)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVirtual("1920x1080@60"); err != nil {
		t.Fatal(err)
	}
	before := len(*calls)
	if err := s.SyncVirtual(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	asked := slices.Clone(f.asked)
	f.mu.Unlock()
	if !slices.Contains(asked, "output create headless "+hypr.VirtualScreen) {
		t.Errorf("no virtual screen made: asked %q", asked)
	}
	after := (*calls)[before:]
	stopped := slices.ContainsFunc(after, func(c string) bool { return c == "systemctl --user stop "+Unit })
	if !stopped || systemdRun(after) == "" {
		t.Errorf("Sunshine not started again for the new screen: %q", after)
	}

	// Nothing to change: Sunshine is left alone.
	f.mu.Lock()
	f.monitors = `[{"name":"INUA-VIRTUAL","width":1920,"height":1080,"refreshRate":60}]`
	f.mu.Unlock()
	before = len(*calls)
	if err := s.SyncVirtual(t.Context()); err != nil {
		t.Fatal(err)
	}
	if systemdRun((*calls)[before:]) != "" {
		t.Errorf("Sunshine restarted with nothing changed: %q", (*calls)[before:])
	}
}

func TestSyncVirtualWithoutASession(t *testing.T) {
	s, _, _ := newSharing(t)
	overSSH(t, s, "/run/user/1000")
	s.Instance = func() (hypr.Instance, error) { return hypr.Instance{}, hypr.ErrNoInstance }
	if err := s.SyncVirtual(t.Context()); !errors.Is(err, ErrNoSession) {
		t.Fatalf("SyncVirtual = %v, want ErrNoSession", err)
	}
}

func TestVirtualAtStartWaitsForTheSessionsHyprland(t *testing.T) {
	runtime, err := os.MkdirTemp("", "hy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtime) })
	s, _, _ := newSharing(t)
	s.Getenv = func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": runtime}[k] }
	if err := s.VirtualAtStart(t.Context(), os.Getppid()); err != nil {
		t.Fatalf("setting off: %v", err)
	}
	if err := s.SetVirtual("1920x1080@60"); err != nil {
		t.Fatal(err)
	}
	// The parent's Hyprland (this process, its child) starts a little later.
	f := &fakeHypr{monitors: `[]`}
	sig := "abc_1800000000_1"
	go func() {
		time.Sleep(300 * time.Millisecond)
		serveHypr(t, runtime, sig, f)
		os.WriteFile(filepath.Join(runtime, "hypr", sig, "hyprland.lock"), []byte(fmt.Sprintf("%d\nwayland-1\n", os.Getpid())), 0o600)
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := s.VirtualAtStart(ctx, os.Getppid()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.asked, "output create headless "+hypr.VirtualScreen) {
		t.Errorf("no virtual screen made: asked %q", f.asked)
	}
}

func TestVirtualAtStartGivesUp(t *testing.T) {
	s, _, _ := newSharing(t)
	s.Getenv = func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": t.TempDir()}[k] }
	if err := s.SetVirtual("1920x1080@60"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 600*time.Millisecond)
	defer cancel()
	if err := s.VirtualAtStart(ctx, os.Getppid()); err == nil {
		t.Fatal("no Hyprland started, and no error")
	}
}

// A session that keeps its compositor in a runtime directory of its own (a
// headless Wayfire, say): Sunshine still gets the screen, by its absolute
// path, and the user manager's directory, where its bus is.
func TestASessionWithItsOwnRuntimeDirectory(t *testing.T) {
	s, _, calls := newSharing(t)
	runtime := t.TempDir()
	s.Getenv = func(k string) string {
		return map[string]string{"WAYLAND_DISPLAY": "wayland-1", "XDG_RUNTIME_DIR": runtime, "PATH": "/usr/bin"}[k]
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	run := systemdRun(*calls)
	for _, want := range []string{"--setenv=WAYLAND_DISPLAY=" + filepath.Join(runtime, "wayland-1"),
		"--setenv=XDG_RUNTIME_DIR=" + userRuntimeDir(),
		"--setenv=DBUS_SESSION_BUS_ADDRESS=unix:path=" + filepath.Join(userRuntimeDir(), "bus")} {
		if !strings.Contains(run, want) {
			t.Errorf("no %q in %q", want, run)
		}
	}
}

// systemctl and systemd-run address the user manager, not the session's
// private runtime directory.
func TestSystemdIsAddressedThroughTheUserManagersDirectory(t *testing.T) {
	s, _, calls := newSharing(t)
	s.Getenv = func(k string) string {
		return map[string]string{"WAYLAND_DISPLAY": "wayland-1", "XDG_RUNTIME_DIR": t.TempDir(), "PATH": "/usr/bin"}[k]
	}
	var used []string
	s.RunSystemd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		used = append(used, name+" "+strings.Join(args, " "))
		if name == "systemctl" && args[1] == "is-active" {
			return []byte("active\n"), nil
		}
		return nil, nil
	}
	if st := s.Status(t.Context()); !st.Running || st.Need != NeedNothing {
		t.Errorf("status = %+v", st)
	}
	if !slices.Contains(used, "systemctl --user is-active "+Unit) {
		t.Errorf("asked %q", used)
	}
	for _, c := range *calls {
		if strings.HasPrefix(c, "systemctl ") || strings.HasPrefix(c, "systemd-run ") {
			t.Errorf("systemd called without the user manager's directory: %q", c)
		}
	}
}
