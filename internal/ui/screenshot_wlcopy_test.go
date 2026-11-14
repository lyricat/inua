//go:build linux

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeWlCopy puts a wl-copy first on PATH that records its arguments, its
// standard input and the file its output goes to in the returned directory,
// prints fail to its standard error and exits 1 when fail is not empty.
// HOME and the XDG directories are throwaway ones; XDG_RUNTIME_DIR is set
// to runtime ("" unsets it).
func fakeWlCopy(t *testing.T, tmpdir, runtime, fail string) string {
	t.Helper()
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("no /proc to read wl-copy's output file from")
	}
	root := t.TempDir()
	bin, log := filepath.Join(root, "bin"), filepath.Join(root, "log")
	for _, d := range []string{bin, log} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\n" +
		"out=$(readlink /proc/$$/fd/1)\n" +
		"echo \"$out\" > " + filepath.Join(log, "out") + "\n" +
		"printf '%s\\n' \"$@\" > " + filepath.Join(log, "args") + "\n" +
		"cat > " + filepath.Join(log, "stdin") + "\n"
	if fail != "" {
		script += "echo '" + fail + "' >&2\nexit 1\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "wl-copy"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("TMPDIR", tmpdir)
	if runtime == "" {
		t.Setenv("XDG_RUNTIME_DIR", "") // restored after the test
		os.Unsetenv("XDG_RUNTIME_DIR")
	} else {
		t.Setenv("XDG_RUNTIME_DIR", runtime)
	}
	return log
}

func readLog(t *testing.T, log, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(log, name))
	if err != nil {
		t.Fatalf("wl-copy left no %s: %v", name, err)
	}
	return strings.TrimSpace(string(b))
}

// noOutputLeft fails when an output file of wlCopy's is still in dir.
func noOutputLeft(t *testing.T, dir string) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(dir, "inua-wl-copy-*"))
	if len(left) > 0 {
		t.Errorf("output files left behind: %q", left)
	}
}

// wl-copy's output goes to a file in TMPDIR, not a pipe its clipboard
// process would hold, and the file is gone afterwards.
func TestWlCopyUsesTempDir(t *testing.T) {
	tmp, runtime := t.TempDir(), t.TempDir()
	log := fakeWlCopy(t, tmp, runtime, "")
	img := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(img, []byte("png bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyImage(img); err != nil {
		t.Fatal(err)
	}
	if out := readLog(t, log, "out"); filepath.Dir(out) != tmp || !strings.HasPrefix(filepath.Base(out), "inua-wl-copy-") {
		t.Errorf("output went to %q, want a file in %s", out, tmp)
	}
	if got := readLog(t, log, "args"); got != "--type\nimage/png" {
		t.Errorf("arguments %q", got)
	}
	if got := readLog(t, log, "stdin"); got != "png bytes" {
		t.Errorf("standard input %q", got)
	}
	noOutputLeft(t, tmp)
	noOutputLeft(t, runtime)
}

// A TMPDIR that has gone sends the output to XDG_RUNTIME_DIR.
func TestWlCopyGoneTempDirUsesRuntimeDir(t *testing.T) {
	gone, runtime := filepath.Join(t.TempDir(), "gone"), t.TempDir()
	log := fakeWlCopy(t, gone, runtime, "")
	if err := copyText("hello"); err != nil {
		t.Fatal(err)
	}
	if out := readLog(t, log, "out"); filepath.Dir(out) != runtime {
		t.Errorf("output went to %q, want a file in %s", out, runtime)
	}
	if got := readLog(t, log, "args"); got != "--\nhello" {
		t.Errorf("arguments %q", got)
	}
	noOutputLeft(t, runtime)
}

// Without XDG_RUNTIME_DIR either, the output goes to /tmp.
func TestWlCopyGoneTempDirNoRuntimeUsesTmp(t *testing.T) {
	probe, err := os.CreateTemp("/tmp", "inua-wl-copy-probe-*")
	if err != nil {
		t.Skip("/tmp is not writable here")
	}
	probe.Close()
	os.Remove(probe.Name())
	gone := filepath.Join(t.TempDir(), "gone")
	log := fakeWlCopy(t, gone, "", "")
	if err := copyText("hello"); err != nil {
		t.Fatal(err)
	}
	out := readLog(t, log, "out")
	if filepath.Dir(out) != "/tmp" || !strings.HasPrefix(filepath.Base(out), "inua-wl-copy-") {
		t.Errorf("output went to %q, want a file in /tmp", out)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("%s left behind (%v)", out, err)
		os.Remove(out)
	}
}

// When the fallback cannot be written either, wl-copy is not run and the
// error says why both places failed.
func TestWlCopyFallbackFails(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	runtimeGone := filepath.Join(t.TempDir(), "runtime-gone")
	log := fakeWlCopy(t, gone, runtimeGone, "")
	err := copyText("hello")
	if err == nil {
		t.Fatal("copy succeeded without anywhere for wl-copy's output")
	}
	for _, want := range []string{"wl-copy", gone, runtimeGone} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(log, "args")); err == nil {
		t.Error("wl-copy ran anyway")
	}
}

// wl-copy failing reports what it printed, and its output file is gone
// all the same, in TMPDIR and in the fallback.
func TestWlCopyCommandFails(t *testing.T) {
	for _, c := range []struct {
		name    string
		tmpGone bool
	}{{"tempdir", false}, {"fallback", true}} {
		t.Run(c.name, func(t *testing.T) {
			tmp, runtime := t.TempDir(), t.TempDir()
			if c.tmpGone {
				tmp = filepath.Join(tmp, "gone")
			}
			fakeWlCopy(t, tmp, runtime, "Failed to connect to a Wayland server")
			err := copyText("hello")
			if err == nil {
				t.Fatal("a failing wl-copy reported no error")
			}
			if !strings.Contains(err.Error(), "Failed to connect to a Wayland server") {
				t.Errorf("error %q lacks wl-copy's message", err)
			}
			noOutputLeft(t, runtime)
			if !c.tmpGone {
				noOutputLeft(t, tmp)
			}
		})
	}
}
