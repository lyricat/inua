package ui

import (
	"testing"

	"github.com/lyricat/inua/internal/appmenu"
)

func TestAppMenuKeyLabel(t *testing.T) {
	for _, tc := range []struct {
		action appmenu.Action
		want   string
	}{
		{appmenu.Action{Kind: appmenu.ActionKeys, Mods: "CTRL SHIFT", Key: "T"}, "Ctrl Shift T"},
		{appmenu.Action{Kind: appmenu.ActionKeys, Mods: "CTRL ALT", Key: "comma"}, "Ctrl Alt ,"},
		{appmenu.Action{Kind: appmenu.ActionKeys, Key: "f12"}, "F12"},
		{appmenu.Action{Kind: appmenu.ActionKeySequence, Sequence: []appmenu.KeyStroke{{Key: "g"}, {Mods: "SHIFT", Key: "d"}}}, "g shift-d"},
		{appmenu.Action{Kind: appmenu.ActionQuitApp}, ""},
	} {
		if got := appMenuKeyLabel(tc.action); got != tc.want {
			t.Errorf("appMenuKeyLabel(%+v) = %q, want %q", tc.action, got, tc.want)
		}
	}
}
