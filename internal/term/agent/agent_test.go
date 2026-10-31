package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func load(t *testing.T, dir string) *Set {
	t.Helper()
	s, errs := Load(dir)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return s
}

func TestDetect(t *testing.T) {
	s := load(t, "")
	for _, c := range []struct {
		argv     []string
		override string
		want     string
	}{
		{[]string{"/home/me/.local/bin/claude"}, "", "claude"},
		{[]string{"node", "/usr/lib/node_modules/.bin/codex", "--full-auto"}, "", "codex"},
		{[]string{"python3", "-m", "aider"}, "", "aider"}, // python -m aider runs aider
		{[]string{"cursor-agent"}, "", "cursor"},
		{[]string{"/usr/bin/opencode", "--continue"}, "", "opencode"},
		{[]string{"/usr/local/bin/mistermorph"}, "", "mistermorph"},
		{[]string{"morph"}, "", "mistermorph"},
		{[]string{"node", "/usr/local/bin/pi"}, "", "pi"},
		{[]string{"mistermorph", "chat"}, "", "mistermorph"},
		{[]string{"vim", "claude"}, "", ""},
		{[]string{"bash"}, "", ""},
		{[]string{"bash"}, "mybot", "mybot"},
		{nil, "", ""},
	} {
		got := ""
		if m := s.Detect(c.argv, c.override); m != nil {
			got = m.Type
		}
		if got != c.want {
			t.Errorf("Detect(%q, %q) = %q, want %q", c.argv, c.override, got, c.want)
		}
	}
}

func TestScreenState(t *testing.T) {
	m := load(t, "").ByType("claude")
	working := []string{"● Update(host.go)", "", "✻ Writing the attach test… (2m 14s · esc to interrupt)", "", "╭───╮", "│ > │", "╰───╯"}
	if st, _ := m.State(working); st != Working {
		t.Errorf("working screen: %q", st)
	}
	// The spinner far up: the todos under it, then a prompt of several
	// lines being typed, the mode and a status line.
	todos := []string{"● Update(host.go)", "", "✻ Writing the attach test… (2m 14s · ↓ 3.1k tokens · esc to interrupt · ctrl+t to hide todos)",
		"  ⎿  ☒ Read the host", "     ☐ Write the attach test", "     ☐ Run the tests", "     ☐ Update the docs", "     ☐ Commit", "",
		"  ⎿  Press up to edit queued messages", "", "────────────", "> and then the readme", "  as well", "────────────",
		"  ⏵⏵ accept edits on (shift+tab to cycle)", "  Opus 5.5 · ~/src/app · main"}
	if st, _ := m.State(todos); st != Working {
		t.Errorf("working screen with todos: %q", st)
	}
	blocked := []string{"╭──────────────────────────╮", "│ Bash command              │", "│   rm -rf build            │", "│ Do you want to proceed?   │", "│ ❯ 1. Yes                  │", "│   2. No                   │", "╰──────────────────────────╯"}
	st, ask := m.State(blocked)
	if st != Blocked || ask != "Do you want to proceed?" {
		t.Errorf("blocked screen: %q %q", st, ask)
	}
	if st, _ := m.State([]string{"> ", ""}); st != "" {
		t.Errorf("prompt: %q", st)
	}
	// Its turn over while agents it started work in the background.
	waiting := []string{"● Two agents are on it; I'll report when they finish.", "", "────────────", "> ", "────────────",
		"  ⏵⏵ auto mode on · 2 local agents · ↓ to manage", "  Opus 5.5 · ~/src/app · main"}
	if st, _ := m.State(waiting); st != Working {
		t.Errorf("waiting on background agents: %q", st)
	}
	shell := []string{"────────────", "> ", "────────────", "  ⏵⏵ auto mode on · 1 shell · ↓ to manage"}
	if st, _ := m.State(shell); st != "" {
		t.Errorf("a background shell: %q", st)
	}
	// A question of AskUserQuestion's: the question shows, not the keys.
	picker := []string{"☐ Layout  ☐ Keys  ✔ Submit", "", "Which layout should the guide use?", "", "› 1. Steps", "     One step at a time", "  2. One page", "  3. Type something.", "", "Enter to select · ↑/↓ to navigate · Esc to cancel", "", "  ? for shortcuts"}
	if st, ask := m.State(picker); st != Blocked || ask != "Which layout should the guide use?" {
		t.Errorf("question: %q %q", st, ask)
	}
}

