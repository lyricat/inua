package ui

import (
	"context"
	"os"
	"slices"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hyprconf"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// The Dictation page (Settings → Devices → Dictation):
//
//	■ [DICTATION]  Talk, and the words are typed where you are
//	Dictation                                               [switch]
//	■ Ready   hold Super Shift V, or click the microphone   [action]
//	Shortcut  hold to talk; Escape drops it   [keycaps] [Hold|Press] [Change]
//	Card      the waveform, then the words                  [where ▾]
//	Microphone                                       [mic ▾]  [Test]
//	■ [ENGINE]     What turns your speech into text
//	Runs      the recording stays here    [On this computer|An AI profile]
//	Speech models (voxtype) / Profile (the AI's)
//	Typing    pasted where you are, your clipboard put back
type voiceSettings struct {
	page    gtk.Box
	filling bool // the page's controls being set, not changed

	enable gtk.Switch
	// The status: a mark, what dictation is doing or what it is missing,
	// and the one thing to do about it.
	statusRow                        gtk.Box
	mark                             gtk.Box
	state, detail                    gtk.Label
	install, site, start, aiSettings gtk.Button
	busy                             bool   // installing voxtype
	progress                         string // how far

	keysRow     gtk.Box
	keysSub     gtk.Label
	keycaps     gtk.Box
	hold, press gtk.ToggleButton
	showRow     gtk.Box
	showDrop    gtk.DropDown
	micRow      gtk.Box
	micSub      gtk.Label
	micPick     devicePicker
	mic         micTest

	engine          gtk.Box // the ENGINE section, while dictation is on
	local, ai       gtk.ToggleButton
	runsSub         gtk.Label
	profileRow      gtk.Box
	profile         gtk.Label
	models          voxtypeModels
	typingRow       gtk.Box
	typing, typeCmd gtk.Label
	status          gtk.Label // the engine's errors, under its rows
}

// voxtypeSite is voxtype's own page.
const voxtypeSite = "https://voxtype.io"

// voiceShowLabels name config.VoiceShows.
func voiceShowLabels() []string {
	return []string{i18n.T("At the top of your window"), i18n.T("In the screen's corner"), i18n.T("Off: the bar's microphone alone")}
}

func (s *shellUI) bindVoicePage() {
	v := &s.settings.voice
	s.onVoiceSwitch = func(_ gtk.Switch, on bool) bool {
		if s.settings.filling || v.filling {
			return false
		}
		c := s.cfg
		c.Voice.Enable = on
		c.AddVoiceShortcut()
		s.changeSettings(c)
		return false // let the switch move
	}
	s.onVoiceMode = func(t gtk.ToggleButton) {
		if s.settings.filling || v.filling || !t.GetActive() {
			return
		}
		c := s.cfg
		c.Voice.PushToTalk = t.GoPointer() == v.hold.GoPointer()
		if c.Voice != s.cfg.Voice {
			s.changeSettings(c)
		}
	}
	s.onVoiceRuns = func(t gtk.ToggleButton) {
		if s.settings.filling || v.filling || !t.GetActive() {
			return
		}
		c := s.cfg
		c.Voice.Engine = "voxtype"
		if t.GoPointer() == v.ai.GoPointer() {
			c.Voice.Engine = "ai"
		}
		if c.Voice != s.cfg.Voice {
			v.setStatus("")
			s.changeSettings(c)
		}
	}
	s.onVoiceShow = func(uintptr, uintptr, uintptr) {
		i := int(v.showDrop.GetSelected())
		if s.settings.filling || v.filling || i < 0 || i >= len(config.VoiceShows) || config.VoiceShows[i] == s.cfg.Voice.Show {
			return
		}
		c := s.cfg
		c.Voice.Show = config.VoiceShows[i]
		s.changeSettings(c)
	}
	s.onVoiceKeys = func(gtk.Button) { s.showSettingsPage("shortcuts") }
	s.onVoiceSetup = func(gtk.Button) { s.showSettingsPage("ai") }
	s.onVoiceAI = func(gtk.Button) { s.showSettingsPage("ai") }
	s.onVoiceInstall = func(gtk.Button) { s.voiceInstall() }
	s.onVoiceStart = func(gtk.Button) { s.voiceStartDaemon() }
	s.onVoiceSite = func(gtk.Button) {
		if err := desktop.Spawn([]string{"xdg-open", voxtypeSite}, ""); err != nil {
			v.setStatus(err.Error())
		}
	}
}

// voiceButton is an outlined button at the end of a row.
func voiceButton(row *gtk.Box, label string, f *func(gtk.Button)) gtk.Button {
	b := gtk.NewButton()
	b.AddCssClass("outlined")
	b.SetValign(gtk.AlignCenterValue)
	b.SetLabel(label)
	b.ConnectClicked(f)
	gtkx.Append(row, &b.Widget)
	return *b
}

// segmented is a strip of toggle buttons, one of which is chosen.
func segmented(row *gtk.Box, f *func(gtk.ToggleButton), labels ...string) []gtk.ToggleButton {
	strip := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	strip.AddCssClass("segmented")
	strip.SetValign(gtk.AlignCenterValue)
	var out []gtk.ToggleButton
	for i, l := range labels {
		t := gtk.NewToggleButtonWithLabel(l)
		if i > 0 {
			t.SetGroup(&out[0])
		}
		t.ConnectToggled(f)
		strip.Append(&t.Widget)
		out = append(out, *t)
	}
	gtkx.Append(row, &strip.Widget)
	return out
}

// getVoiceSettings makes the page.
func (s *shellUI) getVoiceSettings(ui *gtkx.Builder) {
	v := &s.settings.voice
	ui.Get("voice_page", &v.page)
	if s.onVoiceMode == nil {
		s.bindVoicePage()
	}

	sec, body := displaySection(i18n.T("DICTATION"), i18n.T("Talk, and the words are typed where you are"))
	row, _ := displayRow(i18n.T("Dictation"), i18n.T("A microphone in the bar, and a shortcut to talk"))
	v.enable = *gtk.NewSwitch()
	v.enable.SetValign(gtk.AlignCenterValue)
	v.enable.ConnectStateSet(&s.onVoiceSwitch)
	gtkx.Append(row, &v.enable.Widget)
	gtkx.Append(body, &row.Widget)

	row, state, detail := titledRow()
	row.AddCssClass("dictation-status")
	row.SetSpacing(10) // the mark close to what it marks
	v.statusRow, v.state, v.detail = *row, *state, *detail
	v.mark = *gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	v.mark.AddCssClass("mark")
	v.mark.SetValign(gtk.AlignCenterValue)
	row.Prepend(&v.mark.Widget)
	v.site = voiceButton(row, i18n.T("voxtype.io"), &s.onVoiceSite)
	v.install = voiceButton(row, i18n.T("Install"), &s.onVoiceInstall)
	v.start = voiceButton(row, i18n.T("Start"), &s.onVoiceStart)
	v.aiSettings = voiceButton(row, i18n.T("AI Settings"), &s.onVoiceAI)
	gtkx.Append(body, &row.Widget)

	row, keysSub := displayRow(i18n.T("Shortcut"), " ")
	v.keysRow, v.keysSub = *row, *keysSub
	v.keycaps = *gtk.NewBox(gtk.OrientationHorizontalValue, 4)
	v.keycaps.SetValign(gtk.AlignCenterValue)
	gtkx.Append(row, &v.keycaps.Widget)
	modes := segmented(row, &s.onVoiceMode, i18n.T("Hold"), i18n.T("Press"))
	v.hold, v.press = modes[0], modes[1]
	voiceButton(row, i18n.T("Change"), &s.onVoiceKeys)
	gtkx.Append(body, &row.Widget)

	row, _ = displayRow(i18n.T("Card"), i18n.T("The waveform as you talk, then the words"))
	v.showRow = *row
	v.showDrop = *gtk.NewDropDownFromStrings(voiceShowLabels())
	v.showDrop.SetValign(gtk.AlignCenterValue)
	gtkx.ConnectNotify(&v.showDrop, "selected", &s.onVoiceShow)
	gtkx.Append(row, &v.showDrop.Widget)
	gtkx.Append(body, &row.Widget)

	row, micSub := displayRow(i18n.T("Microphone"), " ")
	v.micRow, v.micSub = *row, *micSub
	v.mic.addWave(row)
	v.micPick.build(row, func(text string) { v.micSub.SetText(text) }, &v.filling)
	v.mic.addButton(row, v.micSub)
	v.mic.busy = func() bool { return s.sys.Voice.Status == "recording" }
	gtkx.Append(body, &row.Widget)
	gtkx.Append(&v.page, &sec.Widget)

	sec, body = displaySection(i18n.T("ENGINE"), i18n.T("What turns your speech into text"))
	v.engine = *sec
	row, runsSub := displayRow(i18n.T("Runs"), " ")
	v.runsSub = *runsSub
	runs := segmented(row, &s.onVoiceRuns, i18n.T("On this computer"), i18n.T("With an AI profile"))
	v.local, v.ai = runs[0], runs[1]
	gtkx.Append(body, &row.Widget)
	row, profile := displayRow(i18n.T("Profile"), " ")
	v.profileRow, v.profile = *row, *profile
	voiceButton(row, i18n.T("Change"), &s.onVoiceSetup)
	gtkx.Append(body, &row.Widget)
	s.buildVoxtypeModels(body)
	row, typing := displayRow(i18n.T("Typing"), " ")
	v.typingRow, v.typing = *row, *typing
	v.typeCmd = *gtk.NewLabel("")
	v.typeCmd.AddCssClass("mono-sub")
	v.typeCmd.SetSelectable(true)
	v.typeCmd.SetValign(gtk.AlignCenterValue)
	gtkx.Append(row, &v.typeCmd.Widget)
	gtkx.Append(body, &row.Widget)
	v.status = *gtk.NewLabel("")
	v.status.AddCssClass("row-error")
	v.status.AddCssClass("flush")
	v.status.SetXalign(0)
	v.status.SetWrap(true)
	v.status.SetSelectable(true)
	v.status.SetVisible(false)
	gtkx.Append(body, &v.status.Widget)
	gtkx.Append(&v.page, &sec.Widget)
}

// titledRow is a displayRow whose title changes too: both its labels.
func titledRow() (row *gtk.Box, title, sub *gtk.Label) {
	row = gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	row.AddCssClass("row")
	text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	title, sub = gtk.NewLabel(""), gtk.NewLabel("")
	title.AddCssClass("title")
	sub.AddCssClass("sub")
	for _, l := range []*gtk.Label{title, sub} {
		l.SetXalign(0)
		l.SetWrap(true)
		gtkx.Append(text, &l.Widget)
	}
	gtkx.Append(row, &text.Widget)
	return row, title, sub
}

// fillVoiceSettings sets the page from the config; showVoice follows the
// engine.
func (s *shellUI) fillVoiceSettings() {
	if s.settings.voice.page.GoPointer() == 0 {
		return
	}
	s.showVoice()
}

// setVoiceButton shows the bar's microphone, or hides it while dictation
// is off.
func (s *shellUI) setVoiceButton() {
	for _, b := range s.bars {
		b.voice.SetVisible(s.cfg.Voice.Enable)
	}
}

// showVoice fills the page from the config and the engine: dictation's
// switch alone while it is off; else its status, its settings and the
// engine's.
func (s *shellUI) showVoice() {
	v := &s.settings.voice
	if s.settings.win == nil || v.page.GoPointer() == 0 {
		return
	}
	st, on, ai := s.sys.Voice, s.cfg.Voice.Enable, s.cfg.Voice.Engine == "ai"
	v.filling = true
	defer func() { v.filling = false }()
	v.enable.SetActive(on)
	for _, w := range []*gtk.Widget{&v.statusRow.Widget, &v.keysRow.Widget, &v.showRow.Widget, &v.micRow.Widget, &v.engine.Widget} {
		w.SetVisible(on)
	}
	if !on {
		return
	}
	s.showVoiceStatus(st, ai)
	s.showVoiceMic()

	keys := s.voiceKeys()
	for c := v.keycaps.GetFirstChild(); c != nil; c = v.keycaps.GetFirstChild() {
		v.keycaps.Remove(c)
	}
	if keys == "" {
		gtkx.Append(&v.keycaps, &keycapText(i18n.T("None")).Widget)
	} else {
		for _, k := range strings.Fields(keys) {
			gtkx.Append(&v.keycaps, &keycapText(k).Widget)
		}
	}
	v.hold.SetActive(s.cfg.Voice.PushToTalk)
	v.press.SetActive(!s.cfg.Voice.PushToTalk)
	if s.cfg.Voice.PushToTalk {
		v.keysSub.SetText(i18n.T("Hold it to talk; Escape drops what you said"))
	} else {
		v.keysSub.SetText(i18n.T("Press it to start and again to end; Escape drops what you said"))
	}
	v.showDrop.SetSelected(uint32(max(0, slices.Index(config.VoiceShows, s.cfg.Voice.Show))))

	v.local.SetActive(!ai)
	v.ai.SetActive(ai)
	if ai {
		v.runsSub.SetText(i18n.T("The recording goes to the AI profile's provider"))
	} else {
		v.runsSub.SetText(i18n.T("voxtype turns it into text here; the recording stays on this computer"))
	}
	for _, w := range []*gtk.Widget{&v.local.Widget, &v.ai.Widget} {
		w.SetSensitive(!v.busy)
	}
	v.profileRow.SetVisible(ai && st.Available)
	v.profile.SetText(st.Model)
	s.showVoxtypeModels(!ai && st.Available)

	v.typingRow.SetVisible(st.Available)
	v.typing.SetText(i18n.T("The words are pasted where you are, and your clipboard put back"))
	v.typeCmd.SetText("")
	if st.Available {
		v.setStatus("")
	}
}

// showVoiceStatus fills the status row: its mark (hollow when something is
// missing, filled when ready, red while it listens), what dictation does
// or lacks, and the one thing to do about it.
func (s *shellUI) showVoiceStatus(st system.VoiceState, ai bool) {
	v := &s.settings.voice
	var title, detail string
	switch {
	case v.busy:
		title, detail = i18n.T("Installing voxtype…"), v.progress
	case st.Status == system.VoiceNoProfile:
		title = i18n.T("No AI profile turns speech into text")
		detail = i18n.T("Turn on Voice for a profile in Settings → AI, and choose it for Voice input")
	case ai && st.Status == system.VoiceMissing:
		title = i18n.T("pw-cat is not installed")
		detail = i18n.Tf("Dictation records with PipeWire's pw-cat: %s", installCommand("pipewire-bin"))
	case st.Status == system.VoiceMissing:
		title = i18n.T("voxtype is not installed")
		detail = i18n.Tf("Install downloads voxtype %s and a speech model for your language from GitHub, about %d MB, and checks them before they run", system.VoxtypeVersion, system.VoxtypeStartSize(i18n.Lang()))
	case st.Status == system.VoiceStopped:
		title, detail = i18n.T("voxtype is not running"), i18n.T("It is installed; its service is stopped")
	case !st.Available:
		title, detail = i18n.T("Looking…"), " "
	case st.Status == "recording":
		title, detail = i18n.T("Listening…"), i18n.T("Escape drops what you said")
	case st.Status == "transcribing":
		title, detail = i18n.T("Transcribing…"), " "
	default:
		title = i18n.T("Ready")
		keys := s.voiceKeys()
		switch {
		case keys == "":
			detail = i18n.T("Click the microphone in the bar, and talk")
		case s.cfg.Voice.PushToTalk:
			detail = i18n.Tf("Hold %s, or click the microphone in the bar, and talk", keys)
		default:
			detail = i18n.Tf("Press %s, or click the microphone in the bar, and talk", keys)
		}
	}
	v.state.SetText(title)
	v.detail.SetText(detail)
	toggle(&v.mark.Widget, "on", st.Available && !v.busy)
	toggle(&v.mark.Widget, "listening", st.Available && st.Status == "recording")
	local := !ai && !v.busy
	v.install.SetVisible(local && st.Status == system.VoiceMissing)
	v.site.SetVisible(local && st.Status == system.VoiceMissing)
	v.start.SetVisible(local && st.Status == system.VoiceStopped)
	v.aiSettings.SetVisible(ai && st.Status == system.VoiceNoProfile)
}

// setStatus shows what went wrong under the engine's rows, or nothing.
func (v *voiceSettings) setStatus(text string) {
	v.status.SetText(text)
	v.status.SetVisible(text != "")
}

// voiceKeys is dictation's shortcut as shown ("Super Shift V"), "" for none.
func (s *shellUI) voiceKeys() string {
	for _, sc := range s.cfg.Shortcuts {
		if sc.Action == "voice" {
			return hyprconf.Label(sc.Keys)
		}
	}
	return ""
}

// installCommand is the command that installs a package here, by the
// distribution's package manager.
func installCommand(pkg string) string {
	data, _ := os.ReadFile("/etc/os-release")
	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		if k, val, ok := strings.Cut(line, "="); ok && (k == "ID" || k == "ID_LIKE") {
			ids = append(ids, strings.Fields(strings.Trim(val, `"'`))...)
		}
	}
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu":
			return "sudo apt install " + pkg
		case "fedora", "rhel":
			return "sudo dnf install " + pkg
		case "arch":
			return "sudo pacman -S " + pkg
		case "suse", "opensuse":
			return "sudo zypper install " + pkg
		case "nixos":
			return "nix profile install nixpkgs#" + pkg
		}
	}
	return pkg
}

