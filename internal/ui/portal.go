package ui

import (
	"cmp"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/notify"
	"github.com/lyricat/inua/internal/portal"
	"github.com/lyricat/inua/internal/portalconf"
	"github.com/lyricat/inua/internal/rundir"
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/wallgen"
)

// portals is the shell's backend of xdg-desktop-portal (docs/portals.md).
// It serves whether or not the frontend asks it: inua portals enable
// decides that.
type portals struct {
	settings   *portal.Settings
	notes      *portal.Notifications
	inhibits   *portal.Inhibits
	background *portal.Background
	bgAsks     map[uint32]chan uint32 // background questions, by notification
}

// startPortals serves the shell's portal interfaces under its name.
func (s *shellUI) startPortals() {
	conn, err := system.SessionBus()
	if err != nil {
		log.Printf("portals: %v", err)
		return
	}
	// The session's own shell (a nested one has its instance) puts back
	// the display a nested Hyprland left, before apps ask for a dialog.
	if s.layer && s.opts.Instance == "default" {
		go func() {
			if err := portalconf.ActivationDisplay(conn); err != nil {
				log.Printf("portals: the display for D-Bus and systemd: %v", err)
			}
		}()
	}
	p := &s.portals
	p.settings = portal.NewSettings(conn)
	s.syncPortalSettings()
	icons, _ := rundir.Sub("portal")
	p.notes = portal.NewNotifications(conn, portalNotes{s}, s.portalNoteID, icons)
	p.inhibits = portal.NewInhibits(conn, logindBus())
	shots := portal.NewScreenshots(conn, portalShots{s})
	asks := portalAsks{s}
	home, _ := os.UserHomeDir()
	autostart := filepath.Join(cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config")), "autostart")
	p.background = portal.NewBackground(conn, asks, autostart)
	s.syncPortalApps()
	if _, err := portal.Export(conn, p.settings.Interface(), p.notes.Interface(), p.inhibits.Interface(), shots.Interface(),
		portal.NewAccess(conn, asks).Interface(), p.background.Interface(), portal.NewWallpaper(conn, asks).Interface()); err != nil {
		log.Printf("portals: %v", err)
		return
	}
	if err := portal.Own(conn, portal.ShellName); err != nil {
		log.Printf("portals: %v", err)
	}
}

// logindBus opens the system bus once, when an app first holds off
// suspend.
func logindBus() func() (*dbus.Conn, error) {
	var once sync.Once
	var conn *dbus.Conn
	var err error
	return func() (*dbus.Conn, error) {
		once.Do(func() { conn, err = system.SystemBus() })
		return conn, err
	}
}

// syncPortalSettings gives apps the theme's scheme and accent, the icons
// and the text size, and tells them what changed.
func (s *shellUI) syncPortalSettings() {
	if s.portals.settings == nil {
		return
	}
	a := portal.Appearance{TextScale: float64(s.cfg.Style.TextScale) / 100}
	theme := s.cfg.Style.ForApps() // other apps go with inua's
	if pal, err := wallgen.NewPalette(shellThemeColors(s, theme)); err == nil {
		a.Dark, a.Accent = pal.Dark(), pal.Accent
	} else {
		log.Printf("portals: theme %s: %v", theme, err)
	}
	if icons := s.cfg.Style.AppIconTheme(); icons != "system" {
		a.IconTheme = icons
	}
	s.portals.settings.Set(a.Values())
}

// portalNotes takes the portal's notifications onto the main thread, to
// the center as the server's are.
type portalNotes struct{ s *shellUI }

func (h portalNotes) Notify(n notify.Notification) { gtkx.Post(func() { h.s.postNote(n) }) }
func (h portalNotes) Close(id uint32)              { gtkx.Post(func() { h.s.withdrawNote(id) }) }

// portalNoteID numbers a portal notification as the server numbers apps'
// ones, or gives 0 while the shell does not show notifications.
func (s *shellUI) portalNoteID() uint32 {
	id, _ := gtkx.Call(time.Second, func() uint32 {
		if s.notes.srv == nil || !s.notes.owner {
			return 0
		}
		return s.notes.srv.NewID()
	})
	return id
}

// invokePortalNote answers a click on a portal notification, or one of
// its buttons; it reports whether the notification was the portal's.
// Without an action for the click, the app comes forward.
func (s *shellUI) invokePortalNote(n notify.Notification, key string) bool {
	p := s.portals.notes
	if p == nil || !p.Owns(n.ID) {
		return false
	}
	if !p.Invoke(n.ID, key) && key == "default" {
		s.bringApp(n.Entry)
	}
	return true
}

// forgetPortalNote is a notification gone from the screen and the center.
func (s *shellUI) forgetPortalNote(id uint32) {
	if s.portals.notes != nil {
		s.portals.notes.Forget(id)
	}
}

// bringApp focuses the app's last window, or starts it.
func (s *shellUI) bringApp(appID string) {
	for i := len(s.snap.Windows) - 1; i >= 0; i-- {
		if t := s.snap.Windows[i]; strings.EqualFold(t.AppID, appID) {
			s.focusWindow(t.ID)
			return
		}
	}
	if e := s.notes.apps[appID]; e != nil {
		s.launchApp(e)
	}
}

// portalLocked tells the portal's monitors whether the screen is locked.
func (s *shellUI) portalLocked(on bool) {
	if s.portals.inhibits != nil {
		s.portals.inhibits.SetLocked(on)
	}
}

// portalSessionState tells the portal's monitors where the session is.
func (s *shellUI) portalSessionState(state uint32) {
	if s.portals.inhibits != nil {
		s.portals.inhibits.SetState(state)
	}
}

// holdingLogout are the names of the apps holding off logging out.
func (s *shellUI) holdingLogout() []string {
	if s.portals.inhibits == nil {
		return nil
	}
	var names []string
	for _, h := range s.portals.inhibits.Holding(portal.InhibitLogout) {
		name := h.App
		if e := s.notes.apps[h.App]; e != nil {
			name = e.Name
		}
		names = append(names, name)
	}
	return names
}
