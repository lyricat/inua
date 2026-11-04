package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/assistant/record"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/system"
)

// dictationCard shows a dictation as it goes ([voice] show): at the foot of
// the focused window, or in the screen's bottom right corner. It follows
// the engine's state (dictationState), and says the words heard or why
// there are none for a moment after.
type dictationCard struct {
	win          *gtk.ApplicationWindow // built on first use, then kept
	refs         gtkx.Scope
	rev          gtk.Revealer
	card         gtk.Box
	mark, waveIn gtk.Box
	time, state  gtk.Label
	cancel       gtk.Button
	wave         *gtkx.Wave

	status  string    // the engine's, as last seen
	started time.Time // when the recording began
	gen     uintptr   // the latest showing; older timeouts do nothing
	held    bool      // words or an error, which go on their own time
	// levels is the microphone read for the waveform while an engine
	// records that has no level of its own (voxtype records itself).
	levels *record.Recorder

	onCancel                         func(gtk.Button)
	onTick, onHide, onGone, onRegion glib.SourceFunc
}

const (
	dictationTick   = 50   // ms, the waveform's sampling
	dictationHeard  = 1800 // ms the words heard stay
	dictationFailed = 8000 // ms why they did not, unless closed
	dictationFade   = 200  // ms, the revealer's
)

func (s *shellUI) ensureDictationCard() *dictationCard {
	d := &s.dictation
	if d.win != nil {
		return d
	}
	d.win = &gtk.ApplicationWindow{}
	ui := s.load("dictation.ui", &d.refs)
	ui.Get("dictation", d.win)
	ui.Get("reveal", &d.rev)
	ui.Get("card", &d.card)
	ui.Get("mark", &d.mark)
	ui.Get("wave", &d.waveIn)
	ui.Get("time", &d.time)
	ui.Get("state", &d.state)
	ui.Get("cancel", &d.cancel)
	ui.Close()
	d.rev.SetTransitionDuration(dictationFade)
	d.wave = gtkx.NewWave(18)
	d.wave.Widget.AddCssClass("dictation-wave")
	d.wave.Widget.SetHexpand(true)
	d.waveIn.Append(d.wave.Widget)
	// ✕ drops a recording, or closes the card on the words or an error.
	d.onCancel = func(gtk.Button) {
		if e := s.svc.dictation; e != nil && d.status == "recording" {
			e.Cancel()
			return
		}
		d.gen++
		glib.TimeoutAdd(0, &d.onHide, d.gen)
	}
	d.cancel.ConnectClicked(&d.onCancel)
	d.onTick = func(uintptr) bool {
		if d.status != "recording" {
			return false
		}
		n := int(time.Since(d.started).Seconds())
		d.time.SetText(fmt.Sprintf("%d:%02d", n/60, n%60))
		if l, ok := s.svc.dictation.(interface{ Level() float64 }); ok {
			d.wave.Add(l.Level())
		} else if d.levels != nil {
			d.wave.Add(d.levels.Level())
		}
		return true
	}
	d.onHide = func(gen uintptr) bool {
		if gen == d.gen {
			d.rev.SetRevealChild(false)
			glib.TimeoutAdd(dictationFade, &d.onGone, gen)
		}
		return false
	}
	// Only the card takes the pointer (its ✕), not its shadow around it.
	d.onRegion = func(uintptr) bool {
		if d.win.GetVisible() {
			gtkx.PassInputBut(&d.win.Window, 32, 32, int(max(d.card.GetWidth(), 1)), int(max(d.card.GetHeight(), 1)))
		}
		return false
	}
	d.onGone = func(gen uintptr) bool {
		if gen == d.gen {
			d.win.SetVisible(false)
		}
		return false
	}
	d.win.SetApplication(s.app)
	if s.layer {
		w := d.win.GoPointer()
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-dictation")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardNone)
		layershell.SetExclusiveZone(w, -1)
	}
	return d
}

// dictationState follows the engine into the card: shown when a
// recording begins, "Transcribing…" after, gone when the engine is idle
// again unless it has words or an error to show.
func (s *shellUI) dictationState(st system.VoiceState) {
	d := &s.dictation
	was := d.status
	d.status = st.Status
	if st.Status != "recording" && d.levels != nil {
		d.levels.Cancel()
		d.levels = nil
	}
	if s.cfg.Voice.Show == "off" || !s.layer || was == st.Status {
		return
	}
	switch st.Status {
	case "recording":
		s.ensureDictationCard()
		d.started = time.Now()
		d.wave.Clear()
		d.time.SetText("0:00")
		if _, ok := s.svc.dictation.(interface{ Level() float64 }); !ok {
			d.levels, _ = record.Start() // only for the waveform; none, no waveform
		}
		s.dictationShow("recording", "")
		s.placeDictation() // anew each recording: the window, or the setting, may have changed
		glib.TimeoutAdd(dictationTick, &d.onTick, 0)
	case "transcribing":
		if d.win != nil {
			s.dictationShow("transcribing", i18n.T("Transcribing…"))
		}
	default:
		if d.win != nil && d.win.GetVisible() && !d.held && (was == "recording" || was == "transcribing") {
			d.gen++
			glib.TimeoutAdd(0, &d.onHide, d.gen)
		}
	}
}

