// Package assistantui is the assistant's window, a light app of its own
// (cmd/inua-assistant) so that only it links Mister Morph: the
// conversation, a line for each tool, approval cards, the answer as it
// streams, voice input. internal/assistant does the rest.
package assistantui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/assistant"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/assistant/record"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
)

// Register adds the assistant to the light apps, as "assistant".
func Register(ctx context.Context, background bool) {
	appui.Register("assistant", "Assistant", i18n.M("Assistant"), func() appui.View {
		return &view{ctx: ctx, background: background}
	})
}

// suggestions start an empty conversation.
var suggestions = []string{
	i18n.M("What is using the most CPU right now?"),
	i18n.M("Turn on Focus and set the volume to 30%"),
	i18n.M("What did I miss? Sum up my notifications"),
	i18n.M("Which devices on my tailnet are online?"),
	i18n.M("What is the weather this afternoon in Kyoto?"),
}

// The conversation owns these widgets; reset and shutdown drop the lookup.
type message struct {
	row     *gtk.Box
	text    *gtk.Label
	copy    *gtk.Button
	copyErr *gtk.Label
}

type view struct {
	c          *appui.Ctx
	win        *gtk.ApplicationWindow
	menu       *appui.MenuBar
	ctx        context.Context
	background bool
	enabled    bool
	model      gtk.Label
	notice     gtk.Label
	noticeBx   gtk.Box
	profile    gtk.DropDown
	talk       gtk.Box
	scroll     gtk.ScrolledWindow
	question   gtk.Entry
	send       gtk.Button

	// Voice input: the microphone, and in the question's place the
	// recording, then its transcription.
	voice, voiceDiscard          gtk.Button
	voiceBox, voiceMark, voWave  gtk.Box
	voiceTime, voiceState, voErr gtk.Label
	wave                         *gtkx.Wave       // how loud, a sample each tick
	rec                          *record.Recorder // while it records or is transcribed
	transcribing                 bool
	voiceCancel                  context.CancelFunc

	as        *assistant.Assistant
	stamp     string   // config.toml's and the keys' times when as was made
	names     []string // the profile drop-down's
	running   bool
	cancel    context.CancelFunc
	answer    *message // the answer being streamed
	first     []string // the question of the first start (--ask)
	apiCancel context.CancelFunc
	apiDone   chan struct{}
	reloading bool
	closed    bool
	built     bool // Build has run; before, the window is not shown yet

	approvals map[string]chan bool // by card, while a tool waits
	nextCard  int
	messages  map[uintptr]*message // by Copy button, using one shared callback

	onSend, onReset, onCard, onSuggestion func(gtk.Button)
	onCopy                                func(gtk.Button)
	onVoice, onVoiceDiscard               func(gtk.Button)
	onVoiceTick                           glib.SourceFunc
	onSettings                            func(gtk.Button)
	onProfile, onActive                   func(uintptr, uintptr, uintptr)
	onActivate                            func(gtk.Entry)
	onKey                                 func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onAsk                                 func(gio.SimpleAction, uintptr)
	onClose                               func(gtk.Window) bool
	onShutdown                            func(gio.Application)
	onBackground                          func(gio.SimpleAction, uintptr)
	onRefresh                             glib.SourceFunc
}

