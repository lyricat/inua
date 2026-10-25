package ui

import (
	"context"
	"os/exec"
	"slices"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/assistant/record"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// The Sound page (Settings → Devices → Sound):
//
//	■ [OUTPUT]     Where sound plays
//	Output      both sides: Test                  [device ▾]  [Test]
//	Volume                          [🔊] ━━━━━━━━○────  42%
//	■ [INPUT]      What apps record
//	Microphone  Test, and say something    [wave] [device ▾]  [Test]
//	Volume                          [🎙] ━━━━━━━━○────  80%
//
// It follows PipeWire through the audio service: a device plugged in, the
// volume keys, another app choosing the default.
type soundSettings struct {
	page    gtk.Box
	filling bool // the page's controls being set, not changed

	output, input       devicePicker
	outSub, inSub       gtk.Label
	outVolume, inVolume volumeRow
	play                gtk.Button
	playing             bool
	mic                 micTest
	status              gtk.Label // what failed, or what is missing
}

// volumeRow is a device's volume: a mute button, a slider and a readout.
type volumeRow struct {
	row   gtk.Box
	mute  gtk.Button
	scale gtk.Scale
	value gtk.Label
}

func (s *shellUI) bindSoundPage() {
	o := &s.settings.sound
	s.onSoundSlider = func(r gtk.Range, _ gtk.ScrollType, v float64) bool {
		v = max(0, min(v, 100)) / 100
		if r.GetName() == "input" {
			s.svc.inVolume.Set(v)
			o.inVolume.value.SetText(shell.Percent(v))
		} else {
			s.svc.volume.Set(v)
			o.outVolume.value.SetText(shell.Percent(v))
		}
		return false // let the slider move
	}
	s.onSoundMute = func(b gtk.Button) {
		input := b.GetName() == "input"
		go func() {
			err := s.svc.audio.ToggleMute()
			if input {
				err = s.svc.audio.ToggleInputMute()
			}
			gtkx.Post(func() { o.setStatus(errText(err)) })
		}()
	}
	s.onSoundPlay = func(gtk.Button) { s.playChime() }
}

// getSoundSettings makes the page.
func (s *shellUI) getSoundSettings(ui *gtkx.Builder) {
	o := &s.settings.sound
	ui.Get("sound_page", &o.page)
	if s.onSoundSlider == nil {
		s.bindSoundPage()
	}

	sec, body := displaySection(i18n.T("OUTPUT"), i18n.T("Where sound plays"))
	row, sub := displayRow(i18n.T("Output"), " ")
	o.outSub = *sub
	o.output.build(row, o.setStatus, &o.filling)
	o.play = voiceButton(row, i18n.T("Test"), &s.onSoundPlay)
	gtkx.Append(body, &row.Widget)
	o.outVolume = s.volumeRow(body, "output")
	gtkx.Append(&o.page, &sec.Widget)

	sec, body = displaySection(i18n.T("INPUT"), i18n.T("What apps record"))
	row, sub = displayRow(i18n.T("Microphone"), " ")
	o.inSub = *sub
	o.mic.addWave(row)
	o.input.build(row, o.setStatus, &o.filling)
	o.mic.addButton(row, o.inSub)
	gtkx.Append(body, &row.Widget)
	o.inVolume = s.volumeRow(body, "input")
	o.status = *gtk.NewLabel("")
	o.status.AddCssClass("row-error")
	o.status.AddCssClass("flush")
	o.status.SetXalign(0)
	o.status.SetWrap(true)
	o.status.SetSelectable(true)
	o.status.SetVisible(false)
	gtkx.Append(body, &o.status.Widget)
	gtkx.Append(&o.page, &sec.Widget)
}

// volumeRow adds a volume row for the output or the input (name).
func (s *shellUI) volumeRow(body *gtk.Box, name string) volumeRow {
	row, _ := displayRow(i18n.T("Volume"), "")
	v := volumeRow{row: *row}
	v.mute = *gtk.NewButton()
	v.mute.AddCssClass("plain")
	v.mute.SetValign(gtk.AlignCenterValue)
	v.mute.SetName(name)
	v.mute.ConnectClicked(&s.onSoundMute)
	gtkx.Append(row, &v.mute.Widget)
	v.scale = *gtk.NewScaleWithRange(gtk.OrientationHorizontalValue, 0, 100, 5)
	v.scale.SetName(name)
	v.scale.SetDrawValue(false)
	v.scale.SetSizeRequest(220, -1)
	v.scale.SetValign(gtk.AlignCenterValue)
	v.scale.ConnectChangeValue(&s.onSoundSlider)
	gtkx.Append(row, &v.scale.Widget)
	v.value = *gtk.NewLabel("")
	v.value.AddCssClass("readout")
	v.value.SetWidthChars(5)
	v.value.SetXalign(1)
	gtkx.Append(row, &v.value.Widget)
	gtkx.Append(body, &row.Widget)
	return v
}

// showSound fills the page from the audio service's state.
func (s *shellUI) showSound() {
	o := &s.settings.sound
	if s.settings.win == nil || o.page.GoPointer() == 0 {
		return
	}
	a := s.sys.Audio
	o.filling = true
	defer func() { o.filling = false }()

	o.output.fill(a.Outputs, a.Output)
	o.play.SetVisible(len(a.Outputs) > 0)
	o.play.SetSensitive(!o.playing)
	switch {
	case len(a.Outputs) == 0:
		o.outSub.SetText(i18n.T("PipeWire has no output"))
	case o.playing:
		o.outSub.SetText(i18n.T("Playing on the left, then on the right…"))
	default:
		o.outSub.SetText(i18n.T("Test plays a sound on the left, then on the right"))
	}
	s.showVolumeRow(&o.outVolume, system.VolumeState{Available: a.Available, Volume: a.Volume, Muted: a.Muted},
		shell.VolumeIcon(a), s.svc.volume)

	o.input.fill(a.Inputs, a.Input)
	o.mic.button.SetVisible(len(a.Inputs) > 0)
	if !o.mic.testing() {
		if len(a.Inputs) == 0 {
			o.inSub.SetText(i18n.T("PipeWire has no microphone"))
		} else {
			o.inSub.SetText(i18n.T("The one apps and dictation record from; Test, and say something"))
		}
	}
	icon := "audio-input-microphone-symbolic"
	if !a.In.Available || a.In.Muted || a.In.Volume <= 0 {
		icon = "microphone-slash-symbolic"
	}
	s.showVolumeRow(&o.inVolume, a.In, icon, s.svc.inVolume)

	if _, err := exec.LookPath("wpctl"); err != nil {
		o.setStatus(i18n.Tf("Sound is set with WirePlumber's wpctl, which is not installed: %s", installCommand("wireplumber")))
	}
}

// showVolumeRow sets a volume row: the slider unless the user moved it a
// moment ago (the service has not caught up), the readout and the mute
// button.
func (s *shellUI) showVolumeRow(v *volumeRow, st system.VolumeState, icon string, set *shell.Setter) {
	v.row.SetSensitive(st.Available)
	if _, held := set.Held(); !held {
		v.scale.SetValue(st.Volume * 100)
		v.value.SetText(shell.VolumeText(system.AudioState{Available: st.Available, Volume: st.Volume, Muted: st.Muted}))
	}
	v.mute.SetIconName(icon)
	if st.Muted {
		v.mute.SetTooltipText(i18n.T("Unmute"))
	} else {
		v.mute.SetTooltipText(i18n.T("Mute"))
	}
}

// playChime plays the test sound on the output, saying so in its row.
func (s *shellUI) playChime() {
	o := &s.settings.sound
	if o.playing {
		return
	}
	o.playing = true
	o.setStatus("")
	s.showSound()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := system.PlayChime(ctx)
		gtkx.Post(func() {
			o.playing = false
			o.setStatus(errText(err))
			s.showSound()
		})
	}()
}

