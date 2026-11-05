package ui

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/notify"
	"github.com/lyricat/inua/internal/session"
)

// Settings → Compositor: which compositor the login starts (a setting the
// session script reads, so one login entry serves them all), what the
// login manager has of inua's session, and what the compositor running now
// cannot do. A choice that is not installed is disabled with the reason
// under it. The sections are made once; showCompositor fills them.

type compositorSettings struct {
	page    gtk.Box
	choices map[string]*compositorChoice // by compositor id
	ids     map[uintptr]string           // a choice's check button → its id
	note    *gtk.Label                   // under the choices: when it applies, or why not
	logout  *gtk.Button
	login   *gtk.Label // the login manager and the session entry
	loginN  *gtk.Label // what is wrong with them
	here    *gtk.Label // the compositor running
	missing *gtk.Box   // what it cannot do, a row each
}

type compositorChoice struct {
	state *gtk.Label
	check *gtk.CheckButton
	hint  *gtk.Label
}

// hintLabel is a line under an option saying why it is disabled or what
// it needs.
func hintLabel() *gtk.Label {
	l := gtk.NewLabel("")
	l.AddCssClass("sub")
	l.AddCssClass("warn-text")
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetSelectable(true)
	l.SetVisible(false)
	return l
}

func setHint(l *gtk.Label, text string) {
	l.SetText(text)
	l.SetVisible(text != "")
}

