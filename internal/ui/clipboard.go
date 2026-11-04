package ui

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"
	"github.com/lyricat/inua/internal/clipboard"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
)

type clipboardPanel struct {
	open                             bool
	win                              *gtk.ApplicationWindow
	reveal                           *gtk.Revealer
	backdrop, rows                   *gtk.Box
	detailLayer                      *gtk.Fixed
	detailCard                       *gtk.Box
	detailWidget                     *gtk.Widget
	search                           *gtk.SearchEntry
	preview                          *gtk.Picture
	text, status, detailTitle        *gtk.Label
	entries                          []clipboard.Entry
	filtered                         []clipboard.Entry
	path, stamp, target, targetClass string
	monitor                          string
	timer, gone                      uint32 // gone: the window's removal once slid away
	watcher                          *exec.Cmd
}

func (s *shellUI) bindClipboardHandlers() {
	s.clipboard.path = clipboard.HistoryPath()
	s.onClipboardButton = func(b gtk.Button) {
		var mon *gdk.Monitor
		for _, bar := range s.bars {
			if bar.clipboard.GoPointer() == b.GoPointer() {
				mon = bar.mon
				break
			}
		}
		if s.clipboard.open {
			sameMonitor := mon == nil || s.clipboard.monitor == mon.GetConnector()
			s.closeClipboard()
			if sameMonitor {
				return
			}
		}
		s.openClipboard(mon)
	}
	s.onClipboardSearch = func(gtk.SearchEntry) { s.renderClipboard() }
	s.onClipboardActivate = func(gtk.SearchEntry) {
		if len(s.clipboard.filtered) > 0 {
			action := "paste"
			if !s.cfg.Clipboard.PasteOnClick {
				action = "copy"
			}
			s.pasteClipboard(s.clipboard.filtered[0], action)
		}
	}
	s.onClipboardAction = func(b gtk.Button) {
		c := &s.clipboard
		action, raw, _ := strings.Cut(b.GetName(), ":")
		if action == "clear" {
			if err := clipboard.Clear(c.path); err != nil {
				c.status.SetText(i18n.T("Could not clear the history"))
				return
			}
			s.refreshClipboard()
			return
		}
		index, err := strconv.Atoi(raw)
		if err != nil || index < 0 || index >= len(c.filtered) {
			return
		}
		entry := c.filtered[index]
		if action == "delete" {
			if err := clipboard.Delete(c.path, entry); err != nil {
				c.status.SetText(i18n.T("Could not delete it"))
				return
			}
			s.refreshClipboard()
			return
		}
		if action == "paste" && !s.cfg.Clipboard.PasteOnClick {
			action = "copy"
		}
		s.pasteClipboard(entry, action)
	}
	s.onClipboardHover = func(m gtk.EventControllerMotion, _, _ float64) {
		widget := m.GetWidget()
		if widget == nil {
			return
		}
		defer widget.Unref()
		_, raw, _ := strings.Cut(widget.GetName(), ":")
		index, err := strconv.Atoi(raw)
		if err == nil && index >= 0 && index < len(s.clipboard.filtered) {
			s.showClipboardDetail(widget, s.clipboard.filtered[index])
		}
	}
	s.onClipboardLeave = func(gtk.EventControllerMotion) { s.hideClipboardDetail() }
	s.onClipboardBackdrop = func(g gtk.GestureClick, _ int32, x, y float64) {
		if s.barClick(s.barUnder(g.GetWidget()), x, y) {
			return // an entry on the bar, or the bar beside them
		}
		c := &s.clipboard
		hit := c.backdrop.Pick(x, y, gtk.PickDefaultValue)
		if hit != nil {
			outside := hit.GoPointer() == c.backdrop.GoPointer()
			hit.Unref()
			if outside {
				s.closeClipboard()
			}
		}
	}
	s.onClipboardKey = func(_ gtk.EventControllerKey, key, _ uint32, _ gdk.ModifierType) bool {
		if key == keyEscape {
			s.closeClipboard()
			return true
		}
		return false
	}
	s.onClipboardGone = func(uintptr) bool {
		s.clipboard.gone = 0
		s.dropClipboardWindow()
		return false
	}
	s.onClipboardTick = func(uintptr) bool {
		if !s.clipboard.open {
			return false
		}
		s.refreshClipboard()
		return true
	}
}

