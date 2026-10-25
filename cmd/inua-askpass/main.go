// Command inua-askpass asks in inua's password dialog, and on the web
// terminal's pages open, for programs that ask without a terminal: what
// SSH_ASKPASS, SUDO_ASKPASS and GIT_ASKPASS run, with the question as
// the argument. It prints the answer and a newline and exits 0; any other
// exit is a cancel. SSH_ASKPASS_PROMPT says what ssh asks: unset a
// secret, "confirm" a yes or no (ssh-add -c), "none" a note (touch the
// security key). With no inua shell running it runs another askpass.
package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/passask"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/proc"
)

// fallbacks are the askpass programs run without a shell, the first found.
var fallbacks = []string{
	"ksshaskpass", "lxqt-openssh-askpass", "ssh-askpass",
	"/usr/lib/ssh/ssh-askpass", "/usr/libexec/openssh/ssh-askpass", "/usr/lib/openssh/gnome-ssh-askpass",
}

func main() {
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	if !passask.Listening(sock) {
		passask.Fallback("inua-askpass", fallbacks)
	}
	cfg, _, _ := config.Load(config.DefaultPath())
	i18n.Init(cfg.Language)
	q, yesNo := question(strings.Join(os.Args[1:], " "), os.Getenv("SSH_ASKPASS_PROMPT"))
	// Who asks: ssh, sudo or git, and what runs it; for sudo, the command
	// it runs as root (Mister Morph names the assistant's).
	if asker := os.Getppid(); asker > 1 {
		q.App, q.Chain = proc.Name(asker), proc.Chain(proc.Parent(asker), 8)
		if cmd, ok := sudoCommand(q.App, proc.Args(asker)); ok {
			asRoot(&q, cmp.Or(os.Getenv(morphCommand), cmd))
		}
	}
	deadline := time.Now().Add(time.Hour) // the user takes their time
	if s, err := strconv.ParseInt(os.Getenv(morphDeadline), 10, 64); err == nil && s > 0 {
		deadline = time.Unix(s, 0)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	r := passask.Ask(ctx, sock, q)
	late := ctx.Err() != nil
	cancel()
	out, ok := answer(q, yesNo, r)
	if r.Err != nil && !late {
		fmt.Fprintln(os.Stderr, "inua-askpass:", r.Err)
	}
	if !ok {
		// Mister Morph tells the assistant a declined command from a
		// failed one by its helper's words.
		if os.Getenv(morphAskpass) == "1" {
			fmt.Fprintln(os.Stderr, morphMarker(late, r))
		}
		os.Exit(1)
	}
	fmt.Print(out)
}

// question is what the dialog asks for prompt, the asker's words, and
// kind, SSH_ASKPASS_PROMPT; yesNo when the answer is to be typed "yes"
// (ssh asking whether to trust a host's key).
func question(prompt, kind string) (q pinentry.Request, yesNo bool) {
	prompt = strings.TrimSpace(prompt)
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
	return q, yesNo
}

// Mister Morph's askpass helper's environment, which inua-assistant hands
// on when it becomes inua-askpass: that it is one, the command and when
// the approval gives up (Unix seconds).
const (
	morphAskpass  = "MISTERMORPH_ASKPASS"
	morphCommand  = "MISTERMORPH_ELEVATE_COMMAND"
	morphDeadline = "MISTERMORPH_ELEVATE_DEADLINE"
)

// morphMarker is what Mister Morph's helper writes when an approval did
// not come: timed out (late), else declined.
func morphMarker(late bool, r passask.Reply) string {
	if late {
		return "mistermorph: administrator approval timed out"
	}
	if r.Result == keyring.Failed {
		return "mistermorph: the approval dialog failed: " + errText(r.Err)
	}
	return "mistermorph: administrator approval was declined"
}

// errText is an error's words, "" for none.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// sudoValued are sudo's options that take a value: -u USER, -g GROUP …
var sudoValued = "ugpChDrtUTR"

// sudoCommand is the command sudo (name, run with args) runs as root,
// without sudo's own options; ok when name is sudo and it runs one.
func sudoCommand(name string, args []string) (string, bool) {
	if (name != "sudo" && name != "sudo-rs") || len(args) < 2 {
		return "", false
	}
	rest := args[1:]
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		opt := rest[0]
		rest = rest[1:]
		switch {
		case opt == "--":
			return strings.Join(rest, " "), len(rest) > 0
		case strings.HasPrefix(opt, "--"):
			if !strings.Contains(opt, "=") && slices.Contains([]string{"--user", "--group", "--prompt", "--close-from", "--host", "--chdir", "--role", "--type", "--other-user", "--command-timeout", "--chroot"}, opt) && len(rest) > 0 {
				rest = rest[1:]
			}
		case len(opt) == 2 && strings.ContainsRune(sudoValued, rune(opt[1])) && len(rest) > 0:
			rest = rest[1:] // "-u root": its value follows
		}
	}
	return strings.Join(rest, " "), len(rest) > 0
}

// asRoot makes q sudo's question: the password, to run cmd as
// administrator, the command shown.
func asRoot(q *pinentry.Request, cmd string) {
	q.Mode, q.Visible = pinentry.GetPin, false
	q.Title = i18n.T("Run as Administrator?")
	q.Desc = i18n.Tf("Enter your password to run this command as administrator:\n\n%s", cmd)
	q.Prompt, q.OK = i18n.T("Password"), i18n.T("Allow")
}

// answer is what to print for r, a line, and whether to exit 0: the text
// typed, or "yes" for a host's key; nothing for a yes or no ssh reads by
// the exit alone, or a note.
func answer(q pinentry.Request, yesNo bool, r passask.Reply) (string, bool) {
	if r.Result != keyring.Entered || !r.Answer.OK {
		return "", false
	}
	switch {
	case yesNo:
		return "yes\n", true
	case q.Mode == pinentry.GetPin:
		return r.Answer.Pin + "\n", true
	}
	return "", true
}
