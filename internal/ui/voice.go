package ui

import (
	"context"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// Voice input: the bar's microphone, the shortcut behind it, the voice
// page of the settings. Recording and transcription are the voxtype
// daemon's; inua drives it and says what it is doing. voiceRequest answers
// `inua call voice …`, which scripts and the settings can drive too.
func (s *shellUI) voiceRequest(fn string) ipc.Response {
	if !s.cfg.Voice.Enable {
		return ipc.Response{Error: "voice input is off"}
	}
	v := s.svc.voice
	if v == nil {
		return ipc.Response{Error: "voice input is not available"}
	}
	var err error
	switch fn {
	case "toggle":
		err = v.Toggle()
	case "start":
		err = v.Start()
	case "stop":
		err = v.Stop()
	case "cancel":
		err = v.Cancel()
	default:
		return ipc.Response{Error: "unknown function voice " + fn}
	}
	if err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

func (s *shellUI) bindVoiceHandlers() {
	s.onVoiceButton = func(gtk.Button) {
		if r := s.voiceRequest("toggle"); r.Error != "" {
			log.Printf("voice: %s", r.Error)
		}
	}
}

// applyVoiceState follows the daemon's state into the bars' microphones.
func (s *shellUI) applyVoiceState() {
	st := s.sys.Voice
	for _, b := range s.bars {
		b.voiceIcon.SetFromIconName(shell.VoiceIcon(st))
		b.voiceIcon.SetTooltipText(shell.VoiceTooltip(st))
		toggle(&b.voice.Widget, "recording", st.Available && st.Status == "recording")
		toggle(&b.voice.Widget, "transcribing", st.Available && st.Status == "transcribing")
	}
}

// voiceSettings is the Voice page's part of the settings window.
type voiceSettings struct {
	page                 gtk.Box
	enable, ptt          gtk.Switch
	state, model, status gtk.Label
	stateRow, offRow     gtk.Box
	download             map[string]*gtk.Button
	setup                gtk.Button
	fixRow               gtk.Box
	fix                  gtk.Button
}

// getVoiceSettings makes the page's sections in its box.
func (s *shellUI) getVoiceSettings(ui *gtkx.Builder) {
	v := &s.settings.voice
	ui.Get("voice_page", &v.page)
	v.download = map[string]*gtk.Button{}
	if s.onVoiceSwitch == nil {
		s.onVoiceSwitch = func(sw gtk.Switch, on bool) bool {
			if s.settings.filling {
				return false
			}
			c := s.cfg
			switch sw.GetName() {
			case "voice_enable":
				c.Voice.Enable = on
				if on {
					s.ensureVoiceShortcut(&c)
				}
			case "voice_ptt":
				c.Voice.PushToTalk = on
			default:
				return false
			}
			s.changeSettings(c)
			return false // let the switch move
		}
		s.onVoiceModel = func(b gtk.Button) {
			for _, m := range voiceModels {
				if m.ID == b.GetName() {
					s.voiceDownload(m)
					return
				}
			}
			if b.GetName() == "voice_setup" {
				s.voiceSetupModel()
			}
		}
		s.onVoiceCheck = func(gtk.Button) {
			s.voiceFix()
		}
	}
	sw := func(body *gtk.Box, name, title, sub string) gtk.Switch {
		row, text := displayRow(title, sub)
		y := gtk.NewSwitch()
		y.SetValign(gtk.AlignCenterValue)
		y.SetName(name)
		y.ConnectStateSet(&s.onVoiceSwitch)
		gtkx.Append(row, &y.Widget)
		gtkx.Append(body, &row.Widget)
		_ = text
		return *y
	}

	sec, body := displaySection(i18n.T("VOICE"), i18n.T("Talk, and the words are typed where you are"))
	v.enable = sw(body, "voice_enable", i18n.T("Voice input"), i18n.T("A microphone in the bar; a shortcut holds to talk"))
	v.ptt = sw(body, "voice_ptt", i18n.T("Push to talk"), i18n.T("The shortcut records while it is held"))
	gtkx.Append(&v.page, &sec.Widget)

	sec, body = displaySection(i18n.T("ENGINE"), i18n.T("The voxtype daemon does the listening"))
	row, state := displayRow(i18n.T("State"), " ")
	v.state = *state
	v.stateRow = *row
	gtkx.Append(body, &row.Widget)
	row, model := displayRow(i18n.T("Model"), " ")
	v.model = *model
	gtkx.Append(body, &row.Widget)
	for _, m := range voiceModels {
		row, _ := displayRow(m.Label, m.Size)
		b := gtk.NewButton()
		b.SetName(m.ID)
		b.AddCssClass("outlined")
		dl := i18n.T("Download")
		if s.svc.voice != nil && s.svc.voice.ModelExists(m.ID) {
			dl = i18n.T("Downloaded")
			b.SetSensitive(false)
		}
		b.SetLabel(dl)
		b.ConnectClicked(&s.onVoiceModel)
		gtkx.Append(row, &b.Widget)
		v.download[m.ID] = b
		gtkx.Append(body, &row.Widget)
	}
	row, _ = displayRow(i18n.T("Other models"), i18n.T("voxtype's own setup lists more of them"))
	b := gtk.NewButton()
	b.SetName("voice_setup")
	b.AddCssClass("outlined")
	b.SetLabel(i18n.T("Open voxtype"))
	b.ConnectClicked(&s.onVoiceModel)
	v.setup = *b
	gtkx.Append(row, &v.setup.Widget)
	gtkx.Append(body, &row.Widget)
	row, _ = displayRow(i18n.T("Fix the setup"), i18n.T("The assistant looks at what is wrong and helps you fix it"))
	v.fixRow = *row
	b = gtk.NewButton()
	b.SetName("voice_fix")
	b.AddCssClass("outlined")
	b.SetLabel(i18n.T("Fix"))
	b.ConnectClicked(&s.onVoiceCheck)
	v.fix = *b
	gtkx.Append(row, &v.fix.Widget)
	gtkx.Append(body, &row.Widget)
	v.status = *gtk.NewLabel("")
	v.status.AddCssClass("sub")
	v.status.SetXalign(0)
	v.status.SetWrap(true)
	gtkx.Append(body, &v.status.Widget)
	row, _ = displayRow("", "")
	v.offRow = *row
	note := gtk.NewLabel(i18n.T("Voxtype is not installed, or its daemon is not running."))
	note.AddCssClass("sub")
	note.SetXalign(0)
	note.SetWrap(true)
	gtkx.Append(row, &note.Widget)
	gtkx.Append(body, &row.Widget)
	gtkx.Append(&v.page, &sec.Widget)
}

// fillVoiceSettings sets the page's controls from the config; showVoice
// follows the daemon.
func (s *shellUI) fillVoiceSettings() {
	v := &s.settings.voice
	if v.page.GoPointer() == 0 {
		return
	}
	v.enable.SetActive(s.cfg.Voice.Enable)
	v.ptt.SetActive(s.cfg.Voice.PushToTalk)
	s.showVoice()
}

// setVoiceButton shows the bar's microphone, or hides it while the voice
// input is off.
func (s *shellUI) setVoiceButton() {
	for _, b := range s.bars {
		b.voice.SetVisible(s.cfg.Voice.Enable)
	}
}

// showVoice fills the settings page: the daemon's state, the model, and
// what the shortcut is.
func (s *shellUI) showVoice() {
	st := &s.settings.voice
	if s.settings.win == nil || st.page.GoPointer() == 0 {
		return
	}
	v := s.sys.Voice
	st.state.SetText(voiceStateText(v))
	st.model.SetText(cmpOr(v.Model, i18n.T("Unknown")))
	st.stateRow.SetVisible(v.Available)
	st.offRow.SetVisible(!v.Available)
	st.fixRow.SetVisible(!v.Available)
}

// voiceStateText is the daemon's state in a word for the settings.
func voiceStateText(v system.VoiceState) string {
	switch {
	case !v.Available:
		return i18n.T("Not available")
	case v.Status == "recording":
		return i18n.T("Listening")
	case v.Status == "transcribing":
		return i18n.T("Transcribing")
	}
	return i18n.T("Ready")
}

// voiceDownloading is the model being downloaded, if any; the button that
// starts one says so while it runs.
var voiceDownloading string

// voiceDownload starts a model download in the background and reports
// what happened as the page's status line.
func (s *shellUI) voiceDownload(m system.VoiceModel) {
	if voiceDownloading != "" {
		return
	}
	voiceDownloading = m.ID
	st := &s.settings.voice
	st.status.SetText(i18n.Tf("Downloading %s…", m.Label))
	st.download[m.ID].SetSensitive(false)
	go func() {
		err := s.svc.voice.DownloadModel(context.Background(), m.Engine, m.ID, nil)
		if restart := s.svc.voice.RestartDaemon(); restart != nil {
			log.Printf("voice: restart the daemon: %v", restart)
		}
		gtkx.Post(func() {
			voiceDownloading = ""
			st.download[m.ID].SetSensitive(true)
			switch {
			case err != nil:
				st.status.SetText(err.Error())
			default:
				st.status.SetText(i18n.Tf("%s is ready.", m.Label))
			}
			s.showVoice()
		})
	}()
}

// voiceSetupModel opens voxtype's interactive model setup in a terminal,
// the only path that downloads the ONNX engines' models.
func (s *shellUI) voiceSetupModel() {
	term, dashE := desktop.Terminal(s.cfg.TerminalCommand())
	if err := system.SetupModelCommand(term, dashE).Start(); err != nil {
		s.setStatus(i18n.Tf("Could not start voxtype's setup: %s", err.Error()), true)
	}
}

// voiceModels are the settings page's list, in order.
var voiceModels = system.VoiceModels

// cmpOr is strings.TrimSpace(a) or b when that is empty.
func cmpOr(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

// ensureVoiceShortcut gives the voice action a default shortcut the first
// time the input is turned on, so the button and the key both work.
func (s *shellUI) ensureVoiceShortcut(c *config.Config) {
	for _, sc := range c.Shortcuts {
		if sc.Action == "voice" {
			return
		}
	}
	c.Shortcuts = append(c.Shortcuts, config.Shortcut{Action: "voice", Keys: "SUPER+SHIFT+V"})
}

var voiceFixing bool

// voiceFix asks inua-assistant to inspect and directly fix the voice input environment.
func (s *shellUI) voiceFix() {
	st := &s.settings.voice
	if voiceFixing {
		return
	}
	if _, err := exec.LookPath("inua-assistant"); err != nil {
		st.status.SetText(i18n.T("inua-assistant is not installed"))
		return
	}
	voiceFixing = true
	st.fix.SetSensitive(false)
	st.status.SetText(i18n.T("The assistant is looking at it."))
	prompt := s.voiceAssistantPrompt()
	go func() {
		err := desktop.Spawn([]string{"inua-assistant", "--ask", prompt}, "")
		gtkx.Post(func() {
			voiceFixing = false
			st.fix.SetSensitive(true)
			if err != nil {
				st.status.SetText(i18n.Tf("The assistant could not be started: %v", err))
			} else {
				st.status.SetText(i18n.T("The assistant is looking at it."))
			}
		})
	}()
}

// voiceAssistantPrompt builds the diagnostic prompt for the assistant to directly
// repair the voxtype environment.
func (s *shellUI) voiceAssistantPrompt() string {
	var b strings.Builder
	b.WriteString(i18n.T("The voice input in inua (the voxtype daemon) is not fully set up: this machine is missing something it needs. Check what is wrong below read-only first, then say what you would change and carry it out, reporting item by item."))
	b.WriteString("\n\n")

	distro := hostDistro()
	b.WriteString(i18n.Tf("This host: %s, distribution %s.", runtime.GOOS, distro))
	b.WriteString("\n")

	voxPath, err := exec.LookPath("voxtype")
	if err != nil {
		b.WriteString(i18n.T("voxtype executable: not found in PATH."))
	} else {
		b.WriteString(i18n.Tf("voxtype executable: %s.", voxPath))
	}
	b.WriteString("\n")

	v := s.sys.Voice
	if v.Available {
		b.WriteString(i18n.Tf("The daemon answers: status %s, model %s.", v.Status, v.Model))
	} else {
		b.WriteString(i18n.Tf("The daemon does not answer: %s.", cmpOr(v.Error, i18n.T("not running"))))
	}
	b.WriteString("\n")

	if out, err := exec.Command("systemctl", "--user", "is-active", "voxtype.service").Output(); err == nil {
		b.WriteString(i18n.Tf("systemd user service voxtype.service: %s.", strings.TrimSpace(string(out))))
		b.WriteString("\n")
	}

	var modelsDownloaded, modelsMissing []string
	if s.svc.voice != nil {
		for _, m := range voiceModels {
			if s.svc.voice.ModelExists(m.ID) {
				modelsDownloaded = append(modelsDownloaded, m.Label)
			} else {
				modelsMissing = append(modelsMissing, m.Label)
			}
		}
	}
	if len(modelsDownloaded) > 0 {
		b.WriteString(i18n.Tf("Downloaded models: %s.", strings.Join(modelsDownloaded, ", ")))
		b.WriteString("\n")
	}
	if len(modelsMissing) > 0 {
		b.WriteString(i18n.Tf("Missing models: %s.", strings.Join(modelsMissing, ", ")))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if distro == "nixos" {
		b.WriteString(i18n.T("This host is NixOS: if voxtype is installed, diagnose and fix the user service, configuration, or model files directly. If system configuration changes are required, specify what to add."))
		b.WriteString("\n")
	}

	b.WriteString(i18n.T("First check each thing read-only - which, --version, ls, systemctl status, journalctl --user -u voxtype.service - and tell me what you actually see. Do not change anything yet."))
	b.WriteString("\n")
	b.WriteString(i18n.T("Then carry out the needed fixes directly (e.g. systemctl --user reset-failed voxtype.service, restarting the service, downloading missing models, or fixing permissions with pkexec if needed)."))
	b.WriteString("\n")
	b.WriteString(i18n.T("Finally tell me how to confirm each fix - which command, and what to look for. I will press the button again in inua once it is done."))
	b.WriteString("\n")
	b.WriteString(i18n.T("No keys, tokens or passwords are needed for this, and none are in this message."))
	return b.String()
}

func hostDistro() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && k == "ID" {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}
