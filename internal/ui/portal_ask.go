package ui

import (
	"cmp"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/notify"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/portal"
	"github.com/lyricat/inua/internal/shell"
)

// The portal's questions to the user (docs/portals.md): access, an app
// running without a window, and a new background.

// backgroundEntry marks the notifications that ask about an app running
// in the background.
const backgroundEntry = "inua-portal-background"

// portalAsks is the shell's AccessHandler, BackgroundHandler and
// WallpaperHandler.
type portalAsks struct{ s *shellUI }

// doneContext is a context that ends when done is closed.
func doneContext(done <-chan struct{}) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// appTitle is an app's name from its desktop entry, else what the app
// gave, else its ID; called on the main thread.
func (s *shellUI) appTitle(appID, given string) string {
	if e := s.notes.apps[appID]; e != nil {
		return e.Name
	}
	return cmp.Or(given, appID, i18n.T("An app"))
}

// Ask asks in the shell's dialog, Deny focused.
func (h portalAsks) Ask(q portal.AccessQuestion, done <-chan struct{}) bool {
	s := h.s
	app, _ := gtkx.Call(time.Second, func() string { return s.appTitle(q.AppID, "") })
	desc := strings.TrimSpace(strings.Join([]string{q.Subtitle, q.Body}, "\n\n"))
	d := &passDialog{q: pinentry.Request{Mode: pinentry.Confirm, Title: q.Title, Desc: desc, App: app,
		OK: cmp.Or(q.Grant, i18n.T("Allow")), Cancel: cmp.Or(q.Deny, i18n.T("Deny"))},
		done: make(chan ipc.Response, 1), denyFirst: true}
	ctx, cancel := doneContext(done)
	defer cancel()
	return s.askDialog(ctx, d).OK
}

// Notify asks, in a notification, whether an app may go on running
// without a window: Allow, Quit, or let be this time.
func (h portalAsks) Notify(appID, name string, done <-chan struct{}) uint32 {
	s := h.s
	answer := make(chan uint32, 1)
	id, _ := gtkx.Call(time.Second, func() uint32 {
		if s.notes.srv == nil {
			return 0
		}
		n := notify.Notification{
			ID: s.notes.srv.NewID(), App: s.appTitle(appID, name), Entry: backgroundEntry, Icon: "application-x-executable-symbolic",
			Summary: i18n.Tf("%s is running in the background", s.appTitle(appID, name)),
			Body:    notify.Markup(i18n.T("It has no window open.")),
			Actions: []notify.Action{{Key: "allow", Label: i18n.T("Allow")}, {Key: "quit", Label: i18n.T("Quit")}},
			Time:    time.Now(), Resident: false,
		}
		if e := s.notes.apps[appID]; e != nil {
			n.Icon = e.Icon
		}
		if s.portals.bgAsks == nil {
			s.portals.bgAsks = map[uint32]chan uint32{}
		}
		s.portals.bgAsks[n.ID] = answer
		s.postNote(n)
		return n.ID
	})
	if id == 0 {
		return portal.BackgroundAllowOnce
	}
	select {
	case r := <-answer:
		return r
	case <-done:
		gtkx.Post(func() {
			delete(s.portals.bgAsks, id)
			s.withdrawNote(id)
		})
		return portal.BackgroundAllowOnce
	}
}

// backgroundNoteAction answers the question of a background notification.
func (s *shellUI) backgroundNoteAction(n notify.Notification, key string) {
	ch := s.portals.bgAsks[n.ID]
	if ch == nil {
		return
	}
	delete(s.portals.bgAsks, n.ID)
	switch key {
	case "allow":
		ch <- portal.BackgroundAllow
	case "quit":
		ch <- portal.BackgroundForbid
	default:
		ch <- portal.BackgroundAllowOnce
	}
}

// backgroundNoteGone lets the app be this time, its question dismissed.
func (s *shellUI) backgroundNoteGone(id uint32) {
	if ch := s.portals.bgAsks[id]; ch != nil {
		delete(s.portals.bgAsks, id)
		ch <- portal.BackgroundAllowOnce
	}
}

// syncPortalApps tells the portal which apps have windows, by class (the
// app ID on Wayland), the focused one active.
func (s *shellUI) syncPortalApps() {
	if s.portals.background == nil {
		return
	}
	apps := map[string]uint32{}
	for _, t := range s.snap.Windows {
		if t.AppID == "" {
			continue
		}
		st := portal.AppRunning
		if t.ID == s.snap.FocusedWindow {
			st = portal.AppActive
		}
		apps[t.AppID] = max(apps[t.AppID], st)
	}
	s.portals.background.SetApps(apps)
}

// SetWallpaper keeps a copy of the picture (an app's file may not stay)
// and makes it the background, asking first with it shown when preview.
func (h portalAsks) SetWallpaper(appID, path string, preview bool, done <-chan struct{}) bool {
	s := h.s
	dst, err := keepBackground(path)
	if err != nil {
		return false
	}
	if preview {
		app, _ := gtkx.Call(time.Second, func() string {
			s.previewWallpaper(dst)
			return s.appTitle(appID, "")
		})
		d := &passDialog{q: pinentry.Request{Mode: pinentry.Confirm, Title: i18n.T("Set as background?"),
			Desc: i18n.Tf("%s asks to make %s the background.", app, filepath.Base(path)), App: app,
			OK: i18n.T("Set as background"), Cancel: i18n.T("Cancel")}, done: make(chan ipc.Response, 1), denyFirst: true}
		ctx, cancel := doneContext(done)
		ok := s.askDialog(ctx, d).OK
		cancel()
		if !ok {
			os.Remove(dst)
			gtkx.Post(s.loadWallpaper) // the background as it was
			return false
		}
	}
	gtkx.Post(func() {
		c := s.cfg
		c.Background.Image, c.Background.Generate = shortenHome(dst), false
		s.changeSettings(c)
	})
	return true
}

// previewWallpaper shows a picture as the background, until the settings
// are loaded again.
func (s *shellUI) previewWallpaper(path string) {
	w := shell.Wallpaper{Image: path, Fit: cmp.Or(s.cfg.Background.Fit, "cover"), Color: s.cfg.Background.Color}
	s.styles.wallpaper.LoadFromString(w.CSS())
}

// keepBackground copies a picture to ~/.local/share/inua/backgrounds.
func keepBackground(path string) (string, error) {
	home, _ := os.UserHomeDir()
	data := cmp.Or(os.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
	dir := filepath.Join(data, "inua", "backgrounds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(path))
	if _, err := os.Stat(dst); err == nil {
		ext := filepath.Ext(dst)
		dst = strings.TrimSuffix(dst, ext) + "-" + time.Now().Format("20060102-150405") + ext
	}
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return "", err
	}
	return dst, out.Close()
}
