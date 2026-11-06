package lightui

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/chart"
	"github.com/lyricat/inua/internal/apps/music"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gst"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/mpris"
)

// Music (docs/feat/players.md): the playlist of internal/apps/music,
// played by internal/gst with its equalizer and spectrum, and an MPRIS
// player for the control center and media keys.

// spectrumBands is how many bars the analyser draws.
const spectrumBands = 20

type musicApp struct {
	c   *appui.Ctx
	win *gtk.ApplicationWindow

	pl      *music.Playlist
	st      music.State
	player  *gst.Player
	status  string // "Playing", "Paused" or "Stopped"
	loaded  int    // the track loaded in the player; -1 for none
	info    gst.Info
	filling bool
	seeking bool // the user drags the seek bar
	resume  time.Duration

	mp     *mpris.Server
	mpConn *dbus.Conn

	time, title, kbps, khz, channels, total gtk.Label
	playState                               gtk.Image
	spectrum, eqCurve                       gtk.Picture
	seek, volume, balance                   gtk.Scale
	shuffle, showEQ, showList, eqOn         gtk.ToggleButton
	repeat                                  gtk.Button
	eqReveal, listReveal                    gtk.Revealer
	presets                                 gtk.DropDown
	bandsBox, rows                          gtk.Box
	filter                                  gtk.SearchEntry
	listScroll                              gtk.ScrolledWindow
	preamp                                  *gtk.Scale
	bands                                   [10]*gtk.Scale

	rowBoxes []*gtk.Box // the playlist's rows, by track
	selected int

	levels, peaks []float64
	peakAt        []time.Time
	marquee       int // how far the title has scrolled

	dialog   *gtk.FileDialog
	dialogDo string // what the open dialog is for
	discover chan string

	onButton   func(gtk.Button)
	onToggle   func(gtk.ToggleButton)
	onSeek     func(gtk.Range, gtk.ScrollType, float64) bool
	onVolume   func(gtk.Range)
	onBalance  func(gtk.Range)
	onBand     func(gtk.Range)
	onPreset   func(uintptr, uintptr, uintptr)
	onFilter   func(gtk.SearchEntry)
	onRowClick func(gtk.GestureClick, int32, float64, float64)
	onTimeClk  func(gtk.GestureClick, int32, float64, float64)
	onKey      func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onDrop     func(gtk.DropTarget, uintptr, float64, float64) bool
	onChosen   gio.AsyncReadyCallback
	onTick     glib.SourceFunc
	onClose    func(gtk.Window) bool
	saveGen    int
	onSave     glib.SourceFunc
}

func (a *musicApp) ThemeChanged() { a.drawCurve() }

