package ui

import (
	"math"
	"testing"

	"codeberg.org/puregotk/puregotk/v4/gdk"
)

// The ring takes the first stop of Hyprland's active border gradient, alpha
// included, from getoption's custom text.
func TestActiveBorderColorFromCustom(t *testing.T) {
	near := func(got, want gdk.RGBA) bool {
		const eps = 1e-6
		return math.Abs(float64(got.Red-want.Red)) < eps &&
			math.Abs(float64(got.Green-want.Green)) < eps &&
			math.Abs(float64(got.Blue-want.Blue)) < eps &&
			math.Abs(float64(got.Alpha-want.Alpha)) < eps
	}
	rgba := func(r, g, b, a uint8) gdk.RGBA {
		return gdk.RGBA{Red: float32(r) / 255, Green: float32(g) / 255, Blue: float32(b) / 255, Alpha: float32(a) / 255}
	}
	for _, tc := range []struct {
		name, reply string
		want        gdk.RGBA
	}{
		{"gradient", `{"option":"general:col.active_border","custom":"ee33ccff ee00ff99 45deg","set":true}`, rgba(0x33, 0xcc, 0xff, 0xee)},
		{"one colour", `{"option": "general:col.active_border", "custom": "80112233 0deg", "set": false }`, rgba(0x11, 0x22, 0x33, 0x80)},
	} {
		got, ok := activeBorderColor([]byte(tc.reply))
		if !ok || !near(got, tc.want) {
			t.Errorf("%s: got %+v ok=%v, want %+v", tc.name, got, ok, tc.want)
		}
	}
	// Anything else leaves the default blue to the caller.
	for _, reply := range []string{
		``,
		`not json`,
		`{"option":"general:col.active_border","custom":"","set":true}`,
		`{"option":"general:col.active_border","custom":"zzzzzzzz 45deg","set":true}`,
		`{"option":"general:col.active_border","gradient":"ee33ccff ee00ff99 45deg","color":"0xee33ccff"}`,
		`{"error":"no such option"}`,
	} {
		if got, ok := activeBorderColor([]byte(reply)); ok {
			t.Errorf("%q: got %+v, want no colour", reply, got)
		}
	}
}
