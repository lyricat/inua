package ui

import (
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/apps/pass"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
)

// passwordsForget is how long the panel keeps an entry decrypted after it
// closes; opened again later, it decrypts it again.
const passwordsForget = 5 * time.Minute

// passwordsShown is how many of the search's results the panel lists.
const passwordsShown = 40

// passwordsPanel is the bar's search through the password store, the one
// the Pass app uses (docs/feat/pass.md): a search, what it finds, and the
// entry chosen, with a copy button for each value. Its window is built on
// every open; what was searched and the entry open stay for the next.
type passwordsPanel struct {
	open             bool
	win              *gtk.Window
	backdrop, list   *gtk.Box
	scroll           *gtk.ScrolledWindow
	search           *gtk.SearchEntry
	status           *gtk.Label
	otpCode, otpLeft *gtk.Label // the open entry's one-time code, while shown
	monitor          string
	timer            uint32
	hasStore         bool // there is a store: the bar shows the button

	store    pass.Store
	entries  []string
	query    string
	found    []string
	cursor   int    // the result Enter opens
	selected string // the entry shown; "" for the results
	// The entry decrypted, nil while it decrypts or once forgotten; failed
	// says why it could not be.
	entry   *pass.Entry
	otp     *pass.OTP
	failed  string
	reveal  map[string]bool   // the secret rows shown, by row
	values  map[string]string // what each row's copy button copies
	decGen  uintptr           // drops a decryption that comes back late
	openGen uintptr           // drops a forget timer from an earlier close
	filling bool              // the search is set from Go
	clip    gtkx.SecretClip
	drop    drop // the card hangs from the bar, and goes back up on close
}

func (s *shellUI) bindPasswordsHandlers() {
	p := &s.passwords
	p.reveal, p.values = map[string]bool{}, map[string]string{}
	s.onPasswordsButton = func(b gtk.Button) {
		var mon *gdk.Monitor
		for _, bar := range s.bars {
			if bar.passwords.GoPointer() == b.GoPointer() {
				mon = bar.mon
				break
			}
		}
		if p.open {
			same := mon == nil || p.monitor == mon.GetConnector()
			s.closePasswords()
			if same {
				return
			}
		}
		s.openPasswords(mon)
	}
	s.onPasswordsSearch = func(gtk.SearchEntry) {
		if p.filling || p.search == nil {
			return
		}
		if q := p.search.GetText(); q != p.query {
			// A new search: back to what it finds.
			p.query, p.cursor = q, 0
			s.closePasswordsEntry()
		}
		s.renderPasswords()
	}
	s.onPasswordsActivate = func(gtk.SearchEntry) {
		switch {
		case p.selected == "" && p.cursor < len(p.found):
			s.openPasswordsEntry(p.found[p.cursor])
		case p.selected != "" && p.entry != nil:
			s.copyPassword(p.entry.Password)
		}
	}
	s.onPasswordsAction = func(b gtk.Button) {
		kind, arg, _ := strings.Cut(b.GetName(), ":")
		switch kind {
		case "open":
			if i, err := strconv.Atoi(arg); err == nil && i < len(p.found) {
				s.openPasswordsEntry(p.found[i])
			}
		case "back":
			s.closePasswordsEntry()
			s.renderPasswords()
			p.search.GrabFocus()
		case "retry":
			s.decryptPassword(p.selected)
		case "reveal":
			p.reveal[arg] = !p.reveal[arg]
			s.renderPasswords()
		case "copy":
			s.copyPassword(p.values[arg])
		}
	}
	s.onPasswordsBackdrop = func(g gtk.GestureClick, _ int32, x, y float64) {
		if s.barClick(s.barUnder(g.GetWidget()), x, y) {
			return // an entry on the bar, or the bar beside them
		}
		if !p.open { // going back up
			return
		}
		hit := p.backdrop.Pick(x, y, gtk.PickDefaultValue)
		if hit != nil {
			outside := hit.GoPointer() == p.backdrop.GoPointer() || p.drop.canvas != nil && hit.GoPointer() == p.drop.canvas.GoPointer()
			hit.Unref()
			if outside {
				s.closePasswords()
			}
		}
	}
	s.onPasswordsKey = func(_ gtk.EventControllerKey, key, _ uint32, _ gdk.ModifierType) bool {
		if !p.open { // going back up
			return true
		}
		switch key {
		case keyEscape:
			s.closePasswords()
		case 0xff52: // Up
			s.movePasswordsCursor(-1)
		case 0xff54: // Down
			s.movePasswordsCursor(1)
		default:
			return false
		}
		return true
	}
	s.onPasswordsTick = func(uintptr) bool {
		if !p.open {
			p.timer = 0
			return false
		}
		s.tickPasswordsOTP()
		return true
	}
	s.onPasswordsForget = func(gen uintptr) bool {
		if !p.open && gen == p.openGen {
			s.forgetPasswordsEntry()
		}
		return false
	}
}

