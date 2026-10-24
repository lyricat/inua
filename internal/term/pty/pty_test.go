package pty

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// output reads everything the program writes until it closes the terminal.
func output(t *testing.T, p *PTY) string {
	t.Helper()
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(p)
		done <- b
	}()
	select {
	case b := <-done:
		return string(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
	case <-time.After(5 * time.Second):
		t.Fatal("the program did not finish")
		return ""
	}
}

func TestStart(t *testing.T) {
	dir := t.TempDir()
	p, err := Start([]string{"sh", "-c", "stty size; pwd; echo $TERM; tty; exit 3"}, dir, append(os.Environ(), "TERM=xterm-256color"), 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	got := output(t, p)
	code, err := p.Wait()
	if err != nil || code != 3 {
		t.Errorf("exit %d, %v", code, err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	real, _ := filepath.EvalSymlinks(dir) // macOS's temporary folders are behind /var → /private/var
	tty := "/dev/pts/"
	if runtime.GOOS == "darwin" {
		tty = "/dev/ttys"
	}
	if len(lines) != 4 || lines[0] != "30 100" || (lines[1] != dir && lines[1] != real) || lines[2] != "xterm-256color" || !strings.HasPrefix(lines[3], tty) {
		t.Errorf("output %q", got)
	}
	p.Close()
}

// The terminal controls its program and what it runs, not a process that
// left it (setsid) nor one outside. The one that leaves is this test
// binary again, TestHelperDetach: macOS has no setsid command.
func TestControls(t *testing.T) {
	left := filepath.Join(t.TempDir(), "left")
	p, err := Start([]string{"sh", "-c", `sleep 5 & "$0" -test.run=TestHelperDetach -test.count=1 & echo $! ready; read x`, os.Args[0]},
		t.TempDir(), append(os.Environ(), "INUA_PTY_DETACH="+left), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	buf := make([]byte, 256)
	var out string
	for !strings.Contains(out, "ready") {
		n, err := p.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		out += string(buf[:n])
	}
	detached, _ := strconv.Atoi(strings.Fields(out)[0])
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(left); err == nil {
			break // setsid has run
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper did not leave the terminal")
		}
	}
	if !p.Controls(p.Pid()) {
		t.Error("not its program's terminal")
	}
	if p.Controls(detached) {
		t.Error("the terminal of a process that left it")
	}
	if p.Controls(os.Getpid()) {
		t.Error("the terminal of a process outside")
	}
	syscall.Kill(detached, syscall.SIGKILL)
}

// TestHelperDetach leaves its terminal for TestControls, says so in the
// file INUA_PTY_DETACH, and waits to be killed.
func TestHelperDetach(t *testing.T) {
	left := os.Getenv("INUA_PTY_DETACH")
	if left == "" {
		t.Skip("run by TestControls")
	}
	if _, err := syscall.Setsid(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(left, nil, 0o644)
	time.Sleep(5 * time.Second)
}

// The program sees the new size (and gets SIGWINCH).
func TestResize(t *testing.T) {
	p, err := Start([]string{"sh", "-c", "trap 'stty size; exit' WINCH; echo ready; while :; do sleep 0.05; done"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	buf := make([]byte, 64)
	n, _ := p.Read(buf)
	if !strings.Contains(string(buf[:n]), "ready") {
		t.Fatalf("got %q", buf[:n])
	}
	if err := p.Resize(132, 43); err != nil {
		t.Fatal(err)
	}
	if got := output(t, p); !strings.Contains(got, "43 132") {
		t.Errorf("after resize: %q", got)
	}
	p.Wait()
}

// The foreground group is the shell's, then the program it runs; echo is
// off while a program reads a password.
func TestForegroundAndEcho(t *testing.T) {
	p, err := Start([]string{"sh", "-c", "echo shell; read x; stty -echo; echo hidden; read y"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	buf := make([]byte, 64)
	var out string
	for !strings.Contains(out, "shell") {
		n, err := p.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		out += string(buf[:n])
	}
	if fg, err := p.Foreground(); err != nil || fg != p.Pid() {
		t.Errorf("foreground %d (%v), want the shell %d", fg, err, p.Pid())
	}
	if on, err := p.Echoing(); err != nil || !on {
		t.Errorf("echoing %v (%v), want on", on, err)
	}
	p.Write([]byte("go\n"))
	deadline := time.Now().Add(3 * time.Second)
	for {
		on, err := p.Echoing()
		if err != nil {
			t.Fatal(err)
		}
		if !on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("echo never went off")
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.Write([]byte("secret\n"))
	p.Wait()
}

// Ctrl+C stops the program even when this process started with SIGINT
// ignored, as a background job of a non-interactive shell does. The
// helper below is this test binary run again from such a shell: Go's own
// signal.Ignore would not do, since Go ignores again in every child what
// it ignored itself.
func TestInterruptWhenIgnored(t *testing.T) {
	cmd := exec.Command("sh", "-c", `trap '' INT; exec "$0" -test.run=TestInterruptHelper -test.count=1`, os.Args[0])
	cmd.Env = append(os.Environ(), "INUA_PTY_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Ctrl+C did not stop the program: %v\n%s", err, out)
	}
}

func TestInterruptHelper(t *testing.T) {
	if os.Getenv("INUA_PTY_HELPER") == "" {
		t.Skip("run by TestInterruptWhenIgnored")
	}
	if !signal.Ignored(syscall.SIGINT) {
		t.Fatal("the helper was not started with SIGINT ignored")
	}
	p, err := Start([]string{"sh", "-c", "exec sleep 30"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// Ctrl+C once sleep runs: before, the shell would take it itself.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if procName(p.Pid()) == "sleep" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sleep did not start")
		}
	}
	p.Write([]byte{0x03}) // Ctrl+C
	done := make(chan int)
	go func() { code, _ := p.Wait(); done <- code }()
	select {
	case code := <-done:
		if code == 0 {
			t.Errorf("exit %d after Ctrl+C", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl+C did not stop the program")
	}
}

// Close hangs the program up even while a Read waits for its output.
func TestCloseWhileReading(t *testing.T) {
	p, err := Start([]string{"sleep", "30"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan error, 1)
	go func() {
		_, err := p.Read(make([]byte, 64))
		read <- err
	}()
	time.Sleep(100 * time.Millisecond)
	p.Close()
	select {
	case <-read:
	case <-time.After(3 * time.Second):
		t.Fatal("Read did not end")
	}
	done := make(chan struct{})
	go func() { p.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the program did not get SIGHUP")
	}
}

// procName is a process's program name: from /proc on Linux, from ps
// elsewhere.
func procName(pid int) string {
	if runtime.GOOS == "linux" {
		b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
		return strings.TrimSpace(string(b))
	}
	out, _ := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	return filepath.Base(strings.TrimSpace(string(out)))
}