func (a *musicApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = &gtk.ApplicationWindow{}
	a.pl, a.loaded, a.status, a.selected = music.NewPlaylist(), -1, "Stopped", -1
	a.levels, a.peaks, a.peakAt = make([]float64, spectrumBands), make([]float64, spectrumBands), make([]time.Time, spectrumBands)
	var prev, play, pause, stop, next, open, eqReset, addFiles, addFolder, remove, clear, loadList, saveList gtk.Button
	ui := c.Load("music.ui")
	ui.Get("music", a.win)
	for id, w := range map[string]gobject.Ptr{
		"time": &a.time, "title": &a.title, "kbps": &a.kbps, "khz": &a.khz, "channels": &a.channels, "total": &a.total,
		"play_state": &a.playState, "spectrum": &a.spectrum, "eq_curve": &a.eqCurve,
		"seek": &a.seek, "volume": &a.volume, "balance": &a.balance,
		"shuffle": &a.shuffle, "show_eq": &a.showEQ, "show_list": &a.showList, "eq_on": &a.eqOn, "repeat": &a.repeat,
		"eq_reveal": &a.eqReveal, "list_reveal": &a.listReveal, "presets": &a.presets,
		"bands": &a.bandsBox, "rows": &a.rows, "filter": &a.filter, "list_scroll": &a.listScroll,
		"prev": &prev, "play": &play, "pause": &pause, "stop": &stop, "next": &next, "open": &open,
		"eq_reset": &eqReset, "add_files": &addFiles, "add_folder": &addFolder, "remove": &remove, "clear": &clear,
		"load_list": &loadList, "save_list": &saveList,
	} {
		ui.Get(id, w)
	}
	ui.Close()

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.onToggle = func(b gtk.ToggleButton) { a.toggled(b.GetName(), b.GetActive()) }
	for _, b := range []*gtk.Button{&prev, &play, &pause, &stop, &next, &open, &eqReset, &addFiles, &addFolder, &remove, &clear, &loadList, &saveList, &a.repeat} {
		b.ConnectClicked(&a.onButton)
	}
	for _, b := range []*gtk.ToggleButton{&a.shuffle, &a.showEQ, &a.showList, &a.eqOn} {
		b.ConnectToggled(&a.onToggle)
	}
	a.onSeek = func(_ gtk.Range, _ gtk.ScrollType, v float64) bool {
		if a.player != nil && a.loaded >= 0 {
			if d, ok := a.player.Length(); ok {
				a.player.Seek(time.Duration(min(max(v, 0), 1) * float64(d)))
				if a.mp != nil {
					a.mp.Seeked(time.Duration(v * float64(d)))
				}
			}
		}
		return false
	}
	a.seek.ConnectChangeValue(&a.onSeek)
	a.onVolume = func(gtk.Range) {
		if a.filling {
			return
		}
		a.st.Volume = a.volume.GetValue()
		if a.player != nil {
			a.player.SetVolume(a.st.Volume)
		}
		a.publish()
		a.saveSoon()
	}
	a.volume.ConnectValueChanged(&a.onVolume)
	a.onBalance = func(gtk.Range) {
		if a.filling {
			return
		}
		a.st.Balance = math.Round(a.balance.GetValue()*10) / 10
		if a.player != nil {
			a.player.SetBalance(a.st.Balance)
		}
		a.saveSoon()
	}
	a.balance.ConnectValueChanged(&a.onBalance)
	a.onBand = func(gtk.Range) {
		if a.filling {
			return
		}
		a.st.Preamp = a.preamp.GetValue()
		for i, b := range a.bands {
			a.st.EQ[i] = b.GetValue()
		}
		a.applyEQ()
		a.saveSoon()
	}
	a.buildBands()
	names := []string{i18n.T("Presets")}
	for _, p := range music.Presets {
		names = append(names, i18n.T(p.Name))
	}
	model := gtk.NewStringList(names)
	a.presets.SetModel(model)
	a.onPreset = func(uintptr, uintptr, uintptr) {
		i := int(a.presets.GetSelected()) - 1
		if a.filling || i < 0 || i >= len(music.Presets) {
			return
		}
		p := music.Presets[i]
		a.st.Preamp, a.st.EQ = p.Preamp, p.Bands
		a.showEQValues()
		a.applyEQ()
		a.saveSoon()
	}
	gtkx.ConnectNotify(&a.presets, "selected", &a.onPreset)
	a.onFilter = func(gtk.SearchEntry) { a.applyFilter() }
	a.filter.ConnectSearchChanged(&a.onFilter)
	a.onRowClick = func(g gtk.GestureClick, n int32, _, _ float64) {
		i, err := strconv.Atoi(g.GetWidget().GetName())
		if err != nil {
			return
		}
		a.selectRow(i)
		if n == 2 {
			a.playTrack(i)
		}
	}
	a.onTimeClk = func(gtk.GestureClick, int32, float64, float64) {
		a.st.TimeLeft = !a.st.TimeLeft
		a.tick()
		a.saveSoon()
	}
	g := gtk.NewGestureClick()
	g.ConnectReleased(&a.onTimeClk)
	a.time.AddController(&g.EventController)
	a.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, mods gdk.ModifierType) bool {
		return a.key(keyval, mods)
	}
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&a.onKey)
	a.win.AddController(&keys.EventController)
	a.onDrop = func(dt gtk.DropTarget, _ uintptr, _, _ float64) bool {
		if v := dt.GetValue(); v != nil {
			a.add(droppedPaths(v), false)
		}
		return true
	}
	dt := gtk.NewDropTarget(gdk.FileListGLibType(), gdk.ActionCopyValue|gdk.ActionMoveValue)
	dt.ConnectDrop(&a.onDrop)
	a.win.AddController(&dt.EventController)
	a.onChosen = func(_, result, _ uintptr) { a.chosen(result) }
	a.onTick = func(uintptr) bool {
		a.tick()
		return true
	}
	a.onSave = func(gen uintptr) bool {
		if int(gen) == a.saveGen {
			a.save()
		}
		return false
	}
	a.onClose = func(gtk.Window) bool {
		a.save()
		if a.player != nil {
			a.player.Close()
		}
		if a.mp != nil {
			a.mp.Close()
			a.mpConn.Close()
		}
		if a.discover != nil {
			close(a.discover)
		}
		return false
	}
	a.win.ConnectCloseRequest(&a.onClose)

	// What was kept, and the player.
	a.st = music.DefaultState()
	if err := store.Load(music.StoreName, &a.st); err != nil {
		log.Printf("music: %v", err)
	}
	a.pl.Tracks, a.pl.Shuffle, a.pl.Repeat = a.st.Tracks, a.st.Shuffle, a.st.Repeat
	if a.st.Current >= 0 && a.st.Current < len(a.pl.Tracks) {
		a.pl.Current, a.selected, a.resume = a.st.Current, a.st.Current, a.st.Position
	}
	a.filling = true
	a.volume.SetValue(a.st.Volume)
	a.balance.SetValue(a.st.Balance)
	a.shuffle.SetActive(a.st.Shuffle)
	a.eqOn.SetActive(a.st.EQOn)
	a.showEQ.SetActive(a.st.ShowEQ)
	a.showList.SetActive(a.st.ShowList)
	a.filling = false
	a.eqReveal.SetRevealChild(a.st.ShowEQ)
	a.listReveal.SetRevealChild(a.st.ShowList)
	a.showEQValues()
	a.showRepeat()
	var err error
	if a.player, err = gst.New(gst.Options{Filters: true, SpectrumBands: spectrumBands, AudioSink: os.Getenv("INUA_AUDIO_SINK")}); err != nil {
		a.player = nil
		a.title.SetText(gstProblem(err))
	} else {
		a.player.SetVolume(a.st.Volume)
		a.player.SetBalance(a.st.Balance)
		a.applyEQ()
		go a.events(a.player)
	}
	a.discover = make(chan string, 4096)
	go a.discoverLoop(a.discover)
	a.showRows()
	a.showTrack()
	a.mp, a.mpConn = serveMPRIS("inua.music", i18n.T("Music"), appui.IDBase+".Music", mpris.Handler{
		Raise:     func() { gtkx.Post(func() { a.win.Present() }) },
		PlayPause: func() { gtkx.Post(a.playPause) },
		Play:      func() { gtkx.Post(a.play) },
		Pause:     func() { gtkx.Post(a.pause) },
		Stop:      func() { gtkx.Post(a.stop) },
		Next:      func() { gtkx.Post(func() { a.skip(true) }) },
		Previous:  func() { gtkx.Post(func() { a.skip(false) }) },
		Seek: func(off time.Duration) {
			gtkx.Post(func() {
				if p, ok := a.position(); ok {
					a.seekTo(p + off)
				}
			})
		},
		SetPosition: func(p time.Duration) { gtkx.Post(func() { a.seekTo(p) }) },
		SetVolume:   func(v float64) { gtkx.Post(func() { a.volume.SetValue(v) }) },
		Position: func() time.Duration {
			p, _ := a.position()
			return p
		},
	})
	a.publish()
	glib.TimeoutAdd(250, &a.onTick, 0)
	menuWindow(c, a.win, []appui.MenuGroup{{Title: "File", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "music-open", Label: "Open files", Activate: func(gio.SimpleAction, uintptr) { a.choose("open") }}, {Name: "music-add", Label: "Add files", Activate: func(gio.SimpleAction, uintptr) { a.choose("add") }}, {Name: "music-load", Label: "Load a playlist", Activate: func(gio.SimpleAction, uintptr) { a.choose("load") }}, {Name: "music-save", Label: "Save the playlist", Activate: func(gio.SimpleAction, uintptr) { a.choose("save") }}, {Name: "music-play-pause", Label: "Play or pause", Activate: func(gio.SimpleAction, uintptr) { a.playPause() }}, {Name: "music-next", Label: "Next track", Activate: func(gio.SimpleAction, uintptr) { a.skip(true) }}}}}}})
	return a.win
}

