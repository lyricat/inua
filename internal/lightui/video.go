package lightui

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/music"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/apps/video"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gst"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/mpris"
)

// Video (docs/feat/players.md): one video at a time, its pictures from
// internal/gst drawn in a GtkPicture, the controls over it while the
// mouse moves.

// controlsLinger is how long the controls stay after the mouse stops.
const controlsLinger = 2 * time.Second

type videoApp struct {
	c   *appui.Ctx
	win *gtk.ApplicationWindow

	player   *gst.Player
	st       video.State
	path     string
	playing  bool
	ended    bool
	speed    float64
	muted    bool
	timeLeft bool
	pending  time.Duration // where to go once the video is ready
	cookie   uint32        // the idle inhibitor while it plays
	filling  bool

	shown     bool // the controls show
	moved     time.Time
	mx, my    float64
	onControl bool // the mouse is over the controls
	clicks    int  // counts clicks, to tell one from a double
	notices   int

	frameMu   sync.Mutex
	frame     *gst.Frame
	framePost bool

	mp     *mpris.Server
	mpConn *dbus.Conn

	picture                                          gtk.Picture
	empty, notice, controls                          gtk.Box
	topReveal, controlsReveal                        gtk.Revealer
	title, noticeText, position, length              gtk.Label
	audioLabel, subtitlesLabel, emptyTitle           gtk.Label
	seek, volume                                     gtk.Scale
	playPause, audioTrack, subtitles, speedBtn, mute gtk.Button
	fullscreen, openBtn, noticeStart                 gtk.Button

	dialog     *gtk.FileDialog
	onButton   func(gtk.Button)
	onSeek     func(gtk.Range, gtk.ScrollType, float64) bool
	onVolume   func(gtk.Range)
	onMotion   func(gtk.EventControllerMotion, float64, float64)
	onEnterCtl func(gtk.EventControllerMotion, float64, float64)
	onLeaveCtl func(gtk.EventControllerMotion)
	onClick    func(gtk.GestureClick, int32, float64, float64)
	onLenClick func(gtk.GestureClick, int32, float64, float64)
	onKey      func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onDrop     func(gtk.DropTarget, uintptr, float64, float64) bool
	onChosen   gio.AsyncReadyCallback
	onTick     glib.SourceFunc
	onClose    func(gtk.Window) bool
	onSingle   glib.SourceFunc
	onNotice   glib.SourceFunc
}

func (a *videoApp) ThemeChanged() {}

