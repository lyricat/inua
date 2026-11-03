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
 	"github.com/lyricat/inua/internal/desktop"
 	"github.com/lyricat/inua/internal/gtkx"
 	"github.com/lyricat/inua/internal/hyprconf"
 	"github.com/lyricat/inua/internal/i18n"
 	"github.com/lyricat/inua/internal/shell"
 	"github.com/lyricat/inua/internal/wayfireconf"
)

// The keyboard, touchpad, displays and shortcuts live in config.toml.
// applyHyprland dispatches their generated settings to the active compositor.

// compositorSettingsApply serializes compositor config writes; only the latest request runs.
type compositorSettingsApply struct {
	mu     sync.Mutex
	latest atomic.Uint64
}

// applyHyprland writes Hyprland settings or the Wayfire config, depending on
// the session. Wayfire config changes are saved for its next start.
func (s *shellUI) applyHyprland() {
	if desktop.Wayfire() {
		s.applyWayfire()
		return
	}
	c, client := s.cfg, s.client
	path := hyprconf.Target(s.hyprConfig())
	gen := s.settingsApply.latest.Add(1)
	go func() {
		s.settingsApply.mu.Lock()
		defer s.settingsApply.mu.Unlock()
		if s.settingsApply.latest.Load() != gen {
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

func (s *shellUI) applyWayfire() {
	c := s.cfg
	path := wayfireconf.Path(s.opts.ConfigPath)
	gen := s.settingsApply.latest.Add(1)
	go func() {
		s.settingsApply.mu.Lock()
		defer s.settingsApply.mu.Unlock()
		if s.settingsApply.latest.Load() != gen {
			return
		}
		changed, err := wayfireconf.Apply(path, c)
		gtkx.Post(func() {
			if err != nil {
				log.Printf("Wayfire settings: %v", err)
				s.setStatus(i18n.Tf("Not saved to Wayfire: %s", err.Error()), true)
			} else if changed {
				log.Printf("Wayfire settings: wrote %s", path)
				s.setStatus(i18n.T("Saved in wayfire.ini; restart Wayfire to apply these settings"), false)
			}
		})
	}()
}

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
	if desktop.Wayfire() {
		input, err := wayfireconf.ReadInput(wayfireconf.Path(s.opts.ConfigPath))
		if err != nil {
			log.Printf("Wayfire settings: %v", err)
		}
		if input.KbOptions != nil {
			return *input.KbOptions
		}
		return ""
	}
	return s.settings.hyprKb
}

// hyprKbOptions asks Hyprland for input:kb_options, "" if it cannot say.
func (s *shellUI) hyprKbOptions() string {
	if s.client == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	reply, err := s.client.Request(ctx, "j/getoption input:kb_options")
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
	effective := in
	if desktop.Wayfire() {
		base, err := wayfireconf.ReadInput(wayfireconf.Path(s.opts.ConfigPath))
		if err != nil {
			log.Printf("Wayfire settings: %v", err)
		}
		if effective.TouchpadSpeed == nil {
			effective.TouchpadSpeed = base.TouchpadSpeed
		}
		if effective.TouchpadAccel == "" {
			effective.TouchpadAccel = base.TouchpadAccel
		}
	}
	opts := s.kbOptions()
	st.swap.SetSelected(uint32(max(0, slices.Index(swapChoices, shell.SwapCtrlAlt(opts)))))
	st.caps.SetSelected(uint32(max(0, slices.Index(capsChoices, shell.CapsLock(opts)))))
	if in.KbOptions != nil {
		st.kbState.SetText(i18n.T("Set by inua"))
	} else if desktop.Wayfire() {
		st.kbState.SetText(i18n.T("Set in wayfire.ini"))
	} else {
		st.kbState.SetText(i18n.T("Set in hyprland.conf"))
	}
	st.kbReadout.SetText(cmp.Or(opts, i18n.T("no XKB options")))
	st.kbReset.SetSensitive(in.KbOptions != nil)

	speed := 0.0
	if effective.TouchpadSpeed != nil {
		speed = *effective.TouchpadSpeed
		st.tpValue.SetText(fmt.Sprintf("%+.2f", speed))
	} else if desktop.Wayfire() {
		st.tpValue.SetText(i18n.T("From wayfire.ini"))
	} else {
		st.tpValue.SetText(i18n.T("From hyprland.conf"))
	}
	st.tpSpeed.SetValue(speed)
	st.tpAccel.SetActive(effective.TouchpadAccel != "flat")
	if in.TouchpadSpeed != nil || in.TouchpadAccel != "" {
		st.tpState.SetText(i18n.T("Set by inua"))
	} else if desktop.Wayfire() {
		st.tpState.SetText(i18n.T("Set in wayfire.ini"))
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
	text := ""
	if desktop.Wayfire() {
		text = i18n.T("Wayfire settings use wayfire.ini. Inua supports XKB options, touchpad speed and acceleration, output modes, positions and scales, and recorded command shortcuts. Wayfire does not report alternate output modes, so the resolution menu only offers the current mode and preferred; configure other values in config.toml. Wayfire requires connector names; Hyprland desc: output identifiers do not apply. Inua has no verified Wayfire equivalent for display enable/disable, mirroring, automatic relative positions, or lid-triggered output switching. Existing Wayfire shortcuts cannot be listed here.")
	} else {
		conf := s.hyprConfig()
		_, overrides, err := bootstrap.FindSource(conf, hyprconf.Target(conf))
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
	}
	for _, l := range []*gtk.Label{&st.kbWarn, &st.tpWarn, &st.sc.warn, &st.displays.warn} {
		l.SetText(text)
		l.SetVisible(text != "")
	}
}