// checkPasswordsStore looks, off the main loop, for a store, and shows
// the bar's button when there is one and the settings want it.
func (s *shellUI) checkPasswordsStore() {
	go func() {
		var prefs pass.Prefs
		if err := store.Load(pass.StoreName, &prefs); err != nil {
			log.Printf("passwords: %v", err)
		}
		has := len(pass.Stores(prefs)) > 0
		gtkx.Post(func() {
			s.passwords.hasStore = has
			for _, b := range s.bars {
				b.passwords.SetVisible(s.cfg.Bar.Pass && has)
			}
		})
	}()
}

func (s *shellUI) openPasswords(mon *gdk.Monitor) {
	p := &s.passwords
	if p.open {
		return
	}
	s.closeDrawer()
	s.closeControl()
	s.closeCenter()
	s.closeAgenda()
	s.closeClipboard()
	p.drop.finish() // the last window, still going back up
	p.win = gtk.NewWindow()
	p.drop.app = s.app // it joins the application only if it has to show
	p.win.AddCssClass("clipboard-panel")
	p.win.AddCssClass("passwords-panel")
	p.backdrop = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	p.backdrop.SetHexpand(true)
	p.backdrop.SetVexpand(true)
	// The clipboard history's rail, as tall as what it shows.
	card := gtk.NewBox(gtk.OrientationVerticalValue, 12)
	card.AddCssClass("clipboard-card")
	card.SetSizeRequest(380, -1)
	card.SetHalign(gtk.AlignEndValue)
	card.SetValign(gtk.AlignStartValue)
	card.SetMarginTop(42)
	card.SetMarginEnd(6)
	heading := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(heading, &mark.Widget)
	title := gtk.NewLabel(i18n.T("[PASSWORDS]"))
	title.AddCssClass("tag")
	title.SetHexpand(true)
	title.SetXalign(0)
	gtkx.Append(heading, &title.Widget)
	gtkx.Append(card, &heading.Widget)
	p.search = gtk.NewSearchEntry()
	p.search.SetPlaceholderText(i18n.T("Search every entry"))
	p.search.AddCssClass("clipboard-search")
	p.filling = true
	p.search.SetText(p.query)
	p.filling = false
	p.search.ConnectSearchChanged(&s.onPasswordsSearch)
	p.search.ConnectActivate(&s.onPasswordsActivate)
	gtkx.Append(card, &p.search.Widget)
	p.scroll = gtk.NewScrolledWindow()
	p.scroll.AddCssClass("passwords-scroll") // its bar at the card's edge
	p.scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	p.scroll.SetPropagateNaturalHeight(true)
	p.scroll.SetMaxContentHeight(480)
	p.list = gtk.NewBox(gtk.OrientationVerticalValue, 4)
	p.list.AddCssClass("passwords-list")
	p.scroll.SetChild(&p.list.Widget)
	p.list.Unref()
	gtkx.Append(card, &p.scroll.Widget)
	p.status = gtk.NewLabel("")
	p.status.AddCssClass("readout")
	p.status.SetXalign(0)
	p.status.SetWrap(true)
	p.status.SetWrapMode(pango.WrapWordCharValue)
	gtkx.Append(card, &p.status.Widget)
	gtkx.Append(p.backdrop, &card.Widget)
	p.win.SetChild(&p.backdrop.Widget)
	p.backdrop.Unref()
	click := gtk.NewGestureClick()
	click.ConnectPressed(&s.onPasswordsBackdrop)
	p.backdrop.AddController(&click.EventController)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onPasswordsKey)
	p.win.AddController(&keys.EventController)
	p.drop.attach(p.win, &keys.EventController, p.backdrop, &card.Widget, layershell.LayerTop, false)
	p.drop.resizes = true // an entry's values widen it, the results narrow it again
	if s.layer {
		w := p.win.GoPointer()
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-passwords")
		layershell.SetLayer(w, layershell.LayerTop)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		for _, edge := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, edge, true)
		}
		layershell.SetExclusiveZone(w, -1)
		if mon == nil {
			mon = s.focusedMonitor()
		}
		if mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
			p.monitor = mon.GetConnector()
		}
	} else {
		p.win.SetTitle(i18n.T("Passwords"))
		p.win.SetDefaultSize(400, 560)
	}
	p.open = true
	p.openGen++
	// The last state at once, from what was read before; the store again
	// meanwhile.
	s.renderPasswords()
	if p.selected != "" && p.entry == nil && p.failed == "" {
		s.decryptPassword(p.selected)
	}
	s.readPasswordsStore()
	s.dropFromBar(&p.drop, mon, func(b *bar) *gtk.Widget { return &b.passwords.Widget })
	p.search.GrabFocus()
	p.search.SelectRegion(0, -1) // typing starts a new search; Enter keeps this one
	p.timer = glib.TimeoutAdd(1000, &s.onPasswordsTick, 0)
	for _, b := range s.bars {
		b.passwords.AddCssClass("open")
	}
}

