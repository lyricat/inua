// Package passask asks the user a question of inua-pinentry's or
// inua-askpass's: in the shell's password dialog, and on the web
// terminal's pages open (a phone's, over inua-termd), the first answer
// winning; and runs another program to ask when no shell is listening.
package passask

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/term/host"
)

// Reply is how a question was answered: the answer, and the keyring's
// word for it (keyring.Entered, Cancelled, Failed with Err).
type Reply struct {
	Answer pinentry.Answer
	Result string
	Err    error
	none   bool // no web page was open to ask
}

// Ask asks q in the shell's dialog (on its control socket sock) and on
// the web terminal's pages that can show it; the first answer wins, and
// the other asks no more. A page answers too when the dialog failed.
func Ask(ctx context.Context, sock string, q pinentry.Request) Reply {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	desk := make(chan Reply, 1)
	go func() { desk <- askShell(ctx, sock, q) }()
	pages := make(chan Reply, 1)
	if s, ok := ForPages(q); ok {
		go func() { pages <- askPages(ctx, s) }()
	}
	var failed Reply
	for desk != nil || pages != nil {
		select {
		case r := <-desk:
			desk = nil
			if r.Result != keyring.Failed || pages == nil {
				return r
			}
			failed = r // a page may still answer
		case r := <-pages:
			pages = nil
			if !r.none {
				return r
			}
			if desk == nil { // no page open, and the dialog failed
				return failed
			}
		}
	}
	return failed
}

// askShell asks in the shell's dialog.
func askShell(ctx context.Context, sock string, q pinentry.Request) Reply {
	arg, err := json.Marshal(q)
	if err != nil {
		return Reply{Result: keyring.Failed, Err: err}
	}
	resp, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: "password", Function: string(q.Mode), Args: []string{string(arg)}})
	r := Reply{Result: keyring.Entered}
	switch {
	case err == nil:
		r.Answer.OK, r.Answer.Pin = true, resp.Result
	case resp.Error == "cancelled":
		r.Result = keyring.Cancelled
	case resp.Error == "not ok":
		r.Answer.NotOK = true
	default:
		r.Result, r.Err = keyring.Failed, err
	}
	return r
}

// ForPages is what the web terminal's pages show of q: a passphrase, a
// yes or no, or a note. A new passphrase (asked twice), an answer to be
// seen while typed (a user name) and inua-keyring's approvals are the
// desktop's alone.
func ForPages(q pinentry.Request) (host.Secret, bool) {
	switch {
	case q.Repeat != "" || q.Visible:
		return host.Secret{}, false
	case q.Mode != pinentry.GetPin && q.Mode != pinentry.Confirm && q.Mode != pinentry.Message:
		return host.Secret{}, false
	}
	who := cmp.Or(q.App, "gpg")
	title := i18n.Tf("%s asks for your passphrase", who)
	if q.Mode != pinentry.GetPin {
		title = i18n.Tf("%s asks", who)
	}
	if q.Keyring {
		title = i18n.Tf("inua-keyring asks for your passphrase, for %s", who)
	}
	return host.Secret{
		Mode: string(q.Mode), Title: title, Desc: q.Desc, Prompt: q.Prompt, Error: q.Error,
		OK: q.OK, Cancel: q.Cancel, NotOK: q.NotOK,
	}, true
}

// askPages asks the web terminal's pages open, through its host; none
// when no host runs or no page is open. ctx ending takes the question
// back.
func askPages(ctx context.Context, s host.Secret) Reply {
	c, err := host.Dial(host.SocketPath(), nil, nil)
	if err != nil {
		return Reply{none: true}
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	defer c.Close()
	answer, pin, err := c.AskSecret(s)
	switch {
	case err != nil || answer == "":
		return Reply{none: true}
	case answer == host.AnswerOK:
		return Reply{Answer: pinentry.Answer{OK: true, Pin: pin}, Result: keyring.Entered}
	case answer == host.AnswerNo:
		return Reply{Answer: pinentry.Answer{NotOK: true}, Result: keyring.Entered}
	}
	return Reply{Result: keyring.Cancelled}
}

// Listening reports whether a shell answers on its control socket.
func Listening(sock string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := ipc.Call(ctx, sock, ipc.Request{Op: "show"})
	return err == nil
}

// Fallback replaces this process with the first of others found, given
// this one's arguments, before anything was read; never with this
// program itself (another name for it). With none it says so and exits 1.
func Fallback(name string, others []string) {
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)
	for _, other := range others {
		path, err := exec.LookPath(other)
		if err != nil {
			continue
		}
		if real, _ := filepath.EvalSymlinks(path); real == self {
			continue
		}
		err = syscall.Exec(path, append([]string{path}, os.Args[1:]...), os.Environ())
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, errors.Join(fmt.Errorf("running %s", path), err))
	}
	fmt.Fprintf(os.Stderr, "%s: no inua shell, and none of %v\n", name, others)
	os.Exit(1)
}
