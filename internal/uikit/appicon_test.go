package uikit

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/config"
)

// Every shape the settings name is a shape of its own, in their order,
// and a name they do not have is the squircle.
func TestIconShapeFromName(t *testing.T) {
	want := []IconShape{IconShapeSquircle, IconShapeCircle, IconShapePebble, IconShapeNone}
	if len(config.IconShapes) != len(want) {
		t.Fatalf("config.IconShapes = %q, want %d shapes", config.IconShapes, len(want))
	}
	for i, name := range config.IconShapes {
		if got := IconShapeFromName(name); got != want[i] {
			t.Errorf("IconShapeFromName(%q) = %d, want %d", name, got, want[i])
		}
	}
	for _, name := range []string{"", "Circle", "star"} {
		if got := IconShapeFromName(name); got != IconShapeSquircle {
			t.Errorf("IconShapeFromName(%q) = %d, want the squircle", name, got)
		}
	}
}

// pathStep is one command of an outline: its letter and numbers.
type pathStep struct {
	op   string
	args []float64
}

func parsePath(t *testing.T, path string) []pathStep {
	t.Helper()
	var steps []pathStep
	for _, f := range strings.Fields(path) {
		if n, err := strconv.ParseFloat(f, 64); err == nil {
			if len(steps) == 0 {
				t.Fatalf("a number before a command in %q", path)
			}
			steps[len(steps)-1].args = append(steps[len(steps)-1].args, n)
			continue
		}
		steps = append(steps, pathStep{op: f})
	}
	return steps
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

// The pebble is the square's sides, straight, joined at each corner by a
// quarter circle of its own radius, all inside the square; its corners
// differ, and two on a side leave some of it straight.
func TestPebbleOutline(t *testing.T) {
	const x, y, side = 10, 20, 64
	steps := parsePath(t, pebble(x, y, side))
	if len(steps) != 10 || steps[0].op != "M" || steps[9].op != "Z" {
		t.Fatalf("want M, four sides and corners, Z: %v", steps)
	}
	at := [2]float64{steps[0].args[0], steps[0].args[1]}
	start := at
	inside := func(p [2]float64) bool {
		return p[0] >= x-1e-3 && p[0] <= x+side+1e-3 && p[1] >= y-1e-3 && p[1] <= y+side+1e-3
	}
	onEdge := func(p [2]float64) bool {
		return near(p[0], x) || near(p[0], x+side) || near(p[1], y) || near(p[1], y+side)
	}
	var radii []float64
	for i, st := range steps[1:9] {
		switch {
		case i%2 == 0 && st.op == "L" && len(st.args) == 2:
			to := [2]float64{st.args[0], st.args[1]}
			// A side: along one of the square's edges.
			if !(near(to[0], at[0]) && (near(to[0], x) || near(to[0], x+side))) &&
				!(near(to[1], at[1]) && (near(to[1], y) || near(to[1], y+side))) {
				t.Errorf("side %d from %v to %v is not along an edge", i/2, at, to)
			}
			at = to
		case i%2 == 1 && st.op == "A" && len(st.args) == 7:
			rx, ry, large, sweep := st.args[0], st.args[1], st.args[3], st.args[4]
			to := [2]float64{st.args[5], st.args[6]}
			// A corner: a quarter circle, turning the way the outline goes.
			if !near(rx, ry) || large != 0 || sweep != 1 {
				t.Errorf("corner %d: radii %g %g, large %g, sweep %g", i/2, rx, ry, large, sweep)
			}
			if !near(math.Abs(to[0]-at[0]), rx) || !near(math.Abs(to[1]-at[1]), rx) {
				t.Errorf("corner %d from %v to %v is not a quarter circle of radius %g", i/2, at, to, rx)
			}
			radii = append(radii, rx)
			at = to
		default:
			t.Fatalf("step %d: %v", i+1, st)
		}
		if !inside(at) || !onEdge(at) {
			t.Errorf("step %d ends at %v, off the square's edges", i+1, at)
		}
	}
	if !near(at[0], start[0]) || !near(at[1], start[1]) {
		t.Errorf("the outline ends at %v, not where it began, %v", at, start)
	}
	if len(radii) != 4 {
		t.Fatalf("corners %v", radii)
	}
	uneven := false
	for i, r := range radii {
		if r <= 0 || r+radii[(i+1)%4] >= side {
			t.Errorf("corners %v leave no straight side", radii)
		}
		uneven = uneven || !near(r, radii[0])
	}
	if !uneven {
		t.Errorf("corners %v are all alike", radii)
	}
}

// The circle fills the square: from its left edge's middle to its right
// edge's and back, each half of the side's radius.
func TestCircleOutline(t *testing.T) {
	const x, y, side = 10, 20, 64
	steps := parsePath(t, iconPath(IconShapeCircle, x, y, side))
	if len(steps) != 4 || steps[0].op != "M" || steps[1].op != "a" || steps[2].op != "a" || steps[3].op != "Z" {
		t.Fatalf("want M, two arcs, Z: %v", steps)
	}
	if m := steps[0].args; !near(m[0], x) || !near(m[1], y+side/2) {
		t.Errorf("starts at %v, not the left edge's middle", m)
	}
	for i, a := range steps[1:3] {
		dx := a.args[5]
		if !near(a.args[0], side/2) || !near(a.args[1], side/2) || !near(math.Abs(dx), side) || !near(a.args[6], 0) {
			t.Errorf("half %d: %v", i, a.args)
		}
	}
	if steps[1].args[5]+steps[2].args[5] != 0 {
		t.Errorf("the halves do not meet: %v %v", steps[1].args, steps[2].args)
	}
}