func (s *shellUI) closePasswords() {
	p := &s.passwords
	if !p.open {
		return
	}
	p.open = false
	if p.timer != 0 {
		glib.SourceRemove(p.timer)
		p.timer = 0
	}
	win := p.win
	p.drop.close(func() {
		p.drop.detach()
		destroyPanel(win)
		win.Unref()
	})
	p.win, p.search, p.list, p.scroll, p.status, p.backdrop = nil, nil, nil, nil, nil, nil
	p.otpCode, p.otpLeft = nil, nil
	clear(p.reveal)
	glib.TimeoutAdd(uint32(passwordsForget/time.Millisecond), &s.onPasswordsForget, p.openGen)
	for _, b := range s.bars {
		b.passwords.RemoveCssClass("open")
	}
}

// readPasswordsStore lists the store in use off the main loop; a store
// other than the last one forgets the entry open.
func (s *shellUI) readPasswordsStore() {
	go func() {
		var prefs pass.Prefs
		if err := store.Load(pass.StoreName, &prefs); err != nil {
			log.Printf("passwords: %v", err)
		}
		st := pass.Store{Dir: pass.Current(prefs, pass.Stores(prefs))}
		var entries []string
		if st.Dir != "" {
			var err error
			if entries, err = st.Entries(); err != nil {
				log.Printf("passwords: %v", err)
			}
		}
		gtkx.Post(func() {
			p := &s.passwords
			if p.store.Dir != "" && st.Dir != p.store.Dir {
				p.query, p.cursor = "", 0
				s.closePasswordsEntry()
				if p.search != nil {
					p.filling = true
					p.search.SetText("")
					p.filling = false
				}
			}
			p.store, p.entries = st, entries
			if p.open {
				s.renderPasswords()
			}
		})
	}()
}

// openPasswordsEntry shows an entry, decrypting it; gpg-agent may ask for
// the passphrase meanwhile, in inua's dialog.
func (s *shellUI) openPasswordsEntry(name string) {
	s.passwords.selected = name
	s.decryptPassword(name)
}

