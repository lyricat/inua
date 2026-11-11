package wayfire

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/lyricat/inua/internal/compositor"
)

// The Wayfire versions whose metadata is in testdata/metadata.
var metadataVersions = []string{"0.9.0-5", "0.10.0-1"}

// wfOption is one option as Wayfire's metadata declares it.
type wfOption struct {
	typ, def string
	values   []string // the <desc> values of a choice
}

// metadata reads the options testdata/metadata/VERSION declares, by
// "section/name".
func metadata(t *testing.T, version string) map[string]wfOption {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "metadata", version, "*.xml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no metadata for %s: %v", version, err)
	}
	out := map[string]wfOption{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Plugins []struct {
				Name    string `xml:"name,attr"`
				Options []struct {
					Name    string `xml:"name,attr"`
					Type    string `xml:"type,attr"`
					Default string `xml:"default"`
					Desc    []struct {
						Value string `xml:"value"`
					} `xml:"desc"`
				} `xml:"option"`
			} `xml:"plugin"`
		}
		if err := xml.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, p := range doc.Plugins {
			for _, o := range p.Options {
				opt := wfOption{typ: o.Type, def: strings.TrimSpace(o.Default)}
				for _, d := range o.Desc {
					opt.values = append(opt.values, d.Value)
				}
				out[p.Name+"/"+o.Name] = opt
			}
		}
	}
	return out
}

// animationValue reads an animation option as wf-config 0.9 and 0.10 do
// (src/duration.cpp): "N", or "N ms|s [easing]", written back as
// "Nms easing".
func animationValue(s string) (string, bool) {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return fmt.Sprintf("%dms circle", n), true
	}
	v := strings.TrimSpace(s)
	i := strings.IndexFunc(v, func(r rune) bool { return !strings.ContainsRune("+-.0123456789", r) })
	if i <= 0 {
		return "", false
	}
	n, err := strconv.ParseFloat(v[:i], 64)
	if err != nil {
		return "", false
	}
	rest := strings.Fields(v[i:])
	if len(rest) == 0 || rest[0] != "ms" && rest[0] != "s" || len(rest) > 2 {
		return "", false
	}
	easing := "circle"
	if len(rest) == 2 {
		easing = rest[1]
	}
	if !slices.Contains([]string{"linear", "circle", "sigmoid", "easeOutElastic"}, easing) {
		return "", false
	}
	if rest[0] == "s" {
		n *= 1000
	}
	return fmt.Sprintf("%dms %s", int(n), easing), true
}

// parse reads a value as an option of this type takes it; false where
// Wayfire refuses it.
func (o wfOption) parse(s string) (string, bool) {
	switch o.typ {
	case "animation":
		return animationValue(s)
	case "int":
		_, err := strconv.Atoi(s)
		return s, err == nil
	case "bool":
		return s, s == "true" || s == "false"
	}
	return s, true
}

// optionStore is Wayfire's options as get-config-option and
// set-config-options see them (plugins/ipc-rules/ipc-utility-methods.hpp):
// set takes the options in turn and stops at the first it refuses.
type optionStore struct {
	mu      sync.Mutex
	meta    map[string]wfOption
	values  map[string]string
	refuse  string // an option set refuses, after those before it
	getFail string // an error every get answers with
}

// newOptionStore is a Wayfire started with the metadata of version and
// ini over its defaults.
func newOptionStore(t *testing.T, version string, ini map[string]string) *optionStore {
	s := &optionStore{meta: metadata(t, version)}
	s.restart(t, ini)
	return s
}

// restart is Wayfire started again: the values are the defaults and ini.
func (s *optionStore) restart(t *testing.T, ini map[string]string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = map[string]string{}
	for name, o := range s.meta {
		v, ok := o.parse(o.def)
		if !ok {
			v = o.def
		}
		s.values[name] = v
	}
	for name, v := range ini {
		o, known := s.meta[name]
		v, ok := o.parse(v)
		if !known || !ok {
			t.Fatalf("wayfire.ini %s = %q is not an option of this Wayfire", name, v)
		}
		s.values[name] = v
	}
}

