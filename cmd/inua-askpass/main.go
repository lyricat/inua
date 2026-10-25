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
	// Who asks: ssh, sudo or git, and what runs it.
	if asker := os.Getppid(); asker > 1 {
		q.App, q.Chain = proc.Name(asker), proc.Chain(proc.Parent(asker), 8)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour) // the user takes their time
	r := passask.Ask(ctx, sock, q)
	cancel()
	out, ok := answer(q, yesNo, r)
	if r.Err != nil {
		fmt.Fprintln(os.Stderr, "inua-askpass:", r.Err)
	}
	if !ok {
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
