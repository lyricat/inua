// Command inua-pinentry is a pinentry for gpg-agent that asks in inua's
// password dialog, and on the web terminal's pages open (a phone's, over
// inua-termd): the first answer wins. Point gpg-agent at it
// (pinentry-program in ~/.gnupg/gpg-agent.conf). With no inua shell
// running it runs the usual pinentry instead, so a passphrase can still
// be asked for elsewhere.
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
)

// fallbacks are the pinentries run without a shell, the first found.
var fallbacks = []string{"pinentry-gnome3", "pinentry-gtk-2", "pinentry-qt", "pinentry-curses", "pinentry-tty"}

func main() {
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	if !passask.Listening(sock) {
		passask.Fallback("inua-pinentry", fallbacks)
	}
	cfg, _, _ := config.Load(config.DefaultPath())
	i18n.Init(cfg.Language)
	// inua-keyring names the app and the secret when its pass asks.
	forKeyring, viaKeyring := strings.CutPrefix(os.Getenv("PINENTRY_USER_DATA"), pinentry.UserData)
	if !viaKeyring {
		forKeyring = "" // someone else's
	}
	ask := func(q pinentry.Request) (pinentry.Answer, error) {
		read := keyring.PassphraseAccess(q.Owner, forKeyring, q.Desc, keyring.StoreDir())
		describe(&q, read, forKeyring, viaKeyring)
		wait := time.Hour // the user takes their time
		if q.Timeout > 0 {
			wait = time.Duration(q.Timeout)*time.Second + 5*time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()
		r := passask.Ask(ctx, sock, q)
		if q.Mode == pinentry.GetPin {
			read.Answer = r.Result
			if err := keyring.RecordAccess(keyring.AccessPath(), read); err != nil {
				fmt.Fprintln(os.Stderr, "inua-pinentry: record:", err)
			}
		}
		if r.Result == keyring.Failed {
			return r.Answer, r.Err
		}
		return r.Answer, nil
	}
	if err := pinentry.Serve(os.Stdin, os.Stdout, ask); err != nil {
		fmt.Fprintln(os.Stderr, "inua-pinentry:", err)
		os.Exit(1)
	}
}

// describe tells the dialog who wants the key and what for: the app and
// secret inua-keyring named, else the program running gpg; what that runs
// under ("pass", "zsh ← ghostty"); the pass entry gpg decrypts; and the
// key, from gpg's keyring.
func describe(q *pinentry.Request, a keyring.Access, forKeyring string, viaKeyring bool) {
	q.App, q.Chain, q.Entry = a.App, a.Chain, a.Pass
	if viaKeyring {
		q.Keyring = true
		if app, secret, ok := strings.Cut(forKeyring, "\t"); ok {
			q.Secret = secret
			forKeyring = app
		}
		if q.App == "" {
			q.App, _, _ = strings.Cut(forKeyring, " (pid ")
		}
	}
	if k, ok := pinentry.LookupKey(q.KeyGrip); ok {
		q.Key = &k
	}
}
