package ui

import (
	"log"
	"slices"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/portal"
	"github.com/lyricat/inua/internal/portalconf"
	"github.com/lyricat/inua/internal/system"
)

// portalGroup is a row of the Default Apps page's [DIALOGS]: interfaces
// chosen together, the first the one whose backends the row offers, and
// the process that serves them as inua's.
type portalGroup struct {
	name, about string
	ifaces      []string
	bus         string // inua's backend's bus name; the shell's for the share picker
	activated   bool   // D-Bus starts it at a request
}

var portalGroups = []portalGroup{
	{name: i18n.M("Open and save"), about: i18n.M("The file chooser and Open With, for every app"),
		ifaces: []string{"FileChooser", "AppChooser"}, bus: portal.FilesName, activated: true},
	{name: i18n.M("Share the screen"), about: i18n.M("What asks which screen, window or area to share"),
		ifaces: []string{portalconf.SharePicker}, bus: portal.ShellName},
	{name: i18n.M("Notifications and the rest"), about: i18n.M("Apps' notifications, screenshots, the theme and more"),
		ifaces: []string{"Settings", "Notification", "Inhibit", "Screenshot", "Access", "Background", "Wallpaper"}, bus: portal.ShellName},
	{name: i18n.M("Secrets"), about: i18n.M("Passwords of sandboxed apps"),
		ifaces: []string{"Secret"}, bus: portal.KeyringName, activated: true},
}

// portalsSettings is [DIALOGS]: a drop-down of the backends a
// portalGroup can have, what apps get now under its text, and a notice
// while the running portal service has not read the choices. Read off
// the main thread each time the page is shown.
type portalsSettings struct {
	box     gtk.Box
	notice  *gtk.Box
	apply   *gtk.Button
	drops   []*gtk.DropDown
	options [][]string // each drop-down's backends
	states  []*gtk.Label
	busy    bool // a change is being applied
	onDrop  func(ptr, _, _ uintptr)
	onApply func(gtk.Button)
}

// portalState is what a row shows.
type portalState struct {
	options []string // backends; "" for none
	chosen  int
	now     string
	warn    bool
}

// portalsRead is the section's state.
type portalsRead struct {
	rows    []portalState
	pending bool // the portal service reads other choices than these
}