// open takes the files of a start: added, the first of them played.
func (a *musicApp) Open(args []string) {
	if len(args) > 0 {
		a.add(args, true)
	}
}

// --- playing ---

func (a *musicApp) position() (time.Duration, bool) {
	if a.player == nil || a.loaded < 0 {
		return 0, false
	}
	return a.player.Position()
}

// load puts track i in the player (paused at its start).
func (a *musicApp) load(i int) bool {
	if a.player == nil || i < 0 || i >= len(a.pl.Tracks) {
		return false
	}
	a.loaded = i
	a.info = gst.Info{}
	a.marquee = 0
	a.player.Open(gst.URI(a.pl.Tracks[i].Path), "")
	return true
}

// playTrack plays track i from its start.
func (a *musicApp) playTrack(i int) {
	if !a.pl.Play(i) || !a.load(i) {
		return
	}
	a.player.Play()
	a.setStatus("Playing")
	a.showTrack()
	a.saveSoon()
}

func (a *musicApp) play() {
	switch {
	case a.player == nil || len(a.pl.Tracks) == 0:
		return
	case a.status == "Paused" && a.loaded >= 0:
		a.player.Play()
		a.setStatus("Playing")
	case a.status == "Playing":
		a.seekTo(0) // as Winamp: play again from the start
	default:
		i := a.pl.Current
		if a.selected >= 0 {
			i = a.selected
		}
		if i < 0 {
			i = max(a.pl.Next(false), 0)
		}
		resume := a.resume
		a.playTrack(i)
		if resume > 0 && i == a.st.Current {
			a.player.Seek(resume)
		}
		a.resume = 0
	}
}

func (a *musicApp) pause() {
	switch a.status {
	case "Playing":
		a.player.Pause()
		a.setStatus("Paused")
	case "Paused":
		a.player.Play()
		a.setStatus("Playing")
	}
}

func (a *musicApp) playPause() {
	if a.status == "Playing" {
		a.pause()
	} else {
		a.play()
	}
}

func (a *musicApp) stop() {
	if a.player == nil {
		return
	}
	a.player.Stop()
	a.setStatus("Stopped")
	a.tick()
}