func (s *optionStore) get(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[name]
}

func (s *optionStore) snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.values)
}

func (s *optionStore) serve(method string, data json.RawMessage) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	quote := func(v string) string { b, _ := json.Marshal(v); return string(b) }
	switch method {
	case "wayfire/get-config-option":
		if s.getFail != "" {
			return `{"error":` + quote(s.getFail) + `}`, true
		}
		var req struct{ Option string }
		json.Unmarshal(data, &req)
		o, ok := s.meta[req.Option]
		if !ok {
			return `{"error":"Option not found!"}`, true
		}
		return `{"result":"ok","value":` + quote(s.values[req.Option]) + `,"default":` + quote(o.def) + `}`, true
	case "wayfire/set-config-options":
		var req map[string]any
		json.Unmarshal(data, &req)
		for _, name := range slices.Sorted(maps.Keys(req)) {
			if !strings.Contains(name, "/") || strings.HasPrefix(name, "input/") {
				continue // the input options are no part of this store
			}
			o, ok := s.meta[name]
			if !ok {
				return `{"error":` + quote(name+": Option not found!") + `}`, true
			}
			str, isString := req[name].(string)
			v, valid := o.parse(str)
			if !isString || !valid || name == s.refuse {
				return `{"error":` + quote(fmt.Sprintf("%s: Invalid value for option %v!", name, req[name])) + `}`, true
			}
			s.values[name] = v
		}
		return `{"result":"ok"}`, true
	}
	return "", false
}

// wayfireWith is a fake Wayfire with the ipc-rules and command plugins
// and the options of store.
func wayfireWith(t *testing.T, store *optionStore) *fakeWayfire {
	t.Helper()
	f := newFakeWayfire(t)
	f.mu.Lock()
	f.methods = append(slices.Clone(allMethods), "wayfire/set-config-options", "wayfire/get-config-option",
		"command/register-binding", "command/unregister-binding")
	f.serve = store.serve
	f.mu.Unlock()
	f.set("command/register-binding", `{"result":"ok","binding-id":7}`)
	return f
}

// sets are the set-config-options calls made since the last reset.
func sets(f *fakeWayfire) []string {
	var out []string
	for _, c := range f.did() {
		if strings.HasPrefix(c, "wayfire/set-config-options ") {
			out = append(out, strings.TrimPrefix(c, "wayfire/set-config-options "))
		}
	}
	return out
}