// setStatus shows what went wrong under the input's rows, or nothing.
func (o *soundSettings) setStatus(text string) {
	o.status.SetText(text)
	o.status.SetVisible(text != "")
}

// errText is an error's words, "" for none.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// devicePicker is a drop-down of PipeWire's outputs or inputs; choosing
// one makes it the default.
type devicePicker struct {
	drop     gtk.DropDown
	devs     []system.AudioDevice
	filling  *bool // the page's: set while its controls are filled
	onChoose func(uintptr, uintptr, uintptr)
}

// build adds the drop-down to row; say shows a failure to choose.
func (p *devicePicker) build(row *gtk.Box, say func(string), filling *bool) {
	p.filling = filling
	p.drop = *gtk.NewDropDownFromStrings(nil)
	p.drop.SetValign(gtk.AlignCenterValue)
	p.onChoose = func(uintptr, uintptr, uintptr) {
		i := int(p.drop.GetSelected())
		if *p.filling || i < 0 || i >= len(p.devs) {
			return
		}
		id := p.devs[i].ID
		go func() {
			err := system.SetDefaultDevice(context.Background(), id)
			gtkx.Post(func() {
				if err != nil {
					say(i18n.Tf("Not changed: %s", err.Error()))
				}
			})
		}()
	}
	gtkx.ConnectNotify(&p.drop, "selected", &p.onChoose)
	gtkx.Append(row, &p.drop.Widget)
}

