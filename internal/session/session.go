// Package session is which compositor the Inua login session starts, and
// what a login needs for it: the choice is a setting ([session] compositor
// in config.toml) that scripts/inua-session reads through `inua session
// compositor`, so one session entry in the login manager serves every
// compositor and choosing one needs no administrator.
package session

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	samples "github.com/lyricat/inua/config"
	"github.com/lyricat/inua/internal/niri"
)

// Compositor is one inua runs on.
type Compositor struct {
	ID, Name string
	programs []string // any of them on PATH means it is installed
	sample   string   // in samples.Compositors, "" for Hyprland (inua init writes its)
}

// Compositors are the choices, the default first.
var Compositors = []Compositor{
	{ID: "hyprland", Name: "Hyprland", programs: []string{"start-hyprland", "Hyprland"}},
	{ID: "niri", Name: "niri", programs: []string{"niri"}, sample: "niri/config.kdl"},
	{ID: "wayfire", Name: "Wayfire", programs: []string{"wayfire"}, sample: "wayfire/wayfire.ini"},
}

// Default is the compositor started when none is chosen.
const Default = "hyprland"

// Find is the compositor with an id; false for an unknown one.
func Find(id string) (Compositor, bool) {
	for _, c := range Compositors {
		if c.ID == id {
			return c, true
		}
	}
	return Compositor{}, false
}

// Chosen is the compositor a setting names: the default for "" or an
// unknown one.
func Chosen(setting string) string {
	if _, ok := Find(setting); ok {
		return setting
	}
	return Default
}

// Installed reports whether a compositor's program is on PATH.
func (c Compositor) Installed() bool {
	for _, p := range c.programs {
		if _, err := exec.LookPath(p); err == nil {
			return true
		}
	}
	return false
}

// Running is the compositor this process runs under, "" for none known.
func Running() string {
	switch {
	case os.Getenv("NIRI_SOCKET") != "":
		return "niri"
	case os.Getenv("WAYFIRE_SOCKET") != "":
		return "wayfire"
	case os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "":
		return "hyprland"
	}
	return ""
}

func configHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// ConfigFile is the compositor's own config file.
func (c Compositor) ConfigFile() string {
	switch c.ID {
	case "niri":
		return niri.ConfigPath()
	case "wayfire":
		if p := os.Getenv("WAYFIRE_CONFIG_FILE"); p != "" {
			return p
		}
		return filepath.Join(configHome(), "wayfire.ini")
	}
	return filepath.Join(configHome(), "hypr", "hyprland.conf")
}

var wayfirePlugins = regexp.MustCompile(`(?m)^plugins\s*=.*\bipc-rules\b`)

// startsShell reports whether a config's text starts inua-shell, and for
// Wayfire has the IPC plugins inua talks through.
func (c Compositor) startsShell(text string) bool {
	switch c.ID {
	case "niri":
		return strings.Contains(text, `spawn-at-startup "inua-shell"`)
	case "wayfire":
		return strings.Contains(text, "inua-shell") && wayfirePlugins.MatchString(text)
	}
	return true // Hyprland's is checked by inua config hyprland
}

// Problem is why a login on the compositor would not give the shell, ""
// when it would: it is not installed, or its config does not start
// inua-shell. A missing config is no problem: the sample is written when
// the compositor is chosen, and the compositor's own default starts
// without the shell only for niri and Wayfire, where Prepare writes it.
func (c Compositor) Problem() string {
	if !c.Installed() {
		return fmt.Sprintf("%s is not installed", c.Name)
	}
	data, err := os.ReadFile(c.ConfigFile())
	switch {
	case errors.Is(err, os.ErrNotExist):
		if c.sample != "" {
			return fmt.Sprintf("%s has no config yet: choose it in inua's settings to get one", c.Name)
		}
		return ""
	case err != nil:
		return err.Error()
	case !c.startsShell(string(data)):
		return fmt.Sprintf("%s does not start inua-shell", c.ConfigFile())
	}
	return ""
}

// Prepare writes the compositor's sample config when it has none; it
// reports the file it wrote, "" when there was one already or the
// compositor needs none written here.
func (c Compositor) Prepare() (string, error) {
	if c.sample == "" {
		return "", nil
	}
	path := c.ConfigFile()
	if _, err := os.Stat(path); err == nil {
		return "", nil
	}
	data, err := samples.Compositors.ReadFile(c.sample)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func stateHome() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state")
}

// fallbackFile holds why the last login did not start the chosen
// compositor, for the shell to say once.
func fallbackFile() string { return filepath.Join(stateHome(), "inua", "session-fallback") }

// Start is the compositor a login starts for a setting: the chosen one,
// or the default when the chosen one has a problem, which is then noted
// for the shell to report (Fallback).
func Start(setting string) string {
	id := Chosen(setting)
	c, _ := Find(id)
	problem := ""
	if id != Default {
		problem = c.Problem()
	}
	if problem == "" {
		os.Remove(fallbackFile())
		return id
	}
	if err := os.MkdirAll(filepath.Dir(fallbackFile()), 0o700); err == nil {
		os.WriteFile(fallbackFile(), []byte(problem+"\n"), 0o600)
	}
	return Default
}

// Fallback is why the last login started the default compositor instead of
// the chosen one, "" when it did not; reading it takes the note away.
func Fallback() string {
	data, err := os.ReadFile(fallbackFile())
	if err != nil {
		return ""
	}
	os.Remove(fallbackFile())
	return strings.TrimSpace(string(data))
}

// Login is what the login screen has of inua's session.
type Login struct {
	Manager string // "greetd", "gdm", "sddm", …; "" when none is enabled
	Entry   bool   // the Inua session entry is installed
	Script  bool   // inua-session is installed
	Chooses bool   // and it is one that reads the compositor setting
}

// sessionScript is where scripts/install-session.sh puts inua-session.
const sessionScript = "/usr/local/bin/inua-session"

// LoginStatus reads the login manager and inua's session entry.
func LoginStatus() Login {
	var l Login
	if target, err := os.Readlink("/etc/systemd/system/display-manager.service"); err == nil {
		l.Manager = strings.TrimSuffix(filepath.Base(target), ".service")
	}
	if _, err := os.Stat("/usr/share/wayland-sessions/inua.desktop"); err == nil {
		l.Entry = true
	}
	if data, err := os.ReadFile(sessionScript); err == nil {
		l.Script = true
		l.Chooses = strings.Contains(string(data), "inua session compositor")
	}
	return l
}

// ViaSession reports whether this session was started by inua-session,
// which is when the setting decides the compositor.
func ViaSession() bool { return os.Getenv("INUA_SESSION") != "" }