func (a *videoApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = &gtk.ApplicationWindow{}
	a.speed = 1
	ui := c.Load("video.ui")
	ui.Get("video", a.win)
	for id, w := range map[string]gobject.Ptr{
		"picture": &a.picture, "empty": &a.empty, "notice": &a.notice, "controls": &a.controls,
		"top_reveal": &a.topReveal, "controls_reveal": &a.controlsReveal,
		"title": &a.title, "notice_text": &a.noticeText, "position": &a.position, "length": &a.length,
		"audio_label": &a.audioLabel, "subtitles_label": &a.subtitlesLabel, "empty_title": &a.emptyTitle,
		"seek": &a.seek, "volume": &a.volume,
		"play_pause": &a.playPause, "audio_track": &a.audioTrack, "subtitles": &a.subtitles,
		"speed": &a.speedBtn, "mute": &a.mute, "fullscreen": &a.fullscreen,
		"open": &a.openBtn, "notice_start": &a.noticeStart,
	} {
		ui.Get(id, w)
	}
	ui.Close()

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	for _, b := range []*gtk.Button{&a.openBtn, &a.noticeStart, &a.playPause, &a.audioTrack, &a.subtitles, &a.speedBtn, &a.mute, &a.fullscreen} {
		b.ConnectClicked(&a.onButton)
	}
	a.onSeek = func(_ gtk.Range, _ gtk.ScrollType, v float64) bool {
		if d, ok := a.lengthNow(); ok {
			a.seekTo(time.Duration(min(max(v, 0), 1) * float64(d)))
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
		a.showMute()
		a.publish()
	}
	a.volume.ConnectValueChanged(&a.onVolume)

	// The mouse brings the controls; they go when it rests, unless it is
	// over them.
	a.onMotion = func(_ gtk.EventControllerMotion, x, y float64) {
		if x == a.mx && y == a.my {
			return
		}
		a.mx, a.my = x, y
		a.wake()
	}
	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(&a.onMotion)
	a.win.AddController(&motion.EventController)
	a.onEnterCtl = func(gtk.EventControllerMotion, float64, float64) { a.onControl = true }
	a.onLeaveCtl = func(gtk.EventControllerMotion) { a.onControl = false }
	over := gtk.NewEventControllerMotion()
	over.ConnectEnter(&a.onEnterCtl)
	over.ConnectLeave(&a.onLeaveCtl)
	a.controls.AddController(&over.EventController)

	// A click plays or pauses; a double click is full screen.
	a.onClick = func(_ gtk.GestureClick, n int32, _, _ float64) {
		a.clicks++
		if n >= 2 {
			a.toggleFullscreen()
			return
		}
		glib.TimeoutAdd(260, &a.onSingle, uintptr(a.clicks))
	}
	a.onSingle = func(gen uintptr) bool {
		if int(gen) == a.clicks && a.path != "" {
			a.playOrPause()
		}
		return false
	}
	click := gtk.NewGestureClick()
	click.ConnectPressed(&a.onClick)
	a.picture.AddController(&click.EventController)
	a.onLenClick = func(gtk.GestureClick, int32, float64, float64) {
		a.timeLeft = !a.timeLeft
		a.tick()
	}
	lc := gtk.NewGestureClick()
	lc.ConnectReleased(&a.onLenClick)
	a.length.AddController(&lc.EventController)

	a.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, mods gdk.ModifierType) bool {
		return a.key(keyval, mods)
	}
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&a.onKey)
	a.win.AddController(&keys.EventController)
	a.onDrop = func(dt gtk.DropTarget, _ uintptr, _, _ float64) bool {
		if v := dt.GetValue(); v != nil {
			if paths := droppedPaths(v); len(paths) > 0 {
				a.load(paths[0])
			}
		}
		return true
	}
	dt := gtk.NewDropTarget(gdk.FileListGLibType(), gdk.ActionCopyValue|gdk.ActionMoveValue)
	dt.ConnectDrop(&a.onDrop)
	a.win.AddController(&dt.EventController)
	a.onChosen = func(_, result, _ uintptr) {
		d := a.dialog
		a.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		f, err := d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || f == nil {
			return
		}
		path := f.GetPath()
		unrefObject(f)
		a.load(path)
	}
	a.onTick = func(uintptr) bool {
		a.tick()
		return true
	}
	a.onNotice = func(gen uintptr) bool {
		if int(gen) == a.notices {
			a.notice.SetVisible(false)
		}
		return false
	}
	a.onClose = func(gtk.Window) bool {
		a.leave()
		a.save()
		a.inhibit(false)
		if a.player != nil {
			a.player.Close()
		}
		if a.mp != nil {
			a.mp.Close()
			a.mpConn.Close()
		}
		return false
	}
	a.win.ConnectCloseRequest(&a.onClose)

	a.st = video.DefaultState()
	if err := store.Load(video.StoreName, &a.st); err != nil {
		log.Printf("video: %v", err)
	}
	a.filling = true
	a.volume.SetValue(a.st.Volume)
	a.filling = false
	var err error
	if a.player, err = gst.New(gst.Options{Video: true, AudioSink: os.Getenv("INUA_AUDIO_SINK")}); err != nil {
		a.player = nil
		a.emptyTitle.SetText(gstProblem(err))
		a.openBtn.SetVisible(false)
	} else {
		a.player.SetVolume(a.st.Volume)
		go a.events(a.player)
		go a.frames(a.player)
	}
	a.mp, a.mpConn = serveMPRIS("inua.video", i18n.T("Video"), appui.IDBase+".Video", mpris.Handler{
		Raise:     func() { gtkx.Post(func() { a.win.Present() }) },
		PlayPause: func() { gtkx.Post(a.playOrPause) },
		Play:      func() { gtkx.Post(func() { a.setPlaying(true) }) },
		Pause:     func() { gtkx.Post(func() { a.setPlaying(false) }) },
		Stop:      func() { gtkx.Post(func() { a.setPlaying(false) }) },
		Seek: func(off time.Duration) {
			gtkx.Post(func() {
				if p, ok := a.positionNow(); ok {
					a.seekTo(p + off)
				}
			})
		},
		SetPosition: func(p time.Duration) { gtkx.Post(func() { a.seekTo(p) }) },
		SetVolume:   func(v float64) { gtkx.Post(func() { a.volume.SetValue(v) }) },
		Position: func() time.Duration {
			p, _ := a.positionNow()
			return p
		},
	})
	a.showLoaded()
	glib.TimeoutAdd(250, &a.onTick, 0)
	menuWindow(c, a.win, []appui.MenuGroup{{Title: "File", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "video-open", Label: "Open a video", Activate: func(gio.SimpleAction, uintptr) { a.choose() }}, {Name: "video-play-pause", Label: "Play or pause", Activate: func(gio.SimpleAction, uintptr) { a.playOrPause() }}, {Name: "video-fullscreen", Label: "Fullscreen", Activate: func(gio.SimpleAction, uintptr) { a.toggleFullscreen() }}}}}}})
	return a.win
}

