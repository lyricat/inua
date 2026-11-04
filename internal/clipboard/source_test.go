package clipboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/proc"
)

// pass -c: the job holds pass and the wl-copy serving the password. The
// selection comes from pass, and pass listed as sensitive keeps it out.
func TestFindSource(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pass"), []byte("sleep 30; :\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	job := exec.Command("bash", "-c", "bash "+filepath.Join(dir, "pass")+" & (exec -a wl-copy sleep 30) & wait")
	job.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := job.Start(); err != nil {
		t.Fatal(err)
	}
	defer job.Wait()
	defer syscall.Kill(-job.Process.Pid, syscall.SIGKILL)
	// Only the job's processes: a wl-copy elsewhere on this machine would
	// be taken for the source.
	var procs []int
	for range 100 {
		procs = slices.DeleteFunc(proc.All(), func(p int) bool { return proc.Group(p) != job.Process.Pid })
		if slices.ContainsFunc(procs, func(p int) bool { return proc.Name(p) == "wl-copy" }) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	w := Window{Class: "com.mitchellh.ghostty", PID: os.Getpid()}
	s := findSource(w, []string{"keepassxc", " PASS "}, procs)
	if s.Name != "pass" || !s.Sensitive || !slices.Contains(s.Names, "pass") || !slices.Contains(s.Names, "com.mitchellh.ghostty") {
		t.Errorf("pass -c: %+v", s)
	}
	syscall.Kill(-job.Process.Pid, syscall.SIGKILL)
	job.Wait()
	// A copy in a window: the window, kept unless its class is listed.
	s = findSource(Window{Class: "firefox"}, []string{"keepassxc", "pass"}, nil)
	if s.Name != "firefox" || s.Sensitive {
		t.Errorf("firefox: %+v", s)
	}
	if s := findSource(Window{Class: "org.keepassxc.KeePassXC"}, []string{"org.keepassxc.keepassxc"}, nil); !s.Sensitive {
		t.Errorf("a listed window class: %+v", s)
	}
}

func TestPrimary(t *testing.T) {
	for args, want := range map[string]bool{"wl-copy": false, "wl-copy --primary": true, "wl-copy -p": true, "wl-copy -np": true, "wl-copy --paste-once": false, "wl-copy -n": false} {
		if primary(strings.Fields(args)) != want {
			t.Errorf("%s: want %v", args, want)
		}
	}
}

// A selection from a sensitive source is not kept, and the first
// selection skipped after recording starts is still the one skipped.
func TestCaptureSensitiveSource(t *testing.T) {
	t.Setenv("CLIPBOARD_STATE", "data")
	path := filepath.Join(t.TempDir(), "history.json")
	if err := SkipFirst(path); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		text string
		from Source
	}{
		{"on the clipboard before", Source{}},
		{"hunter2", Source{Name: "pass", Sensitive: true}},
		{"a link", Source{Name: "firefox"}},
	} {
		if err := Capture(path, DefaultLimit, c.from, strings.NewReader(c.text)); err != nil {
			t.Fatal(err)
		}
	}
	if entries, _ := Read(path); len(entries) != 1 || entries[0].Text != "a link" || entries[0].Source != "firefox" {
		t.Errorf("%+v", entries)
	}
}

func TestMarkedSensitive(t *testing.T) {
	if !MarkedSensitive([]string{"text/plain;charset=utf-8", "text/plain", "x-kde-passwordManagerHint"}) {
		t.Error("the hint missed")
	}
	if MarkedSensitive([]string{"text/plain", "UTF8_STRING"}) || MarkedSensitive(nil) {
		t.Error("plain text taken for a secret")
	}
}