func (v *view) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	v.c = c
	c.StartHidden = v.background
	v.approvals = map[string]chan bool{}
	v.messages = map[uintptr]*message{}
	win := &gtk.ApplicationWindow{}
	v.win = win
	var reset, settings, openSettings gtk.Button
	ui := c.Load("assistant.ui")
	ui.Get("assistant", win)
	ui.Get("model", &v.model)
	ui.Get("notice", &v.notice)
	ui.Get("notice_box", &v.noticeBx)
	ui.Get("profile", &v.profile)
	ui.Get("settings", &settings)
	ui.Get("open_settings", &openSettings)
	ui.Get("talk", &v.talk)
	ui.Get("scroll", &v.scroll)
	ui.Get("question", &v.question)
	ui.Get("send", &v.send)
	ui.Get("reset", &reset)
	ui.Get("voice", &v.voice)
	ui.Get("voice_discard", &v.voiceDiscard)
	ui.Get("voice_box", &v.voiceBox)
	ui.Get("voice_mark", &v.voiceMark)
	ui.Get("voice_time", &v.voiceTime)
	ui.Get("voice_state", &v.voiceState)
	ui.Get("voice_wave", &v.voWave)
	ui.Get("voice_error", &v.voErr)
	ui.Close()
	v.wave = gtkx.NewWave(waveHeight)
	v.wave.Widget.AddCssClass("as-voice-wave")
	v.wave.Widget.SetHexpand(true)
	v.voWave.Append(v.wave.Widget)

	v.onSend = func(gtk.Button) {
		if v.running {
			v.cancel() // the button is Stop meanwhile
		} else {
			v.submit()
		}
	}
	v.onSettings = func(gtk.Button) { openAISettings() }
	v.onProfile = func(uintptr, uintptr, uintptr) {
		i := int(v.profile.GetSelected())
		if v.as == nil || i >= len(v.names) || v.names[i] == v.as.Profile() {
			return
		}
		if err := v.as.UseProfile(v.names[i]); err != nil {
			v.line("as-error", err.Error())
			return
		}
		v.model.SetText(v.as.Model())
		v.toolLine(i18n.Tf("%s answers from here", v.names[i]))
	}
	// Back from the settings: what changed applies to the next question.
	v.onActive = func(uintptr, uintptr, uintptr) {
		if win.IsActive() {
			v.refresh()
		}
	}
	v.onActivate = func(gtk.Entry) { v.submit() }
	v.onReset = func(gtk.Button) { v.newConversation() }
	v.onCopy = func(b gtk.Button) {
		m := v.messages[b.GoPointer()]
		if v.closed || m == nil || !b.GetSensitive() {
			return
		}
		m.copyErr.SetVisible(false)
		text := m.text.GetText()
		if text == "" {
			return
		}
		display := b.GetDisplay()
		if display == nil {
			m.copyErr.SetVisible(true)
			return
		}
		defer display.Unref()
		if display.IsClosed() {
			m.copyErr.SetVisible(true)
			return
		}
		clip := display.GetClipboard()
		if clip == nil {
			m.copyErr.SetVisible(true)
			return
		}
		defer clip.Unref()
		clip.SetText(text)
	}
	v.onSuggestion = func(b gtk.Button) { v.ask(b.GetName()) }
	v.onCard = func(b gtk.Button) {
		verb, id, _ := strings.Cut(b.GetName(), ":")
		ch := v.approvals[id]
		delete(v.approvals, id)
		if ch != nil {
			ch <- verb == "allow"
		}
	}
	v.onVoice = func(gtk.Button) {
		if v.rec == nil {
			v.startVoice()
		} else if !v.transcribing {
			v.stopVoice()
		}
	}
	v.onVoiceDiscard = func(gtk.Button) { v.dropVoice() }
	v.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == 0xff1b && v.rec != nil { // Escape drops the recording first
			v.dropVoice()
			return true
		}
		if keyval == 0xff1b && v.running { // Escape stops the answer
			v.cancel()
			return true
		}
		return false
	}
	v.onAsk = func(_ gio.SimpleAction, param uintptr) {
		q := (*glib.Variant)(unsafe.Add(unsafe.Pointer(nil), param)).GetString(nil)
		win.Present()
		v.ask(q)
	}
	v.onClose = func(gtk.Window) bool {
		v.dropVoice()
		if v.enabled || v.reloading {
			win.SetVisible(false)
			return true
		}
		return false
	}
	v.onShutdown = func(gio.Application) {
		if v.rec != nil && !v.transcribing {
			v.rec.Cancel() // before the program ends
		}
		v.dropVoice()
		v.closed = true
		clear(v.messages)
		v.answer = nil
		if v.cancel != nil {
			v.cancel()
		}
		if v.apiCancel != nil {
			v.apiCancel()
			if v.menu != nil {
				v.menu.Close()
			}
		}
	}
	win.ConnectCloseRequest(&v.onClose)
	c.App.ConnectShutdown(&v.onShutdown)
	v.onBackground = func(gio.SimpleAction, uintptr) { v.refresh() }
	background := gio.NewSimpleAction("background", nil)
	background.ConnectActivate(&v.onBackground)
	c.App.AddAction(background)
	background.Unref()
	v.send.ConnectClicked(&v.onSend)
	v.voice.ConnectClicked(&v.onVoice)
	v.voiceDiscard.ConnectClicked(&v.onVoiceDiscard)
	settings.ConnectClicked(&v.onSettings)
	openSettings.ConnectClicked(&v.onSettings)
	gtkx.ConnectNotify(&v.profile, "selected", &v.onProfile)
	gtkx.ConnectNotify(win, "is-active", &v.onActive)
	v.question.ConnectActivate(&v.onActivate)
	reset.ConnectClicked(&v.onReset)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&v.onKey)
	win.AddController(&keys.EventController)
	ask := gio.NewSimpleAction("ask", glib.NewVariantType("s"))
	ask.ConnectActivate(&v.onAsk)
	c.App.AddAction(ask)
	ask.Unref()
	v.menu = v.menuBar(win)
	v.setUp()
	v.built = true
	v.showSuggestions()
	v.question.GrabFocus()
	v.onRefresh = func(uintptr) bool {
		if v.closed {
			return false
		}
		v.refresh()
		return true
	}
	glib.TimeoutAdd(2000, &v.onRefresh, 0)
	go func() {
		<-v.ctx.Done()
		gtkx.Post(func() {
			v.closed = true
			if v.cancel != nil {
				v.cancel()
			}
			if v.apiCancel != nil {
				v.apiCancel()
			}
			done := v.apiDone
			go func() {
				if done != nil {
					<-done
				}
				gtkx.Post(func() { c.App.Quit() })
			}()
		})
	}()
	return win
}

