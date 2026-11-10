// Package asker tells who asks the user for a secret from the asking
// process itself, not from what it says: the shell's password dialog and
// the web terminal's pages show a question only from inua's own helpers
// (inua-askpass, inua-pinentry, inua-assistant), and name who asks, and
// for sudo the command it runs as root, from /proc. Any program of the
// user's can reach their sockets; without this it could ask as sudo, for
// a command of its choosing, and be given the password.
package asker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/proc"
)

// inua's programs that ask, and inua-termd, whose serve relays the pages.
const (
	Askpass   = "inua-askpass"
	Pinentry  = "inua-pinentry"
	Assistant = "inua-assistant"
	Termd     = "inua-termd"
)

// ElevateCommandEnv is Mister Morph's name for the command it runs as
// root (tools.bash.elevation.askpass), in the askpass helper's environment.
const ElevateCommandEnv = "MISTERMORPH_ELEVATE_COMMAND"

// checked is whether processes can be told apart here: on Linux, from
// /proc. Elsewhere questions are shown as they come.
var checked = runtime.GOOS == "linux"

// systemDirs are the system's own folders for programs, root's alone: an
// inua program there is installed (sudo's askpass, which sudo does not look
// for on PATH, often is).
var systemDirs = []string{"/usr/local/bin", "/usr/bin"}

// installed are the paths inua's program name may run from: beside the
// program asking, where PATH finds it, and the system's folders.
var installed = func(name string) []string {
	var paths []string
	for _, dir := range systemDirs {
		paths = append(paths, filepath.Join(dir, name))
	}
	if self, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(self), name))
	}
	if p, err := exec.LookPath(name); err == nil {
		if p, err = filepath.Abs(p); err == nil {
			paths = append(paths, p)
		}
	}
	for i, p := range paths {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			paths[i] = real
		}
	}
	return paths
}

// realSudo is whether a process named sudo is sudo: setuid, so root's.
var realSudo = proc.OwnedByRoot

// Is reports whether pid runs inua's program name, as installed; always
// where processes cannot be told apart.
func Is(pid int, name string) bool {
	if !checked {
		return true
	}
	// A program replaced since it started (inua installed again) is
	// "… (deleted)".
	exe := strings.TrimSuffix(proc.Exe(pid), " (deleted)")
	return exe != "" && filepath.Base(exe) == name && slices.Contains(installed(name), exe)
}

// Vouch makes q, sent by the process pid, say who really asks, or says
// why it is not shown. inua-askpass's question is made again from its own
// command line and the program that ran it; inua-pinentry's is gpg's as
// sent when gpg-agent ran it, else it names what did; inua-assistant only
// asks for approvals. Anything else is refused.
func Vouch(pid int, q *pinentry.Request) error {
	if !checked {
		return nil
	}
	switch {
	case Is(pid, Askpass):
		if q.Mode == pinentry.Approve {
			return fmt.Errorf("%s asks for no approvals", Askpass)
		}
		*q, _ = OfAskpass(pid)
	case Is(pid, Pinentry):
		if q.Mode == pinentry.Approve {
			return fmt.Errorf("%s asks for no approvals", Pinentry)
		}
		if parent := proc.Parent(pid); proc.Name(parent) != "gpg-agent" {
			// Not gpg's: what gpg would tell (the key, the pass entry, the
			// app inua-keyring serves) is only what the program said.
			q.Title, q.Keyring, q.Entry, q.Secret, q.Key, q.SecurityKey = "", false, "", "", nil, ""
			q.App, q.Chain = proc.Name(parent), proc.Chain(proc.Parent(parent), 8)
		}
	case Is(pid, Assistant):
		if q.Mode != pinentry.Approve {
			return fmt.Errorf("%s asks for approvals only", Assistant)
		}
	default:
		return fmt.Errorf("%s is none of inua's programs that ask", proc.Describe(pid))
	}
	return nil
}

// OfAskpass is the question of inua-askpass running as pid, from its
// command line and environment and the program that ran it.
func OfAskpass(pid int) (q pinentry.Request, yesNo bool) {
	var args []string
	if a := proc.Args(pid); len(a) > 1 {
		args = a[1:]
	}
	return Question(args, proc.Env(pid, "SSH_ASKPASS_PROMPT"), proc.Env(pid, ElevateCommandEnv), proc.Parent(pid))
}

