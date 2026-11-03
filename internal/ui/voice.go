package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/clipboard"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/hyprconf"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/notify"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/wayfire"
)

// Dictation (voice input into any window): the bar's microphone, the
// shortcut behind it, the Dictation page of the settings. Recording and transcription are the voxtype
// daemon's; inua drives it and says what it is doing. voiceRequest answers
// `inua call voice …`, which scripts and the settings can drive too.
func (s *shellUI) voiceRequest(fn string, args []string) ipc.Response {
	if !s.cfg.Voice.Enable {
		return ipc.Response{Error: "dictation is off (Settings → Dictation)"}
	}
	v := s.svc.dictation
	if v == nil {
		return ipc.Response{Error: "dictation is not available"}
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
	case "type": // voxtype's words (its post-process command): typed as the AI's are
		text := strings.TrimSpace(strings.Join(args, " "))
		if text != "" {
			go func() {
				if err := s.svc.typer.typeText(text); err != nil {
					gtkx.Post(func() { s.dictationFailed(err) })
				}
			}()
		}
	default:
		return ipc.Response{Error: "unknown function voice " + fn}
	}
	if err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true}
}

func (s *shellUI) bindVoiceHandlers() {
	// The bar's microphone records when the daemon is there, else opens
	// the page that says what is missing.
	s.onVoiceButton = func(gtk.Button) {
		if !s.sys.Voice.Available {
			s.openSettings()
			s.showSettingsPage("voice")
			return
		}
		if r := s.voiceRequest("toggle", nil); r.Error != "" {
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
	s.dictationState(st)
	s.showVoice()
	s.handOverVoxtype(st)
	// While it records, Escape drops it and its keys end it.
	if recording := st.Available && st.Status == "recording"; recording != s.svc.inSubmap {
		s.svc.inSubmap = recording
		if recording {
			s.dispatchSubmap(hyprconf.DictationSubmap)
		} else {
			s.dispatchSubmap("reset")
		}
	}
}

// dictationEngine turns speech into text: voxtype's daemon (system.Voice)
// or the AI profile for voice input (system.Dictation).
type dictationEngine interface {
	Run(ctx context.Context, onChange func(system.VoiceState))
	Start() error
	Stop() error
	Toggle() error
	Cancel() error
}

// followVoice runs the engine dictation is set to while it is on, a new
// one when the engine or the AI's profile changes, and none while it is
// off, so nothing is read then.
func (s *shellUI) followVoice() {
	v := &s.svc
	var want, profile string
	if s.cfg.Voice.Enable {
		want = s.cfg.Voice.Engine
		if want == "ai" {
			if s.cfg.Assistant.Enable {
				profile = s.cfg.Assistant.TranscriptionProfile()
			}
			want += ":" + profile
		}
	}
	if want == v.voiceRun {
		return
	}
	if v.voiceStop != nil {
		v.voiceStop()
		v.voiceStop, v.dictation = nil, nil
	}
	v.voiceRun, v.voxtypeSet = want, false
	v.typer = dictationTyper{history: s.clipboard.path,
		hypr: func() (hyprNow, error) {
			return gtkx.Call(time.Second, func() hyprNow {
				return hyprNow{client: s.client, wayfire: s.wayfire, lua: s.hyprLua}
			})
		},
		wayfireFocus: func(ctx context.Context) (string, error) {
			if s.wayfire == nil {
				return "", errors.New(i18n.T("Wayfire is not connected"))
			}
			return s.wayfire.FocusedAppID(ctx)
		},
		heard: func(text string) { gtkx.Post(func() { s.dictationHeard(text) }) }}
	s.sys.Voice = system.VoiceState{}
	if want == "" {
		s.applyVoiceState()
		return
	}
	var e dictationEngine = v.voice
	if s.cfg.Voice.Engine == "ai" {
		e = &system.Dictation{Profile: profile, Assistant: "inua-assistant", Type: v.typer.typeText,
			Failed: func(err error) { gtkx.Post(func() { s.dictationFailed(err) }) }}
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.dictation, v.voiceStop = e, cancel
	s.applyVoiceState()
	go e.Run(ctx, func(st system.VoiceState) {
		s.update(func(x *shell.System) {
			if ctx.Err() != nil { // from an engine since stopped
				return
			}
			if !st.Available && x.Voice.Available && time.Now().Before(v.voxtypeQuiet) {
				return // inua restarting voxtype: back in a moment
			}
			x.Voice = st
		})
	})
}

// handOverVoxtype has voxtype, once it is up, hand its words to inua to
// paste (`inua call voice type -`) rather than output them itself: inua
// pastes them as it does the AI engine's, and voxtype's own output would
// put them on the clipboard and leave them there.
func (s *shellUI) handOverVoxtype(st system.VoiceState) {
	v := &s.svc
	if v.voxtypeSet || !st.Available || s.cfg.Voice.Engine != "voxtype" {
		return
	}
	helper, err := inuaHelper()
	if err != nil {
		log.Printf("dictation: no inua beside inua-shell to hand voxtype's words to: %v", err)
		return
	}
	v.voxtypeSet = true
	v.voxtypeQuiet = time.Now().Add(15 * time.Second) // it may restart it
	command := system.VoxtypeHandsOver(helper, s.opts.Instance)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if changed, err := system.ConfigureVoxtype(ctx, command); err != nil {
			log.Printf("dictation: voxtype's hand-over: %v", err)
		} else if changed {
			log.Printf("dictation: voxtype hands its words to %s", command)
		}
	}()
}

// dictationNote says why a dictation came to nothing, as a notification.
func (s *shellUI) dictationNote(err error) {
	if s.notes.srv == nil {
		log.Printf("dictation: %v", err)
		return
	}
	s.postNote(notify.Notification{
		ID: s.notes.srv.NewID(), App: "inua", Icon: "microphone-slash-symbolic",
		Summary: i18n.T("Dictation did not work"), Body: notify.Markup(err.Error()), Urgency: notify.Normal, Time: time.Now(),
	})
}

// spaced ends a dictation with a space, so the next one does not run on,
// unless it ends in a space already or in a script written without them
// (Chinese, Japanese).
func spaced(text string) string {
	r, _ := utf8.DecodeLastRuneInString(text)
	if text == "" || unicode.IsSpace(r) || unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) ||
		r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef { // CJK and full-width punctuation
		return text
	}
	return text + " "
}

// dictationTyper puts dictation's words into the focused window, by the
// clipboard and the paste shortcut: Ctrl+V, Ctrl+Shift+V in a terminal,
// sent to the window by address so a window under the pointer cannot take
// it, and the clipboard's text put back, neither copy kept in the history.
// Made on the main thread, typeText runs on the engine's goroutine.
//
// Not wtype: its synthetic keys go through whichever keyboard has the
// keymap, and with an input method's virtual keyboard holding it (fcitx5,
// say) they arrive as other characters ("hello" as "v22h3hhhhhh"), which is
// what made dictation paste nothing usable into apps like Telegram.
type dictationTyper struct {
	history string // the clipboard history's file
	// hypr is the shell's Hyprland client, read on the main thread, and
	// whether it takes 0.55's Lua dispatchers: both are known only after
	// start-up.
	hypr func() (hyprNow, error)
	// wayfireFocus names the application of the window focused on Wayfire.
	wayfireFocus func(context.Context) (string, error)
	heard        func(text string)
	// The clipboard, for a test: its text, and putting text on it. Left
	// nil, wl-paste and wl-copy serve it.
	clipboard func() []byte
	copy      func([]byte) error
}

// hyprNow is the shell's Hyprland client (nil without Hyprland) and
// whether it takes 0.55's Lua dispatchers, both known only after start-up.
type hyprNow struct {
	client  *hypr.Client
	wayfire *wayfire.Client
	lua     bool
}

func (t dictationTyper) typeText(text string) error {
	t.heard(text)
	return t.paste(spaced(text))
}

// paste puts text into the focused window through the clipboard: the
// history's watcher skips it, the window is sent the paste shortcut, and the
// clipboard's own text is put back once the window has read it. A terminal
// gets Ctrl+Shift+V, which pastes there.
func (t dictationTyper) paste(text string) error {
	h, err := t.hypr()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if h.client == nil {
		if t.wayfireFocus == nil {
			return errors.New(i18n.T("dictation pastes with Hyprland or Wayfire, which is not running"))
		}
		class, err := t.wayfireFocus(ctx)
		if err != nil {
			return err
		}
		return t.withClipboard(text, func() error {
			return wtypePaste(ctx, isTerminalClass(class))
		})
	}
	reply, err := h.client.Request(ctx, "j/activewindow")
	if err != nil {
		return err
	}
	var w struct{ Address, Class string }
	if json.Unmarshal(reply, &w) != nil || w.Address == "" {
		return errors.New(i18n.T("no window has the focus to type into"))
	}
	mods := "CTRL"
	if isTerminalClass(w.Class) {
		mods = "CTRL SHIFT"
	}
	return t.withClipboard(text, func() error {
		if h.lua {
			return h.client.LuaDispatch(ctx, fmt.Sprintf(`hl.dsp.send_shortcut({ mods = %q, key = "V", window = %q })`, mods, "address:"+w.Address))
		}
		return h.client.Dispatch(ctx, fmt.Sprintf("sendshortcut %s, V, address:%s", mods, w.Address))
	})
}

// withClipboard puts text on the clipboard, waits for the window to notice,
// sends the paste shortcut, then puts the clipboard's own text back.
func (t dictationTyper) withClipboard(text string, send func() error) error {
	old := t.readClipboard()
	if err := t.copyClipboard([]byte(text)); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	err := send()
	if len(old) > 0 {
		time.Sleep(400 * time.Millisecond) // the window has read the clipboard
		t.copyClipboard(old)
	}
	return err
}

func (t dictationTyper) readClipboard() []byte {
	if t.clipboard != nil {
		return t.clipboard()
	}
	out, _ := exec.Command("wl-paste", "--no-newline", "--type", "text/plain").Output()
	return out
}

func (t dictationTyper) copyClipboard(b []byte) error {
	if t.copy != nil {
		return t.copy(b)
	}
	// Not into the clipboard history: its capture skips the next one.
	if t.history != "" {
		clipboard.SkipFirst(t.history)
	}
	cmd := exec.Command("wl-copy", "--type", "text/plain;charset=utf-8")
	cmd.Stdin = bytes.NewReader(b)
	// wl-copy's daemon must not inherit a pipe waited on by Cmd.Run.
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// isTerminalClass reports whether an application pastes with Ctrl+Shift+V.
func isTerminalClass(class string) bool {
	class = strings.ToLower(class)
	return slices.ContainsFunc(terminalClasses, func(t string) bool { return strings.Contains(class, t) })
}

// wtypePaste sends the paste shortcut through the virtual-keyboard protocol,
// which Wayfire supports.
func wtypePaste(ctx context.Context, terminal bool) error {
	args := []string{"-M", "ctrl"}
	if terminal {
		args = append(args, "-M", "shift")
	}
	args = append(args, "-k", "v")
	if terminal {
		args = append(args, "-m", "shift")
	}
	args = append(args, "-m", "ctrl")
	out, err := exec.CommandContext(ctx, "wtype", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wtype: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