// open takes the file of a start.
func (a *videoApp) Open(args []string) {
	if len(args) > 0 {
		a.load(args[0])
	}
}

// --- playing ---

// load plays a video, from where it was left.
func (a *videoApp) load(path string) {
	if a.player == nil {
		return
	}
	a.leave()
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	a.path, a.ended, a.speed = path, false, 1
	a.st.LastDir = filepath.Dir(path)
	a.picture.SetPaintable(&gdk.PaintableBase{}) // none
	a.player.Open(gst.URI(path), suburi(video.Subtitles(path)))
	a.pending = a.st.Resume(path)
	if a.pending > 0 {
		a.say(i18n.Tf("Resumed at %s", music.Clock(a.pending)), true)
	}
	a.setPlaying(true)
	a.showLoaded()
	a.save()
}

func suburi(path string) string {
	if path == "" {
		return ""
	}
	return gst.URI(path)
}

// leave notes where the video is, for the next time.
func (a *videoApp) leave() {
	if a.path == "" || a.player == nil {
		return
	}
	p, ok := a.positionNow()
	d, _ := a.lengthNow()
	if a.ended {
		p, ok = 0, true
	}
	if ok {
		a.st.Leave(a.path, p, d, time.Now())
	}
}

func (a *videoApp) positionNow() (time.Duration, bool) {
	if a.player == nil || a.path == "" {
		return 0, false
	}
	return a.player.Position()
}

func (a *videoApp) lengthNow() (time.Duration, bool) {
	if a.player == nil || a.path == "" {
		return 0, false
	}
	return a.player.Length()
}

func (a *videoApp) setPlaying(on bool) {
	if a.player == nil || a.path == "" {
		return
	}
	if on && a.ended { // again from the start
		a.ended = false
		a.player.Seek(0)
	}
	a.playing = on
	if on {
		a.player.Play()
	} else {
		a.player.Pause()
		a.showControls(true)
	}
	icon := "media-playback-start-symbolic"
	if on {
		icon = "media-playback-pause-symbolic"
	}
	a.playPause.SetIconName(icon)
	a.inhibit(on)
	a.publish()
}

func (a *videoApp) playOrPause() { a.setPlaying(!a.playing) }

func (a *videoApp) seekTo(p time.Duration) {
	if a.player == nil || a.path == "" {
		return
	}
	if d, ok := a.lengthNow(); ok {
		p = min(p, d)
	}
	a.ended = false
	a.player.Seek(max(p, 0))
	if a.mp != nil {
		a.mp.Seeked(max(p, 0))
	}
	a.wake()
	a.tick()
}

// inhibit keeps the screen awake while a video plays.
func (a *videoApp) inhibit(on bool) {
	switch {
	case on && a.cookie == 0:
		a.cookie = a.c.App.Inhibit(&a.win.Window, gtk.ApplicationInhibitIdleValue, i18n.T("Playing a video"))
	case !on && a.cookie != 0:
		a.c.App.Uninhibit(a.cookie)
		a.cookie = 0
	}
}

func (a *videoApp) events(p *gst.Player) {
	for e := range p.Events {
		gtkx.Post(func() { a.event(e) })
	}
}

func (a *videoApp) event(e gst.Event) {
	switch e.Kind {
	case gst.EOS:
		a.ended = true
		a.setPlaying(false)
		a.leave()
		a.save()
	case gst.Error:
		log.Printf("video: %v", e.Err)
		a.setPlaying(false)
		a.say(i18n.Tf("Cannot play it: %s", e.Err.Error()), false)
	case gst.Duration, gst.Started:
		if a.pending > 0 {
			if _, ok := a.lengthNow(); ok {
				a.player.Seek(a.pending)
				a.pending = 0
			}
		}
		a.showTracks()
		a.publish()
	}
}