func (v *view) ThemeChanged() { v.refresh() }

// Open takes `--ask QUESTION` from the first start.
func (v *view) Open(args []string) {
	for i, a := range args {
		if a == "--ask" && i+1 < len(args) {
			v.ask(args[i+1])
			return
		}
	}
}

// setUp makes the assistant from config.toml and the saved keys, or says
// why it cannot. A conversation going on carries over to the new one.
func (v *view) setUp() {
	v.apiCancel = nil
	v.stamp = stamp()
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		cfg = config.Default()
	}
	v.enabled = cfg.Assistant.Enable
	old := v.as
	v.as = nil
	say := func(msg string) {
		v.notice.SetText(msg)
		v.noticeBx.SetVisible(true)
		v.question.SetSensitive(false)
		v.send.SetSensitive(false)
		v.profile.SetVisible(false)
		v.model.SetText("")
		v.dropVoice()
		v.voice.SetVisible(false)
	}
	if !cfg.Assistant.Enable {
		say(i18n.T("The assistant is turned off. Turn it on in Settings → AI."))
		// Hidden, it has nothing to do: a background start, or a closed
		// window when it was turned off. A window opened says so instead.
		if !v.win.GetVisible() && (v.built || v.background) {
			v.c.App.Quit()
		}
		return
	}
	// The keyring may ask for its passphrase (pass: gpg's) here.
	set, err := assistant.Load(cfg.Assistant, keys.New())
	if err != nil {
		say(err.Error())
		return
	}
	// inua's tools come from its MCP server, this program with --mcp, as
	// for agents (docs/feat/mcp.md).
	exe, err := os.Executable()
	if err != nil {
		say(err.Error())
		return
	}
	set.MCPServer = []string{exe, "--mcp"}
	v.as, err = assistant.New(set, nil, v.approve)
	if err != nil {
		say(err.Error())
		return
	}
	if old != nil {
		v.as.Carry(old)
	}
	v.noticeBx.SetVisible(false)
	v.question.SetSensitive(true)
	v.send.SetSensitive(true)
	v.model.SetText(v.as.Model())
	v.names = v.as.Profiles()
	list := gtk.NewStringList(v.names)
	v.profile.SetModel(list)
	list.Unref()
	v.profile.SetSelected(0)
	v.profile.SetVisible(len(v.names) > 1)
	v.voice.SetVisible(v.as.Transcribes() || v.rec != nil)
	if v.rec == nil {
		v.voice.SetSensitive(record.Available())
		v.voice.SetTooltipText(voiceTip())
	}
	if set.ServeListen != "" {
		ctx, cancel := context.WithCancel(v.ctx)
		v.apiCancel = cancel
		done := make(chan struct{})
		v.apiDone = done
		as := v.as
		go func() {
			err := as.Serve(ctx)
			close(done)
			gtkx.Post(func() {
				if v.closed || v.apiDone != done || ctx.Err() != nil {
					return
				}
				v.apiCancel = nil
				if err != nil {
					log.Printf("assistant endpoint: %v", err)
					v.notice.SetText(i18n.Tf("Could not start the Console endpoint: %s", err.Error()))
					v.noticeBx.SetVisible(true)
					v.win.Present()
				}
			})
		}()
	}
}

