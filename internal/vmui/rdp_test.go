package vmui

import (
	"slices"
	"testing"
)

func TestRDPArgsFollowMonitorScale(t *testing.T) {
	args := rdpArgs("127.0.0.1", 3389, "Inua", "secret", &rdpDisplay{Width: 1920, Height: 1080, Scale: 2})
	for _, want := range []string{"/size:1536x864", "/scale-desktop:200", "/scale-device:180"} {
		if !slices.Contains(args, want) {
			t.Errorf("2x display: missing %q in %v", want, args)
		}
	}
	args = rdpArgs("127.0.0.1", 3389, "Inua", "secret", &rdpDisplay{Width: 1280, Height: 1024, Scale: 1})
	if !slices.Contains(args, "/scale-desktop:100") || !slices.Contains(args, "/size:1024x819") {
		t.Errorf("1x display: %v", args)
	}
	if slices.Contains(args, "/f") {
		t.Errorf("rdp opens fullscreen: %v", args)
	}
	if args := rdpArgs("127.0.0.1", 3389, "Inua", "secret", nil); slices.Contains(args, "/size:0x0") {
		t.Errorf("unknown display got a size: %v", args)
	}
}