// Question is what an askpass program asks: args are its arguments, the
// asker's words; kind is SSH_ASKPASS_PROMPT; parent the program that ran
// it (ssh, sudo, git), named with what it runs under; and for sudo the
// command it runs as root, as Mister Morph (morph) names it when that is
// what sudo runs. yesNo is set when the answer is to be typed "yes" (ssh
// asking whether to trust a host's key).
func Question(args []string, kind, morph string, parent int) (q pinentry.Request, yesNo bool) {
	prompt := strings.TrimSpace(strings.Join(args, " "))
	q.Desc = prompt
	lower := strings.ToLower(prompt)
	switch {
	case kind == "none":
		q.Mode = pinentry.Message
	case kind == "confirm":
		q.Mode, q.OK, q.Cancel = pinentry.Confirm, i18n.T("Yes"), i18n.T("No")
	case strings.Contains(lower, "(yes/no"):
		// "Are you sure you want to continue connecting
		// (yes/no/[fingerprint])?", asked as a secret.
		q.Mode, q.OK, q.Cancel = pinentry.Confirm, i18n.T("Yes"), i18n.T("No")
		yesNo = true
		if i := strings.LastIndex(lower, " (yes/no"); i > 0 { // the buttons say it
			q.Desc = prompt[:i] + "?"
		}
	case strings.HasPrefix(lower, "username"): // git's "Username for 'https://…':"
		q.Mode, q.Visible = pinentry.GetPin, true
		q.Title, q.Prompt = i18n.T("User Name Required"), i18n.T("User name")
	case strings.Contains(lower, "password"): // "user@host's password:", sudo's
		q.Mode = pinentry.GetPin
		q.Title, q.Prompt = i18n.T("Password Required"), i18n.T("Password")
	default: // "Enter passphrase for key '…':", "Enter PIN for …"
		q.Mode = pinentry.GetPin
	}
	if parent > 1 {
		q.App, q.Chain = proc.Name(parent), proc.Chain(proc.Parent(parent), 8)
		if cmd, ok := SudoCommand(q.App, proc.Args(parent)); ok && realSudo(parent) {
			asRoot(&q, shown(cmd, morph))
		}
	}
	return q, yesNo
}

// shown is sudo's command as the dialog shows it: Mister Morph's own
// words for it when sudo runs exactly them (bash -c …), else what sudo
// runs, whatever the environment says.
func shown(cmd []string, morph string) string {
	if morph != "" && len(cmd) == 3 && slices.Contains([]string{"bash", "sh"}, filepath.Base(cmd[0])) && cmd[1] == "-c" && cmd[2] == morph {
		return morph
	}
	return strings.Join(cmd, " ")
}

// sudoValued are sudo's options that take a value: -u USER, -g GROUP …
var sudoValued = "ugpChDrtUTR"

// SudoCommand is the command sudo (name, run with args) runs as root,
// without sudo's own options; ok when name is sudo and it runs one.
func SudoCommand(name string, args []string) ([]string, bool) {
	if (name != "sudo" && name != "sudo-rs") || len(args) < 2 {
		return nil, false
	}
	rest := args[1:]
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		opt := rest[0]
		rest = rest[1:]
		switch {
		case opt == "--":
			return rest, len(rest) > 0
		case strings.HasPrefix(opt, "--"):
			if !strings.Contains(opt, "=") && slices.Contains([]string{"--user", "--group", "--prompt", "--close-from", "--host", "--chdir", "--role", "--type", "--other-user", "--command-timeout", "--chroot"}, opt) && len(rest) > 0 {
				rest = rest[1:]
			}
		case len(opt) == 2 && strings.ContainsRune(sudoValued, rune(opt[1])) && len(rest) > 0:
			rest = rest[1:] // "-u root": its value follows
		}
	}
	return rest, len(rest) > 0
}

// asRoot makes q sudo's question: the password, to run cmd as
// administrator, the command shown.
func asRoot(q *pinentry.Request, cmd string) {
	q.Mode, q.Visible = pinentry.GetPin, false
	q.Title = i18n.T("Run as Administrator?")
	q.Desc = i18n.Tf("Enter your password to run this command as administrator:\n\n%s", cmd)
	q.Prompt, q.OK = i18n.T("Password"), i18n.T("Allow")
}