// The screens below are as opencode, pi and Mister Morph draw them.
func TestScreenStateOthers(t *testing.T) {
	s := load(t, "")
	for _, c := range []struct {
		agent  string
		screen []string
		want   State
		ask    string
	}{
		{"opencode", []string{"  Read main.go", "", "┃ Fix the tests", "┃", "┃ Build  claude-sonnet", "", " ⣾ esc interrupt          ~/src/app"}, Working, ""},
		{"opencode", []string{" ⣾ esc again to interrupt"}, Working, ""},
		{"opencode", []string{"△ Permission required", "  # rm -rf build", "", "Allow once  Allow always  Reject", "⇆ select  enter confirm"}, Blocked, "△ Permission required"},
		{"opencode", []string{"┃ ", "", " Build  claude-sonnet   ~/src/app"}, "", ""},
		{"pi", []string{"Reading main.go", "", "⠼ Working", "", "─────────", "> ", "─────────", "~/src/app", "↑12k ↓3k $0.04"}, Working, ""},
		{"pi", []string{"──── ⠋ Working ────", "> ", "─────────", "~/src/app"}, Working, ""},
		{"pi", []string{"─────────", "", " Trust this folder?", "", "→ Yes", "  No", "", " ↑↓ navigate  enter select  esc cancel", "", "─────────"}, Blocked, "Trust this folder?"},
		{"pi", []string{"Working tree clean.", "─────────", "> ", "─────────", "~/src/app"}, "", ""},
		{"mistermorph", []string{"", "⠙ Running · waiting for model · 4s", "", "> ", "", "  Enter steer · Ctrl+J newline · Esc/Ctrl+C stop"}, Working, ""},
		{"mistermorph", []string{"", "⠙ Running · waiting for model · 1:27", "", "> ", "", "qwen3 · lyric · ctx 3% · hi   Enter steer · Ctrl+C stop"}, Working, ""}, // a narrow footer
		{"mistermorph", []string{"", "! Approval · bash", "  $ rm -rf build", "", "  y approve · n deny · Esc stop"}, Blocked, "! Approval · bash"},
		{"mistermorph", []string{"✓ bash · ls", "", "> ", "", "gpt-5 · app · ctx 3%     Ctrl+J newline · / commands"}, "", ""},
	} {
		st, ask := s.ByType(c.agent).State(c.screen)
		if st != c.want || st == Blocked && ask != c.ask {
			t.Errorf("%s %q: %q %q, want %q %q", c.agent, c.screen, st, ask, c.want, c.ask)
		}
	}
}

func TestOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude.toml"), []byte(`type = "claude"
name = "Mine"
process = ["claude", "cc"]
`), 0o600)
	os.WriteFile(filepath.Join(dir, "bot.toml"), []byte(`type = "bot"
process = ["bot"]
[[state]]
state = "blocked"
any = ["approve?"]
`), 0o600)
	s := load(t, dir)
	if m := s.Detect([]string{"cc"}, ""); m == nil || m.Name != "Mine" {
		t.Errorf("override %+v", m)
	}
	if m := s.Detect([]string{"bot"}, ""); m == nil || m.Name != "bot" {
		t.Fatalf("new agent %+v", m)
	} else if st, _ := m.State([]string{"approve?"}); st != Blocked {
		t.Errorf("its rule: %q", st)
	}
	os.WriteFile(filepath.Join(dir, "bad.toml"), []byte("type = "), 0o600)
	if _, errs := Load(dir); len(errs) != 1 {
		t.Errorf("errors %v", errs)
	}
	os.WriteFile(filepath.Join(dir, "bad.toml"), []byte("type = \"x\"\ncolor = \"orange\"\n"), 0o600)
	if _, errs := Load(dir); len(errs) != 1 {
		t.Errorf("a colour not #rrggbb: %v", errs)
	}
	if m := load(t, "").ByType("claude"); m == nil || m.Color != "#d97757" {
		t.Errorf("claude's colour %+v", m)
	}
}

func TestTitle(t *testing.T) {
	for in, want := range map[string]string{
		"⠙ Move the PTY reader": "Move the PTY reader",
		"✳ Claude Code":         "Claude Code",
		"vim main.go":           "vim main.go",
	} {
		if got, _ := Title(in); got != want {
			t.Errorf("Title(%q) = %q", in, got)
		}
	}
}

func TestTracker(t *testing.T) {
	var tr Tracker
	t0 := time.Unix(1000, 0)
	tr.Set(Idle, "", t0)
	if !tr.Set(Working, "", t0.Add(time.Second)) || tr.Since != t0.Add(time.Second) {
		t.Errorf("working %+v", tr)
	}
	tr.Set(Idle, "", t0.Add(time.Minute))
	if tr.State != Done {
		t.Errorf("after working, unseen: %q", tr.State)
	}
	if tr.Set(Idle, "", t0.Add(2*time.Minute)) || tr.State != Done {
		t.Errorf("done stays: %+v", tr)
	}
	if !tr.Seen(t0.Add(3*time.Minute)) || tr.State != Idle {
		t.Errorf("seen: %q", tr.State)
	}
	tr.Set(Blocked, "Allow?", t0)
	if tr.State != Blocked || tr.Ask != "Allow?" {
		t.Errorf("blocked %+v", tr)
	}
}

func TestStateOnShortScreen(t *testing.T) {
	m := load(t, "").ByType("claude")
	screen := append([]string{"● Done thinking", "✻ Working (esc to interrupt)"}, make([]string, 30)...)
	if st, _ := m.State(screen); st != Working {
		t.Errorf("inline near the top: %q", st)
	}
}