// refresh makes the assistant again when its settings changed, between
// answers.
func (v *view) refresh() {
	if v.closed || v.running || v.reloading || stamp() == v.stamp {
		return
	}
	if v.apiCancel != nil {
		v.reloading = true
		v.question.SetSensitive(false)
		v.send.SetSensitive(false)
		v.apiCancel()
		done := v.apiDone
		go func() {
			<-done
			gtkx.Post(func() {
				if v.closed {
					return
				}
				v.setUp()
				v.reloading = false
			})
		}()
		return
	}
	hadNone := v.as == nil
	v.setUp()
	if hadNone && v.as != nil {
		clear(v.messages)
		v.answer = nil
		gtkx.Clear(&v.talk)
		v.showSuggestions()
	}
}

// stamp is when config.toml and the saved keys last changed.
func stamp() string {
	var b strings.Builder
	if info, err := os.Stat(config.DefaultPath()); err == nil {
		fmt.Fprint(&b, info.ModTime().UnixNano(), info.Size(), ";")
	}
	saved, _ := keys.New().Saved()
	for _, n := range slices.Sorted(maps.Keys(saved)) {
		fmt.Fprint(&b, n, "=", saved[n], ";")
	}
	return b.String()
}

// openAISettings opens the shell's settings at the AI page.
func openAISettings() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
		if _, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: "settings", Function: "open", Args: []string{"ai"}}); err != nil {
			log.Printf("open settings: %v", err)
		}
	}()
}

func (v *view) submit() {
	q := strings.TrimSpace(v.question.GetText())
	if q == "" {
		return
	}
	v.question.SetText("")
	v.ask(q)
}

// ask puts a question to the assistant; its answer streams in below it.
func (v *view) ask(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	v.refresh()
	if v.reloading {
		v.question.SetText(q)
		return
	}
	if v.as == nil {
		return
	}
	if v.running { // kept in the field for when this answer is done
		v.question.SetText(q)
		v.question.SetPosition(-1)
		return
	}
	v.dropSuggestions()
	v.line("as-you", q)
	v.answer = v.line("as-answer", "")
	v.answer.text.SetText("…") // waiting is not an answer to copy
	v.running = true
	v.send.SetLabel(i18n.T("Stop"))
	ctx, cancel := context.WithCancel(v.ctx)
	v.cancel = cancel
	v.profile.SetSensitive(false)
	ev := assistant.Events{
		Answer: func(text string) { gtkx.Post(func() { v.setAnswer(text) }) },
		Tool:   func(_, doing string) { gtkx.Post(func() { v.toolLine("→ " + doing + " …") }) },
		Denied: func(_, doing string) { gtkx.Post(func() { v.toolLine(i18n.Tf("✕ Not done: %s", doing)) }) },
		Error:  func(name string, err error) { gtkx.Post(func() { v.toolLine("! " + name + ": " + err.Error()) }) },
	}
	go func() {
		answer, err := v.as.Ask(ctx, q, ev)
		gtkx.Post(func() {
			if v.closed {
				return
			}
			v.running = false
			v.cancel()
			v.send.SetLabel(i18n.T("Ask"))
			v.send.SetSensitive(v.rec == nil)
			v.profile.SetSensitive(true)
			switch {
			case errors.Is(err, context.Canceled):
				v.setAnswer(cmp.Or(answer, i18n.T("Stopped.")))
			case err != nil:
				log.Printf("assistant: %v", err)
				v.setAnswer(i18n.Tf("Something went wrong: %s", err.Error()))
				v.answer.text.AddCssClass("as-error")
			case answer == "":
				v.setAnswer(i18n.T("(no answer)"))
			}
			v.answer = nil
			v.question.GrabFocus()
		})
	}()
}

