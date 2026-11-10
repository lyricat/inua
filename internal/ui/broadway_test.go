//go:build linux

package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/sockdir"
)

// startBroadway starts a Broadway display for the test, with its own
// home, runtime and session bus, and skips the test without
// gtk4-broadwayd. The caller locks its OS thread for GTK.
func startBroadway(t *testing.T) {
	t.Helper()
	broadway, err := exec.LookPath("gtk4-broadwayd")
	if err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
	// Both display sockets and all application files belong to this test.
	dir := sockdir.New(t)
	for key, value := range map[string]string{
		"HOME": dir, "XDG_RUNTIME_DIR": dir,
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"),
		"XDG_DATA_HOME":   filepath.Join(dir, "data"),
		"XDG_CACHE_HOME":  filepath.Join(dir, "cache"),
		"XDG_STATE_HOME":  filepath.Join(dir, "state"),
		"GDK_BACKEND":     "broadway", "BROADWAY_DISPLAY": ":0",
		"GTK_A11Y": "none", "GTK_THEME": "", "LANGUAGE": "en",
		"DBUS_SESSION_BUS_ADDRESS": dbustest.Start(t),
		"DBUS_SYSTEM_BUS_ADDRESS":  "unix:path=" + filepath.Join(dir, "no-system-bus"),
	} {
		t.Setenv(key, value)
	}
	cmd := exec.Command(broadway, "--unixsocket="+filepath.Join(dir, "http.sock"), ":0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "broadway1.socket")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Broadway display did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