func (s *shellUI) decryptPassword(name string) {
	p := &s.passwords
	s.forgetPasswordsEntry()
	p.decGen++
	gen, st := p.decGen, p.store
	s.renderPasswords()
	go func() {
		text, err := st.Show(name)
		gtkx.Post(func() {
			if gen != p.decGen || p.selected != name {
				return // another entry since
			}
			if err != nil {
				p.failed = err.Error()
			} else {
				e := pass.Parse(text)
				p.entry = &e
				if e.OTP != "" {
					if o, err := pass.ParseOTP(e.OTP); err == nil {
						p.otp = &o
					}
				}
			}
			if p.open {
				s.renderPasswords()
			}
		})
	}()
}

// closePasswordsEntry goes back to the results.
func (s *shellUI) closePasswordsEntry() {
	s.passwords.selected = ""
	s.passwords.decGen++
	s.forgetPasswordsEntry()
}

// forgetPasswordsEntry drops the decrypted entry, keeping which it was.
func (s *shellUI) forgetPasswordsEntry() {
	p := &s.passwords
	p.entry, p.otp, p.failed = nil, nil, ""
	clear(p.reveal)
	clear(p.values)
}

// copyPassword copies a value as a secret and closes the panel, for the
// window under it to paste.
func (s *shellUI) copyPassword(value string) {
	if value == "" {
		return
	}
	s.passwords.clip.Copy(value)
	s.closePasswords()
}

func (s *shellUI) renderPasswords() {
	p := &s.passwords
	if !p.open {
		return
	}
	gtkx.Clear(p.list)
	p.otpCode, p.otpLeft = nil, nil
	if p.selected != "" {
		s.renderPasswordsEntry()
		return
	}
	p.found = pass.Search(p.entries, p.query)
	shown := p.found
	if len(shown) > passwordsShown {
		shown = shown[:passwordsShown]
	}
	p.cursor = min(p.cursor, max(len(shown)-1, 0))
	for i, e := range shown {
		btn := gtk.NewButton()
		btn.AddCssClass("clipboard-item")
		btn.SetName("open:" + strconv.Itoa(i))
		btn.SetFocusable(false) // the keys stay in the search
		if i == p.cursor {
			btn.AddCssClass("selected")
		}
		btn.ConnectClicked(&s.onPasswordsAction)
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		icon := "dialog-password-symbolic"
		if name := pass.Icon(e); name != "" {
			icon = "pass-" + name + "-symbolic"
		}
		img := gtk.NewImageFromIconName(icon)
		gtkx.Append(row, &img.Widget)
		col := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		col.SetHexpand(true)
		folder, name := "", e
		if j := strings.LastIndexByte(e, '/'); j >= 0 {
			folder, name = e[:j], e[j+1:]
		}
		n := gtk.NewLabel(name)
		n.SetXalign(0)
		n.SetEllipsize(pango.EllipsizeMiddleValue)
		gtkx.Append(col, &n.Widget)
		if folder != "" {
			f := gtk.NewLabel(folder)
			f.AddCssClass("passwords-path")
			f.SetXalign(0)
			f.SetEllipsize(pango.EllipsizeStartValue) // the nearest folder tells most
			gtkx.Append(col, &f.Widget)
		}
		gtkx.Append(row, &col.Widget)
		btn.SetChild(&row.Widget)
		row.Unref()
		gtkx.Append(p.list, &btn.Widget)
	}
	p.scroll.SetVisible(len(shown) > 0)
	switch n := len(p.found); {
	case p.store.Dir == "":
		p.status.SetText(i18n.T("No password store. pass init YOUR-GPG-KEY-ID makes one in ~/.password-store."))
	case strings.TrimSpace(p.query) == "":
		p.status.SetText(i18n.Nf("%d entry · type to search · Escape to close", "%d entries · type to search · Escape to close", len(p.entries), len(p.entries)))
	case n == 0:
		p.status.SetText(i18n.T("Nothing matches. Each word is looked for in the whole path."))
	case n > len(shown):
		p.status.SetText(i18n.Nf("%d found, the first %d shown · another word narrows them", "%d found, the first %d shown · another word narrows them", n, n, len(shown)))
	default:
		p.status.SetText(i18n.Nf("%d found · Enter opens · Escape to close", "%d found · Enter opens · Escape to close", n, n))
	}
}