// skip goes to the next track, or the previous (from the start of the
// track, when past its first seconds).
func (a *musicApp) skip(forward bool) {
	if a.player == nil || len(a.pl.Tracks) == 0 {
		return
	}
	if !forward {
		if p, ok := a.position(); ok && p > 3*time.Second && a.status != "Stopped" {
			a.seekTo(0)
			return
		}
		i := a.pl.Previous()
		a.selected = i
		a.playTrack(i)
		return
	}
	i := a.pl.Next(false)
	if i < 0 {
		return
	}
	a.pl.Advance(i)
	a.selected = i
	a.playTrack(i)
}

func (a *musicApp) seekTo(p time.Duration) {
	if a.player == nil || a.loaded < 0 {
		return
	}
	a.player.Seek(max(p, 0))
	if a.mp != nil {
		a.mp.Seeked(max(p, 0))
	}
	a.tick()
}

func (a *musicApp) setStatus(s string) {
	a.status = s
	icon := map[string]string{"Playing": "media-playback-start-symbolic", "Paused": "media-playback-pause-symbolic"}[s]
	if icon == "" {
		icon = "media-playback-stop-symbolic"
	}
	a.playState.SetFromIconName(icon)
	toggleClass(&a.playState.Widget, "playing", s == "Playing")
	a.seek.SetSensitive(a.loaded >= 0 && s != "Stopped")
	if s != "Playing" {
		a.levels = make([]float64, spectrumBands)
		a.drawSpectrum()
	}
	a.markCurrent()
	a.publish()
}

// events takes the player's news to the main loop.
func (a *musicApp) events(p *gst.Player) {
	for e := range p.Events {
		gtkx.Post(func() { a.event(e) })
	}
}

func (a *musicApp) event(e gst.Event) {
	switch e.Kind {
	case gst.EOS:
		i := a.pl.Next(true)
		if i < 0 {
			a.stop()
			return
		}
		a.pl.Advance(i)
		a.playTrack(i)
	case gst.Error:
		log.Printf("music: %v", e.Err)
		if a.loaded >= 0 {
			a.title.SetText(i18n.Tf("Cannot play %s: %s", a.pl.Tracks[a.loaded].Name(), e.Err.Error()))
		}
		a.setStatus("Stopped")
	case gst.Tags:
		if e.Info.Bitrate > 0 {
			a.info.Bitrate = e.Info.Bitrate
		}
		if a.loaded >= 0 {
			t := &a.pl.Tracks[a.loaded]
			changed := false
			for _, f := range []struct {
				dst *string
				v   string
			}{{&t.Title, e.Info.Title}, {&t.Artist, e.Info.Artist}, {&t.Album, e.Info.Album}} {
				if f.v != "" && *f.dst == "" {
					*f.dst, changed = f.v, true
				}
			}
			if changed {
				a.refreshRow(a.loaded)
				a.publish()
			}
		}
		a.showTrack()
	case gst.Spectrum:
		if a.status == "Playing" {
			a.spectrumIn(e.Bands)
		}
	case gst.Duration:
		if a.loaded >= 0 {
			if d, ok := a.player.Length(); ok && a.pl.Tracks[a.loaded].Length != d {
				a.pl.Tracks[a.loaded].Length = d
				a.refreshRow(a.loaded)
				a.showTotal()
				a.publish()
			}
		}
	}
}

// tick shows the time and the seek bar's place, and scrolls the title.
func (a *musicApp) tick() {
	p, ok := a.position()
	var d time.Duration
	if a.loaded >= 0 {
		d = a.pl.Tracks[a.loaded].Length
	}
	if !ok || a.status == "Stopped" {
		p = 0
	}
	text := music.Clock(p)
	if a.st.TimeLeft && d > 0 {
		text = "-" + music.Clock(d-p)
	}
	a.time.SetText(text)
	if d > 0 {
		a.seek.SetValue(float64(p) / float64(d))
	} else {
		a.seek.SetValue(0)
	}
	a.scrollTitle()
}

// --- the display ---

func (a *musicApp) trackTitle() string {
	i := a.loaded
	if i < 0 {
		i = a.pl.Current
	}
	if i < 0 || i >= len(a.pl.Tracks) {
		if a.player == nil {
			return a.title.GetText()
		}
		return i18n.T("Nothing playing")
	}
	t := a.pl.Tracks[i]
	s := fmt.Sprintf("%d. %s", i+1, t.Name())
	if t.Length > 0 {
		s += " (" + music.Clock(t.Length) + ")"
	}
	return s
}

// titleWidth is how many characters the display shows of the title.
const titleWidth = 34

