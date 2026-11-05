package sockdir

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With a temporary directory too deep for a socket, as a CI run's own can
// be, a socket still binds in the directory New makes.
func TestNewIsShortUnderALongTempDir(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", long)
	dir := New(t)
	if strings.HasPrefix(dir, long) {
		t.Fatalf("%s is in the long temporary directory", dir)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "a-socket-with-a-fairly-long-name.sock"))
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

// A short temporary directory is the one used: a sandbox's may be the only
// one a test can write.
func TestNewKeepsAShortTempDir(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "sd")
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	t.Setenv("TMPDIR", base)
	if dir := New(t); filepath.Dir(dir) != base {
		t.Fatalf("%s is not in %s", dir, base)
	}
}

func TestNewIsRemovedWithTheTest(t *testing.T) {
	var dir string
	t.Run("inner", func(t *testing.T) { dir = New(t) })
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("%s is still there: %v", dir, err)
	}
}
