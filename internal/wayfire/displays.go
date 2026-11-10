package wayfire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/wlroutput"
)

// Wayfire's IPC lists an output's place but not its modes, so the
// displays are read through wlr-output-management, which Wayfire has.
// They are set as the options of the output's section ("output:DP-1/mode",
// "/scale", "/position"), like the input options: over the IPC, each time
// the shell starts, nothing written to wayfire.ini. What an option was
// before inua first set it is kept in the runtime directory and put back
// when the settings no longer say anything about it.

// heads reads the displays; tests replace it.
var heads = wlroutput.Heads

// settle is how long Wayfire is given to lay its outputs out again before
// a display is placed beside the others.
var settle = 250 * time.Millisecond

// modeName writes a mode as the display settings do: "2560x1440@59.95",
// without the rate when the head names none.
func modeName(m wlroutput.Mode) string {
	if m.Refresh < 1000 {
		return fmt.Sprintf("%dx%d", m.Width, m.Height)
	}
	return fmt.Sprintf("%dx%d@%.2f", m.Width, m.Height, float64(m.Refresh)/1000)
}

// plugged leaves out the headless outputs, which nobody plugs in, unless
// the session has no other: then they are its screens (a remote desktop).
func plugged(all []wlroutput.Head) []wlroutput.Head {
	real := slices.DeleteFunc(slices.Clone(all), func(h wlroutput.Head) bool { return strings.HasPrefix(h.Name, "HEADLESS-") })
	if len(real) == 0 {
		return all
	}
	return real
}

// errNoOption is an option Wayfire does not have: no metadata names it.
var errNoOption = errors.New("wayfire: no such option")

// option reads one config option as Wayfire writes it.
func (b *Backend) option(ctx context.Context, name string) (string, error) {
	body, err := b.Client.Call(ctx, "wayfire/get-config-option", map[string]any{"option": name})
	if err != nil {
		var e struct{ Error string }
		if json.Unmarshal(body, &e) == nil && e.Error == "Option not found!" {
			return "", fmt.Errorf("%w: %s", errNoOption, name)
		}
		return "", err
	}
	var r struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("wayfire: get-config-option: %w", err)
	}
	var v string
	if json.Unmarshal(r.Value, &v) != nil {
		v = string(r.Value) // a number
	}
	return v, nil
}

