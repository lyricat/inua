package desk

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/apps/sysmon"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/compositor/compositortest"
	"github.com/lyricat/inua/internal/desktop"
)

// fakeDesktop records what the tools do.
func fakeDesktop() (Desktop, *[]string) {
	var did []string
	comp := compositortest.New()
	comp.Snap = compositor.Snapshot{
		Workspaces: []compositor.Workspace{{ID: "1", Name: "1", Order: 1}, {ID: "2", Name: "2", Order: 2}},
		Windows: []compositor.Window{
			{ID: "0xa", Title: "Inbox — Mail", AppID: "thunderbird", Workspace: "1", FocusRank: 2},
			{ID: "0xb", Title: "inua — Firefox", AppID: "firefox", Workspace: "2", FocusRank: 1, Focused: true},
			{ID: "0xc", Title: "Docs — Firefox", AppID: "firefox", Workspace: "2", FocusRank: 3},
		},
		FocusedWindow: "0xb",
	}
	d := Desktop{
		Call: func(_ context.Context, target, fn string, args ...string) (string, error) {
			did = append(did, strings.Join(append([]string{"call", target, fn}, args...), " "))
			if target == "system" && fn == "status" {
				return `{"wifi":true}`, nil
			}
			return "", nil
		},
		Compositor: comp,
		Apps: func() []*desktop.Entry {
			return []*desktop.Entry{{ID: "org.gnome.Nautilus", Name: "Files"}, {ID: "firefox", Name: "Firefox"}, {ID: "x", Name: "Hidden", NoDisplay: true}}
		},
		Launch: func(e *desktop.Entry) error { did = append(did, "launch "+e.ID); return nil },
		Processes: func() []sysmon.Process {
			return []sysmon.Process{{PID: 100, Name: "mine", UID: os.Getuid()}, {PID: 200, Name: "root's", UID: os.Getuid() + 1}}
		},
	}
	return d, &did
}

// actions are the compositor actions a test's desktop took.
func actions(d Desktop) []string { return d.Compositor.(*compositortest.Fake).Actions() }

func run(t *testing.T, d Desktop, name string, args map[string]any) (string, error) {
	t.Helper()
	for _, tool := range Tools(d) {
		if tool.Name == name {
			out, err := tool.Call(context.Background(), args)
			return out, err
		}
	}
	t.Fatalf("no tool %s", name)
	return "", nil
}

func TestWindowTools(t *testing.T) {
	d, _ := fakeDesktop()
	out, err := run(t, d, "list_windows", nil)
	var ws []window
	json.Unmarshal([]byte(out), &ws)
	if err != nil || len(ws) != 3 || !ws[1].Focused {
		t.Fatalf("list: %v %+v", err, ws)
	}
	for args, want := range map[string]string{
		`{"window":"mail"}`:                 "focus-window 0xa",
		`{"window":"0xc"}`:                  "focus-window 0xc",
		`{}`:                                "focus-window 0xb", // the focused one
		`{"window":"Mail","workspace":"3"}`: "move-window 0xa 3 follow=false",
	} {
		var a map[string]any
		json.Unmarshal([]byte(args), &a)
		tool := "focus_window"
		if _, ok := a["workspace"]; ok {
			tool = "move_window"
		}
		before := len(actions(d))
		if _, err := run(t, d, tool, a); err != nil || len(actions(d)) != before+1 || actions(d)[before] != want {
			t.Errorf("%s %s: %v %q, want %q", tool, args, err, actions(d), want)
		}
	}
	// Two Firefox windows: an error naming both, nothing done.
	before := len(actions(d))
	if _, err := run(t, d, "close_window", map[string]any{"window": "firefox"}); err == nil || !strings.Contains(err.Error(), "0xb") || len(actions(d)) != before {
		t.Errorf("ambiguous: %v %q", err, actions(d))
	}
	if _, err := run(t, d, "move_window", map[string]any{"window": "mail", "workspace": "1,address:0xb"}); err == nil {
		t.Error("a workspace smuggling a second address")
	}
}

func TestSystemTools(t *testing.T) {
	d, did := fakeDesktop()
	if out, _ := run(t, d, "system_status", nil); out != `{"wifi":true}` {
		t.Errorf("status %q", out)
	}
	*did = nil
	run(t, d, "set_system", map[string]any{"setting": "volume", "value": "30%"})
	if len(*did) != 1 || (*did)[0] != "call system set volume 30" {
		t.Errorf("set: %q", *did)
	}
	if _, err := run(t, d, "set_system", map[string]any{"setting": "rm", "value": "-rf"}); err == nil {
		t.Error("an unknown setting went through")
	}
	if _, err := run(t, d, "power", map[string]any{"action": "explode"}); err == nil {
		t.Error("an unknown power action went through")
	}
	*did = nil
	run(t, d, "open_app", map[string]any{"app": "files"})
	if len(*did) != 1 || (*did)[0] != "launch org.gnome.Nautilus" {
		t.Errorf("open: %q", *did)
	}
	if _, err := run(t, d, "open_app", map[string]any{"app": "hidden"}); err == nil {
		t.Error("opened a hidden app")
	}
	if _, err := run(t, d, "end_process", map[string]any{"pid": float64(200)}); err == nil || !strings.Contains(err.Error(), "not the user's") {
		t.Errorf("another user's process: %v", err)
	}
	// Describe says what a call is about to do.
	for _, tool := range Tools(d) {
		if tool.Name == "set_system" {
			if got := tool.Describe(map[string]any{"setting": "focus", "value": "on"}); got != "Turning Focus on" {
				t.Errorf("describe %q", got)
			}
		}
	}
}
