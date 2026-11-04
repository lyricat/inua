package ui

import (
	"context"
	"errors"
	"log"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/polkit"
	"github.com/lyricat/inua/internal/system"
)

// The shell is the session's polkit agent: when a program asks for rights
// that take a password (pkexec, a system setting), a dialog over the
// focused screen says what is asked and takes the password. What the user
// types goes to polkit's helper, which checks it and tells polkitd; the
// shell keeps none of it. One request shows at a time; others wait.

type authAgent struct {
	stop context.CancelFunc
	slot chan struct{} // held by the request on screen
	cur  *authDialog
}

type authDialog struct {
	req  polkit.Request
	who  int // which of req.Users answers
	done chan error

	win                          gtk.Window
	message, command, user, info gtk.Label
	prompt                       gtk.Label
	password                     gtk.PasswordEntry
	ok, switchUser               gtk.Button
	refs                         gtkx.Scope

	conv      *polkit.Conversation // the attempt under way
	answering bool                 // the helper waits for what is typed
	problem   bool                 // the helper said what went wrong
	closed    bool
}

// startPolkit registers the shell as the session's authentication agent.
// Another agent already registered (polkit-gnome) keeps the job.
func (s *shellUI) startPolkit() {
	a := &s.auth
	if a.stop != nil {
		return
	}
	a.slot = make(chan struct{}, 1)
	ctx, stop := context.WithCancel(context.Background())
	a.stop = stop
	go func() {
		conn, err := system.SystemBus()
		if err != nil {
			log.Printf("polkit: %v", err)
			return
		}
		defer conn.Close()
		session, err := polkit.Session(ctx, conn)
		if err != nil {
			log.Printf("polkit: %v", err)
			return
		}
		log.Printf("polkit: authentication agent for session %s", session)
		if err := polkit.NewAgent(conn, authHandler{s}).Serve(ctx, session); err != nil {
			log.Printf("%v (is another authentication agent running?)", err)
		}
	}()
}

// authHandler has polkitd's requests answered on the main loop.
type authHandler struct{ s *shellUI }

func (h authHandler) Authenticate(ctx context.Context, r polkit.Request) error {
	a := &h.s.auth
	select { // one dialog at a time
	case a.slot <- struct{}{}:
	case <-ctx.Done():
		return polkit.ErrCancelled
	}
	defer func() { <-a.slot }()
	log.Printf("polkit: %s asks for authentication as %s", r.Action, strings.Join(r.Users, ", "))
	d := &authDialog{req: r, done: make(chan error, 1)}
	gtkx.Post(func() { h.s.openAuth(d) })
	select {
	case err := <-d.done:
		return err
	case <-ctx.Done(): // polkitd withdrew it
		gtkx.Post(func() { h.s.finishAuth(d, polkit.ErrCancelled) })
		return <-d.done
	}
}

func (s *shellUI) bindPolkitHandlers() {
	later := func(f func()) { gtkx.Post(f) } // the dialog sent the signal
	s.onAuthOK = func(gtk.Button) { s.answerAuth() }
	s.onAuthActivate = func(gtk.PasswordEntry) { s.answerAuth() }
	s.onAuthCancel = func(gtk.Button) {
		if d := s.auth.cur; d != nil {
			later(func() { s.finishAuth(d, polkit.ErrCancelled) })
		}
	}
	s.onAuthSwitch = func(gtk.Button) {
		if d := s.auth.cur; d != nil && len(d.req.Users) > 1 {
			d.who = (d.who + 1) % len(d.req.Users)
			s.showAuthUser(d)
			s.startAuthAttempt(d)
		}
	}
	s.onAuthKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		d := s.auth.cur
		if keyval != keyEscape || d == nil {
			return false
		}
		later(func() { s.finishAuth(d, polkit.ErrCancelled) })
		return true
	}
}

// openAuth shows the dialog for a request and starts the first attempt.
func (s *shellUI) openAuth(d *authDialog) {
	var cancel gtk.Button
	var icon gtk.Image
	ui := s.load("polkit.ui", &d.refs)
	ui.Get("polkit_dialog", &d.win)
	ui.Get("icon", &icon)
	ui.Get("message", &d.message)
	ui.Get("command", &d.command)
	ui.Get("user", &d.user)
	ui.Get("switch_user", &d.switchUser)
	ui.Get("prompt", &d.prompt)
	ui.Get("password", &d.password)
	ui.Get("info", &d.info)
	ui.Get("ok", &d.ok)
	ui.Get("cancel", &cancel)
	ui.Close()

	if d.req.Icon != "" {
		icon.SetFromIconName(d.req.Icon)
	}
	d.message.SetText(d.req.Message)
	if cmd := d.req.Command(); cmd != "" {
		d.command.SetText(cmd)
		d.command.SetVisible(true)
	}
	d.switchUser.SetVisible(len(d.req.Users) > 1)
	s.showAuthUser(d)
	d.ok.ConnectClicked(&s.onAuthOK)
	cancel.ConnectClicked(&s.onAuthCancel)
	d.switchUser.ConnectClicked(&s.onAuthSwitch)
	d.password.ConnectActivate(&s.onAuthActivate)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&s.onAuthKey)
	d.win.AddController(&keys.EventController)

	d.win.SetApplication(s.app)
	w := d.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-polkit")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1) // over the bars too
		if mon := s.monitorByName(s.snap.FocusedOutput); mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
		}
	} else {
		d.win.SetTitle(i18n.T("Authentication Required"))
	}
	s.auth.cur = d
	d.win.Present()
	d.password.GrabFocus()
	s.startAuthAttempt(d)
}