func resetCalls(f *fakeWayfire) {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

func motionSaved(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "inua", "wayfire-motion.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]string
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

// Every option inua stills is one stock Wayfire declares (all but
// minimize_animation, new in 0.10, in 0.9 too), with a value it takes:
// a choice it lists, the matcher its metadata uses for "no window", an
// animation of no length.
func TestMotionOptionsInMetadata(t *testing.T) {
	for _, version := range metadataVersions {
		meta := metadata(t, version)
		if def := meta["animate/zoom_enabled_for"].def; def != "none" {
			t.Fatalf("%s: zoom_enabled_for defaults to %q, not the matcher of no window", version, def)
		}
		for _, o := range motionOptions {
			m, ok := meta[o.name]
			if !ok {
				if version == "0.9.0-5" && o.name == "animate/minimize_animation" {
					continue
				}
				t.Errorf("%s has no option %s", version, o.name)
				continue
			}
			switch {
			case m.typ == "animation":
				v, ok := animationValue(o.off)
				if !ok || !still(o.off, v) {
					t.Errorf("%s %s: %q is not an animation of no length (%q)", version, o.name, o.off, v)
				}
			case m.typ == "string" && len(m.values) > 0:
				if !slices.Contains(m.values, o.off) {
					t.Errorf("%s %s: %q is none of %q", version, o.name, o.off, m.values)
				}
			case m.typ == "string" && strings.HasSuffix(o.name, "_enabled_for"):
				// A matcher: "none", as zoom_enabled_for's default.
			default:
				t.Errorf("%s %s: a %s option", version, o.name, m.typ)
			}
			if still(o.off, m.def) && o.name != "animate/zoom_enabled_for" && o.name != "animate/fire_enabled_for" {
				t.Errorf("%s %s: still by default (%q)", version, o.name, m.def)
			}
		}
	}
}

func TestStill(t *testing.T) {
	for _, c := range []struct {
		off, value string
		want       bool
	}{
		{"0", "0ms circle", true}, {"0", "0", true}, {"0", "0 s linear", true}, {"0", "300ms circle", false},
		{"0", "300", false}, {"0", "0.5s", false}, {"0", "", false},
		{"none", "none", true}, {"none", " none ", true}, {"none", "zoom", false}, {"none", `type equals "overlay"`, false},
	} {
		if got := still(c.off, c.value); got != c.want {
			t.Errorf("still(%q, %q) = %v", c.off, c.value, got)
		}
	}
}

// Off stills the motion options Wayfire has and keeps what they were; on
// puts those back; nil touches nothing. A repeat, or another shell of the
// same session, sets nothing again.
func TestApplyAnimations(t *testing.T) {
	ini := map[string]string{"animate/open_animation": "fade", "vswitch/duration": "250ms linear", "grid/type": "wobbly"}
	for _, version := range metadataVersions {
		t.Run(version, func(t *testing.T) {
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			store := newOptionStore(t, version, ini)
			_, hasMinimize := store.meta["animate/minimize_animation"]
			before := store.snapshot()
			f := wayfireWith(t, store)
			b := New(NewClient(f.socket))
			off, on := false, true
			opts := "caps:escape"
			ctx := context.Background()

			if err := b.ApplySettings(ctx, compositor.Settings{KbOptions: &opts, Animations: &off}); err != nil {
				t.Fatal(err)
			}
			want := []string{
				`{"input/xkb_options":"caps:escape"}`,
				`{"animate/close_animation":"none",` + map[bool]string{true: `"animate/fade_enabled_for":"none","animate/minimize_animation":"none",`, false: `"animate/fade_enabled_for":"none",`}[hasMinimize] +
					`"animate/open_animation":"none","grid/type":"none","vswitch/duration":"0"}`,
			}
			if got := sets(f); !slices.Equal(got, want) {
				t.Fatalf("off: %q\nwant %q", got, want)
			}
			for _, o := range motionOptions {
				if v, ok := store.snapshot()[o.name]; ok && !still(o.off, v) {
					t.Errorf("off: %s is %q", o.name, v)
				}
			}
			wantSaved := map[string]string{
				"animate/open_animation": "fade", "animate/close_animation": "zoom", "animate/fade_enabled_for": `type equals "overlay"`,
				"grid/type": "wobbly", "vswitch/duration": "250ms linear",
			}
			if hasMinimize {
				wantSaved["animate/minimize_animation"] = "squeezimize"
			}
			if got := motionSaved(t); !maps.Equal(got, wantSaved) {
				t.Fatalf("kept %q\nwant %q", got, wantSaved)
			}

			// Again, and from the next shell of the session: nothing to set.
			for _, b := range []*Backend{b, New(NewClient(f.socket))} {
				resetCalls(f)
				if err := b.ApplySettings(ctx, compositor.Settings{Animations: &off}); err != nil {
					t.Fatal(err)
				}
				if got := sets(f); len(got) != 0 {
					t.Fatalf("off again: %q", got)
				}
			}
			if got := motionSaved(t); !maps.Equal(got, wantSaved) {
				t.Fatalf("kept after a repeat %q", got)
			}

			// Unset: not even read.
			resetCalls(f)
			if err := b.ApplySettings(ctx, compositor.Settings{}); err != nil {
				t.Fatal(err)
			}
			if got := f.did(); len(got) != 0 {
				t.Fatalf("unset: %q", got)
			}

			// On: what they were, the record gone.
			resetCalls(f)
			if err := b.ApplySettings(ctx, compositor.Settings{Animations: &on}); err != nil {
				t.Fatal(err)
			}
			if got := sets(f); len(got) != 1 {
				t.Fatalf("on: %q", got)
			}
			if got := store.snapshot(); !maps.Equal(got, before) {
				for name := range before {
					if got[name] != before[name] {
						t.Errorf("on: %s is %q, was %q", name, got[name], before[name])
					}
				}
			}
			if got := motionSaved(t); got != nil {
				t.Fatalf("on: kept %q", got)
			}

			// On again: Wayfire is not even asked.
			resetCalls(f)
			if err := b.ApplySettings(ctx, compositor.Settings{Animations: &on}); err != nil {
				t.Fatal(err)
			}
			if got := f.did(); len(got) != 0 {
				t.Fatalf("on again: %q", got)
			}
		})
	}
}

// Wayfire started again while inua had its motion stilled: off keeps
// what wayfire.ini gives it now, and on puts back only options still
// stilled, leaving the others as wayfire.ini has them. Wayfire cannot say
// who stilled an option, so one that wayfire.ini now turns off itself
// gets the value from before the restart back.
func TestApplyAnimationsWayfireRestarted(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	store := newOptionStore(t, "0.10.0-1", map[string]string{"animate/open_animation": "fade"})
	f := wayfireWith(t, store)
	b := New(NewClient(f.socket))
	off, on := false, true
	ctx := context.Background()
	if err := b.ApplySettings(ctx, compositor.Settings{Animations: &off}); err != nil {
		t.Fatal(err)
	}

	store.restart(t, map[string]string{"animate/open_animation": "spin", "animate/close_animation": "none"})
	resetCalls(f)
	if err := b.ApplySettings(ctx, compositor.Settings{Animations: &off}); err != nil {
		t.Fatal(err)
	}
	got := motionSaved(t)
	if got["animate/open_animation"] != "spin" || got["animate/close_animation"] != "zoom" {
		t.Fatalf("kept after a restart %q", got)
	}
	if s := sets(f); len(s) != 1 || strings.Contains(s[0], "close_animation") {
		t.Fatalf("off after a restart: %q", s)
	}

	store.restart(t, map[string]string{"animate/open_animation": "zap"})
	resetCalls(f)
	if err := b.ApplySettings(ctx, compositor.Settings{Animations: &on}); err != nil {
		t.Fatal(err)
	}
	if s := sets(f); len(s) != 0 {
		t.Fatalf("on after a restart set %q over wayfire.ini", s)
	}
	if store.get("animate/open_animation") != "zap" || store.get("animate/close_animation") != "zoom" {
		t.Fatal("on after a restart changed wayfire.ini's values")
	}
	if motionSaved(t) != nil {
		t.Fatal("on after a restart kept the record")
	}

	// Stilled again, then Wayfire again with close_animation off in
	// wayfire.ini: on gives it its old value.
	if err := b.ApplySettings(ctx, compositor.Settings{Animations: &off}); err != nil {
		t.Fatal(err)
	}
	store.restart(t, map[string]string{"animate/open_animation": "zap", "animate/close_animation": "none"})
	resetCalls(f)
	if err := b.ApplySettings(ctx, compositor.Settings{Animations: &on}); err != nil {
		t.Fatal(err)
	}
	if s := sets(f); !slices.Equal(s, []string{`{"animate/close_animation":"zoom"}`}) {
		t.Fatalf("on after a restart: %q", s)
	}
}

// A failure with the motion options is returned once the input options,
// the displays and the shortcuts are applied; what was set before it is
// put back by the next on, and nothing else.
func TestApplyAnimationsFailures(t *testing.T) {
	off, on := false, true
	opts := "caps:escape"
	ctx := context.Background()
	settings := func(anim *bool) compositor.Settings {
		return compositor.Settings{KbOptions: &opts, Animations: anim,
			Binds: []compositor.KeyBind{{Keys: "SUPER+comma", Command: "inua call settings toggle"}}}
	}
	registered := func(t *testing.T) {
		t.Helper()
		ids, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "inua", "wayfire-binds.json"))
		if string(ids) != "[7]" {
			t.Fatalf("shortcuts not applied: registered ids %s", ids)
		}
	}
	applied := func(t *testing.T, f *fakeWayfire) {
		t.Helper()
		if calls := f.did(); len(calls) == 0 || calls[0] != `wayfire/set-config-options {"input/xkb_options":"caps:escape"}` {
			t.Fatalf("input options not applied: %q", calls)
		}
		registered(t)
	}

	t.Run("refused by halves", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		store := newOptionStore(t, "0.10.0-1", nil)
		store.refuse = "grid/type"
		f := wayfireWith(t, store)
		b := New(NewClient(f.socket))
		err := b.ApplySettings(ctx, settings(&off))
		if err == nil || !strings.Contains(err.Error(), "grid/type") {
			t.Fatalf("a refused option: %v", err)
		}
		applied(t, f)
		if store.get("animate/open_animation") != "none" || store.get("grid/type") != "crossfade" || store.get("vswitch/duration") != "300ms circle" {
			t.Fatalf("set by halves: %q", store.snapshot())
		}
		store.refuse = ""
		resetCalls(f)
		if err := b.ApplySettings(ctx, compositor.Settings{Animations: &on}); err != nil {
			t.Fatal(err)
		}
		if s := sets(f); len(s) != 1 || strings.Contains(s[0], "grid/") || strings.Contains(s[0], "vswitch/") {
			t.Fatalf("on after a refusal: %q", s)
		}
		if store.get("animate/open_animation") != "zoom" || store.get("animate/minimize_animation") != "squeezimize" {
			t.Fatalf("on after a refusal: %q", store.snapshot())
		}
	})

	t.Run("on refused", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		store := newOptionStore(t, "0.10.0-1", nil)
		f := wayfireWith(t, store)
		b := New(NewClient(f.socket))
		if err := b.ApplySettings(ctx, compositor.Settings{Animations: &off}); err != nil {
			t.Fatal(err)
		}
		kept := motionSaved(t)
		store.refuse = "animate/close_animation"
		if err := b.ApplySettings(ctx, compositor.Settings{Animations: &on}); err == nil {
			t.Fatal("a refused on was not said")
		}
		if got := motionSaved(t); !maps.Equal(got, kept) {
			t.Fatalf("a refused on dropped the record: %q", got)
		}
		store.refuse = ""
		if err := b.ApplySettings(ctx, compositor.Settings{Animations: &on}); err != nil {
			t.Fatal(err)
		}
		if store.get("animate/close_animation") != "zoom" || store.get("vswitch/duration") != "300ms circle" {
			t.Fatalf("on once taken: %q", store.snapshot())
		}
	})

	t.Run("cannot read", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		store := newOptionStore(t, "0.10.0-1", nil)
		store.getFail = "broken"
		f := wayfireWith(t, store)
		b := New(NewClient(f.socket))
		if err := b.ApplySettings(ctx, settings(&off)); err == nil || !strings.Contains(err.Error(), "broken") {
			t.Fatalf("an option that cannot be read: %v", err)
		}
		applied(t, f)
		if s := sets(f); len(s) != 1 {
			t.Fatalf("set without reading: %q", s)
		}
		if motionSaved(t) != nil {
			t.Fatal("kept without reading")
		}
	})

	t.Run("without ipc-rules", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		f := newFakeWayfire(t)
		f.mu.Lock()
		f.methods = append(slices.Clone(allMethods), "command/register-binding", "command/unregister-binding")
		f.mu.Unlock()
		f.set("command/register-binding", `{"result":"ok","binding-id":7}`)
		b := New(NewClient(f.socket))
		binds := []compositor.KeyBind{{Keys: "SUPER+comma", Command: "inua call settings toggle"}}
		err := b.ApplySettings(ctx, compositor.Settings{Animations: &off, Binds: binds})
		if !errors.Is(err, compositor.ErrUnavailable) {
			t.Fatalf("off without ipc-rules: %v", err)
		}
		registered(t)
		if err := b.ApplySettings(ctx, compositor.Settings{Animations: &on, Binds: binds}); err != nil {
			t.Fatalf("on without ipc-rules and nothing stilled: %v", err)
		}
	})
}
