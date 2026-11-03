package ui

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/display"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/uikit"
	"github.com/lyricat/inua/internal/wayfire"
)

// displaysSettings is the Displays page: a section for each connected output.
// Hyprland supplies its modes and layout through IPC; Wayfire supplies its
// outputs through IPC and reads selected settings from wayfire.ini.
type displaysSettings struct {
	page     gtk.Box // the page; the displays' sections follow its own
	lid      gtk.Box // the lid's section
	lidOff   gtk.Switch
	warn     gtk.Label
	sections []*gtk.Box // the displays', owned by page

	outputs  []display.Output
	off      []string // the panels off because the lid is closed
	shown    string   // what the sections show, to lay them out only when it changes
	gen      int      // the latest query; an earlier answer is moot
	controls map[uintptr]displayControl
}

// displayControl is what a control on the page changes.
type displayControl struct {
	output display.Output
	kind   string   // "mode", "scale", "position", "mirror", "on" or "reset"
	values []string // a drop-down's choices, as the rule writes them
}

// displayPositions are the Position menu's choices beside the others.
var displayPositions = []struct{ value, label string }{
	{"auto", i18n.M("Automatic")},
	{"auto-left", i18n.M("Left of the others")},
	{"auto-right", i18n.M("Right of the others")},
	{"auto-up", i18n.M("Above the others")},
	{"auto-down", i18n.M("Below the others")},
}

func (s *shellUI) getDisplaySettings(ui *gtkx.Builder) {
	d := &s.settings.displays
	ui.Get("displays_page", &d.page)
	ui.Get("displays_lid", &d.lid)
	ui.Get("lid_off", &d.lidOff)
	ui.Get("displays_warn", &d.warn)
	d.sections, d.outputs, d.off, d.shown = nil, nil, nil, ""
	d.controls = map[uintptr]displayControl{}
	if s.onDisplayDrop != nil {
		return
	}
	s.onDisplayDrop = func(ptr, _, _ uintptr) {
		ctl, ok := s.settings.displays.controls[ptr]
		if !ok || s.settings.filling {
			return
		}
		i := int(gtk.DropDownNewFromInternalPtr(ptr).GetSelected())
		if i < 0 || i >= len(ctl.values) {
			return
		}
		v := ctl.values[i]
		s.changeDisplay(ctl.output, func(m *config.Monitor) {
			switch ctl.kind {
			case "mode":
				m.Mode = v
			case "scale":
				m.Scale, _ = strconv.ParseFloat(v, 64)
			case "position":
				m.Position = v
			case "mirror":
				m.Mirror = v
			}
		})
	}
	s.onDisplaySwitch = func(sw gtk.Switch, on bool) bool {
		ctl, ok := s.settings.displays.controls[sw.GoPointer()]
		if ok && !s.settings.filling {
			s.changeDisplay(ctl.output, func(m *config.Monitor) { m.Disabled = !on })
		}
		return false
	}
	s.onDisplayButton = func(b gtk.Button) {
		if ctl, ok := s.settings.displays.controls[b.GoPointer()]; ok {
			c := s.cfg
			if m, ok := ctl.output.RuleIn(c.Displays); ok {
				c.Displays.Monitors = c.Displays.WithoutMonitor(m.Output)
				s.changeSettings(c)
			}
		}
	}
}

// changeDisplay changes the rule for o, made from how it is now if inua
// has none yet.
func (s *shellUI) changeDisplay(o display.Output, edit func(*config.Monitor)) {
	c := s.cfg
	m, ok := o.RuleIn(c.Displays)
	if !ok {
		m = o.Seed(slices.Contains(s.settings.displays.off, o.Name))
	}
	edit(&m)
	if m.Mirror == m.Output || m.Mirror == o.Name {
		m.Mirror = ""
	}
	c.Displays.Monitors = c.Displays.WithMonitor(m)
	s.changeSettings(c)
}