// renderPasswordsEntry shows the entry chosen: its name, then a row for
// each value it has, each with its copy button.
func (s *shellUI) renderPasswordsEntry() {
	p := &s.passwords
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	head.AddCssClass("passwords-head")
	back := gtk.NewButtonFromIconName("go-previous-symbolic")
	back.AddCssClass("plain")
	back.SetName("back")
	back.SetTooltipText(i18n.T("Back to the results"))
	back.SetValign(gtk.AlignCenterValue)
	back.ConnectClicked(&s.onPasswordsAction)
	gtkx.Append(head, &back.Widget)
	col := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	col.SetHexpand(true)
	_, sub := pass.Split(p.selected)
	t := gtk.NewLabel(sub)
	t.AddCssClass("passwords-title")
	t.SetXalign(0)
	t.SetEllipsize(pango.EllipsizeMiddleValue)
	gtkx.Append(col, &t.Widget)
	where := gtk.NewLabel(p.selected)
	where.AddCssClass("passwords-path")
	where.SetXalign(0)
	where.SetEllipsize(pango.EllipsizeStartValue)
	gtkx.Append(col, &where.Widget)
	gtkx.Append(head, &col.Widget)
	gtkx.Append(p.list, &head.Widget)
	p.scroll.SetVisible(true)
	clear(p.values)
	switch {
	case p.failed != "":
		msg := gtk.NewLabel(p.failed)
		msg.AddCssClass("passwords-error")
		msg.SetXalign(0)
		msg.SetWrap(true)
		msg.SetWrapMode(pango.WrapWordCharValue)
		gtkx.Append(p.list, &msg.Widget)
		retry := gtk.NewButtonWithLabel(i18n.T("Try again"))
		retry.AddCssClass("link")
		retry.SetName("retry")
		retry.SetHalign(gtk.AlignStartValue)
		retry.ConnectClicked(&s.onPasswordsAction)
		gtkx.Append(p.list, &retry.Widget)
		p.status.SetText(i18n.T("It could not be decrypted · Escape to close"))
		return
	case p.entry == nil:
		line := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		line.AddCssClass("passwords-row")
		spin := gtk.NewSpinner()
		spin.Start()
		gtkx.Append(line, &spin.Widget)
		l := gtk.NewLabel(i18n.T("Decrypting; gpg may ask for your passphrase."))
		l.AddCssClass("readout")
		l.SetXalign(0)
		l.SetWrap(true)
		gtkx.Append(line, &l.Widget)
		gtkx.Append(p.list, &line.Widget)
		p.status.SetText(i18n.T("Escape to close"))
		return
	}
	e := p.entry
	// The last part of its path is, as pass users name entries, its user.
	if cat, _ := pass.Split(p.selected); cat != s.cfg.Keyring.Folder {
		if u := pass.Username(p.selected); u != "" && !slices.ContainsFunc(e.Fields, func(f pass.Field) bool { return f.Value == u }) {
			s.passwordsRow("user", i18n.T("USERNAME"), u, false)
		}
	}
	s.passwordsRow("password", i18n.T("PASSWORD"), e.Password, true)
	if e.OTP != "" {
		if p.otp != nil {
			s.passwordsRow("otp", i18n.T("ONE-TIME CODE"), "", false)
			s.tickPasswordsOTP()
		} else {
			s.passwordsRow("otp", i18n.T("ONE-TIME CODE"), e.OTP, true)
		}
	}
	for i, f := range e.Fields {
		s.passwordsRow("field:"+strconv.Itoa(i), f.Key, f.Value, f.Secret)
	}
	if e.Notes != "" {
		s.passwordsRow("notes", i18n.T("NOTES"), e.Notes, false)
	}
	p.status.SetText(i18n.Tf("Copied secrets are cleared after %d seconds · Enter copies the password", int(gtkx.SecretClearAfter/time.Second)))
}

