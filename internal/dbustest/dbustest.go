// Package dbustest runs a private dbus-daemon for tests, so code that talks to
// D-Bus services can be tested against fakes without touching the real
// session or system bus.
package dbustest

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/sockdir"
)

// No service activation: a call to a missing name fails instead of starting
// something from /usr/share/dbus-1.
const config = `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:dir=%DIR%</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
`

// Start runs a dbus-daemon for the rest of the test and returns its address.
// The test is skipped when dbus-daemon is not installed.
func Start(t testing.TB) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	dir := sockdir.New(t) // the socket is in it
	conf := filepath.Join(dir, "bus.conf")
	if err := os.WriteFile(conf, []byte(strings.ReplaceAll(config, "%DIR%", dir)), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--config-file="+conf, "--print-address=1", "--nofork", "--nopidfile")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon: %v", err)
	}
	return strings.TrimSpace(addr)
}

// Conn opens a new connection to the bus at addr, closed when the test ends.
func Conn(t testing.TB, addr string) *dbus.Conn {
	t.Helper()
	conn, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// Dial opens a connection to the bus at addr; the caller closes it.
func Dial(addr string) (*dbus.Conn, error) {
	return dbus.Connect(addr)
}