// startClipboardCapture records what is copied, when the settings say so:
// inuaHelper is the inua command beside the running inua-shell, which runs
// what has no GTK: the clipboard's capture, voxtype's hand-over.
func inuaHelper() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	helper := filepath.Join(filepath.Dir(strings.TrimSuffix(exe, " (deleted)")), "inua")
	if _, err := os.Stat(helper); err != nil {
		return "", err
	}
	return helper, nil
}

// wl-paste --watch runs `inua clipboard capture` (no GTK) for each new
// selection, and the panel reads the history it writes.
func (s *shellUI) startClipboardCapture() {
	if !s.cfg.Clipboard.Enable || s.clipboard.watcher != nil {
		return
	}
	helper, err := inuaHelper()
	if err != nil {
		log.Printf("clipboard: capture helper unavailable: %v", err)
		return
	}
	if err := clipboard.SkipFirst(s.clipboard.path); err != nil {
		log.Printf("clipboard: %v", err)
		return
	}
	cmd := exec.Command("wl-paste", "--watch", helper, "clipboard", "capture", s.clipboard.path)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		log.Printf("clipboard watcher: %v (is wl-clipboard installed?)", err)
		return
	}
	s.clipboard.watcher = cmd
	go func() {
		if err := cmd.Wait(); err != nil && !strings.Contains(err.Error(), "signal: killed") {
			log.Printf("clipboard watcher stopped: %v", err)
		}
	}()
}
func (s *shellUI) stopClipboardCapture() {
	if cmd := s.clipboard.watcher; cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		s.clipboard.watcher = nil
	}
}

