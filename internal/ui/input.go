package ui

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/bootstrap"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/compsettings"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hyprconf"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
)

// The keyboard and touchpad settings live in config.toml like the rest,
// but Hyprland applies them: applyHyprland writes them to hypr/inua.conf,
// which hyprland.conf sources, and Hyprland reloads.

// hyprApply serializes writing inua.conf; only the latest request runs.
type hyprApply struct {
	mu     sync.Mutex
	latest atomic.Uint64
}

// applyHyprland writes inua.conf from the input settings in the
// background, and reloads Hyprland if the file changed.
func (s *shellUI) applyHyprland() {
	if s.comp != nil && s.hypr == nil {
		// Another compositor takes the same settings its own way;
		// inua.conf is Hyprland's.
		if cf := s.configurer(); cf != nil {
			s.applyBackendSettings(cf)
		}
		return
	}
	c, client := s.cfg, s.hyprClient()
	path := hyprconf.Target(s.hyprConfig())
	gen := s.hyprApply.latest.Add(1)
	go func() {
		s.hyprApply.mu.Lock()
		defer s.hyprApply.mu.Unlock()
		if s.hyprApply.latest.Load() != gen {
			return // a newer request follows
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		changed, err := hyprconf.Apply(ctx, c, path, client)
		gtkx.Post(func() {
			switch {
			case err != nil:
				log.Printf("hyprland settings: %v", err)
				s.setStatus(i18n.Tf("Not applied to Hyprland: %s", err.Error()), true)
			case changed:
				log.Printf("hyprland settings: wrote %s", path)
			}
		})
	}()
}

// backendSettings are inua's settings as a compositor backend takes them.
func (s *shellUI) backendSettings() compositor.Settings { return compsettings.From(s.cfg) }

// refreshHyprInput rereads what Hyprland has in effect after it reloads
// its config, for the settings window.
func (s *shellUI) refreshHyprInput() {
	if s.settings.win == nil {
		return
	}
	s.settings.hyprKb = s.hyprKbOptions()
	s.settings.sc.binds = s.hyprBinds()
	s.showSourceWarning()
	s.showSettings()
}

// kbOptions is the kb_options the keyboard controls change: inua's, or
// Hyprland's while inua sets none.
func (s *shellUI) kbOptions() string {
	if o := s.cfg.Input.KbOptions; o != nil {
		return *o
	}
	return s.settings.hyprKb
}

// hyprKbOptions asks Hyprland for input:kb_options, "" if it cannot say.
func (s *shellUI) hyprKbOptions() string {
	if s.hypr == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	reply, err := s.hypr.Client.Request(ctx, "j/getoption input:kb_options")
	if err != nil {
		log.Printf("hyprland: kb_options: %v", err)
		return ""
	}
	var opt struct {
		Str string `json:"str"`
	}
	if err := json.Unmarshal(reply, &opt); err != nil {
		log.Printf("hyprland: kb_options: %v", err)
	}
	return opt.Str
}

// showInput sets the keyboard and touchpad controls.
func (s *shellUI) showInput() {
	st := &s.settings
	in := s.cfg.Input
	opts := s.kbOptions()
	st.swap.SetSelected(uint32(max(0, slices.Index(swapChoices, shell.SwapCtrlAlt(opts)))))
	st.caps.SetSelected(uint32(max(0, slices.Index(capsChoices, shell.CapsLock(opts)))))
	if in.KbOptions != nil {
		st.kbState.SetText(i18n.T("Set by inua"))
	} else {
		st.kbState.SetText(i18n.T("Set in hyprland.conf"))
	}
	st.kbReadout.SetText(cmp.Or(opts, i18n.T("no XKB options")))
	st.kbReset.SetSensitive(in.KbOptions != nil)

	speed := 0.0
	if in.TouchpadSpeed != nil {
		speed = *in.TouchpadSpeed
		st.tpValue.SetText(fmt.Sprintf("%+.2f", speed))
	} else {
		st.tpValue.SetText(i18n.T("From hyprland.conf"))
	}
	st.tpSpeed.SetValue(speed)
	st.tpAccel.SetActive(in.TouchpadAccel != "flat")
	if in.TouchpadSpeed != nil || in.TouchpadAccel != "" {
		st.tpState.SetText(i18n.T("Set by inua"))
	} else {
		st.tpState.SetText(i18n.T("Set in hyprland.conf"))
	}
	st.tpReset.SetSensitive(in.TouchpadSpeed != nil || in.TouchpadAccel != "")
}

// hyprConfig is the config Hyprland reads: hyprland.lua when it runs Lua
// and the file is there, else hyprland.conf.
func (s *shellUI) hyprConfig() string {
	return hyprconf.ConfigFile(filepath.Dir(hyprconf.Path(s.opts.ConfigPath)), s.hyprLua)
}

// showSourceWarning explains on the keyboard and touchpad pages when
// Hyprland's config does not load inua's file (inua.conf, or inua.lua for
// hyprland.lua), or overrides it.
func (s *shellUI) showSourceWarning() {
	st := &s.settings
	if s.comp != nil && s.hypr == nil {
		// Another compositor: each page's options are there when the
		// compositor takes them, and disabled with the reason under them
		// when it does not. What it takes can change under us (a line
		// added to its config, a plugin loaded).
		s.setCaps(s.comp.Capabilities())
		for _, g := range []struct {
			warn *gtk.Label
			c    compositor.Capability
		}{{&st.kbWarn, compositor.CapKeyboard}, {&st.tpWarn, compositor.CapTouchpad}, {&st.sc.warn, compositor.CapBinds}} {
			s.gatePage(g.warn, s.capabilityHint(g.c))
		}
		st.displays.warn.SetVisible(false)
		s.showBackendLabels()
		s.showCompositor()
		return
	}
	conf := s.hyprConfig()
	_, overrides, err := bootstrap.FindSource(conf, hyprconf.Target(conf))
	text := ""
	switch {
	case errors.Is(err, bootstrap.ErrNoSource) && strings.HasSuffix(conf, ".lua"):
		text = i18n.Tf("hyprland.lua does not load inua.lua, so these settings do nothing yet. Add this as its last line: %s", hyprconf.LuaRequire)
	case errors.Is(err, bootstrap.ErrNoSource):
		text = i18n.Tf("hyprland.conf does not load inua.conf, so these settings do nothing yet. Add this as its last line: source = ./%s", hyprconf.Name)
	case err != nil:
		text = err.Error()
	case len(overrides) > 0 && strings.HasSuffix(conf, ".lua"):
		text = i18n.Tf("hyprland.lua sets these again after loading inua.lua, which wins over inua: %s. Move the require line below them.", strings.Join(overrides, ", "))
	case len(overrides) > 0:
		text = i18n.Tf("hyprland.conf sets these again after loading inua.conf, which wins over inua: %s. Move the source line below them.", strings.Join(overrides, ", "))
	}
	for _, l := range []*gtk.Label{&st.kbWarn, &st.tpWarn, &st.sc.warn, &st.displays.warn} {
		l.SetText(text)
		l.SetVisible(text != "")
	}
}
