package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// home gives the test its own config and state directories and a PATH
// with only the programs it names.
func home(t *testing.T, programs ...string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	for _, p := range programs {
		os.WriteFile(filepath.Join(bin, p), []byte("#!/bin/sh\n"), 0o755)
	}
	t.Setenv("PATH", bin)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("NIRI_CONFIG", "")
	t.Setenv("WAYFIRE_CONFIG_FILE", "")
	return dir
}

func TestChosen(t *testing.T) {
	for in, want := range map[string]string{"": "hyprland", "hyprland": "hyprland", "niri": "niri", "wayfire": "wayfire", "plasma": "hyprland"} {
		if got := Chosen(in); got != want {
			t.Errorf("Chosen(%q) = %q, want %q", in, got, want)
		}
	}
}

// The login starts the chosen compositor only when it can give the shell;
// otherwise the default, with the reason kept for the shell to say once.
func TestStartFallsBack(t *testing.T) {
	home(t, "Hyprland")
	if got := Start("niri"); got != "hyprland" {
		t.Fatalf("niri not installed: %q", got)
	}
	if note := Fallback(); !strings.Contains(note, "niri is not installed") {
		t.Fatalf("note %q", note)
	}
	if Fallback() != "" {
		t.Fatal("the note is said twice")
	}

	home(t, "Hyprland", "niri")
	niri, _ := Find("niri")
	if got := Start("niri"); got != "hyprland" || !strings.Contains(Fallback(), "no config yet") {
		t.Fatalf("niri without a config: %q", got)
	}
	// Choosing it in the settings writes the sample, which starts the shell.
	path, err := niri.Prepare()
	if err != nil || path != niri.ConfigFile() {
		t.Fatalf("Prepare: %q %v", path, err)
	}
	if again, _ := niri.Prepare(); again != "" {
		t.Fatal("the sample was written over a config")
	}
	if p := niri.Problem(); p != "" {
		t.Fatalf("the sample has a problem: %s", p)
	}
	if got := Start("niri"); got != "niri" || Fallback() != "" {
		t.Fatalf("niri ready: %q", got)
	}
	// A config of the user's that does not start the shell is passed over.
	os.WriteFile(niri.ConfigFile(), []byte("binds { Mod+Q { close-window; } }\n"), 0o644)
	if got := Start("niri"); got != "hyprland" || !strings.Contains(Fallback(), "does not start inua-shell") {
		t.Fatalf("niri config without the shell: %q", got)
	}
	// The default is never second-guessed here: the script checks it.
	if got := Start(""); got != "hyprland" || Fallback() != "" {
		t.Fatalf("default: %q", got)
	}
}

func TestWayfireSample(t *testing.T) {
	home(t, "wayfire")
	wf, _ := Find("wayfire")
	if !wf.Installed() {
		t.Fatal("wayfire on PATH not seen")
	}
	if _, err := wf.Prepare(); err != nil {
		t.Fatal(err)
	}
	if p := wf.Problem(); p != "" {
		t.Fatalf("the sample has a problem: %s", p)
	}
	os.WriteFile(wf.ConfigFile(), []byte("[core]\nplugins = vswitch autostart\n[autostart]\ninua = inua-shell\n"), 0o644)
	if p := wf.Problem(); !strings.Contains(p, "does not start inua-shell") {
		t.Fatalf("a config without the IPC plugins: %q", p)
	}
}

func TestRunning(t *testing.T) {
	for _, k := range []string{"NIRI_SOCKET", "WAYFIRE_SOCKET", "HYPRLAND_INSTANCE_SIGNATURE"} {
		t.Setenv(k, "")
	}
	if Running() != "" {
		t.Fatal("something runs")
	}
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "x")
	if Running() != "hyprland" {
		t.Fatal(Running())
	}
	t.Setenv("WAYFIRE_SOCKET", "/x")
	if Running() != "wayfire" {
		t.Fatal(Running())
	}
}