// scrollTitle moves a long title along, as Winamp's.
func (a *musicApp) scrollTitle() {
	if a.player == nil {
		return
	}
	full := []rune(a.trackTitle())
	if len(full) <= titleWidth || a.status != "Playing" {
		a.title.SetText(string(full))
		return
	}
	loop := append(full, []rune("  ***  ")...)
	a.marquee = (a.marquee + 1) % len(loop)
	out := make([]rune, titleWidth)
	for k := range out {
		out[k] = loop[(a.marquee+k)%len(loop)]
	}
	a.title.SetText(string(out))
}

// showTrack shows the track's figures.
func (a *musicApp) showTrack() {
	i := a.loaded
	if i < 0 {
		i = a.pl.Current
	}
	var t music.Track
	if i >= 0 && i < len(a.pl.Tracks) {
		t = a.pl.Tracks[i]
	}
	br := a.info.Bitrate
	if br == 0 {
		br = t.Bitrate
	}
	a.kbps.SetText(figure(br/1000, "kbps"))
	a.khz.SetText(figure(int(math.Round(float64(t.Rate)/1000)), "kHz"))
	switch {
	case t.Channels == 1:
		a.channels.SetText(i18n.T("MONO"))
	case t.Channels >= 2:
		a.channels.SetText(i18n.T("STEREO"))
	default:
		a.channels.SetText("")
	}
	a.title.SetText(a.trackTitle())
	a.tick()
}

func figure(n int, unit string) string {
	if n <= 0 {
		return "--- " + unit
	}
	return strconv.Itoa(n) + " " + unit
}

// spectrumIn takes the analyser's bands, in dB: bars jump up and fall
// back, peaks wait a moment before they fall.
func (a *musicApp) spectrumIn(db []float64) {
	now := time.Now()
	for i := range a.levels {
		v := 0.0
		if i < len(db) {
			v = min(max((db[i]+72)/66, 0), 1) // -72 dB … -6 dB
		}
		a.levels[i] = max(v, a.levels[i]-0.08)
		switch {
		case a.levels[i] >= a.peaks[i]:
			a.peaks[i], a.peakAt[i] = a.levels[i], now
		case now.Sub(a.peakAt[i]) > 400*time.Millisecond:
			a.peaks[i] = max(a.peaks[i]-0.03, a.levels[i])
		}
	}
	a.drawSpectrum()
}

func (a *musicApp) drawSpectrum() {
	scale := max(1, int(a.spectrum.GetScaleFactor()))
	w, h := 152*scale, 38*scale
	peaks := a.peaks
	if a.status != "Playing" {
		peaks = nil
	}
	tex := imageTexture(chart.Bars(a.levels, peaks, w, h, hexColor(a.c.Colors["accent"])))
	a.spectrum.SetPaintable(tex)
	tex.Unref()
}

// --- the equalizer ---

func (a *musicApp) buildBands() {
	add := func(label string, onChange *func(gtk.Range)) *gtk.Scale {
		col := gtk.NewBox(gtk.OrientationVerticalValue, 4)
		col.SetHexpand(true)
		s := gtk.NewScaleWithRange(gtk.OrientationVerticalValue, -12, 12, 0.5)
		s.SetInverted(true)
		s.SetDrawValue(false)
		s.SetSizeRequest(-1, 112)
		s.AddMark(0, gtk.PosLeftValue, "")
		s.AddCssClass("music-band")
		s.ConnectValueChanged(onChange)
		gtkx.Append(col, &s.Widget)
		l := gtk.NewLabel(label)
		l.AddCssClass("kicker")
		gtkx.Append(col, &l.Widget)
		gtkx.Append(&a.bandsBox, &col.Widget)
		return s
	}
	a.preamp = add(i18n.T("PRE"), &a.onBand)
	a.preamp.GetParent().AddCssClass("music-pre") // a gap after it
	for i, name := range music.Bands {
		a.bands[i] = add(name, &a.onBand)
	}
}

func (a *musicApp) showEQValues() {
	a.filling = true
	a.preamp.SetValue(a.st.Preamp)
	for i, b := range a.bands {
		b.SetValue(a.st.EQ[i])
	}
	a.filling = false
	a.drawCurve()
}

func (a *musicApp) applyEQ() {
	a.drawCurve()
	if a.player == nil {
		return
	}
	if a.st.EQOn {
		a.player.SetEQ(a.st.Preamp, a.st.EQ)
	} else {
		a.player.SetEQ(0, [10]float64{})
	}
}

func (a *musicApp) drawCurve() {
	if a.c == nil {
		return
	}
	scale := max(1, int(a.eqCurve.GetScaleFactor()))
	pts := a.st.EQ[:]
	if !a.st.EQOn {
		pts = make([]float64, 10)
	}
	img := chart.Line(pts, 180*scale, 34*scale, hexColor(a.c.Colors["accent"]), chart.Options{Lo: -12, Hi: 12})
	tex := imageTexture(img)
	a.eqCurve.SetPaintable(tex)
	tex.Unref()
}

// --- buttons and toggles ---