// passwordsRow adds a value's row: its name over it, then its buttons; a
// secret as dots until shown. The one-time code's row ("otp", parsed)
// gets its code from tickPasswordsOTP.
func (s *shellUI) passwordsRow(id, key, value string, secret bool) {
	p := &s.passwords
	p.values[id] = value
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
	row.AddCssClass("passwords-row")
	col := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	col.SetHexpand(true)
	k := gtk.NewLabel(key)
	k.AddCssClass("kicker")
	k.SetXalign(0)
	k.SetEllipsize(pango.EllipsizeEndValue)
	gtkx.Append(col, &k.Widget)
	shown := !secret || p.reveal[id]
	v := gtk.NewLabel(value)
	v.AddCssClass("passwords-value")
	if !shown {
		v.SetText("••••••••••")
		v.AddCssClass("masked")
	}
	v.SetXalign(0)
	v.SetWrap(true)
	v.SetWrapMode(pango.WrapWordCharValue)
	v.SetLines(3)
	v.SetEllipsize(pango.EllipsizeEndValue)
	v.SetSelectable(shown)
	v.SetFocusable(false)
	if id == "otp" && p.otp != nil {
		// The code, and beside it the seconds it stays. The code is one
		// line: wrapping, a label beside another gets only its least
		// width, and the code broke at its space.
		line := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		p.otpCode = v
		v.AddCssClass("otp")
		v.SetWrap(false)
		v.SetEllipsize(pango.EllipsizeNoneValue)
		gtkx.Append(line, &v.Widget)
		p.otpLeft = gtk.NewLabel("")
		p.otpLeft.AddCssClass("readout")
		p.otpLeft.SetValign(gtk.AlignCenterValue)
		gtkx.Append(line, &p.otpLeft.Widget)
		gtkx.Append(col, &line.Widget)
	} else {
		gtkx.Append(col, &v.Widget)
	}
	gtkx.Append(row, &col.Widget)
	button := func(name, icon, tip string) {
		b := gtk.NewButtonFromIconName(icon)
		b.AddCssClass("plain")
		b.SetName(name)
		b.SetTooltipText(tip)
		b.SetValign(gtk.AlignCenterValue)
		b.SetFocusable(false)
		b.ConnectClicked(&s.onPasswordsAction)
		gtkx.Append(row, &b.Widget)
	}
	if secret {
		if shown {
			button("reveal:"+id, "view-conceal-symbolic", i18n.T("Hide"))
		} else {
			button("reveal:"+id, "view-reveal-symbolic", i18n.T("Show"))
		}
	}
	button("copy:"+id, "edit-copy-symbolic", i18n.T("Copy"))
	gtkx.Append(p.list, &row.Widget)
}

// tickPasswordsOTP shows the one-time code now and the seconds it stays.
func (s *shellUI) tickPasswordsOTP() {
	p := &s.passwords
	if p.otp == nil || p.otpCode == nil {
		return
	}
	code, left := p.otp.Code(time.Now())
	p.values["otp"] = code
	p.otpCode.SetText(code[:len(code)/2] + " " + code[len(code)/2:])
	secs := int(left.Round(time.Second) / time.Second)
	p.otpLeft.SetText(i18n.Nf("%d second left", "%d seconds left", secs, secs))
}

// movePasswordsCursor moves the result Enter opens by d, keeping it in
// sight.
func (s *shellUI) movePasswordsCursor(d int) {
	p := &s.passwords
	n := min(len(p.found), passwordsShown)
	if p.selected != "" || n == 0 {
		return
	}
	p.cursor = (p.cursor + d + n) % n
	i := 0
	for c := p.list.GetFirstChild(); c != nil; i++ {
		toggle(c, "selected", i == p.cursor)
		if i == p.cursor {
			var r graphene.Rect
			if c.ComputeBounds(&p.list.Widget, &r) {
				adj := p.scroll.GetVadjustment()
				y, h, top := float64(r.GetY()), float64(r.GetHeight()), adj.GetValue()
				switch page := adj.GetPageSize(); {
				case y < top:
					adj.SetValue(y)
				case y+h > top+page:
					adj.SetValue(y + h - page)
				}
			}
		}
		next := c.GetNextSibling()
		c.Unref() // GetFirstChild and GetNextSibling return new references
		c = next
	}
}