// frames brings the newest picture to the main loop, one waiting at
// most.
func (a *videoApp) frames(p *gst.Player) {
	for f := range p.Frames {
		a.frameMu.Lock()
		a.frame = &f
		post := !a.framePost
		a.framePost = true
		a.frameMu.Unlock()
		if post {
			gtkx.Post(a.drawFrame)
		}
	}
}

func (a *videoApp) drawFrame() {
	a.frameMu.Lock()
	f := a.frame
	a.frame, a.framePost = nil, false
	a.frameMu.Unlock()
	if f == nil || a.path == "" {
		return
	}
	tex := rgbaTexture(f.Pix, f.W, f.H, f.Stride)
	a.picture.SetPaintable(tex)
	tex.Unref()
}

// tick shows the time, and puts the controls away when the mouse rests.
func (a *videoApp) tick() {
	p, _ := a.positionNow()
	d, ok := a.lengthNow()
	a.position.SetText(music.Clock(p))
	switch {
	case !ok:
		a.length.SetText("0:00")
		a.seek.SetValue(0)
	case a.timeLeft:
		a.length.SetText("-" + music.Clock(d-p))
	default:
		a.length.SetText(music.Clock(d))
	}
	if ok && d > 0 {
		a.seek.SetValue(float64(p) / float64(d))
	}
	if a.shown && a.playing && !a.onControl && time.Since(a.moved) > controlsLinger {
		a.showControls(false)
	}
}

// wake shows the controls for a while.
func (a *videoApp) wake() {
	a.moved = time.Now()
	if !a.shown {
		a.showControls(true)
	}
}

func (a *videoApp) showControls(on bool) {
	a.shown = on
	a.controlsReveal.SetRevealChild(on && a.path != "")
	a.topReveal.SetRevealChild(on && a.path != "")
	if on {
		a.moved = time.Now()
		a.win.SetCursorFromName("default")
	} else {
		a.win.SetCursorFromName("none")
	}
}

// showLoaded shows the empty window's offer, or the video's name.
func (a *videoApp) showLoaded() {
	loaded := a.path != ""
	a.empty.SetVisible(!loaded)
	toggleClass(&a.win.Widget, "video-loaded", loaded)
	if loaded {
		name := filepath.Base(a.path)
		a.title.SetText(name)
		a.win.SetTitle(name)
	} else {
		a.win.SetTitle(i18n.T("Video"))
	}
	a.showControls(true)
	a.showTracks()
}

// showTracks offers the sound tracks and subtitles there are.
func (a *videoApp) showTracks() {
	a.speedBtn.SetLabel(strconv.FormatFloat(a.speed, 'f', -1, 64) + "×")
	if a.player == nil || a.path == "" {
		a.audioTrack.SetVisible(false)
		a.subtitles.SetVisible(false)
		return
	}
	n, t, ca, ct := a.player.Tracks()
	a.audioTrack.SetVisible(n > 1)
	a.audioLabel.SetText(strconv.Itoa(ca+1) + "/" + strconv.Itoa(n))
	a.subtitles.SetVisible(t > 0)
	if ct < 0 {
		a.subtitlesLabel.SetText(i18n.T("Off"))
	} else {
		a.subtitlesLabel.SetText(strconv.Itoa(ct+1) + "/" + strconv.Itoa(t))
	}
}

// say shows a line over the video for a few seconds; back offers to go
// back to the start.
func (a *videoApp) say(text string, back bool) {
	a.notices++
	a.noticeText.SetText(text)
	a.noticeStart.SetVisible(back)
	a.notice.SetVisible(true)
	glib.TimeoutAdd(6000, &a.onNotice, uintptr(a.notices))
}

func (a *videoApp) toggleFullscreen() {
	if a.win.IsFullscreen() {
		a.win.Unfullscreen()
		a.fullscreen.SetIconName("view-fullscreen-symbolic")
	} else {
		a.win.Fullscreen()
		a.fullscreen.SetIconName("view-restore-symbolic")
	}
}

func (a *videoApp) showMute() {
	icon := "audio-volume-high-symbolic"
	switch v := a.volume.GetValue(); {
	case a.muted || v == 0:
		icon = "audio-volume-muted-symbolic"
	case v < 0.34:
		icon = "audio-volume-low-symbolic"
	case v < 0.67:
		icon = "audio-volume-medium-symbolic"
	}
	a.mute.SetIconName(icon)
}