func (a *musicApp) button(name string) {
	switch name {
	case "prev":
		a.skip(false)
	case "play":
		a.play()
	case "pause":
		a.pause()
	case "stop":
		a.stop()
	case "next":
		a.skip(true)
	case "open":
		a.choose("open")
	case "add-files":
		a.choose("add")
	case "add-folder":
		a.choose("folder")
	case "load-list":
		a.choose("load")
	case "save-list":
		a.choose("save")
	case "remove":
		a.removeSelected()
	case "clear":
		a.clearList()
	case "repeat":
		a.pl.Repeat = (a.pl.Repeat + 1) % 3
		a.st.Repeat = a.pl.Repeat
		a.showRepeat()
		a.saveSoon()
	case "eq-reset":
		a.st.Preamp, a.st.EQ = 0, [10]float64{}
		a.filling = true
		a.presets.SetSelected(0)
		a.filling = false
		a.showEQValues()
		a.applyEQ()
		a.saveSoon()
	}
}

func (a *musicApp) toggled(name string, on bool) {
	if a.filling {
		return
	}
	switch name {
	case "shuffle":
		a.pl.Shuffle, a.st.Shuffle = on, on
	case "show-eq":
		a.st.ShowEQ = on
		a.eqReveal.SetRevealChild(on)
	case "show-list":
		a.st.ShowList = on
		a.listReveal.SetRevealChild(on)
	case "eq-on":
		a.st.EQOn = on
		a.applyEQ()
	}
	a.saveSoon()
}

func (a *musicApp) showRepeat() {
	icon, tip := "media-playlist-repeat-symbolic", i18n.T("Repeat: off (R)")
	switch a.pl.Repeat {
	case music.RepeatAll:
		tip = i18n.T("Repeat: the playlist (R)")
	case music.RepeatOne:
		icon, tip = "media-playlist-repeat-song-symbolic", i18n.T("Repeat: this track (R)")
	}
	a.repeat.SetIconName(icon)
	a.repeat.SetTooltipText(tip)
	toggleClass(&a.repeat.Widget, "on", a.pl.Repeat != music.RepeatOff)
}

// key answers Winamp's keys, unless the filter has the keyboard.
func (a *musicApp) key(keyval uint32, mods gdk.ModifierType) bool {
	ctrl := mods&gdk.ControlMaskValue != 0
	alt := mods&gdk.AltMaskValue != 0
	if a.filter.HasFocus() && !ctrl {
		return false
	}
	switch {
	case ctrl && (keyval == 'o' || keyval == 'O'):
		a.choose("open")
	case ctrl && (keyval == 'l' || keyval == 'L'):
		a.choose("load")
	case ctrl:
		return false
	case alt && keyval == 0xff52 && a.selected > 0: // Alt+Up
		a.moveSelected(-1)
	case alt && keyval == 0xff54 && a.selected >= 0 && a.selected < len(a.pl.Tracks)-1: // Alt+Down
		a.moveSelected(1)
	case keyval == 'z' || keyval == 'Z':
		a.skip(false)
	case keyval == 'x' || keyval == 'X':
		a.play()
	case keyval == 'c' || keyval == 'C':
		a.pause()
	case keyval == 'v' || keyval == 'V':
		a.stop()
	case keyval == 'b' || keyval == 'B':
		a.skip(true)
	case keyval == 's' || keyval == 'S':
		a.shuffle.SetActive(!a.shuffle.GetActive())
	case keyval == 'r' || keyval == 'R':
		a.button("repeat")
	case keyval == ' ':
		a.playPause()
	case keyval == 0xff51: // Left
		if p, ok := a.position(); ok {
			a.seekTo(p - 5*time.Second)
		}
	case keyval == 0xff53: // Right
		if p, ok := a.position(); ok {
			a.seekTo(p + 5*time.Second)
		}
	case keyval == 0xff52: // Up
		a.volume.SetValue(a.volume.GetValue() + 0.05)
	case keyval == 0xff54: // Down
		a.volume.SetValue(a.volume.GetValue() - 0.05)
	case keyval == 0xffff: // Delete
		a.removeSelected()
	case keyval == 0xff0d && a.selected >= 0: // Return
		a.playTrack(a.selected)
	default:
		return false
	}
	return true
}

// --- the playlist ---

// add puts files, folders and playlists at the end; play plays the
// first of them.
func (a *musicApp) add(paths []string, play bool) {
	ts := music.Expand(paths)
	if len(ts) == 0 {
		return
	}
	first := len(a.pl.Tracks)
	a.pl.Add(ts...)
	if len(paths) > 0 {
		a.st.LastDir = filepath.Dir(paths[0])
	}
	a.showRows()
	for _, t := range ts {
		if t.Length == 0 || t.Rate == 0 {
			a.queueDiscover(t.Path)
		}
	}
	if play || a.status == "Stopped" && a.loaded < 0 && first == 0 {
		a.selected = first
		if play {
			a.playTrack(first)
		} else {
			a.pl.Current = first
			a.showTrack()
			a.markCurrent()
		}
	}
	a.saveSoon()
}