// fill lists devs, def chosen; the list is made again only when the
// devices changed, so an open list stays open through volume changes.
func (p *devicePicker) fill(devs []system.AudioDevice, def string) {
	if !slices.Equal(devs, p.devs) {
		titles := make([]string, len(devs))
		for i, d := range devs {
			titles[i] = d.Title
		}
		list := gtk.NewStringList(titles)
		p.drop.SetModel(list)
		list.Unref()
		p.devs = slices.Clone(devs)
	}
	p.drop.SetVisible(len(devs) > 0)
	i := slices.IndexFunc(devs, func(d system.AudioDevice) bool { return d.Name == def })
	if i >= 0 && uint32(i) != p.drop.GetSelected() {
		p.drop.SetSelected(uint32(i))
	}
}

// micTest records a few seconds from the default input, drawing how loud
// it is in its row, then says whether it heard anything.
type micTest struct {
	wave    *gtkx.Wave
	button  gtk.Button
	sub     gtk.Label // where it says how it goes
	rec     *record.Recorder
	peak    float64
	until   time.Time
	onClick func(gtk.Button)
	onTick  glib.SourceFunc
	busy    func() bool // whether something else records (dictation)
}

// addWave puts the test's waveform in row, hidden until a test.
func (m *micTest) addWave(row *gtk.Box) {
	m.wave = gtkx.NewWave(18)
	m.wave.Widget.AddCssClass("dictation-wave")
	m.wave.Widget.SetSizeRequest(120, 18)
	m.wave.Widget.SetValign(gtk.AlignCenterValue)
	m.wave.Widget.SetVisible(false)
	row.Append(m.wave.Widget) // a canvas: floating, the box takes it
}

// addButton puts the Test button in row; sub is the row's line, which
// says how the test goes.
func (m *micTest) addButton(row *gtk.Box, sub gtk.Label) {
	m.sub = sub
	m.onClick = func(gtk.Button) { m.start() }
	m.onTick = func(uintptr) bool { return m.tick() }
	m.button = voiceButton(row, i18n.T("Test"), &m.onClick)
}

// testing reports whether a test runs: its row's line is the test's.
func (m *micTest) testing() bool { return m.rec != nil }

func (m *micTest) start() {
	if m.rec != nil || (m.busy != nil && m.busy()) {
		return
	}
	r, err := record.Start()
	if err != nil {
		m.sub.SetText(err.Error())
		return
	}
	m.rec, m.peak, m.until = r, 0, time.Now().Add(4*time.Second)
	m.wave.Clear()
	m.wave.Widget.SetVisible(true)
	m.button.SetSensitive(false)
	m.sub.SetText(i18n.T("Say something…"))
	glib.TimeoutAdd(dictationTick, &m.onTick, 0)
}

// tick draws the test's level, and ends it.
func (m *micTest) tick() bool {
	r := m.rec
	if r == nil {
		return false
	}
	l := r.Level()
	m.peak = max(m.peak, l)
	m.wave.Add(l)
	if time.Now().Before(m.until) && !r.Full() {
		return true
	}
	r.Cancel()
	m.rec = nil
	m.button.SetSensitive(true)
	if m.peak > 0.35 {
		m.sub.SetText(i18n.T("It hears you."))
	} else {
		m.sub.SetText(i18n.T("It heard nothing: is it the right one, or muted?"))
	}
	return false
}