func (a *videoApp) setSpeed(s float64) {
	if a.player == nil || a.path == "" {
		return
	}
	a.speed = s
	a.player.SetRate(s)
	a.showTracks()
	a.say(i18n.Tf("Speed %s×", strconv.FormatFloat(s, 'f', -1, 64)), false)
}

func (a *videoApp) button(name string) {
	switch name {
	case "open":
		a.choose()
	case "from-start":
		a.notice.SetVisible(false)
		a.pending = 0
		a.seekTo(0)
	case "play-pause":
		a.playOrPause()
	case "audio-track":
		if n, _, ca, _ := a.player.Tracks(); n > 1 {
			a.player.SetAudioTrack((ca + 1) % n)
			a.showTracks()
		}
	case "subtitles":
		a.nextSubtitles()
	case "speed":
		next := video.Faster(a.speed, true)
		if next == a.speed {
			next = video.Speeds[0]
		}
		a.setSpeed(next)
	case "mute":
		a.muted = !a.muted
		if a.player != nil {
			a.player.SetMute(a.muted)
		}
		a.showMute()
	case "fullscreen":
		a.toggleFullscreen()
	}
}

// nextSubtitles shows the next subtitle track, then none.
func (a *videoApp) nextSubtitles() {
	if a.player == nil {
		return
	}
	_, t, _, ct := a.player.Tracks()
	if t == 0 {
		return
	}
	next := ct + 1
	if next >= t {
		next = -1
	}
	a.player.SetSubtitles(next)
	a.showTracks()
}

func (a *videoApp) key(keyval uint32, mods gdk.ModifierType) bool {
	if mods&gdk.ControlMaskValue != 0 {
		if keyval == 'o' || keyval == 'O' {
			a.choose()
			return true
		}
		return false
	}
	step := func(d time.Duration) {
		if p, ok := a.positionNow(); ok {
			a.seekTo(p + d)
		}
	}
	switch keyval {
	case ' ', 'k', 'K':
		a.playOrPause()
	case 0xff51: // Left
		step(-5 * time.Second)
	case 0xff53: // Right
		step(5 * time.Second)
	case 'j', 'J':
		step(-10 * time.Second)
	case 'l', 'L':
		step(10 * time.Second)
	case 0xff52: // Up
		a.volume.SetValue(a.volume.GetValue() + 0.05)
	case 0xff54: // Down
		a.volume.SetValue(a.volume.GetValue() - 0.05)
	case 'm', 'M':
		a.button("mute")
	case 'f', 'F':
		a.toggleFullscreen()
	case 0xff1b: // Escape
		if !a.win.IsFullscreen() {
			return false
		}
		a.toggleFullscreen()
	case '[':
		a.setSpeed(video.Faster(a.speed, false))
	case ']':
		a.setSpeed(video.Faster(a.speed, true))
	case 's', 'S':
		a.nextSubtitles()
	default:
		return false
	}
	a.wake()
	return true
}

func (a *videoApp) choose() {
	if a.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Open a video"))
	if a.st.LastDir != "" {
		if f := gio.FileNewForPath(a.st.LastDir); f != nil {
			d.SetInitialFolder(f)
			unrefObject(f)
		}
	}
	f := gtk.NewFileFilter()
	f.SetName(i18n.T("Videos"))
	f.AddMimeType("video/*")
	for _, ext := range video.Videos {
		f.AddSuffix(ext[1:])
	}
	list := gio.NewListStore(gtk.FileFilterGLibType())
	list.Append(&f.Object)
	d.SetFilters(list)
	a.dialog = d
	d.Open(&a.win.Window, nil, &a.onChosen, 0)
}

func (a *videoApp) save() {
	if err := store.Save(video.StoreName, a.st); err != nil {
		log.Printf("video: %v", err)
	}
}

func (a *videoApp) publish() {
	if a.mp == nil {
		return
	}
	status := "Stopped"
	switch {
	case a.path != "" && a.playing:
		status = "Playing"
	case a.path != "":
		status = "Paused"
	}
	st := mpris.State{Status: status, Volume: a.st.Volume, CanSeek: a.path != ""}
	if a.path != "" {
		st.Title, st.URL, st.Track = filepath.Base(a.path), gst.URI(a.path), 1
		st.Length, _ = a.lengthNow()
	}
	a.mp.Update(st)
}