func (s *shellUI) showAuthUser(d *authDialog) {
	d.user.SetText(d.req.Users[d.who])
}

// startAuthAttempt begins a conversation with the helper, ending the one
// under way, and follows what it says.
func (s *shellUI) startAuthAttempt(d *authDialog) {
	if d.conv != nil {
		d.conv.Close()
		d.conv = nil
	}
	d.answering, d.problem = false, false
	d.password.SetSensitive(false)
	d.ok.SetSensitive(false)
	user, cookie := d.req.Users[d.who], d.req.Cookie
	go func() {
		conv, err := polkit.Start(user, cookie)
		gtkx.Post(func() {
			if err != nil {
				if !d.closed {
					log.Printf("polkit: %v", err)
					s.showAuthInfo(d, i18n.T("The password cannot be checked: polkit's helper did not answer."), true)
					d.ok.SetSensitive(true) // to try again
				}
				return
			}
			if d.closed {
				conv.Close()
				return
			}
			d.conv = conv
			go s.followAuth(d, conv)
		})
	}()
}

// followAuth reads a conversation to its end, handing each event to the
// main loop.
func (s *shellUI) followAuth(d *authDialog, conv *polkit.Conversation) {
	for {
		ev, err := conv.Next()
		gtkx.Post(func() { s.authEvent(d, conv, ev, err) })
		if err != nil || ev.Kind == polkit.Done {
			return
		}
	}
}

func (s *shellUI) authEvent(d *authDialog, conv *polkit.Conversation, ev polkit.Event, err error) {
	if d.closed || d.conv != conv { // a conversation since ended
		return
	}
	switch {
	case err != nil:
		log.Printf("polkit: %v", err)
		d.conv = nil
		conv.Close()
		s.showAuthInfo(d, i18n.T("The password could not be checked."), true)
		d.ok.SetSensitive(true)
	case ev.Kind == polkit.Prompt || ev.Kind == polkit.Question:
		text := strings.TrimSpace(ev.Text)
		standard := strings.EqualFold(strings.TrimSuffix(text, ":"), "password")
		d.prompt.SetText(text)
		d.prompt.SetVisible(!standard && text != "")
		d.answering = true
		d.password.SetSensitive(true)
		d.ok.SetSensitive(true)
		d.password.GrabFocus()
	case ev.Kind == polkit.Info:
		s.showAuthInfo(d, ev.Text, false)
	case ev.Kind == polkit.Problem:
		d.problem = true
		s.showAuthInfo(d, ev.Text, true)
	case ev.OK:
		log.Printf("polkit: authenticated as %s", d.req.Users[d.who])
		d.conv = nil
		conv.Close()
		s.finishAuth(d, nil)
	default: // failed: say so, and start over
		d.conv = nil
		conv.Close()
		if !d.problem {
			s.showAuthInfo(d, i18n.T("Sorry, that didn't work. Try again."), true)
		}
		d.password.SetText("")
		s.startAuthAttempt(d)
	}
}

// answerAuth sends what is typed to the helper, or tries again after the
// helper could not be reached.
func (s *shellUI) answerAuth() {
	d := s.auth.cur
	if d == nil || d.closed {
		return
	}
	if d.conv == nil {
		s.startAuthAttempt(d)
		return
	}
	if !d.answering {
		return
	}
	answer := d.password.GetText()
	d.password.SetText("")
	d.answering = false
	d.password.SetSensitive(false)
	d.ok.SetSensitive(false)
	s.showAuthInfo(d, i18n.T("Checking…"), false)
	conv := d.conv
	go func() {
		if err := conv.Respond(answer); err != nil {
			log.Printf("polkit: %v", err)
		}
	}()
}

func (s *shellUI) showAuthInfo(d *authDialog, text string, bad bool) {
	d.info.SetText(text)
	d.info.SetVisible(text != "")
	toggle(&d.info.Widget, "error", bad)
}

// finishAuth closes the dialog and gives polkitd the answer: nil once
// authenticated, else why not.
func (s *shellUI) finishAuth(d *authDialog, err error) {
	if d.closed {
		return
	}
	d.closed = true
	if d.conv != nil {
		d.conv.Close()
		d.conv = nil
	}
	if errors.Is(err, polkit.ErrCancelled) {
		log.Printf("polkit: cancelled")
	}
	if s.auth.cur == d {
		s.auth.cur = nil
	}
	d.password.SetText("")
	d.win.Destroy()
	d.refs.Release()
	d.done <- err
}