func (a *musicApp) queueDiscover(path string) {
	select {
	case a.discover <- path:
	default:
	}
}

// discoverLoop reads tracks' lengths and tags, one at a time, off the
// main loop.
func (a *musicApp) discoverLoop(paths chan string) {
	for path := range paths {
		in, err := gst.Discover(gst.URI(path))
		if err != nil {
			continue
		}
		gtkx.Post(func() {
			for i := range a.pl.Tracks {
				t := &a.pl.Tracks[i]
				if t.Path != path {
					continue
				}
				if in.Duration > 0 {
					t.Length = in.Duration
				}
				t.Title, t.Artist, t.Album = cmpStr(t.Title, in.Title), cmpStr(t.Artist, in.Artist), cmpStr(t.Album, in.Album)
				t.Rate, t.Channels, t.Bitrate = in.Rate, in.Channels, cmpInt(t.Bitrate, in.Bitrate)
				a.refreshRow(i)
				if i == a.loaded || i == a.pl.Current {
					a.showTrack()
				}
			}
			a.showTotal()
			a.saveSoon()
		})
	}
}

func cmpStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func cmpInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// showRows makes the playlist's rows again.
func (a *musicApp) showRows() {
	gtkx.Clear(&a.rows)
	a.rowBoxes = a.rowBoxes[:0]
	for i := range a.pl.Tracks {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("music-row")
		row.SetName(strconv.Itoa(i))
		num := gtk.NewLabel("")
		num.AddCssClass("music-num")
		num.SetXalign(1)
		num.SetWidthChars(4)
		gtkx.Append(row, &num.Widget)
		name := gtk.NewLabel("")
		name.SetXalign(0)
		name.SetHexpand(true)
		name.SetEllipsize(3)
		gtkx.Append(row, &name.Widget)
		length := gtk.NewLabel("")
		length.AddCssClass("music-length")
		gtkx.Append(row, &length.Widget)
		g := gtk.NewGestureClick()
		g.ConnectPressed(&a.onRowClick)
		row.AddController(&g.EventController)
		gtkx.Append(&a.rows, &row.Widget)
		a.rowBoxes = append(a.rowBoxes, row)
		a.refreshRow(i)
	}
	a.markCurrent()
	a.applyFilter()
	a.showTotal()
}

// refreshRow shows track i's number, name and length in its row.
func (a *musicApp) refreshRow(i int) {
	if i < 0 || i >= len(a.rowBoxes) {
		return
	}
	t := a.pl.Tracks[i]
	num := a.rowBoxes[i].GetFirstChild()
	name := num.GetNextSibling()
	length := name.GetNextSibling()
	(&gtk.Label{Widget: *num}).SetText(strconv.Itoa(i+1) + ".")
	(&gtk.Label{Widget: *name}).SetText(t.Name())
	l := ""
	if t.Length > 0 {
		l = music.Clock(t.Length)
	}
	(&gtk.Label{Widget: *length}).SetText(l)
}

func (a *musicApp) markCurrent() {
	for i, r := range a.rowBoxes {
		toggleClass(&r.Widget, "current", i == a.pl.Current)
		toggleClass(&r.Widget, "selected", i == a.selected)
	}
}

func (a *musicApp) selectRow(i int) {
	a.selected = i
	a.markCurrent()
}

func (a *musicApp) applyFilter() {
	show := map[int]bool{}
	for _, i := range a.pl.Filter(a.filter.GetText()) {
		show[i] = true
	}
	for i, r := range a.rowBoxes {
		r.SetVisible(show[i])
	}
}

func (a *musicApp) showTotal() {
	d, all := a.pl.Total()
	n := len(a.pl.Tracks)
	text := i18n.Nf("%d track", "%d tracks", n, n)
	if n > 0 {
		t := music.Clock(d)
		if !all {
			t += "+"
		}
		text += " · " + t
	}
	a.total.SetText(text)
	a.publish()
}

func (a *musicApp) removeSelected() {
	i := a.selected
	if i < 0 || i >= len(a.pl.Tracks) {
		return
	}
	wasLoaded := i == a.loaded
	a.pl.Remove(i)
	switch {
	case wasLoaded:
		a.stop()
		a.loaded = -1
	case a.loaded > i:
		a.loaded--
	}
	a.selected = min(i, len(a.pl.Tracks)-1)
	a.showRows()
	a.showTrack()
	a.saveSoon()
}

func (a *musicApp) moveSelected(by int) {
	from, to := a.selected, a.selected+by
	a.pl.Move(from, to)
	switch a.loaded {
	case from:
		a.loaded = to
	case to:
		a.loaded = from
	}
	a.selected = to
	a.showRows()
	a.saveSoon()
}

func (a *musicApp) clearList() {
	a.stop()
	a.pl.Clear()
	a.loaded, a.selected = -1, -1
	a.showRows()
	a.showTrack()
	a.saveSoon()
}

// --- dialogs ---

