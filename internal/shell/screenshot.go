package shell

import (
	"encoding/json"
	"image"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/hypr"
)

// Rect is a rectangle in a monitor's logical pixels.
type Rect struct{ X, Y, W, H float64 }

// Contains reports whether the point is inside the rectangle.
func (r Rect) Contains(x, y float64) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// Empty reports whether the rectangle is too small to be a screenshot.
func (r Rect) Empty() bool { return r.W < 2 || r.H < 2 }

// SpanRect is the rectangle a drag from (x0, y0) to (x1, y1) covers, kept
// inside a monitor of the given size.
func SpanRect(x0, y0, x1, y1, w, h float64) Rect {
	clamp := func(v, hi float64) float64 { return max(0, min(v, hi)) }
	x0, x1 = clamp(x0, w), clamp(x1, w)
	y0, y1 = clamp(y0, h), clamp(y1, h)
	return Rect{X: min(x0, x1), Y: min(y0, y1), W: math.Abs(x1 - x0), H: math.Abs(y1 - y0)}
}

// SpanSquareRect is a square from the drag's starting point toward its end,
// sized by the larger pointer delta and kept inside the monitor.
func SpanSquareRect(x0, y0, x1, y1, w, h float64) Rect {
	clamp := func(v, hi float64) float64 { return max(0, min(v, hi)) }
	x0, x1 = clamp(x0, w), clamp(x1, w)
	y0, y1 = clamp(y0, h), clamp(y1, h)
	dx, dy := x1-x0, y1-y0
	sx, sy := 1.0, 1.0
	if dx < 0 || dx == 0 && x0 > w-x0 {
		sx = -1
	}
	if dy < 0 || dy == 0 && y0 > h-y0 {
		sy = -1
	}
	side := max(math.Abs(dx), math.Abs(dy))
	if sx < 0 {
		side = min(side, x0)
	} else {
		side = min(side, w-x0)
	}
	if sy < 0 {
		side = min(side, y0)
	} else {
		side = min(side, h-y0)
	}
	x2, y2 := x0+sx*side, y0+sy*side
	return Rect{X: min(x0, x2), Y: min(y0, y2), W: side, H: side}
}

// Pixels turns a logical rectangle into the pixels of a capture whose
// width is scale times the monitor's logical width, kept inside bounds.
func (r Rect) Pixels(scale float64, bounds image.Rectangle) image.Rectangle {
	px := image.Rect(
		int(math.Floor(r.X*scale)), int(math.Floor(r.Y*scale)),
		int(math.Ceil((r.X+r.W)*scale)), int(math.Ceil((r.Y+r.H)*scale)),
	).Add(bounds.Min)
	return px.Intersect(bounds)
}

// ShotWindow is a window as the screenshot tool sees it: where it is on
// its monitor, and how it stacks.
type ShotWindow struct {
	Address string
	Title   string
	Class   string
	Rect    Rect // on the monitor, logical pixels
}

// ShotWindows reads Hyprland's clients (j/clients) and returns the ones
// shown on a monitor (its position, and the workspaces it shows) in the
// order they stack, the top one first: floating windows over tiled ones,
// then the most recently focused first.
func ShotWindows(clients []byte, monX, monY float64, workspaces []int) ([]ShotWindow, error) {
	var raw []struct {
		Address        string     `json:"address"`
		Title          string     `json:"title"`
		Class          string     `json:"class"`
		At             [2]float64 `json:"at"`
		Size           [2]float64 `json:"size"`
		Mapped         bool       `json:"mapped"`
		Hidden         bool       `json:"hidden"`
		Floating       bool       `json:"floating"`
		Fullscreen     int        `json:"fullscreen"`
		FocusHistoryID int        `json:"focusHistoryID"`
		Workspace      struct {
			ID int `json:"id"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(clients, &raw); err != nil {
		return nil, err
	}
	type ranked struct {
		w          ShotWindow
		over, hist int
	}
	var out []ranked
	for _, c := range raw {
		if !c.Mapped || c.Hidden || c.Size[0] <= 0 || !slices.Contains(workspaces, c.Workspace.ID) {
			continue
		}
		over := 2
		switch {
		case c.Fullscreen > 0:
			over = 0
		case c.Floating:
			over = 1
		}
		out = append(out, ranked{
			w: ShotWindow{
				Address: hypr.NormalizeAddress(c.Address), Title: c.Title, Class: c.Class,
				Rect: Rect{X: c.At[0] - monX, Y: c.At[1] - monY, W: c.Size[0], H: c.Size[1]},
			},
			over: over, hist: c.FocusHistoryID,
		})
	}
	slices.SortStableFunc(out, func(a, b ranked) int {
		if a.over != b.over {
			return a.over - b.over
		}
		return a.hist - b.hist
	})
	ws := make([]ShotWindow, len(out))
	for i, r := range out {
		ws[i] = r.w
	}
	return ws, nil
}

// WindowAt is the index of the top window under a point, or -1.
func WindowAt(ws []ShotWindow, x, y float64) int {
	return slices.IndexFunc(ws, func(w ShotWindow) bool { return w.Rect.Contains(x, y) })
}

// ShotName names a screenshot taken at t.
func ShotName(t time.Time) string {
	return "Screenshot_" + t.Format("2006-01-02_15-04-05") + ".png"
}

// ShotItem is a screenshot or a screen recording, as the screenshot
// tool's drawer lists it.
type ShotItem struct {
	Path  string
	Video bool
	Time  time.Time // when it was last written
	Size  int64
}

// RecentShots are the newest n screenshots and recordings in dirs, newest
// first: the files the shell names (ShotName, and "Recording_….mp4").
// A directory given twice, or missing, is no matter.
func RecentShots(dirs []string, n int) []ShotItem {
	var out []ShotItem
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[filepath.Clean(dir)] {
			continue
		}
		seen[filepath.Clean(dir)] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			shot := strings.HasPrefix(name, "Screenshot_") && strings.HasSuffix(name, ".png")
			video := strings.HasPrefix(name, "Recording_") && strings.HasSuffix(name, ".mp4")
			if e.IsDir() || !shot && !video {
				continue
			}
			info, err := e.Info()
			if err != nil || info.Size() == 0 { // being written, or failed
				continue
			}
			out = append(out, ShotItem{Path: filepath.Join(dir, name), Video: video, Time: info.ModTime(), Size: info.Size()})
		}
	}
	slices.SortStableFunc(out, func(a, b ShotItem) int { return b.Time.Compare(a.Time) })
	return out[:min(len(out), n)]
}

// ThumbSize fits a w×h picture in maxW×maxH, keeping its shape and never
// enlarging it; each side at least 1.
func ThumbSize(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	f := min(1, float64(maxW)/float64(w), float64(maxH)/float64(h))
	return max(1, int(math.Round(float64(w)*f))), max(1, int(math.Round(float64(h)*f)))
}