func (s *shellUI) openClipboard(mon *gdk.Monitor) {
	c := &s.clipboard
	if c.open {
		return
	}
	s.closeDrawer()
	s.closeControl()
	s.closeCenter()
	s.closeAgenda()
	s.closePasswords()
	s.dropClipboardWindow() // opened again while sliding away
	c.target, c.targetClass = s.snap.FocusedWindow, ""
	for _, w := range s.snap.Windows {
		if w.ID == c.target {
			c.targetClass = w.AppID
			break
		}
	}
	c.win = gtk.NewApplicationWindow(s.app)
	c.win.AddCssClass("clipboard-panel")
	c.backdrop = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	c.backdrop.SetHexpand(true)
	c.backdrop.SetVexpand(true)
	// The notification center's right-hand rail, sliding in from the
	// right as it does. The revealer clips only while it moves, so the
	// card's shadow shows; the card is as wide as the center's because
	// nothing in it asks for more (the rows scroll, the labels wrap).
	c.reveal = gtk.NewRevealer()
	c.reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideLeftValue)
	c.reveal.SetTransitionDuration(revealMS)
	c.reveal.SetHalign(gtk.AlignEndValue)
	c.reveal.SetHexpand(true)
	c.reveal.SetVexpand(true)
	c.reveal.SetMarginTop(42)
	c.reveal.SetMarginBottom(6)
	c.reveal.SetMarginEnd(6)
	card := gtk.NewBox(gtk.OrientationVerticalValue, 12)
	card.AddCssClass("clipboard-card")
	card.SetSizeRequest(380, -1)
	card.SetVexpand(true)
	heading := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(heading, &mark.Widget)
	title := gtk.NewLabel(i18n.T("[CLIPBOARD]"))
	title.AddCssClass("tag")
	title.SetHexpand(true)
	title.SetXalign(0)
	gtkx.Append(heading, &title.Widget)
	clear := gtk.NewButtonWithLabel(i18n.T("Clear all"))
	clear.AddCssClass("link")
	clear.SetName("clear")
	clear.ConnectClicked(&s.onClipboardAction)
	gtkx.Append(heading, &clear.Widget)
	gtkx.Append(card, &heading.Widget)
	c.search = gtk.NewSearchEntry()
	c.search.SetPlaceholderText(i18n.T("Search clipboard history"))
	c.search.AddCssClass("clipboard-search")
	c.search.ConnectSearchChanged(&s.onClipboardSearch)
	c.search.ConnectActivate(&s.onClipboardActivate)
	gtkx.Append(card, &c.search.Widget)

	// Hover details live in a card of their own beside the row.
	c.detailCard = gtk.NewBox(gtk.OrientationVerticalValue, 8)
	c.detailCard.AddCssClass("clipboard-card")
	c.detailCard.AddCssClass("clipboard-detail")
	c.detailCard.SetSizeRequest(380, 300)
	c.detailTitle = gtk.NewLabel("")
	c.detailTitle.AddCssClass("tag")
	c.detailTitle.SetXalign(0)
	gtkx.Append(c.detailCard, &c.detailTitle.Widget)
	divider := gtk.NewSeparator(gtk.OrientationHorizontalValue)
	gtkx.Append(c.detailCard, &divider.Widget)
	c.preview = gtk.NewPicture()
	c.preview.SetCanShrink(true)
	c.preview.SetContentFit(gtk.ContentFitContainValue)
	c.preview.SetVexpand(true)
	c.preview.SetVisible(false)
	gtkx.Append(c.detailCard, &c.preview.Widget)
	textScroll := gtk.NewScrolledWindow()
	textScroll.SetPolicy(gtk.PolicyExternalValue, gtk.PolicyAutomaticValue)
	textScroll.SetVexpand(true)
	c.text = gtk.NewLabel("")
	c.text.SetWrap(true)
	c.text.SetWrapMode(pango.WrapWordCharValue)
	c.text.SetSelectable(true)
	c.text.SetXalign(0)
	c.text.SetYalign(0)
	c.text.SetMaxWidthChars(35)
	textScroll.SetChild(&c.text.Widget)
	gtkx.Append(c.detailCard, &textScroll.Widget)

	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyExternalValue, gtk.PolicyAutomaticValue)
	scroll.SetVexpand(true)
	scroll.SetPropagateNaturalWidth(false)
	c.rows = gtk.NewBox(gtk.OrientationVerticalValue, 4)
	scroll.SetChild(&c.rows.Widget)
	gtkx.Append(card, &scroll.Widget)
	c.status = gtk.NewLabel("")
	c.status.AddCssClass("readout")
	c.status.SetXalign(0)
	c.status.SetWrap(true)
	c.status.SetWrapMode(pango.WrapWordCharValue)
	c.status.SetMaxWidthChars(40) // wraps within the card rather than widening it
	gtkx.Append(card, &c.status.Widget)
	c.reveal.SetChild(&card.Widget)
	card.Unref()
	gtkx.Append(c.backdrop, &c.reveal.Widget)
	overlay := gtk.NewOverlay()
	overlay.SetChild(&c.backdrop.Widget)
	c.detailLayer = gtk.NewFixed()
	c.detailLayer.SetHexpand(true)
	c.detailLayer.SetVexpand(true)
	c.detailLayer.SetCanTarget(false)
	overlay.AddOverlay(&c.detailLayer.Widget)
	overlay.SetMeasureOverlay(&c.detailLayer.Widget, false)
	c.detailWidget = &c.detailCard.Widget
	c.detailLayer.Put(c.detailWidget, 0, 0)
	c.detailWidget.SetVisible(false)
	c.win.SetChild(&overlay.Widget)
	c.detailCard.Unref()
	c.detailLayer.Unref()
	overlay.Unref()
	c.backdrop.Unref()
	c.rows.Unref()
	c.text.Unref()
	click := gtk.NewGestureClick()
	click.ConnectPressed(&s.onClipboardBackdrop)
	c.backdrop.AddController(&click.EventController)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onClipboardKey)
	c.win.AddController(&keys.EventController)
	s.followBar(&c.win.Widget)
	if s.layer {
		w := c.win.GoPointer()
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-clipboard")
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
			c.monitor = mon.GetConnector()
		}
	} else {
		c.win.SetTitle(i18n.T("Clipboard"))
		c.win.SetDefaultSize(400, 640)
	}
	c.open = true
	c.stamp = ""
	s.refreshClipboard()
	c.win.Present()
	c.reveal.SetRevealChild(true) // after Present, so it slides in
	c.search.GrabFocus()
	c.timer = glib.TimeoutAdd(750, &s.onClipboardTick, 0)
	for _, b := range s.bars {
		b.clipboard.AddCssClass("open")
	}
}

func (s *shellUI) closeClipboard() {
	c := &s.clipboard
	if !c.open {
		return
	}
	c.open = false
	if c.timer != 0 {
		glib.SourceRemove(c.timer)
		c.timer = 0
	}
	// It slides away; the keyboard goes back at once, for a paste.
	s.hideClipboardDetail()
	if s.layer {
		layershell.SetKeyboardMode(c.win.GoPointer(), layershell.KeyboardNone)
	}
	c.reveal.SetRevealChild(false)
	c.gone = glib.TimeoutAdd(revealMS, &s.onClipboardGone, 0)
	for _, b := range s.bars {
		b.clipboard.RemoveCssClass("open")
	}
}

// dropClipboardWindow removes the panel's window, closed and sliding away.
func (s *shellUI) dropClipboardWindow() {
	c := &s.clipboard
	if c.gone != 0 {
		glib.SourceRemove(c.gone)
		c.gone = 0
	}
	if c.win == nil || c.open {
		return
	}
	c.win.Destroy()
	c.win.Unref()
	c.win, c.reveal = nil, nil
}