// showDisplays asks Hyprland for its displays and the kernel for the lid,
// off the main loop, and lays the page out with the answer.
func (s *shellUI) showDisplays() {
	d := &s.settings.displays
	if s.settings.win == nil {
		return
	}
	s.settings.filling = true
	d.lidOff.SetActive(s.cfg.Displays.LidOff)
	d.lidOff.SetSensitive(!desktop.Wayfire())
	if desktop.Wayfire() {
		d.lidOff.SetTooltipText(i18n.T("Wayfire does not expose a lid switch binding that can disable and restore this output"))
	} else {
		d.lidOff.SetTooltipText("")
	}
	s.settings.filling = false
	d.gen++
	gen, client, lidOff := d.gen, s.client, s.cfg.Displays.LidOff
	configured := slices.Clone(s.cfg.Displays.Monitors)
	go func() {
		var outputs []display.Output
		var err error
		if desktop.Wayfire() {
			wf, connectErr := wayfire.FromEnv()
			err = connectErr
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				outputs, err = wf.SettingsOutputs(ctx)
				cancel()
			}
		} else if client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			var reply []byte
			if reply, err = client.Request(ctx, "j/monitors all"); err == nil {
				outputs, err = display.Outputs(reply)
			}
			cancel()
		}
		if desktop.Wayfire() {
			for _, monitor := range configured {
				if !monitor.Disabled || slices.ContainsFunc(outputs, func(output display.Output) bool { return output.Name == monitor.Output }) {
					continue
				}
				output := display.Output{Name: monitor.Output, Disabled: true}
				if width, height, refresh, ok := display.ParseMode(monitor.Mode); ok {
					output.Width, output.Height, output.Refresh = width, height, refresh
					output.Modes = []string{monitor.Mode}
				}
				outputs = append(outputs, output)
			}
		}
		if err != nil {
			log.Printf("settings: displays: %v", err)
		}
		hasLid := display.Local.HasLid()
		var off []string
		if lidOff {
			off = display.Local.PanelsOff()
		}
		gtkx.Post(func() {
			if s.settings.win == nil || d.gen != gen {
				return
			}
			d.lid.SetVisible(hasLid || slices.ContainsFunc(outputs, display.Output.Internal))
			s.layOutDisplays(outputs, off)
		})
	}()
}

// layOutDisplays makes the displays' sections, if what they would show
// changed.
func (s *shellUI) layOutDisplays(outputs []display.Output, off []string) {
	st := &s.settings
	d := &st.displays
	shown := fmt.Sprint(outputs, off, s.cfg.Displays.Monitors)
	if shown == d.shown {
		return
	}
	d.shown, d.outputs, d.off = shown, outputs, off
	st.filling = true
	defer func() { st.filling = false }()
	for _, sec := range d.sections {
		d.page.Remove(&sec.Widget)
	}
	d.sections = nil
	clear(d.controls)
	if len(outputs) == 0 {
		sec, body := displaySection("", i18n.T("No displays"))
		question := i18n.T("Is the shell connected to Hyprland?")
		if desktop.Wayfire() {
			question = i18n.T("Is Wayfire's IPC plugin enabled and connected?")
		}
		title := i18n.T("Hyprland lists no displays")
		if desktop.Wayfire() {
			title = i18n.T("Wayfire lists no displays")
		}
		row, _ := displayRow(title, question)
		gtkx.Append(body, &row.Widget)
		s.addDisplaySection(sec)
		return
	}
	on := 0
	for _, o := range outputs {
		if !o.Disabled {
			on++
		}
	}
	for _, o := range outputs {
		s.addDisplaySection(s.outputSection(o, outputs, on))
	}
}

func (s *shellUI) addDisplaySection(sec *gtk.Box) {
	d := &s.settings.displays
	d.page.Append(&sec.Widget)
	d.sections = append(d.sections, sec)
	sec.Unref() // the page owns it; d.sections only points
}