func (s *shellUI) getPortalsSettings(ui *gtkx.Builder) {
	p := &s.settings.portals
	ui.Get("portal_rows", &p.box)
	p.drops, p.options, p.states = nil, nil, nil

	p.notice = gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	p.notice.AddCssClass("warn")
	p.notice.SetVisible(false)
	say := gtk.NewLabel(i18n.T("The portal service started before these choices, so apps still get what it read then. Applying restarts it, which stops a screen share in progress."))
	say.SetXalign(0)
	say.SetWrap(true)
	say.SetHexpand(true)
	gtkx.Append(p.notice, &say.Widget)
	p.apply = gtk.NewButtonWithLabel(i18n.T("Apply now"))
	p.apply.AddCssClass("outlined")
	p.apply.AddCssClass("small")
	p.apply.SetValign(gtk.AlignCenterValue)
	gtkx.Append(p.notice, &p.apply.Widget)
	gtkx.Append(&p.box, &p.notice.Widget)
	p.onApply = func(gtk.Button) { s.applyPortalsNow() }
	p.apply.ConnectClicked(&p.onApply)

	for i, g := range portalGroups {
		if i > 0 {
			gtkx.Append(&p.box, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		}
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
		row.AddCssClass("row")
		text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		title := gtk.NewLabel(i18n.T(g.name))
		title.AddCssClass("title")
		title.SetXalign(0)
		gtkx.Append(text, &title.Widget)
		sub := gtk.NewLabel(i18n.T(g.about))
		sub.AddCssClass("sub")
		sub.SetXalign(0)
		sub.SetWrap(true)
		gtkx.Append(text, &sub.Widget)
		state := gtk.NewLabel("")
		state.AddCssClass("sub")
		state.SetXalign(0)
		state.SetVisible(false)
		state.SetWrap(true)
		gtkx.Append(text, &state.Widget)
		gtkx.Append(row, &text.Widget)
		dd := gtk.NewDropDownFromStrings(nil)
		dd.SetValign(gtk.AlignCenterValue)
		dd.SetSensitive(false) // until read
		gtkx.Append(row, &dd.Widget)
		gtkx.Append(&p.box, &row.Widget)
		p.drops, p.states = append(p.drops, dd), append(p.states, state)
		p.options = append(p.options, nil)
	}
	p.onDrop = func(ptr, _, _ uintptr) {
		if s.settings.filling || p.busy {
			return
		}
		for i, dd := range p.drops {
			if dd.GoPointer() != ptr {
				continue
			}
			if k := int(dd.GetSelected()); k < len(p.options[i]) && p.options[i][k] != "" {
				s.setPortals(portalGroups[i], p.options[i][k])
			}
		}
	}
	for _, dd := range p.drops {
		gtkx.ConnectNotify(dd, "selected", &p.onDrop)
	}
}

// showPortals reads the groups' states and shows them.
func (s *shellUI) showPortals() {
	go func() {
		r := readPortals()
		gtkx.Post(func() { s.fillPortals(r) })
	}()
}

func (s *shellUI) fillPortals(r portalsRead) {
	st := &s.settings
	p := &st.portals
	if st.win == nil || len(r.rows) != len(p.drops) {
		return
	}
	st.filling = true
	defer func() { st.filling = false }()
	for i, ps := range r.rows {
		labels := make([]string, len(ps.options))
		for k, b := range ps.options {
			labels[k] = backendLabel(b)
		}
		if !slices.Equal(p.options[i], ps.options) {
			setChoices(p.drops[i], labels)
			p.options[i] = ps.options
		}
		p.drops[i].SetSelected(uint32(ps.chosen))
		p.drops[i].SetSensitive(!p.busy && len(ps.options) > 1)
		p.states[i].SetText(ps.now)
		p.states[i].SetVisible(ps.now != "")
		if ps.warn {
			p.states[i].AddCssClass("warn-text")
		} else {
			p.states[i].RemoveCssClass("warn-text")
		}
	}
	p.notice.SetVisible(r.pending)
	p.apply.SetSensitive(!p.busy)
}

// backendLabel is a backend's name in a drop-down or a state line.
func backendLabel(b string) string {
	if b == "" {
		return i18n.T("None")
	}
	return portalconf.Label(b)
}

// readPortals is each group's backends, the one chosen and the one apps
// get now. It asks D-Bus, systemd and /proc, so not on the main thread.
func readPortals() portalsRead {
	d := portalconf.Default()
	now, want := portalconf.ReadFrontend(), d.Wanted()
	var conn *dbus.Conn
	if c, err := system.SessionBus(); err == nil {
		conn = c
		defer conn.Close()
	}
	r := portalsRead{pending: now.Dir != want.Dir}
	for _, g := range portalGroups {
		key := g.ifaces[0]
		chosen, serving := d.Serving(want, key), d.Serving(now, key)
		ps := portalState{options: d.Options(key)}
		if !slices.Contains(ps.options, chosen) {
			ps.options = append([]string{chosen}, ps.options...) // none, or a backend gone
		}
		ps.chosen = slices.Index(ps.options, chosen)
		ps.now = i18n.Tf("Now: %s", backendLabel(serving))
		switch {
		case serving != chosen:
			ps.warn = true
		case chosen != "" && portalconf.Label(chosen) == "inua" && !g.activated && conn != nil && !portalconf.HasOwner(conn, g.bus):
			ps.now, ps.warn = i18n.T("Not served: restart the shell"), true
		}
		r.rows = append(r.rows, ps)
	}
	return r
}

// setPortals gives a group the backend, as inua portals does: the files,
// then the portal service restarted, and xdph for the share picker. Apps
// started from then on see it.
func (s *shellUI) setPortals(g portalGroup, backend string) {
	s.portalsJob(func() error {
		d := portalconf.Default()
		if _, err := d.Choose(backend, g.ifaces...); err != nil {
			return err
		}
		if _, err := d.Apply(d.Wanted().Dir != ""); err != nil {
			return err
		}
		if slices.Contains(g.ifaces, portalconf.SharePicker) {
			return portalconf.RestartXdph()
		}
		return nil
	})
}

// applyPortalsNow gives the running session the choices: the directory
// written again and the portal service restarted with it, as a new inua
// session does.
func (s *shellUI) applyPortalsNow() {
	s.portalsJob(func() error {
		d := portalconf.Default()
		on, err := d.Refresh()
		if err != nil {
			return err
		}
		_, err = d.Apply(on)
		return err
	})
}

// portalsJob runs a change off the main thread with the section's
// controls off, then shows the section again.
func (s *shellUI) portalsJob(job func() error) {
	p := &s.settings.portals
	if p.busy {
		return
	}
	p.busy = true
	for _, dd := range p.drops {
		dd.SetSensitive(false)
	}
	p.apply.SetSensitive(false)
	go func() {
		err := job()
		r := readPortals()
		gtkx.Post(func() {
			p.busy = false
			if err != nil {
				log.Printf("settings: portals: %v", err)
				s.setStatus(i18n.Tf("Not saved: %s", err.Error()), true)
			} else {
				s.setStatus("", false)
			}
			s.fillPortals(r)
		})
	}()
}