// voiceTip is the microphone's tooltip: what it does, or what it needs.
func voiceTip() string {
	if !record.Available() {
		return i18n.T("Voice input records with pw-cat, from PipeWire (pipewire-bin on Debian and Ubuntu, pipewire on Arch, pipewire-utils on Fedora)")
	}
	return i18n.T("Voice input")
}

// startVoice starts recording, in the question's place.
func (v *view) startVoice() {
	v.voErr.SetVisible(false)
	r, err := record.Start()
	if err != nil {
		v.voiceError(i18n.Tf("Cannot record: %s", err.Error()))
		return
	}
	v.rec, v.transcribing = r, false
	v.question.SetVisible(false)
	v.voiceBox.SetVisible(true)
	v.voiceBox.RemoveCssClass("transcribing")
	for _, w := range []*gtk.Widget{&v.voiceMark.Widget, &v.voiceTime.Widget, &v.voWave.Widget} {
		w.SetVisible(true)
	}
	v.voiceState.SetVisible(false)
	v.voiceTime.SetText("0:00")
	v.wave.Clear()
	v.voice.SetIconName("media-playback-stop-symbolic")
	v.voice.SetTooltipText(i18n.T("Stop and transcribe"))
	v.voice.AddCssClass("as-recording")
	if !v.running { // Stop for the answer meanwhile
		v.send.SetSensitive(false)
	}
	v.voice.GrabFocus()
	v.onVoiceTick = func(uintptr) bool {
		if v.closed || v.rec != r || v.transcribing {
			return false
		}
		n := int(r.Length().Seconds())
		v.voiceTime.SetText(fmt.Sprintf("%d:%02d", n/60, n%60))
		v.wave.Add(r.Level())
		if r.Full() { // five minutes, or the microphone went away
			v.stopVoice()
			return false
		}
		return true
	}
	glib.TimeoutAdd(waveTick, &v.onVoiceTick, 0)
}

// The waveform: a sample of the level each tick.
const (
	waveTick   = 50 // ms
	waveHeight = 20 // px
)

// stopVoice ends the recording and has it transcribed; the words go into
// the question, to read over before asking.
func (v *view) stopVoice() {
	r, as := v.rec, v.as
	if r == nil || v.transcribing {
		return
	}
	v.transcribing = true
	for _, w := range []*gtk.Widget{&v.voiceMark.Widget, &v.voiceTime.Widget, &v.voWave.Widget} {
		w.SetVisible(false)
	}
	v.voiceState.SetVisible(true)
	v.voiceBox.AddCssClass("transcribing")
	v.voice.SetSensitive(false)
	ctx, cancel := context.WithCancel(v.ctx)
	v.voiceCancel = cancel
	go func() {
		defer cancel()
		wav, err := r.Stop()
		text := ""
		switch {
		case err != nil:
			err = errors.New(i18n.Tf("Cannot record: %s", err.Error()))
		case as == nil:
			err = errors.New(i18n.T("The assistant is not set up."))
		default:
			if text, err = as.Transcribe(ctx, wav, "voice.wav"); err != nil {
				log.Printf("assistant: transcribe: %v", err)
				err = errors.New(i18n.Tf("Cannot transcribe: %s", err.Error()))
			}
		}
		gtkx.Post(func() {
			if v.closed || v.rec != r || ctx.Err() != nil {
				return // dropped meanwhile
			}
			v.endVoice()
			switch {
			case err != nil:
				v.voiceError(err.Error())
			case text == "":
				v.voiceError(i18n.T("No speech was heard."))
			default:
				q := v.question.GetText()
				if q != "" && !strings.HasSuffix(q, " ") {
					q += " "
				}
				v.question.SetText(q + text)
				v.question.GrabFocus()
				v.question.SetPosition(-1)
			}
		})
	}()
}

// dropVoice drops the recording, or its transcription.
func (v *view) dropVoice() {
	r := v.rec
	if r == nil {
		return
	}
	if v.transcribing {
		v.voiceCancel()
	} else {
		go r.Cancel()
	}
	v.endVoice()
	if !v.closed {
		v.question.GrabFocus()
	}
}