// outputSection is a display's section: on or off, then its settings.
func (s *shellUI) outputSection(o display.Output, outputs []display.Output, on int) *gtk.Box {
	d := &s.settings.displays
	rule, ruled := o.RuleIn(s.cfg.Displays)
	lidOff := slices.Contains(d.off, o.Name)
	meta := outputTitle(o)
	if meta == o.Name {
		meta = "" // nothing more than the head's name
	}
	sec, body := displaySection(o.Name, meta)

	row, _ := displayRow(i18n.T("Use this display"), outputState(o, outputs, lidOff))
	sw := gtk.NewSwitch()
	sw.SetValign(gtk.AlignCenterValue)
	sw.SetActive(!o.Disabled || lidOff)
	// The last display on stays on; one off for the lid follows the lid.
	sw.SetSensitive(!desktop.Wayfire() && !lidOff && (o.Disabled || on > 1))
	if desktop.Wayfire() {
		sw.SetTooltipText(i18n.T("Inua has no verified Wayfire output enable or disable option"))
	}
	d.controls[sw.GoPointer()] = displayControl{output: o, kind: "on"}
	sw.ConnectStateSet(&s.onDisplaySwitch)
	gtkx.Append(row, &sw.Widget)
	gtkx.Append(body, &row.Widget)

	usable := !o.Disabled
	// Resolution: the preferred mode, then the display's own.
	modes := []string{"preferred"}
	labels := []string{i18n.T("Preferred")}
	for _, m := range o.ModeNames() {
		modes, labels = append(modes, m), append(labels, modeLabel(m))
	}
	mode := o.Mode()
	if ruled {
		mode = cmp.Or(rule.Mode, "preferred")
	}
	if !slices.Contains(modes, mode) {
		modes, labels = append(modes, mode), append(labels, modeLabel(mode))
	}
	s.displayDrop(body, i18n.T("Resolution"), "", o, "mode", modes, labels, mode, usable)

	// Scale: automatic, the usual ones as the mode takes them, and the
	// one it has if another.
	scale := o.Scale
	if ruled {
		scale = rule.Scale
	}
	w, h := o.Width, o.Height
	if mw, mh, _, ok := display.ParseMode(mode); ok {
		w, h = mw, mh
	}
	scales := display.ScalesFor(w, h)
	if !slices.Contains(scales, scale) {
		scales = append(scales, scale)
	}
	var values []string
	labels = nil
	for _, f := range scales {
		values = append(values, strconv.FormatFloat(f, 'f', -1, 64))
		if !desktop.Wayfire() && display.ValidScale(w, h, f) != f {
			labels = append(labels, i18n.Tf("%s (Hyprland uses %s)", scaleLabel(f), scaleLabel(display.ValidScale(w, h, f))))
		} else {
			labels = append(labels, scaleLabel(f))
		}
	}
	s.displayDrop(body, i18n.T("Scale"), i18n.T("Text and windows larger or smaller"), o, "scale", values, labels, strconv.FormatFloat(scale, 'f', -1, 64), usable)

	// Position: beside the others, or where it is.
	pos := fmt.Sprintf("%dx%d", o.X, o.Y)
	if ruled {
		pos = cmp.Or(rule.Position, "auto")
	}
	values, labels = nil, nil
	for _, p := range displayPositions {
		if desktop.Wayfire() && p.value != "auto" {
			continue
		}
		values, labels = append(values, p.value), append(labels, i18n.T(p.label))
	}
	if desktop.Wayfire() {
		outputScale := o.Scale
		if outputScale <= 0 {
			outputScale = 1
		}
		width, height := int(math.Round(float64(o.Width)/outputScale)), int(math.Round(float64(o.Height)/outputScale))
		for _, other := range outputs {
			if other.Name == o.Name {
				continue
			}
			otherScale := other.Scale
			if otherScale <= 0 {
				otherScale = 1
			}
			otherWidth := int(math.Round(float64(other.Width) / otherScale))
			otherHeight := int(math.Round(float64(other.Height) / otherScale))
			choices := []struct {
				x, y  int
				label string
			}{
				{other.X - width, other.Y, i18n.Tf("Left of %s", outputTitle(other))},
				{other.X + otherWidth, other.Y, i18n.Tf("Right of %s", outputTitle(other))},
				{other.X, other.Y - height, i18n.Tf("Above %s", outputTitle(other))},
				{other.X, other.Y + otherHeight, i18n.Tf("Below %s", outputTitle(other))},
			}
			for _, choice := range choices {
				value := fmt.Sprintf("%dx%d", choice.x, choice.y)
				if !slices.Contains(values, value) {
					values, labels = append(values, value), append(labels, choice.label)
				}
			}
		}
	}
	if !slices.Contains(values, pos) {
		x, y, _ := strings.Cut(pos, "x")
		values, labels = append(values, pos), append(labels, i18n.Tf("Where it is (%s, %s)", x, y))
	}
	s.displayDrop(body, i18n.T("Position"), "", o, "position", values, labels, pos, usable && len(outputs) > 1)

	// Mirror: nothing, or another display's picture.
	if len(outputs) > 1 {
		mirror := strings.TrimPrefix(o.MirrorOf, "none")
		if ruled {
			mirror = rule.Mirror
		}
		values, labels = []string{""}, []string{i18n.T("Off")}
		for _, other := range outputs {
			if other.Name != o.Name {
				values, labels = append(values, other.Name), append(labels, i18n.Tf("Same picture as %s", outputTitle(other)))
			}
		}
		if !slices.Contains(values, mirror) {
			values, labels = append(values, mirror), append(labels, i18n.Tf("Same picture as %s", mirror))
		}
		sub := ""
		if desktop.Wayfire() {
			sub = i18n.T("Wayfire does not support output mirroring")
		}
		s.displayDrop(body, i18n.T("Mirror"), sub, o, "mirror", values, labels, mirror, usable && !desktop.Wayfire())
	}

	// Where the settings come from.
	gtkx.Append(body, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
	source := i18n.T("Set in hyprland.conf")
	resetLabel := i18n.T("Use hyprland.conf")
	if desktop.Wayfire() {
		source = i18n.T("Set in wayfire.ini")
		resetLabel = i18n.T("Use Wayfire's output settings")
	}
	if ruled {
		source = i18n.T("Set by inua")
	}
	row, _ = displayRow(source, "")
	reset := gtk.NewButtonWithLabel(resetLabel)
	reset.SetValign(gtk.AlignCenterValue)
	reset.SetTooltipText(i18n.T("Stop setting this display in inua"))
	reset.SetSensitive(ruled)
	d.controls[reset.GoPointer()] = displayControl{output: o, kind: "reset"}
	reset.ConnectClicked(&s.onDisplayButton)
	gtkx.Append(row, &reset.Widget)
	gtkx.Append(body, &row.Widget)
	return sec
}

// displayDrop adds a row with a drop-down of labels, value selected.
func (s *shellUI) displayDrop(body *gtk.Box, title, sub string, o display.Output, kind string, values, labels []string, value string, sensitive bool) {
	gtkx.Append(body, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
	row, _ := displayRow(title, sub)
	drop := gtk.NewDropDownFromStrings(labels)
	drop.SetValign(gtk.AlignCenterValue)
	drop.SetSelected(uint32(max(0, slices.Index(values, value))))
	drop.SetSensitive(sensitive)
	s.settings.displays.controls[drop.GoPointer()] = displayControl{output: o, kind: kind, values: values}
	gtkx.ConnectNotify(drop, "selected", &s.onDisplayDrop)
	gtkx.Append(row, &drop.Widget)
	gtkx.Append(body, &row.Widget)
}

// displaySection is a section headed [name] and meta, as the page's own,
// and its body, which it owns.
func displaySection(name, meta string) (sec, body *gtk.Box) {
	s := uikit.NewSection(name, meta)
	return s.Box, s.Body
}

// displayRow is a row with a title and a line under it; controls go at
// its end.
func displayRow(title, sub string) (*gtk.Box, *gtk.Label) {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	row.AddCssClass("row")
	text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	t := gtk.NewLabel(title)
	t.AddCssClass("title")
	t.SetXalign(0)
	t.SetWrap(true)
	gtkx.Append(text, &t.Widget)
	var line *gtk.Label
	if sub != "" {
		line = gtk.NewLabel(sub)
		line.AddCssClass("sub")
		line.SetXalign(0)
		line.SetWrap(true)
		gtkx.Append(text, &line.Widget)
	}
	gtkx.Append(row, &text.Widget)
	return row, line
}

// outputTitle names a display: the built-in one, else its make and model.
func outputTitle(o display.Output) string {
	if o.Internal() {
		return i18n.T("Built-in display")
	}
	name := o.Model
	if o.Make != "" && !strings.HasPrefix(o.Model, o.Make) {
		name = o.Make + " " + o.Model
	}
	return cmp.Or(strings.TrimSpace(name), o.Description, o.Name)
}

// outputState says how a display is now.
func outputState(o display.Output, outputs []display.Output, lidOff bool) string {
	switch {
	case lidOff:
		return i18n.T("Off while the lid is closed")
	case o.Disabled:
		return i18n.T("Off")
	case o.MirrorOf != "" && o.MirrorOf != "none":
		for _, other := range outputs {
			if other.Name == o.MirrorOf {
				return i18n.Tf("Shows the same picture as %s", outputTitle(other))
			}
		}
		return i18n.Tf("Shows the same picture as %s", o.MirrorOf)
	}
	return i18n.Tf("%s, at %s", modeLabel(o.Mode()), scaleLabel(o.Scale))
}

// modeLabel is "3840 × 2160, 60 Hz" for "3840x2160@60.00".
func modeLabel(mode string) string {
	w, h, r, ok := display.ParseMode(mode)
	switch {
	case !ok:
		return mode
	case r < 1:
		return i18n.Tf("%d × %d", w, h)
	}
	return i18n.Tf("%d × %d, %s Hz", w, h, strconv.FormatFloat(math.Round(r*100)/100, 'f', -1, 64))
}

// scaleLabel is "150%" for 1.5, "Automatic" for 0.
func scaleLabel(f float64) string {
	if f == 0 {
		return i18n.T("Automatic")
	}
	return i18n.Tf("%d%%", int(math.Round(f*100)))
}
