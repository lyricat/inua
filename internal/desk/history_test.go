package desk

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/clipboard"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/compositor/compositortest"
	"github.com/lyricat/inua/internal/notify"
)

// The shell's panels, media and notifications go to its control socket;
// unknown values do not.
func TestShellCalls(t *testing.T) {
	d, did := fakeDesktop()
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"show_panel", map[string]any{"panel": "control_center"}, "call control open"},
		{"show_panel", map[string]any{"panel": "settings", "page": "keyring"}, "call settings open keyring"},
		{"show_panel", map[string]any{"panel": "launcher"}, "call launcher apps"},
		{"media_status", nil, "call media status"},
		{"media_control", map[string]any{"action": "play_pause"}, "call media play-pause"},
		{"dismiss_notification", map[string]any{"id": float64(7)}, "call notifications dismiss 7"},
		{"notification_action", map[string]any{"id": float64(7)}, "call notifications invoke 7 default"},
		{"notification_action", map[string]any{"id": float64(7), "action": "reply"}, "call notifications invoke 7 reply"},
	} {
		*did = nil
		if _, err := run(t, d, c.tool, c.args); err != nil || len(*did) != 1 || (*did)[0] != c.want {
			t.Errorf("%s %v: %v %q, want %q", c.tool, c.args, err, *did, c.want)
		}
	}
	*did = nil
	for tool, args := range map[string]map[string]any{
		"show_panel":           {"panel": "everything"},
		"media_control":        {"action": "eject"},
		"dismiss_notification": {},
	} {
		if _, err := run(t, d, tool, args); err == nil {
			t.Errorf("%s %v went through", tool, args)
		}
	}
	if len(*did) != 0 {
		t.Errorf("called %q", *did)
	}
}

func TestListWorkspaces(t *testing.T) {
	d, _ := fakeDesktop()
	d.Compositor.(*compositortest.Fake).Snap = compositor.Snapshot{
		Outputs: []compositor.Output{{Name: "DP-1", Width: 2560, Height: 1440, Scale: 1.25, Focused: true, ActiveWorkspace: "2"},
			{Name: "eDP-1", Width: 1920, Height: 1200, Scale: 1, ActiveWorkspace: "5"}},
		Workspaces: []compositor.Workspace{{ID: "1", Name: "1", Output: "DP-1", Order: 1},
			{ID: "2", Name: "web", Output: "DP-1", Order: 2, Active: true, Focused: true},
			{ID: "5", Name: "5", Output: "eDP-1", Order: 5, Active: true}},
		Windows:       []compositor.Window{{ID: "0x1", Workspace: "5"}, {ID: "0x2", Workspace: "2"}, {ID: "0x3", Workspace: "2"}, {ID: "0x4", Workspace: "2"}},
		FocusedOutput: "DP-1",
	}
	out, err := run(t, d, "list_workspaces", nil)
	var got struct {
		Workspaces []struct {
			ID, Name       string
			Windows        int
			Shown, Focused bool
		}
		Monitors []struct{ Name string }
	}
	json.Unmarshal([]byte(out), &got)
	if err != nil || len(got.Workspaces) != 3 || len(got.Monitors) != 2 {
		t.Fatalf("%v %s", err, out)
	}
	w := got.Workspaces
	if w[0].ID != "1" || w[1].Name != "web" || w[1].Windows != 3 || !w[1].Shown || !w[1].Focused || !w[2].Shown || w[2].Focused || w[0].Shown {
		t.Errorf("workspaces %+v", w)
	}
}

// The clipboard history: listed newest first and searched, long text cut,
// an entry copied back, deleted, and the history cleared.
func TestClipboardHistory(t *testing.T) {
	d, _ := fakeDesktop()
	d.ClipHistory = filepath.Join(t.TempDir(), "clipboard-history.json")
	var copied []string
	d.ClipWrite = func(_ context.Context, text string) error { copied = append(copied, text); return nil }
	start := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	for i, e := range []clipboard.Entry{
		{Type: "text", Text: "ssh lyric@example.org", Source: "kitty"},
		{Type: "text", Text: strings.Repeat("long ", 1000), Source: "firefox"},
		{Type: "text", Text: "Meeting at 3", Source: "thunderbird"},
	} {
		e.Time = start.Add(time.Duration(i) * time.Minute)
		if err := clipboard.Add(d.ClipHistory, 0, e); err != nil {
			t.Fatal(err)
		}
	}
	list := func(args map[string]any) []clipEntry {
		t.Helper()
		out, err := run(t, d, "clipboard_history", args)
		var es []clipEntry
		if err != nil || json.Unmarshal([]byte(out), &es) != nil {
			t.Fatalf("%v %s", err, out)
		}
		return es
	}
	es := list(nil)
	if len(es) != 3 || es[0].Text != "Meeting at 3" || !es[1].Cut || len([]rune(es[1].Text)) != clipText || es[2].Source != "kitty" {
		t.Fatalf("%+v", es)
	}
	if got := list(map[string]any{"query": "KITTY"}); len(got) != 1 || got[0].Text != "ssh lyric@example.org" {
		t.Errorf("query: %+v", got)
	}
	if got := list(map[string]any{"limit": float64(1)}); len(got) != 1 {
		t.Errorf("limit: %+v", got)
	}

	if _, err := run(t, d, "clipboard_history_copy", map[string]any{"id": es[2].ID}); err != nil || len(copied) != 1 || copied[0] != "ssh lyric@example.org" {
		t.Errorf("copy: %v %q", err, copied)
	}
	if _, err := run(t, d, "clipboard_history_copy", map[string]any{"id": "nope"}); err == nil {
		t.Error("copied an entry that is not there")
	}
	if _, err := run(t, d, "clipboard_history_delete", map[string]any{"id": es[0].ID}); err != nil {
		t.Fatal(err)
	}
	if got := list(nil); len(got) != 2 || got[0].ID != es[1].ID {
		t.Errorf("after delete: %+v", got)
	}
	if _, err := run(t, d, "clipboard_history_clear", nil); err != nil {
		t.Fatal(err)
	}
	if got := list(nil); len(got) != 0 {
		t.Errorf("after clear: %+v", got)
	}
}

func TestNotificationHistory(t *testing.T) {
	d, _ := fakeDesktop()
	d.NoteHistory = filepath.Join(t.TempDir(), "notification-history.jsonl")
	for i, s := range []string{"Build passed", "Lunch?", "Build failed"} {
		notify.AppendHistory(d.NoteHistory, notify.Record{ID: uint32(i + 1), App: "CI", Summary: s, Time: time.Now()})
	}
	out, err := run(t, d, "notification_history", map[string]any{"query": "build"})
	var rs []notify.Record
	json.Unmarshal([]byte(out), &rs)
	if err != nil || len(rs) != 2 || rs[0].Summary != "Build failed" {
		t.Errorf("%v %s", err, out)
	}
	d.NoteHistoryOn = func() bool { return false }
	if _, err := run(t, d, "notification_history", nil); err == nil || !strings.Contains(err.Error(), "Keep a history") {
		t.Errorf("off: %v", err)
	}
}
