package portalconf

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// choiceDirs is fakeDirs with backends that say what they serve: GTK's
// file chooser and settings, GNOME's file chooser and screenshots,
// Hyprland's screenshots, and a keyring for GNOME alone.
func choiceDirs(t *testing.T) Dirs {
	d := fakeDirs(t)
	portals := filepath.Join(d.System, "portals")
	write(t, filepath.Join(portals, "gtk.portal"), "[portal]\nDBusName=gtk\nInterfaces=org.freedesktop.impl.portal.FileChooser;org.freedesktop.impl.portal.AppChooser;org.freedesktop.impl.portal.Settings;\nUseIn=gnome\n")
	write(t, filepath.Join(portals, "gnome.portal"), "[portal]\nDBusName=gnome\nInterfaces=org.freedesktop.impl.portal.FileChooser;org.freedesktop.impl.portal.Screenshot;\nUseIn=gnome\n")
	write(t, filepath.Join(portals, "hyprland.portal"), "[portal]\nDBusName=hyprland\nInterfaces=org.freedesktop.impl.portal.Screenshot;\nUseIn=Hyprland\n")
	write(t, filepath.Join(portals, "gnome-keyring.portal"), "[portal]\nDBusName=org.freedesktop.secrets\nInterfaces=org.freedesktop.impl.portal.Secret\nUseIn=gnome\n")
	return d
}

func TestOptions(t *testing.T) {
	d := choiceDirs(t)
	if got := d.Options("FileChooser"); !slices.Equal(got, []string{"inua-files", "gnome", "gtk"}) {
		t.Errorf("FileChooser: %v", got)
	}
	if got := d.Options("Secret"); !slices.Equal(got, []string{"inua-keyring", "gnome-keyring"}) {
		t.Errorf("Secret: %v", got)
	}
}

func TestChooseSystemBackend(t *testing.T) {
	d := choiceDirs(t)
	hypr := Frontend{Desktop: "Hyprland"}
	if got := d.Serving(hypr, "Secret"); got != "" {
		t.Errorf("Secret served by %q before a choice", got)
	}
	if on, err := d.Choose("gnome-keyring", "Secret"); err != nil || !on {
		t.Fatalf("Choose = %v, %v", on, err)
	}
	if conf := read(t, filepath.Join(d.Own, confName)); !strings.Contains(conf, "org.freedesktop.impl.portal.Secret=gnome-keyring;hyprland;gtk\n") {
		t.Errorf("config:\n%s", conf)
	}
	if slices.Contains(d.Enabled(), "Secret") {
		t.Error("Secret counted as inua's")
	}
	if w := d.Wanted(); w.Dir != d.Own || d.Serving(w, "Secret") != "gnome-keyring" {
		t.Errorf("wanted %+v serves %q", w, d.Serving(w, "Secret"))
	}
	if d.Serving(hypr, "Secret") != "" {
		t.Error("a frontend without the directory serves the choice")
	}
	if on, err := d.Refresh(); !on || err != nil {
		t.Errorf("Refresh = %v, %v", on, err)
	}
	if on, err := d.Choose("", "Secret"); on || err != nil {
		t.Fatalf("Choose back = %v, %v", on, err)
	}
	if _, err := os.Stat(d.Own); !os.IsNotExist(err) {
		t.Errorf("directory left: %v", err)
	}
}

func TestChooseWhatTheSessionGives(t *testing.T) {
	d := choiceDirs(t)
	d.Set(true, "FileChooser")
	if got := d.Serving(d.Wanted(), "FileChooser"); got != "inua-files" {
		t.Errorf("serving %q", got)
	}
	// GTK is the session's own: no line for it, and none left.
	if on, err := d.Choose("gtk", "FileChooser"); on || err != nil {
		t.Fatalf("Choose = %v, %v", on, err)
	}
	if _, err := os.Stat(d.Own); !os.IsNotExist(err) {
		t.Errorf("directory written for the session's own backend")
	}
	if got := d.Serving(d.Wanted(), "FileChooser"); got != "gtk" {
		t.Errorf("serving %q", got)
	}
}

func TestChooseGroup(t *testing.T) {
	d := choiceDirs(t)
	d.Set(true, "FileChooser", "AppChooser")
	// GNOME has no AppChooser: that one goes back to the session's.
	if _, err := d.Choose("gnome", "FileChooser", "AppChooser"); err != nil {
		t.Fatal(err)
	}
	w := d.Wanted()
	if a, b := d.Serving(w, "FileChooser"), d.Serving(w, "AppChooser"); a != "gnome" || b != "gtk" {
		t.Errorf("FileChooser %q, AppChooser %q", a, b)
	}
	if slices.Contains(d.Enabled(), "AppChooser") {
		t.Error("AppChooser still inua's")
	}
	if _, err := d.Choose("gnome-keyring", "FileChooser"); err == nil {
		t.Error("a backend that serves none of them chosen")
	}
	if _, err := d.Choose("gnome", "SharePicker"); err == nil {
		t.Error("GNOME chosen as the share picker")
	}
}

func TestServingUseInAndNone(t *testing.T) {
	d := choiceDirs(t)
	os.Remove(filepath.Join(d.System, confName)) // no config: UseIn decides
	if got := d.Serving(Frontend{Desktop: "Hyprland"}, "Screenshot"); got != "hyprland" {
		t.Errorf("UseIn: %q", got)
	}
	if got := d.Serving(Frontend{Desktop: "Hyprland"}, "FileChooser"); got != "gtk" {
		t.Errorf("last resort: %q", got)
	}
	write(t, filepath.Join(d.Configs[0], "portals.conf"), "[preferred]\ndefault=*\norg.freedesktop.impl.portal.Screenshot=none\n")
	if got := d.Serving(Frontend{Desktop: "Hyprland"}, "Screenshot"); got != "" {
		t.Errorf("none: %q", got)
	}
	if got := d.Serving(Frontend{Desktop: "Hyprland"}, "FileChooser"); got != "gnome" {
		t.Errorf("*: %q", got)
	}
}

func TestChooseNothingServedChangesNothing(t *testing.T) {
	d := choiceDirs(t)
	d.Set(true, SharePicker, "FileChooser")
	if _, err := d.Choose("gnome-keyring", SharePicker, "FileChooser"); err == nil {
		t.Fatal("chosen")
	}
	if got := d.Enabled(); !slices.Equal(got, []string{"FileChooser", SharePicker}) {
		t.Errorf("Enabled = %v after a refused choice", got)
	}
}