func (s *shellUI) refreshClipboard() {
	c := &s.clipboard
	info, err := os.Stat(c.path)
	stamp := "missing"
	if err == nil {
		stamp = fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
	}
	if stamp == c.stamp {
		return
	}
	entries, err := clipboard.Read(c.path)
	if err != nil {
		c.status.SetText(i18n.T("Could not read the clipboard history"))
		return
	}
	c.stamp, c.entries = stamp, entries
	s.renderClipboard()
}
func (s *shellUI) renderClipboard() {
	c := &s.clipboard
	if !c.open {
		return
	}
	gtkx.Clear(c.rows)
	c.filtered = nil
	query := strings.ToLower(strings.TrimSpace(c.search.GetText()))
	for _, entry := range c.entries {
		if entry.Type != "text" && entry.Type != "image" {
			continue
		}
		if entry.Type == "text" && strings.IndexFunc(entry.Text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSymbol(r) }) < 0 {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Text+" "+entry.MIME+" "+entry.Path), query) {
			continue
		}
		c.filtered = append(c.filtered, entry)
		limit := s.cfg.Clipboard.Limit
		if limit <= 0 {
			limit = clipboard.DefaultLimit
		}
		if len(c.filtered) == limit {
			break
		}
	}
	for i, entry := range c.filtered {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 2)
		row.AddCssClass("clipboard-row")
		btn := gtk.NewButton()
		btn.AddCssClass("clipboard-item")
		btn.SetHexpand(true)
		btn.SetName(fmt.Sprintf("paste:%d", i))
		btn.ConnectClicked(&s.onClipboardAction)
		content := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		icon := gtk.NewImageFromIconName("text-x-generic-symbolic")
		label := strings.Join(strings.FieldsFunc(clipboardExcerpt(entry.Text, 300), unicode.IsSpace), " ")
		if entry.Type == "image" {
			icon.SetFromIconName("image-x-generic-symbolic")
			label = i18n.Tf("Image · %s", entry.MIME)
		}
		runes := []rune(label)
		if len(runes) > 65 {
			label = string(runes[:65]) + "…"
		}
		gtkx.Append(content, &icon.Widget)
		text := gtk.NewLabel(label)
		text.SetXalign(0)
		text.SetWrap(true)
		text.SetMaxWidthChars(22)
		text.SetWrapMode(pango.WrapWordCharValue)
		text.SetEllipsize(pango.EllipsizeEndValue)
		text.SetLines(2)
		gtkx.Append(content, &text.Widget)
		btn.SetChild(&content.Widget)
		content.Unref()
		motion := gtk.NewEventControllerMotion()
		motion.ConnectEnter(&s.onClipboardHover)
		motion.ConnectLeave(&s.onClipboardLeave)
		btn.AddController(&motion.EventController)
		gtkx.Append(row, &btn.Widget)
		action := func(name, icon, tooltip string) {
			b := gtk.NewButtonFromIconName(icon)
			b.AddCssClass("plain")
			b.SetName(fmt.Sprintf("%s:%d", name, i))
			b.SetTooltipText(tooltip)
			b.ConnectClicked(&s.onClipboardAction)
			gtkx.Append(row, &b.Widget)
		}
		action("copy", "edit-copy-symbolic", i18n.T("Copy without pasting"))
		if entry.Type == "image" {
			action("path", "go-next-symbolic", i18n.T("Paste the image file's path"))
		}
		action("delete", "edit-delete-symbolic", i18n.T("Delete"))
		gtkx.Append(c.rows, &row.Widget)
	}
	if len(c.filtered) == 0 {
		text := i18n.T("Nothing copied yet")
		if query != "" {
			text = i18n.T("Nothing matches")
		}
		label := gtk.NewLabel(text)
		label.AddCssClass("readout")
		gtkx.Append(c.rows, &label.Widget)
		s.hideClipboardDetail()
		c.text.SetText(text)
	}
	if s.cfg.Clipboard.PasteOnClick {
		c.status.SetText(i18n.Nf("%d entry · click to paste · Escape to close", "%d entries · click to paste · Escape to close", len(c.filtered), len(c.filtered)))
	} else {
		c.status.SetText(i18n.Nf("%d entry · click to copy · Escape to close", "%d entries · click to copy · Escape to close", len(c.filtered), len(c.filtered)))
	}
}

