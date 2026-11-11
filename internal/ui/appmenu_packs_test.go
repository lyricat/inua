package ui

import (
	"testing"

	"github.com/lyricat/inua/internal/compositor"
)

func TestAppWindowOfPrefersFocusedWindowForSharedPID(t *testing.T) {
	s := shellUI{snap: compositor.Snapshot{Windows: []compositor.Window{
		{ID: "first", PID: 42, AppID: "browser"},
		{ID: "focused", PID: 42, AppID: "browser", Focused: true},
	}}}

	got, ok := s.appWindowOf(42)
	if !ok || got.ID != "focused" {
		t.Fatalf("appWindowOf(42) = (%q, %t), want focused window", got.ID, ok)
	}
}

func TestAppWindowOfUsesFirstWindowWhenProcessIsUnfocused(t *testing.T) {
	s := shellUI{snap: compositor.Snapshot{Windows: []compositor.Window{
		{ID: "first", PID: 42, AppID: "browser"},
		{ID: "other", PID: 7, AppID: "editor", Focused: true},
		{ID: "second", PID: 42, AppID: "browser"},
	}}}

	got, ok := s.appWindowOf(42)
	if !ok || got.ID != "first" {
		t.Fatalf("appWindowOf(42) = (%q, %t), want first matching window", got.ID, ok)
	}
}