// endVoice puts the question back in its place.
func (v *view) endVoice() {
	v.rec, v.transcribing, v.voiceCancel = nil, false, nil
	if v.closed {
		return
	}
	v.voiceBox.SetVisible(false)
	v.question.SetVisible(true)
	v.voice.RemoveCssClass("as-recording")
	v.voice.SetIconName("audio-input-microphone-symbolic")
	v.voice.SetSensitive(record.Available())
	v.voice.SetTooltipText(voiceTip())
	v.send.SetSensitive(v.as != nil && !v.reloading)
}

// voiceError says why voice input did not work, under the question.
func (v *view) voiceError(msg string) {
	v.voErr.SetText(msg)
	v.voErr.SetVisible(true)
}

// approve shows a card and waits for Allow or Deny.
func (v *view) approve(ctx context.Context, _, doing string) (allowed bool, err error) {
	ch := make(chan bool, 1)
	var card *gtk.Box
	var id string
	gtkx.Post(func() {
		if v.closed || ctx.Err() != nil {
			return
		}
		v.win.Present()
		v.dropSuggestions()
		v.nextCard++
		id = fmt.Sprint(v.nextCard)
		v.approvals[id] = ch
		card = gtk.NewBox(gtk.OrientationVerticalValue, 10)
		card.AddCssClass("as-card")
		l := gtk.NewLabel(doing + "?")
		l.SetXalign(0)
		l.SetWrap(true)
		gtkx.Append(card, &l.Widget)
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.SetHalign(gtk.AlignEndValue)
		for _, b := range [][2]string{{"deny", i18n.T("Don't")}, {"allow", i18n.T("Allow")}} {
			btn := gtk.NewButtonWithLabel(b[1])
			btn.SetName(b[0] + ":" + id)
			btn.AddCssClass(map[string]string{"deny": "outlined", "allow": "suggested-action"}[b[0]])
			btn.ConnectClicked(&v.onCard)
			gtkx.Append(row, &btn.Widget)
		}
		gtkx.Append(card, &row.Widget)
		v.insertBeforeAnswer(&card.Widget)
	})
	defer func() {
		gtkx.Post(func() {
			delete(v.approvals, id)
			if card == nil {
				return
			}
			defer card.Unref()
			if v.closed {
				return
			}
			gtkx.Clear(card)
			status := i18n.T("Not allowed")
			if err != nil {
				status = i18n.T("Canceled")
			} else if allowed {
				status = i18n.T("Allowed")
			}
			l := gtk.NewLabel(doing + " — " + status)
			l.AddCssClass("as-tool")
			l.SetXalign(0)
			l.SetWrap(true)
			gtkx.Append(card, &l.Widget)
		})
	}()
	select {
	case ok := <-ch:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// line adds selectable text and a keyboard-reachable Copy action.
func (v *view) line(class, text string) *message {
	row := gtk.NewBox(gtk.OrientationVerticalValue, 4)
	l := gtk.NewLabel(text)
	l.AddCssClass(class)
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordCharValue)
	l.SetSelectable(true)
	l.SetCanFocus(false) // or the first answer takes focus, all selected
	align := gtk.AlignStartValue
	if class == "as-you" {
		l.SetHalign(gtk.AlignEndValue)
		l.SetXalign(1)
		align = gtk.AlignEndValue
	}
	gtkx.Append(row, &l.Widget)
	copy := gtk.NewButtonWithLabel(i18n.T("Copy"))
	copy.AddCssClass("outlined")
	copy.AddCssClass("small")
	copy.SetHalign(align)
	copy.SetFocusOnClick(false)
	copy.SetSensitive(text != "")
	copy.ConnectClicked(&v.onCopy)
	gtkx.Append(row, &copy.Widget)
	copyErr := gtk.NewLabel(i18n.T("Clipboard is unavailable."))
	copyErr.AddCssClass("as-error")
	copyErr.AddCssClass("sub")
	copyErr.SetXalign(0)
	copyErr.SetWrap(true)
	copyErr.SetSelectable(true)
	copyErr.SetCanFocus(false)
	copyErr.SetVisible(false)
	gtkx.Append(row, &copyErr.Widget)
	m := &message{row: row, text: l, copy: copy, copyErr: copyErr}
	v.messages[copy.GoPointer()] = m
	gtkx.Append(&v.talk, &row.Widget)
	v.toBottom()
	return m
}

// toolLine goes before the answer being streamed, which stays last.
func (v *view) toolLine(text string) {
	if v.closed {
		return
	}
	l := gtk.NewLabel(text)
	l.AddCssClass("as-tool")
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetWrapMode(pango.WrapWordCharValue)
	v.insertBeforeAnswer(&l.Widget)
	l.Unref()
}

func (v *view) insertBeforeAnswer(w *gtk.Widget) {
	if v.answer != nil {
		if prev := v.answer.row.GetPrevSibling(); prev != nil {
			v.talk.InsertChildAfter(w, prev)
			prev.Unref()
		} else {
			v.talk.Prepend(w)
		}
	} else {
		v.talk.Append(w)
	}
	v.toBottom()
}

func (v *view) setAnswer(text string) {
	if !v.closed && v.answer != nil {
		v.answer.text.SetText(text)
		v.answer.copy.SetSensitive(text != "")
		v.toBottom()
	}
}

// toBottom scrolls to the newest line once it is laid out.
func (v *view) toBottom() {
	glib.IdleAdd(&scrollDown, uintptr(unsafe.Pointer(v)))
}

var scrollDown glib.SourceFunc = func(p uintptr) bool {
	v := (*view)(unsafe.Add(unsafe.Pointer(nil), p))
	adj := v.scroll.GetVadjustment()
	adj.SetValue(adj.GetUpper() - adj.GetPageSize())
	return false
}

func (v *view) showSuggestions() {
	if v.as == nil { // it cannot answer them
		return
	}
	box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	box.AddCssClass("as-suggestions")
	box.SetName("suggestions")
	head := gtk.NewLabel(i18n.T("TRY"))
	head.AddCssClass("kicker")
	head.SetXalign(0)
	gtkx.Append(box, &head.Widget)
	for _, s := range suggestions {
		b := gtk.NewButtonWithLabel(i18n.T(s))
		b.AddCssClass("as-suggestion")
		b.SetName(s)
		if l := b.GetChild(); l != nil {
			l.SetHalign(gtk.AlignStartValue)
		}
		b.ConnectClicked(&v.onSuggestion)
		gtkx.Append(box, &b.Widget)
	}
	gtkx.Append(&v.talk, &box.Widget)
}

func (v *view) dropSuggestions() {
	for w := v.talk.GetFirstChild(); w != nil; w = w.GetNextSibling() {
		if w.GetName() == "suggestions" {
			v.talk.Remove(w)
			return
		}
	}
}

func (v *view) menuBar(win *gtk.ApplicationWindow) *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		switch action.GetName() {
		case "new-conversation", "clear-conversation":
			v.newConversation()
		case "copy-reply":
			v.copyLastReply()
		case "settings":
			openAISettings()
		case "close-window":
			if v.win != nil {
				v.win.Close()
			}
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: i18n.T("File"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("new-conversation", i18n.T("New conversation")),
			action("close-window", i18n.T("Close Window")),
		}}}},
		{Title: i18n.T("Edit"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("clear-conversation", i18n.T("Clear conversation")),
		}}}},
		{Title: i18n.T("View"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("settings", i18n.T("AI settings")),
		}}}},
	}
	return appui.NewMenuBar(v.c, groups)
}

func (v *view) newConversation() {
	if v.running || v.as == nil || len(v.approvals) != 0 {
		return
	}
	v.as.Reset()
	clear(v.messages)
	v.answer = nil
	gtkx.Clear(&v.talk)
	v.showSuggestions()
}

func (v *view) copyLastReply() {
	if v.closed || v.answer == nil {
		return
	}
	text := v.answer.text.GetText()
	if text == "" || v.win == nil {
		return
	}
	display := v.win.GetDisplay()
	if display == nil || display.IsClosed() {
		if display != nil {
			display.Unref()
		}
		return
	}
	defer display.Unref()
	clip := display.GetClipboard()
	if clip == nil {
		return
	}
	defer clip.Unref()
	clip.SetText(text)
}
