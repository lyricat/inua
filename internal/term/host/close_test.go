package host

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/term/agent"
)

func TestCloseNeedsConfirmation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fg, pid int
		args    []string
		err     error
		want    bool
	}{
		{"idle shell", 10, 10, []string{"/bin/bash"}, nil, false},
		{"login shell", 10, 10, []string{"-zsh"}, nil, false},
		{"foreground program", 11, 10, []string{"bash"}, nil, true},
		{"direct program", 10, 10, []string{"sleep", "60"}, nil, true},
		{"shell command", 10, 10, []string{"sh", "-c", "sleep 60"}, nil, true},
		{"inspection failed", 0, 10, nil, errors.New("denied"), true},
		{"arguments unavailable", 10, 10, nil, nil, true},
		{"invalid foreground", 0, 10, []string{"sh"}, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := closeNeedsConfirmation(tt.fg, tt.pid, tt.args, tt.err); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestRepeatedCloseRequestsPreserveProgram(t *testing.T) {
	h, path := startHost(t)
	c := dial(t, path, newWatcher())
	id, err := c.Create([]string{"sleep", "60"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := c.ClosePane(id); !errors.Is(err, ErrCloseConfirmation) {
			t.Fatalf("close: %v", err)
		}
		if h.pane(id) == nil {
			t.Fatal("unconfirmed request removed pane")
		}
	}
	// Cancel sends no confirmation. The program must still be alive.
	process, err := os.FindProcess(h.pane(id).pty.Pid())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("program after cancel: %v", err)
	}
	// The pane must still accept input.
	if err := c.Input(id, []byte{}); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfirmClosePane(id); err != nil {
		t.Fatal(err)
	}
	if h.pane(id) != nil {
		t.Fatal("confirmed close retained pane")
	}
}

func TestCloseCurrentProcessState(t *testing.T) {
	h, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	id, err := c.Create([]string{"sh", "-i"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Attach(id); err != nil {
		t.Fatal(err)
	}
	if err := c.Input(id, []byte("sleep 60\n")); err != nil {
		t.Fatal(err)
	}
	p := h.pane(id)
	w.waitFor(t, "foreground child", func() bool { fg, err := p.pty.Foreground(); return err == nil && fg != p.pty.Pid() })
	if err := c.ClosePane(id); !errors.Is(err, ErrCloseConfirmation) {
		t.Fatalf("running child: %v", err)
	}
	if err := c.Input(id, []byte{3}); err != nil {
		t.Fatal(err)
	}
	w.waitFor(t, "idle shell", func() bool { fg, err := p.pty.Foreground(); return err == nil && fg == p.pty.Pid() })
	if err := c.ClosePane(id); err != nil {
		t.Fatalf("idle shell: %v", err)
	}
}

func TestClosePreservesAgentWarnings(t *testing.T) {
	for _, state := range []agent.State{agent.Working, agent.Blocked} {
		t.Run(string(state), func(t *testing.T) {
			h, path := startHost(t)
			c := dial(t, path, newWatcher())
			id, err := c.Create([]string{"sh", "-i"}, "", nil, 80, 24)
			if err != nil {
				t.Fatal(err)
			}
			p := h.pane(id)
			p.mu.Lock()
			p.agent.m = &agent.Manifest{Type: "test", Name: "Test"}
			p.agent.t.Set(state, "question", time.Now())
			p.mu.Unlock()
			if err := c.ClosePane(id); !errors.Is(err, ErrCloseConfirmation) {
				t.Fatalf("agent: %v", err)
			}
		})
	}
}

func TestCloseChecksExecReplacement(t *testing.T) {
	h, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	id, err := c.Create([]string{"sh", "-i"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	p := h.pane(id)
	if err := c.Input(id, []byte("exec sleep 60\n")); err != nil {
		t.Fatal(err)
	}
	w.waitFor(t, "exec replacement", func() bool { args := procArgs(p.pty.Pid()); return len(args) > 0 && args[0] == "sleep" })
	if err := c.ClosePane(id); !errors.Is(err, ErrCloseConfirmation) {
		t.Fatalf("exec replacement: %v", err)
	}
}