// Displays are the displays as the settings show them.
func (b *Backend) Displays(ctx context.Context) ([]compositor.Display, error) {
	hs, err := heads(ctx)
	if err != nil {
		return nil, err
	}
	var out []compositor.Display
	for _, h := range plugged(hs) {
		d := compositor.Display{Name: h.Name, Description: h.Description, Make: h.Make, Model: h.Model,
			X: h.X, Y: h.Y, Scale: h.Scale, Disabled: !h.Enabled}
		for _, m := range h.Modes {
			if name := modeName(m); !slices.Contains(d.Modes, name) {
				d.Modes = append(d.Modes, name)
			}
		}
		if h.Current != nil {
			d.Width, d.Height, d.Refresh = h.Current.Width, h.Current.Height, float64(h.Current.Refresh)/1000
		}
		// Only the option says that an output shows another's picture.
		if v, err := b.option(ctx, "output:"+h.Name+"/mode"); err == nil {
			if of, ok := strings.CutPrefix(v, "mirror "); ok {
				d.MirrorOf = strings.TrimSpace(of)
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// headFor is the display a rule names: by its connector, or by "desc:"
// and its description.
func headFor(hs []wlroutput.Head, name string) (wlroutput.Head, bool) {
	desc, byDesc := strings.CutPrefix(name, "desc:")
	for _, h := range hs {
		if byDesc && h.Description == desc || !byDesc && h.Name == name {
			return h, true
		}
	}
	return wlroutput.Head{}, false
}

// modeOption writes a rule's mode as the option takes it,
// "WIDTHxHEIGHT@MILLIHERTZ": the head's own mode of that size nearest
// the rate asked for (the highest without one), so that the rate is one
// the display has; the size alone for a head with no such mode.
func modeOption(h wlroutput.Head, mode string) (string, bool) {
	sizeText, rateText, _ := strings.Cut(mode, "@")
	ws, hs, _ := strings.Cut(sizeText, "x")
	w, err1 := strconv.Atoi(ws)
	ht, err2 := strconv.Atoi(hs)
	rate, _ := strconv.ParseFloat(rateText, 64)
	if err1 != nil || err2 != nil || w <= 0 || ht <= 0 {
		return "", false
	}
	best, found := wlroutput.Mode{}, false
	for _, m := range h.Modes {
		if m.Width != w || m.Height != ht {
			continue
		}
		better := m.Refresh > best.Refresh
		if rate > 0 {
			better = math.Abs(float64(m.Refresh)-rate*1000) < math.Abs(float64(best.Refresh)-rate*1000)
		}
		if !found || better {
			best, found = m, true
		}
	}
	switch {
	case found && best.Refresh > 0:
		return fmt.Sprintf("%dx%d@%d", w, ht, best.Refresh), true
	case !found && rate > 0:
		return fmt.Sprintf("%dx%d@%d", w, ht, int(math.Round(rate*1000))), true
	}
	return fmt.Sprintf("%dx%d", w, ht), true
}

// size is a head's size in the layout: its mode by its scale, turned
// with it.
func size(h wlroutput.Head) (w, ht int) {
	if h.Current == nil {
		return 0, 0
	}
	scale := h.Scale
	if scale <= 0 {
		scale = 1
	}
	w, ht = int(math.Round(float64(h.Current.Width)/scale)), int(math.Round(float64(h.Current.Height)/scale))
	if h.Transform%2 == 1 {
		w, ht = ht, w
	}
	return w, ht
}

// beside is where a head goes to be on that side of the others
// ("auto-left", …): against their bounding box, level with its top or
// left edge.
func beside(h wlroutput.Head, others []wlroutput.Head, side string) (x, y int, ok bool) {
	left, top, right, bottom := math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
	for _, o := range others {
		w, ht := size(o)
		if o.Name == h.Name || !o.Enabled || w == 0 {
			continue
		}
		left, top = min(left, o.X), min(top, o.Y)
		right, bottom = max(right, o.X+w), max(bottom, o.Y+ht)
	}
	w, ht := size(h)
	if left == math.MaxInt || w == 0 {
		return 0, 0, false
	}
	switch side {
	case "auto-left":
		return left - w, top, true
	case "auto-right":
		return right, top, true
	case "auto-up":
		return left, top - ht, true
	case "auto-down":
		return left, bottom, true
	}
	return 0, 0, false
}

// outputsFile keeps what the output options were before inua set them.
func outputsFile() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "inua", "wayfire-outputs.json")
}

// applyDisplays sets the output options the rules ask for, and puts back
// those an earlier apply set that no rule asks for now.
func (b *Backend) applyDisplays(ctx context.Context, rules []compositor.DisplayRule) error {
	saved := map[string]string{}
	if data, err := os.ReadFile(outputsFile()); err == nil {
		json.Unmarshal(data, &saved)
	}
	if len(rules) == 0 && len(saved) == 0 {
		return nil
	}
	if !b.has("wayfire/set-config-options") || !b.has("wayfire/get-config-option") {
		return fmt.Errorf("%w: display settings need Wayfire's ipc-rules plugin", compositor.ErrUnavailable)
	}
	hs, err := heads(ctx)
	if err != nil {
		return err
	}

	want := map[string]string{}
	type placed struct{ name, side string }
	var relative []placed
	for _, r := range rules {
		h, ok := headFor(hs, r.Output)
		if !ok {
			continue // not connected now
		}
		section := "output:" + h.Name + "/"
		switch {
		case r.Disabled:
			want[section+"mode"] = "off"
		case r.Mirror != "":
			if of, ok := headFor(hs, r.Mirror); ok {
				want[section+"mode"] = "mirror " + of.Name
			}
		case r.Mode != "":
			if mode, ok := modeOption(h, r.Mode); ok {
				want[section+"mode"] = mode
			}
		}
		if r.Scale > 0 {
			want[section+"scale"] = strconv.FormatFloat(r.Scale, 'f', -1, 64)
		}
		switch r.Position {
		case "", "auto":
		case "auto-left", "auto-right", "auto-up", "auto-down":
			if !r.Disabled && r.Mirror == "" {
				relative = append(relative, placed{h.Name, r.Position})
			}
		default:
			if xs, ys, ok := strings.Cut(r.Position, "x"); ok {
				x, err1 := strconv.Atoi(xs)
				y, err2 := strconv.Atoi(ys)
				if err1 == nil && err2 == nil {
					want[section+"position"] = fmt.Sprintf("%d,%d", x, y)
				}
			}
		}
	}

	// Wayfire lays the displays with no place of their own out again to
	// the right of those that have one, so one placed display would send
	// the rest elsewhere: they are kept where they are.
	isRelative := func(name string) bool {
		return slices.ContainsFunc(relative, func(p placed) bool { return p.name == name })
	}
	pin := func(hs []wlroutput.Head, into map[string]string) {
		for _, h := range hs {
			opt := "output:" + h.Name + "/position"
			if _, placed := want[opt]; placed || !h.Enabled || isRelative(h.Name) || want["output:"+h.Name+"/mode"] == "off" {
				continue
			}
			into[opt] = fmt.Sprintf("%d,%d", h.X, h.Y)
		}
	}
	for opt := range want {
		if strings.HasSuffix(opt, "/position") {
			pin(hs, want)
			break
		}
	}

	save := func() {
		os.MkdirAll(filepath.Dir(outputsFile()), 0o700)
		data, _ := json.Marshal(saved)
		os.WriteFile(outputsFile(), data, 0o600)
	}
	// remember keeps an option's value from before inua's first change.
	remember := func(opt string) bool {
		if _, ok := saved[opt]; ok {
			return true
		}
		was, err := b.option(ctx, opt)
		if err != nil {
			return false
		}
		saved[opt] = was
		return true
	}
	set := map[string]any{}
	for opt, was := range saved {
		// With a display to place beside the others, every place is set
		// in the second step.
		later := len(relative) > 0 && strings.HasSuffix(opt, "/position")
		if _, ok := want[opt]; !ok && !later {
			set[opt] = was
			delete(saved, opt)
		}
	}
	for opt, v := range want {
		if remember(opt) {
			set[opt] = v
		}
	}
	save()
	if len(set) > 0 {
		if err := b.call(ctx, "wayfire/set-config-options", set); err != nil {
			return err
		}
	}
	if len(relative) == 0 {
		return nil
	}

	// Beside the others as they are once the modes and scales took.
	select {
	case <-time.After(settle):
	case <-ctx.Done():
		return ctx.Err()
	}
	if hs, err = heads(ctx); err != nil {
		return err
	}
	places := map[string]string{}
	pin(hs, places)
	for _, p := range relative {
		h, ok := headFor(hs, p.name)
		if !ok {
			continue
		}
		if x, y, ok := beside(h, hs, p.side); ok {
			places["output:"+p.name+"/position"] = fmt.Sprintf("%d,%d", x, y)
		}
	}
	set = map[string]any{}
	for opt, v := range places {
		if remember(opt) {
			set[opt] = v
		}
	}
	save()
	if len(set) == 0 {
		return nil
	}
	return b.call(ctx, "wayfire/set-config-options", set)
}