// showVoiceMic fills the microphone row from the audio service, while
// showVoice fills the page.
func (s *shellUI) showVoiceMic() {
	v := &s.settings.voice
	a := s.sys.Audio
	v.micPick.fill(a.Inputs, a.Input)
	v.mic.button.SetVisible(len(a.Inputs) > 0)
	switch {
	case v.mic.testing():
	case len(a.Inputs) == 0:
		v.micSub.SetText(i18n.T("PipeWire has no microphone"))
	default:
		v.micSub.SetText(i18n.T("The system's input, which dictation records from"))
	}
}

// voiceInstall installs voxtype (system.InstallVoxtype), saying each step
// in the status row; the poll shows it come up.
func (s *shellUI) voiceInstall() {
	v := &s.settings.voice
	if v.busy {
		return
	}
	v.busy, v.progress = true, i18n.T("Downloading…")
	v.setStatus("")
	s.showVoice()
	lang := i18n.Lang()
	go func() {
		err := system.InstallVoxtype(context.Background(), lang, func(step system.VoxtypeStep, done float64) {
			var text string
			switch {
			case step == system.VoxtypeDownloading:
				text = i18n.Tf("Downloading voxtype %s… %s", system.VoxtypeVersion, shell.Percent(done))
			case step == system.VoxtypeModelDownloading && done >= 0:
				text = i18n.Tf("Downloading a speech model… %s", shell.Percent(done))
			case step == system.VoxtypeModelDownloading:
				text = i18n.T("Downloading a speech model…")
			case step == system.VoxtypeService:
				text = i18n.T("Starting its service…")
			}
			gtkx.Post(func() {
				v.progress = text
				s.showVoice()
			})
		})
		gtkx.Post(func() {
			v.busy, v.models.loaded = false, false
			if err != nil {
				v.setStatus(i18n.Tf("voxtype was not installed: %s", err.Error()))
			}
			s.showVoice()
		})
	}()
}

// voiceStartDaemon starts voxtype's user service; the poll shows it come
// up, and its error shows under the engine's rows.
func (s *shellUI) voiceStartDaemon() {
	v := &s.settings.voice
	v.start.SetSensitive(false)
	v.setStatus("")
	go func() {
		err := s.svc.voice.StartDaemon()
		gtkx.Post(func() {
			v.start.SetSensitive(true)
			if err != nil {
				v.setStatus(i18n.Tf("voxtype did not start: %s", err.Error()))
			}
		})
	}()
}
