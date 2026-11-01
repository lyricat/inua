package host

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/lyricat/inua/internal/term/agent"
)

// ErrCloseConfirmation means the pane was preserved and needs explicit consent.
var ErrCloseConfirmation = errors.New("closing this pane requires confirmation")

// closeNeedsConfirmation permits only a known interactive shell in the
// foreground. Unknown process state is never evidence that closing is safe.
// Background jobs in other process groups are not detected here.
func closeNeedsConfirmation(fg, pid int, args []string, err error) bool {
	if err != nil || fg <= 0 || fg != pid || len(args) == 0 {
		return true
	}
	switch strings.TrimPrefix(filepath.Base(args[0]), "-") {
	case "sh", "bash", "zsh", "fish", "dash", "ksh", "mksh", "ash":
	default:
		return true
	}
	for _, arg := range args[1:] {
		if arg != "-l" && arg != "--login" && arg != "-i" {
			return true
		}
	}
	return false
}

func (p *pane) closeNeedsConfirmation() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return false
	}
	fg, err := p.pty.Foreground()
	return closeNeedsConfirmation(fg, p.pty.Pid(), procArgs(p.pty.Pid()), err) ||
		(p.agent.m != nil && (p.agent.t.State == agent.Working || p.agent.t.State == agent.Blocked))
}