// choose opens a file dialog: files to play now ("open") or to add
// ("add"), a folder to add, a playlist to load, or where to save one.
func (a *musicApp) choose(what string) {
	if a.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	a.dialog, a.dialogDo = d, what
	if a.st.LastDir != "" {
		if f := gio.FileNewForPath(a.st.LastDir); f != nil {
			d.SetInitialFolder(f)
			unrefObject(f)
		}
	}
	filter := func(name string, patterns []string) {
		f := gtk.NewFileFilter()
		f.SetName(name)
		for _, p := range patterns {
			f.AddPattern(p)
		}
		list := gio.NewListStore(gtk.FileFilterGLibType())
		list.Append(&f.Object)
		d.SetFilters(list)
	}
	var music_ []string
	for _, ext := range music.Audio {
		music_ = append(music_, "*"+ext, "*"+strings.ToUpper(ext))
	}
	switch what {
	case "open", "add":
		d.SetTitle(map[string]string{"open": i18n.T("Open files"), "add": i18n.T("Add files")}[what])
		filter(i18n.T("Music"), append(music_, "*.m3u", "*.m3u8"))
		d.OpenMultiple(&a.win.Window, nil, &a.onChosen, 0)
	case "folder":
		d.SetTitle(i18n.T("Add a folder"))
		d.SelectFolder(&a.win.Window, nil, &a.onChosen, 0)
	case "load":
		d.SetTitle(i18n.T("Load a playlist"))
		filter(i18n.T("Playlists"), []string{"*.m3u", "*.m3u8"})
		d.Open(&a.win.Window, nil, &a.onChosen, 0)
	case "save":
		d.SetTitle(i18n.T("Save the playlist"))
		d.SetInitialName(i18n.T("Playlist") + ".m3u")
		d.Save(&a.win.Window, nil, &a.onChosen, 0)
	}
}

func (a *musicApp) chosen(result uintptr) {
	d, what := a.dialog, a.dialogDo
	a.dialog = nil
	if d == nil {
		return
	}
	defer d.Unref()
	res := &gio.AsyncResultBase{Ptr: result}
	var paths []string
	switch what {
	case "open", "add":
		files, err := d.OpenMultipleFinish(res)
		if err != nil || files == nil {
			return
		}
		for i := range files.GetNItems() {
			f := &gio.FileBase{Ptr: files.GetItem(i)}
			if p := f.GetPath(); p != "" {
				paths = append(paths, p)
			}
			unrefObject(f)
		}
		unrefObject(files)
	default:
		var f *gio.FileBase
		var err error
		switch what {
		case "folder":
			f, err = d.SelectFolderFinish(res)
		case "load":
			f, err = d.OpenFinish(res)
		case "save":
			f, err = d.SaveFinish(res)
		}
		if err != nil || f == nil {
			return
		}
		paths = []string{f.GetPath()}
		unrefObject(f)
	}
	switch what {
	case "open":
		a.clearList()
		a.add(paths, true)
	case "add", "folder":
		a.add(paths, false)
	case "load":
		a.clearList()
		a.add(paths, false)
	case "save":
		a.saveList(paths[0])
	}
}

func (a *musicApp) saveList(path string) {
	f, err := os.Create(path)
	if err != nil {
		a.title.SetText(err.Error())
		return
	}
	defer f.Close()
	if err := music.WriteM3U(f, a.pl.Tracks); err != nil {
		a.title.SetText(err.Error())
	}
}

// --- keeping and telling ---

func (a *musicApp) saveSoon() {
	a.saveGen++
	glib.TimeoutAdd(1000, &a.onSave, uintptr(a.saveGen))
}

func (a *musicApp) save() {
	a.st.Tracks, a.st.Current = a.pl.Tracks, a.pl.Current
	a.st.Shuffle, a.st.Repeat = a.pl.Shuffle, a.pl.Repeat
	if p, ok := a.position(); ok && a.status != "Stopped" {
		a.st.Position = p
	} else if a.status == "Stopped" {
		a.st.Position = 0
	}
	if err := store.Save(music.StoreName, a.st); err != nil {
		log.Printf("music: %v", err)
	}
}

// publish tells the MPRIS player what plays.
func (a *musicApp) publish() {
	if a.mp == nil {
		return
	}
	st := mpris.State{Status: a.status, Volume: a.st.Volume, CanSeek: a.loaded >= 0,
		CanNext: len(a.pl.Tracks) > 1 || a.pl.Repeat != music.RepeatOff, CanPrev: len(a.pl.Tracks) > 0}
	i := a.loaded
	if i < 0 {
		i = a.pl.Current
	}
	if i >= 0 && i < len(a.pl.Tracks) {
		t := a.pl.Tracks[i]
		st.Title, st.Album, st.Length, st.URL, st.Track = cmpStr(t.Title, t.Name()), t.Album, t.Length, gst.URI(t.Path), i+1
		if t.Artist != "" {
			st.Artists = []string{t.Artist}
		}
	}
	a.mp.Update(st)
}
