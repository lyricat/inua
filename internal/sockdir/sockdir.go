// Package sockdir gives a test a directory for its Unix sockets.
//
// A socket's path holds at most 107 bytes on Linux and 103 on macOS, and
// t.TempDir() is $TMPDIR/<the test's name><a number>/001: with a long
// TMPDIR (a CI run's own directory, macOS's /var/folders/…) or a long test
// name, binding a socket there fails with "invalid argument", and a
// dbus-daemon told to listen there exits at once.
package sockdir

import (
	"os"
	"testing"
)

// maxBase is the longest temporary directory a socket directory is made
// in: with "/inua" and MkdirTemp's number after it, a directory of at most
// 47 bytes, which leaves a socket 55 for its name on macOS.
const maxBase = 32

// New makes a directory with a short path, removed when the test ends. It
// is in the temporary directory, or in /tmp when that one's path is long.
func New(t testing.TB) string {
	t.Helper()
	base := os.TempDir()
	if len(base) > maxBase {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "inua")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("sockdir: %v", err)
		}
	})
	return dir
}
