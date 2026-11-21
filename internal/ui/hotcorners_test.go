package ui

import (
	"testing"
	"time"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/config"
)

func TestHotCornerAtSingleMonitor(t *testing.T) {
	s := &shellUI{
		cfg: config.Config{
			HotCorners: config.HotCorners{
				TopLeft:     config.HotCornerOverview,
				TopRight:    config.HotCornerShowDesktop,
				BottomLeft:  config.HotCornerOff,
				BottomRight: config.HotCornerOverview,
			},
		},
		snap: compositor.Snapshot{
			Outputs: []compositor.Output{
				{Name: "DP-1", X: 0, Y: 0, Width: 1920, Height: 1080, Scale: 1},
			},
		},
	}

	// Inside top-left threshold (size = 24)
	hit := s.hotCornerAt(10, 10)
	if hit.corner != "top_left" || hit.action != config.HotCornerOverview || hit.monitor != "DP-1" {
		t.Fatalf("unexpected hit for top-left: %+v", hit)
	}

	// Boundary test: exactly at (24, 24)
	hit = s.hotCornerAt(24, 24)
	if hit.corner != "top_left" {
		t.Fatalf("expected top-left at boundary (24, 24), got %+v", hit)
	}

	// Outside corner zone
	hit = s.hotCornerAt(25, 25)
	if hit.corner != "" {
		t.Fatalf("expected no hit at (25, 25), got %+v", hit)
	}

	// Top-right corner
	hit = s.hotCornerAt(1920-15, 10)
	if hit.corner != "top_right" || hit.action != config.HotCornerShowDesktop || hit.monitor != "DP-1" {
		t.Fatalf("unexpected hit for top-right: %+v", hit)
	}

	// Bottom-left is off
	hit = s.hotCornerAt(10, 1080-10)
	if hit.corner != "" || hit.action != "" {
		t.Fatalf("expected no hit for off bottom-left, got %+v", hit)
	}

	// Bottom-right corner
	hit = s.hotCornerAt(1920-10, 1080-10)
	if hit.corner != "bottom_right" || hit.action != config.HotCornerOverview {
		t.Fatalf("unexpected hit for bottom-right: %+v", hit)
	}
}

func TestHotCornerAtMultiMonitor(t *testing.T) {
	// HDMI-A-1 at -1280x0 (1280x1024, scale 1)
	// HDMI-A-2 at 0x0 (3840x2160, scale 2 -> logical 1920x1080)
	s := &shellUI{
		cfg: config.Config{
			HotCorners: config.HotCorners{
				TopLeft:     config.HotCornerOverview,
				TopRight:    config.HotCornerShowDesktop,
				BottomLeft:  config.HotCornerOff,
				BottomRight: config.HotCornerShowDesktop,
			},
		},
		snap: compositor.Snapshot{
			Outputs: []compositor.Output{
				{Name: "HDMI-A-1", X: -1280, Y: 0, Width: 1280, Height: 1024, Scale: 1},
				{Name: "HDMI-A-2", X: 0, Y: 0, Width: 3840, Height: 2160, Scale: 2},
			},
		},
	}

	// Top-left of HDMI-A-2 (main monitor) at (0, 0)
	hit := s.hotCornerAt(10, 10)
	if hit.corner != "top_left" || hit.action != config.HotCornerOverview || hit.monitor != "HDMI-A-2" {
		t.Fatalf("expected top-left on main monitor HDMI-A-2, got %+v", hit)
	}

	// Top-right of HDMI-A-2 at (1920-10, 10)
	hit = s.hotCornerAt(1910, 10)
	if hit.corner != "top_right" || hit.action != config.HotCornerShowDesktop || hit.monitor != "HDMI-A-2" {
		t.Fatalf("expected top-right on HDMI-A-2, got %+v", hit)
	}

	// Top-left of secondary monitor HDMI-A-1 at (-1280+10, 10)
	hit = s.hotCornerAt(-1270, 10)
	if hit.corner != "top_left" || hit.action != config.HotCornerOverview || hit.monitor != "HDMI-A-1" {
		t.Fatalf("expected top-left on HDMI-A-1, got %+v", hit)
	}

	// Bottom-right of HDMI-A-1 at (-1280+1280-10, 1024-10) = (-10, 1014)
	hit = s.hotCornerAt(-10, 1014)
	if hit.corner != "bottom_right" || hit.action != config.HotCornerShowDesktop || hit.monitor != "HDMI-A-1" {
		t.Fatalf("expected bottom-right on HDMI-A-1, got %+v", hit)
	}
}

func TestHotCornersConfigured(t *testing.T) {
	c := config.HotCorners{
		TopLeft:     config.HotCornerOff,
		TopRight:    config.HotCornerOff,
		BottomLeft:  config.HotCornerOff,
		BottomRight: config.HotCornerOff,
	}
	if hotCornersConfigured(c) {
		t.Fatalf("expected false when all corners are off")
	}

	c.TopLeft = config.HotCornerOverview
	if !hotCornersConfigured(c) {
		t.Fatalf("expected true when TopLeft is on")
	}
}

func TestHotCornerDwellProgress(t *testing.T) {
	var firedAction string
	s := &shellUI{
		cfg: config.Config{
			HotCorners: config.HotCorners{
				TopLeft: config.HotCornerOverview,
				Delay:   150,
			},
		},
		snap: compositor.Snapshot{
			Outputs: []compositor.Output{
				{Name: "DP-1", X: 0, Y: 0, Width: 1920, Height: 1080, Scale: 1},
			},
		},
		onHotCornerAction: func(hit hotCornerHit) {
			firedAction = hit.action
		},
	}

	now := time.Now()
	hit := hotCornerHit{corner: "top_left", action: config.HotCornerOverview, monitor: "DP-1"}

	// Entry
	s.updateHotCorner(hit, now)
	if s.hotCorner.fired {
		t.Fatalf("should not fire immediately with 150ms delay")
	}

	// Halfway
	s.updateHotCorner(hit, now.Add(75*time.Millisecond))
	if s.hotCorner.fired {
		t.Fatalf("should not fire at 75ms")
	}

	// Trigger reached
	s.updateHotCorner(hit, now.Add(150*time.Millisecond))
	if !s.hotCorner.fired || firedAction != config.HotCornerOverview {
		t.Fatalf("expected fired overview after 150ms, got fired=%v action=%q", s.hotCorner.fired, firedAction)
	}

	// Should not re-fire while staying in corner
	s.updateHotCorner(hit, now.Add(200*time.Millisecond))
	if !s.hotCorner.fired {
		t.Fatalf("should remain fired")
	}

	// Leaving corner resets fired
	s.updateHotCorner(hotCornerHit{}, now.Add(250*time.Millisecond))
	if s.hotCorner.current.corner != "" {
		t.Fatalf("current should be reset")
	}
}
