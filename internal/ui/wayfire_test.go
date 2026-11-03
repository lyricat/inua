package ui

import (
	"testing"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/shell"
)

func TestWayfireWindowsFeedDockAndSwitcher(t *testing.T) {
	snapshot := compositor.Snapshot{
		Toplevels: []compositor.Toplevel{
			{Address: "wf:9", Title: "Editor", Class: "org.example.Editor"},
			{Address: "wf:5", Title: "Browser", Class: "org.example.Browser", Activated: true},
		},
		ActiveToplevel: "wf:5",
	}
	windows := wayfireSnapshot(snapshot)
	apps := shell.NewAppIndex([]*desktop.Entry{
		{ID: "org.example.Editor", Name: "Editor", Icon: "editor"},
		{ID: "org.example.Browser", Name: "Browser", Icon: "browser"},
	})

	items := shell.Dock([]string{"org.example.Browser"}, apps, windows, nil)
	if len(items) != 2 || items[0].Key != "org.example.Browser" || !items[0].Pinned || !items[0].Active || len(items[0].Windows) != 1 || items[0].Windows[0].Address != "wf:5" {
		t.Fatalf("Dock() = %+v; want pinned active Browser and running Editor", items)
	}
	if items[1].Key != "org.example.Editor" || items[1].Pinned || len(items[1].Windows) != 1 || items[1].Windows[0].Address != "wf:9" {
		t.Fatalf("Dock running item = %+v; want Editor window wf:9", items[1])
	}

	var launcher shell.Launcher
	launcher.SetConnected(true)
	launcher.SetSnapshot(windows)
	launcher.Show(shell.Windows)
	if len(launcher.Results) != 2 || launcher.Results[0].Address != "wf:5" || launcher.Results[1].Address != "wf:9" || launcher.Selected != 1 {
		t.Fatalf("window switcher results=%+v selected=%d; want active first and previous window selected", launcher.Results, launcher.Selected)
	}
}