// Bound rendering work without truncating the stored or pasted payload.
func clipboardExcerpt(text string, limit int) string {
	n := 0
	for i := range text {
		if n == limit {
			return text[:i] + "…"
		}
		n++
	}
	return text
}

func (s *shellUI) showClipboardDetail(row *gtk.Widget, entry clipboard.Entry) {
	c := &s.clipboard
	image := entry.Type == "image"
	c.preview.SetVisible(image)
	c.detailTitle.SetText(i18n.T("[DETAILS]"))
	switch when := strings.ToUpper(readTime(entry.Time, time.Now())); {
	case !entry.Time.IsZero() && entry.Source != "":
		c.detailTitle.SetText(i18n.Tf("[COPIED %s FROM %s]", when, strings.ToUpper(entry.Source)))
	case !entry.Time.IsZero():
		c.detailTitle.SetText(i18n.Tf("[COPIED %s]", when))
	}
	if image {
		c.preview.SetFilename(entry.Path)
		c.text.SetText(entry.MIME)
	} else {
		c.text.SetText(clipboardExcerpt(entry.Text, 16000))
	}
	var bounds graphene.Rect
	if !row.ComputeBounds(&c.win.Widget, &bounds) {
		return
	}
	height := int(c.win.GetHeight())
	x := int(bounds.GetX()) - 390
	if x < 6 {
		x = 6
	}
	y := int(bounds.GetY()+bounds.GetHeight()/2) - 150
	if y < 42 {
		y = 42
	}
	if y+300 > height-6 {
		y = max(42, height-306)
	}
	c.detailLayer.Move(c.detailWidget, float64(x), float64(y))
	c.detailWidget.SetVisible(true)
}

func (s *shellUI) hideClipboardDetail() {
	if s.clipboard.detailWidget != nil {
		s.clipboard.detailWidget.SetVisible(false)
	}
}

// terminalClasses are app classes that paste with Ctrl+Shift+V, as
// Ctrl+V means something else in a terminal.
var terminalClasses = []string{"kitty", "alacritty", "foot", "wezterm", "ghostty", "terminal", "konsole", "xterm",
	"inua-term", "inua.term", "tilix", "urxvt"}

// pasteClipboard puts entry on the clipboard (action "copy", "paste", or
// "path" for an image's file name) and, unless it is "copy", pastes it into
// the window that was focused when the panel opened: focused again without
// moving the pointer, then sent the paste shortcut, to that window by
// address so a window under the pointer cannot take it. Hyprland 0.53
// gets the classic dispatchers, 0.55 the Lua ones; without Hyprland, wtype
// types the shortcut into whatever is focused.
func (s *shellUI) pasteClipboard(entry clipboard.Entry, action string) {
	c := &s.clipboard
	target, class := c.target, strings.ToLower(c.targetClass)
	keys, _ := s.comp.(compositor.Keystroker)
	if !s.caps.Has(compositor.CapKeystrokes) {
		keys = nil
	}
	// The panel takes the keyboard; it goes before the paste.
	s.closeClipboard()
	go func() {
		data, mime := []byte(entry.Text), "text/plain;charset=utf-8"
		switch {
		case action == "path":
			data = []byte(entry.Path)
		case entry.Type == "image":
			var err error
			if data, err = os.ReadFile(entry.Path); err != nil {
				log.Printf("clipboard: image unavailable: %v", err)
				return
			}
			mime = entry.MIME
		}
		cmd := exec.Command("wl-copy", "--type", mime)
		cmd.Stdin = bytes.NewReader(data)
		// wl-copy's daemon must not inherit a pipe waited on by Cmd.Run.
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("clipboard copy: %v", err)
			return
		}
		if action == "copy" || (keys != nil && target == "") {
			return
		}
		time.Sleep(150 * time.Millisecond) // the panel has let go of the keyboard
		mods := "CTRL"
		if slices.ContainsFunc(terminalClasses, func(t string) bool { return strings.Contains(class, t) }) {
			mods = "CTRL SHIFT"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var err error
		switch {
		case keys == nil:
			args := []string{"-M", "ctrl", "-k", "v", "-m", "ctrl"}
			if mods != "CTRL" {
				args = []string{"-M", "ctrl", "-M", "shift", "-k", "v", "-m", "shift", "-m", "ctrl"}
			}
			err = exec.CommandContext(ctx, "wtype", args...).Run()
		default:
			err = keys.SendKey(ctx, target, mods, "V")
		}
		if err != nil {
			log.Printf("clipboard paste: %v", err)
		}
	}()
}
