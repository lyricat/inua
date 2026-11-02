package desktop

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// LaunchWith starts the entry detached from this process: own session, no
// stdio. Terminal=true entries run in the terminal (Terminal, with the
// launcher's terminal). Window placement and focus are left to Hyprland.
func LaunchWith(e *Entry, terminal []string) error {
	if len(e.Command) == 0 {
		return errors.New(e.ID + ": no command")
	}
	argv := e.Command
	if e.RunInTerminal {
		term, dashE := Terminal(terminal)
		if dashE {
			term = append(term, "-e")
		}
		argv = append(term, argv...)
	}
	return Spawn(argv, e.WorkingDir)
}

// Spawn starts argv detached and reaps it in the background.
func Spawn(argv []string, dir string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = spawnEnv(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// spawnEnv is what a started app gets: this process's environment without
// LD_PRELOAD. A Wayfire session starts inua-shell with inua's layer-shell
// preload, which the shell needs and other programs do not; when they inherit
// it, some Wayland clients lose their own client-side decorations (kitty's
// titlebar, for one).
func spawnEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "LD_PRELOAD=") {
			out = append(out, kv)
		}
	}
	return out
}

// ReapInherited waits for the children this process already has when it
// starts: apps launched before `shell restart` re-executed it in place,
// whose waits ended with the old image. Without it they stay zombies.
func ReapInherited() {
	self := os.Getpid()
	dirs, _ := os.ReadDir("/proc")
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil || pid == self || parentOf(pid) != self {
			continue
		}
		go func() {
			var ws syscall.WaitStatus
			for {
				if _, err := syscall.Wait4(pid, &ws, 0, nil); err != syscall.EINTR {
					return
				}
			}
		}()
	}
}

// parentOf reads a process's parent from /proc/PID/stat, or -1.
func parentOf(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return -1
	}
	// The name in parentheses may hold spaces; the fields after it are
	// state, then the parent.
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return -1
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 2 {
		return -1
	}
	ppid, err := strconv.Atoi(f[1])
	if err != nil {
		return -1
	}
	return ppid
}