// dictationHeard shows the words a dictation typed, for a moment.
func (s *shellUI) dictationHeard(text string) {
	if d := &s.dictation; d.win != nil && d.win.GetVisible() {
		s.dictationShow("heard", text)
		d.gen++
		glib.TimeoutAdd(dictationHeard, &d.onHide, d.gen)
	}
}

// dictationFailed says why a dictation came to nothing: on its card when
// there is one, selectable, until ✕ or a while; else as a notification.
// The window it was for keeps the focus either way.
func (s *shellUI) dictationFailed(err error) {
	if d := &s.dictation; s.cfg.Voice.Show != "off" && d.win != nil && d.win.GetVisible() {
		s.dictationShow("failed", err.Error())
		d.gen++
		glib.TimeoutAdd(dictationFailed, &d.onHide, d.gen)
		return
	}
	s.dictationNote(err)
}

// dictationShow lays the card out for a stage: "recording" (the mark, the
// time, the waveform, ✕ to drop it), "transcribing" (the waveform faded,
// the text pulsing), "heard" (the words), "failed" (why, selectable, ✕ to
// close it). Shown again, it is placed again.
func (s *shellUI) dictationShow(stage, text string) {
	d := &s.dictation
	recording := stage == "recording"
	d.mark.SetVisible(recording)
	d.time.SetVisible(recording)
	d.waveIn.SetVisible(recording || stage == "transcribing")
	toggle(d.wave.Widget, "settled", !recording)
	d.cancel.SetVisible(recording || stage == "failed")
	d.state.SetVisible(!recording)
	d.state.SetText(text)
	d.state.SetSelectable(stage == "failed")
	for _, c := range []string{"heard", "failed", "working"} {
		toggle(&d.state.Widget, c, c == stage || c == "working" && stage == "transcribing")
	}
	d.held = stage == "heard" || stage == "failed"
	d.gen++ // a hide still to come is stale
	if !d.win.GetVisible() && !recording {
		s.placeDictation()
	}
	glib.TimeoutAdd(100, &d.onRegion, 0) // once it has its new size
}

// placeDictation puts the card at the foot of the focused window, centred
// under it ([voice] show = "window"), else in the focused monitor's
// bottom right corner, then shows it. Where the window is comes from
// Hyprland, asked once a showing.
func (s *shellUI) placeDictation() {
	d := &s.dictation
	client, inWindow := s.hyprClient(), s.cfg.Voice.Show == "window"
	go func() {
		var at hyprPlace
		if client != nil {
			at = activePlace(client, inWindow)
		}
		gtkx.Post(func() {
			w := d.win.GoPointer()
			for _, b := range s.bars {
				if b.connector() == at.monitor || at.monitor == "" && b.connector() == s.snap.FocusedOutput {
					layershell.SetMonitor(w, b.mon.GoPointer())
				}
			}
			var min, nat, mb, nb int32
			d.win.Measure(gtk.OrientationHorizontalValue, -1, &min, &nat, &mb, &nb)
			// At the top of the window, over its title and toolbar, not
			// at its foot, where a prompt or a message box is; else in the
			// corner.
			layershell.SetAnchor(w, layershell.EdgeTop, at.window)
			layershell.SetAnchor(w, layershell.EdgeBottom, !at.window)
			layershell.SetAnchor(w, layershell.EdgeLeft, at.window)
			layershell.SetAnchor(w, layershell.EdgeRight, !at.window)
			layershell.SetMargin(w, layershell.EdgeLeft, 0)
			layershell.SetMargin(w, layershell.EdgeTop, 0)
			if at.window {
				// The card 8 px into the window, its shadow margin (32)
				// around it.
				layershell.SetMargin(w, layershell.EdgeLeft, int32(max(0, at.centre-int(nat)/2)))
				layershell.SetMargin(w, layershell.EdgeTop, int32(max(0, at.top+8-32)))
			}
			d.win.SetVisible(true)
			d.win.Present()
			d.rev.SetRevealChild(true)
			glib.TimeoutAdd(100, &d.onRegion, 0)
		})
	}()
}

// hyprPlace is where the focused window is on its monitor, in the
// monitor's logical pixels: its centre from the left edge and its top;
// window false when the card goes in the corner.
type hyprPlace struct {
	monitor     string
	window      bool
	centre, top int
}

// activePlace asks Hyprland for the focused window and its monitor.
func activePlace(client interface {
	Request(context.Context, string) ([]byte, error)
}, inWindow bool) hyprPlace {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var win struct {
		At, Size [2]int
		Monitor  int
	}
	var mons []struct {
		ID, X, Y, Width, Height, Transform int
		Name                               string
		Scale                              float64
	}
	reply, err := client.Request(ctx, "j/activewindow")
	if err != nil || json.Unmarshal(reply, &win) != nil {
		return hyprPlace{}
	}
	if reply, err = client.Request(ctx, "j/monitors"); err != nil || json.Unmarshal(reply, &mons) != nil {
		return hyprPlace{}
	}
	for _, m := range mons {
		if m.ID != win.Monitor {
			continue
		}
		p := hyprPlace{monitor: m.Name}
		if !inWindow || win.Size[0] == 0 || m.Scale <= 0 {
			return p
		}
		p.window = true
		p.centre = win.At[0] - m.X + win.Size[0]/2
		p.top = win.At[1] - m.Y
		return p
	}
	return hyprPlace{}
}
