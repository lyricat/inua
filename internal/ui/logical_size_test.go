package ui

import (
	"testing"

	"github.com/lyricat/inua/internal/hypr"
)

func TestLogicalSizeForCompositors(t *testing.T) {
	wayfire := hypr.Monitor{Width: 1920, Height: 1080, Scale: 2, Raw: map[string]any{"wayfire": true}}
	if w, h := logicalSize(wayfire); w != 1920 || h != 1080 {
		t.Fatalf("Wayfire logical size = %vx%v", w, h)
	}
	hyprland := hypr.Monitor{Width: 3840, Height: 2160, Scale: 2}
	if w, h := logicalSize(hyprland); w != 1920 || h != 1080 {
		t.Fatalf("Hyprland logical size = %vx%v", w, h)
	}
}