// getCompositorSettings makes the page's sections in its box.
func (s *shellUI) getCompositorSettings(ui *gtkx.Builder) {
	p := &s.settings.compositor
	ui.Get("compositor_page", &p.page)
	p.choices, p.ids = map[string]*compositorChoice{}, map[uintptr]string{}
	if s.onCompositorPick == nil {
		s.onCompositorPick = func(b gtk.CheckButton) {
			id, ok := s.settings.compositor.ids[b.GoPointer()]
			if ok && b.GetActive() && !s.settings.filling {
				s.chooseCompositor(id)
			}
		}
		s.onCompositorLogout = func(gtk.Button) { s.logout() }
	}

	sec, body := displaySection(i18n.T("COMPOSITOR"), i18n.T("What the next login runs inua on"))
	var group *gtk.CheckButton
	for _, c := range session.Compositors {
		if body.GetFirstChild() != nil {
			gtkx.Append(body, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		}
		row, state := displayRow(c.Name, " ")
		check := gtk.NewCheckButton()
		check.SetValign(gtk.AlignCenterValue)
		if group == nil {
			group = check
		} else {
			check.SetGroup(group)
		}
		p.ids[check.GoPointer()] = c.ID
		check.ConnectToggled(&s.onCompositorPick)
		gtkx.Append(row, &check.Widget)
		gtkx.Append(body, &row.Widget)
		hint := hintLabel()
		gtkx.Append(body, &hint.Widget)
		p.choices[c.ID] = &compositorChoice{state: state, check: check, hint: hint}
	}
	gtkx.Append(body, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
	row, note := displayRow(i18n.T("Applies at the next login"), " ")
	p.note = note
	p.logout = gtk.NewButtonWithLabel(i18n.T("Log out…"))
	p.logout.AddCssClass("outlined")
	p.logout.SetValign(gtk.AlignCenterValue)
	p.logout.ConnectClicked(&s.onCompositorLogout)
	gtkx.Append(row, &p.logout.Widget)
	gtkx.Append(body, &row.Widget)
	gtkx.Append(&p.page, &sec.Widget)

	sec, body = displaySection(i18n.T("LOGIN"), i18n.T("The login screen starts the Inua session, which starts the compositor"))
	row, p.login = displayRow(i18n.T("Login manager"), " ")
	gtkx.Append(body, &row.Widget)
	p.loginN = hintLabel()
	gtkx.Append(body, &p.loginN.Widget)
	gtkx.Append(&p.page, &sec.Widget)

	sec, body = displaySection(i18n.T("THIS SESSION"), i18n.T("What the compositor running now does not give inua"))
	row, p.here = displayRow(i18n.T("Running on"), " ")
	gtkx.Append(body, &row.Widget)
	p.missing = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	gtkx.Append(body, &p.missing.Widget)
	gtkx.Append(&p.page, &sec.Widget)
}

// capabilityNames says what each capability gives, for the list of what a
// compositor lacks.
var capabilityNames = []struct {
	c    compositor.Capability
	name string
}{
	{compositor.CapPreviews, i18n.M("Live window previews in the overview")},
	{compositor.CapGeometry, i18n.M("Windows laid out as they are, in the overview")},
	{compositor.CapWorkspaces, i18n.M("New workspaces from the overview")},
	{compositor.CapMoveWithoutFollow, i18n.M("Dragging windows between workspaces in the overview")},
	{compositor.CapKeyObserver, i18n.M("Super alone for the overview, and its number slots")},
	{compositor.CapKeyboard, i18n.M("Keyboard settings from inua")},
	{compositor.CapTouchpad, i18n.M("Touchpad settings from inua")},
	{compositor.CapBinds, i18n.M("Shortcuts set in inua")},
	{compositor.CapModes, i18n.M("Holding other shortcuts back while one is recorded")},
	{compositor.CapDisplays, i18n.M("Display settings from inua")},
	{compositor.CapKeystrokes, i18n.M("Pasting into a window from the clipboard history and dictation")},
	{compositor.CapVirtualOutput, i18n.M("A virtual screen for remote desktop without a monitor")},
	{compositor.CapPointer, i18n.M("Moving the pointer for you")},
}

// showCompositor fills the page from the settings, what is installed and
// what the running compositor can do.
func (s *shellUI) showCompositor() {
	st := &s.settings
	p := &st.compositor
	if st.win == nil || p.choices == nil {
		return
	}
	st.filling = true
	defer func() { st.filling = false }()
	chosen := session.Chosen(s.cfg.Session.Compositor)
	running := session.Running()
	if s.comp != nil {
		running = s.comp.Name()
	}
	for _, c := range session.Compositors {
		ch := p.choices[c.ID]
		installed := c.Installed()
		state := i18n.T("Installed")
		switch {
		case c.ID == running:
			state = i18n.T("Running now")
		case !installed:
			state = i18n.T("Not installed")
		}
		ch.state.SetText(state)
		ch.check.SetActive(c.ID == chosen)
		ch.check.SetSensitive(installed || c.ID == chosen)
		hint := ""
		switch problem := c.Problem(); {
		case !installed:
			hint = i18n.Tf("Install %s to choose it.", c.Name)
		case c.ID == chosen && c.ID != session.Default && problem != "":
			hint = i18n.Tf("%s. The login starts Hyprland instead until this is put right.", problem)
		}
		setHint(ch.hint, hint)
	}
	name := func(id string) string {
		c, _ := session.Find(id)
		return c.Name
	}
	switch {
	case running == "":
		p.note.SetText(i18n.T("No known compositor is running."))
	case !session.ViaSession():
		p.note.SetText(i18n.T("This session was not started by the Inua session entry; the choice applies when you log in through it."))
	case chosen == running:
		p.note.SetText(i18n.Tf("%s is running, and is what the next login starts.", name(running)))
	default:
		p.note.SetText(i18n.Tf("%s is running; %s starts at the next login.", name(running), name(chosen)))
	}
	p.logout.SetVisible(chosen != running && running != "")

	login := session.LoginStatus()
	manager := login.Manager
	if manager == "" {
		manager = i18n.T("None enabled")
	}
	entry := i18n.T("the Inua session is installed")
	note := ""
	switch {
	case !login.Entry || !login.Script:
		entry = i18n.T("the Inua session is not installed")
		note = i18n.T("Install it from inua's source: make install-session (asks for the administrator's password).")
	case !login.Chooses:
		entry = i18n.T("the Inua session is an older one")
		note = i18n.T("The installed session script always starts Hyprland. Update it from inua's source: sudo sh scripts/install-session.sh")
	}
	p.login.SetText(manager + " · " + entry)
	setHint(p.loginN, note)

	if running == "" {
		p.here.SetText(i18n.T("Nothing inua knows"))
	} else {
		p.here.SetText(name(running))
	}
	gtkx.Clear(p.missing)
	for _, cn := range capabilityNames {
		state := s.caps[cn.c]
		if s.comp == nil || state == compositor.Supported {
			continue
		}
		gtkx.Append(p.missing, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		what := i18n.Tf("Not available on %s", s.compositorName())
		if state == compositor.Unavailable {
			what = i18n.T("Not available now")
			if hint := s.configHint(cn.c); hint != "" {
				what = hint
			}
		}
		row, _ := displayRow(i18n.T(cn.name), what)
		gtkx.Append(p.missing, &row.Widget)
	}
	if p.missing.GetFirstChild() == nil && s.comp != nil {
		row, _ := displayRow(i18n.T("Everything inua uses"), i18n.T("Nothing is missing on this compositor."))
		gtkx.Append(p.missing, &row.Widget)
	}
}

// chooseCompositor sets the compositor the next login starts. One chosen
// for the first time gets its sample config, which starts the shell.
func (s *shellUI) chooseCompositor(id string) {
	c, ok := session.Find(id)
	if !ok || session.Chosen(s.cfg.Session.Compositor) == id {
		return
	}
	cfg := s.cfg
	cfg.Session.Compositor = id
	s.changeSettings(cfg)
	if path, err := c.Prepare(); err != nil {
		log.Printf("settings: %s: %v", c.Name, err)
		s.setStatus(i18n.Tf("%s's config was not written: %s", c.Name, err.Error()), true)
	} else if path != "" {
		log.Printf("settings: wrote %s", path)
		s.setStatus(i18n.Tf("Wrote %s, which starts inua on %s.", path, c.Name), false)
	}
	s.showCompositor()
}

// logout ends the session: the compositor's own way, else logind's.
func (s *shellUI) logout() {
	if ex, ok := s.comp.(compositor.Exiter); ok {
		s.compositorDo("log out", ex.Exit)
		return
	}
	if id := os.Getenv("XDG_SESSION_ID"); id != "" {
		go run("loginctl terminate-session " + shellQuote(id))
		return
	}
	log.Printf("log out: the compositor has no way and there is no session id")
}

// sayFallback tells, once after a login, that the compositor chosen in the
// settings was passed over and why.
func (s *shellUI) sayFallback() {
	note := session.Fallback()
	if note == "" {
		return
	}
	log.Printf("session: started the default compositor: %s", note)
	if s.notes.srv == nil {
		return
	}
	n := notify.Notification{
		App: "inua", Icon: "dialog-warning-symbolic",
		Summary: i18n.Tf("Started on %s", s.compositorName()),
		Body:    notify.Markup(note + ". " + i18n.T("The compositor chosen in Settings was passed over.")),
		Time:    time.Now(),
	}
	n.ID = s.notes.srv.NewID()
	s.postNote(n)
}

// configurer is the compositor's way to take inua's settings, nil on
// Hyprland (which has its own config writer) and without a compositor.
func (s *shellUI) configurer() compositor.Configurer {
	if s.hypr != nil || s.comp == nil {
		return nil
	}
	cf, _ := s.comp.(compositor.Configurer)
	return cf
}

// configHint is what the user can do so that a capability works.
func (s *shellUI) configHint(c compositor.Capability) string {
	if cf := s.configurer(); cf != nil {
		return cf.Hint(c)
	}
	return ""
}

// capabilityHint is why the settings a capability stands for do not reach
// the compositor, "" when they do. Hyprland's pages have their own checks.
func (s *shellUI) capabilityHint(c compositor.Capability) string {
	if s.comp == nil || s.hypr != nil {
		return ""
	}
	switch s.caps[c] {
	case compositor.Supported:
		return ""
	case compositor.Unavailable:
		if hint := s.configHint(c); hint != "" {
			return hint
		}
		return i18n.Tf("Not available on %s now.", s.compositorName())
	}
	return i18n.Tf("Not available on %s.", s.compositorName())
}

// gatePage disables a page's sections when hint is not empty, dimmed, and
// says why in the page's notice at its top: the options stay to be seen,
// with the reason they do nothing where it is read first.
func (s *shellUI) gatePage(warn *gtk.Label, hint string) {
	parent := warn.GetParent()
	if parent == nil {
		return
	}
	for w := parent.GetFirstChild(); w != nil; w = w.GetNextSibling() {
		if !w.HasCssClass("section") {
			continue
		}
		w.SetSensitive(hint == "")
		toggle(w, "gated", hint != "")
	}
	warn.SetText(hint)
	warn.SetVisible(hint != "")
}

// showBackendLabels names the compositor where the pages speak of it: the
// source of the keyboard and touchpad settings, the compositor's own
// shortcuts.
func (s *shellUI) showBackendLabels() {
	st := &s.settings
	cf := s.configurer()
	if st.win == nil || cf == nil {
		return // Hyprland: the layout's own words
	}
	name := s.compositorName()
	source := i18n.Tf("inua writes these to %s", cf.Source())
	st.kbSourceMeta.SetText(source)
	st.tpSourceMeta.SetText(source)
	own := i18n.Tf("Use %s's own", name)
	st.kbReset.SetLabel(own)
	st.tpReset.SetLabel(own)
	st.scOtherLabel.SetMarkup("<span alpha='55%'>[</span>" + strings.ToUpper(name) + "<span alpha='55%'>]</span>")
	st.scOtherMeta.SetText(i18n.Tf("Other shortcuts, from %s's own config", name))
}

// applyBackendSettings hands inua's keyboard, touchpad and shortcut
// settings to a compositor other than Hyprland, in the background.
func (s *shellUI) applyBackendSettings(cf compositor.Configurer) {
	settings := s.backendSettings()
	gen := s.hyprApply.latest.Add(1)
	go func() {
		s.hyprApply.mu.Lock()
		defer s.hyprApply.mu.Unlock()
		if s.hyprApply.latest.Load() != gen {
			return // a newer request follows
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := cf.ApplySettings(ctx, settings)
		gtkx.Post(func() {
			if err != nil {
				log.Printf("%s settings: %v", s.comp.Name(), err)
				s.setStatus(i18n.Tf("Not applied to %s: %s", s.compositorName(), err.Error()), true)
			}
			s.refreshHyprInput() // the shortcuts and hints as they are now
		})
	}()
}
