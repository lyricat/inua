package ui

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

type services struct {
	net       *system.Network
	bt        *system.Bluetooth
	prof      *system.PowerProfiles
	media     *system.Media
	audio     *system.Audio
	backlight *system.Backlight // nil without a panel backlight
	ts        *system.Tailscale
	voice     *system.Voice

	volume     *shell.Setter
	brightness *shell.Setter
}

// startSystem runs the services; their updates reach the bars and the
// control center on the main thread.
func (s *shellUI) startSystem() {
	ctx := context.Background()
	v := &s.svc
	v.net = system.NewNetwork(system.SystemBus)
	v.bt = system.NewBluetooth(system.SystemBus)
	v.prof = system.NewPowerProfiles(system.SystemBus)
	v.media = system.NewMedia(system.SessionBus)
	v.audio = system.NewAudio()
	v.ts = system.NewTailscale("")
	v.voice = system.NewVoice("", "")
	v.volume = shell.NewSetter(func(f float64) { logErr("volume", v.audio.SetVolume(f)) })
	if b, err := system.FindBacklight(system.BacklightDir, system.SystemBus); err == nil {
		v.backlight = b
		v.brightness = shell.NewSetter(func(f float64) { logErr("brightness", b.SetPercent(f)) })
		go func() {
			logErr("backlight", b.Watch(ctx, func(p float64) { gtkx.Post(func() { s.showBrightness(p) }) }))
		}()
	}

	go v.net.Run(ctx, func(st system.NetworkState) { s.update(func(x *shell.System) { x.Net = st }) })
	v.bt.SetAsker(s.askBluetooth)
	go v.bt.Run(ctx, func(st system.BluetoothState) { s.update(func(x *shell.System) { x.BT = st }) })
	go v.prof.Run(ctx, func(st system.PowerProfileState) { s.update(func(x *shell.System) { x.Prof = st }) })
	go v.media.Run(ctx, func(st system.MediaState) { s.update(func(x *shell.System) { x.Media = st }) })
	go v.audio.Run(ctx, func(st system.AudioState) { s.update(func(x *shell.System) { x.Audio = st }) })
	go v.voice.Run(ctx, func(st system.VoiceState) { s.update(func(x *shell.System) { x.Voice = st }) })
	go system.NewBattery(system.SystemBus).Run(ctx, func(st system.BatteryState) { s.update(func(x *shell.System) { x.Bat = st }) })
}

func (s *shellUI) update(f func(*shell.System)) {
	gtkx.Post(func() {
		audio := s.sys.Audio
		f(&s.sys)
		if shell.VolumeChanged(audio, s.sys.Audio) {
			s.showVolume(s.sys.Audio)
		}
		for _, b := range s.bars {
			b.setStatus(&s.sys)
		}
		s.applyVoiceState()
		if s.control.open {
			s.applyControl()
		}
		s.showBluetooth()
		s.showWifi()
		s.showPower()      // the battery, the power mode
		s.checkOSD(&s.sys) // Wi-Fi and Bluetooth, as a card
		s.showVoice()
		if s.wantWifi() {
			s.readWifiList() // connected, or off, meanwhile
		}
	})
}

func logErr(what string, err error) {
	if err != nil {
		log.Printf("%s: %v", what, err)
	}
}

// volumeStep is one notch of the scroll on the bar's status button, the
// volume keys' 5%.
const volumeStep = 0.05

// stepVolume lowers (n > 0) or raises the output volume n notches, as the
// volume keys do; setting also unmutes. A burst of notches builds on the
// values it asked for, not on the service's state, which lags behind.
func (s *shellUI) stepVolume(n int) {
	a := s.sys.Audio
	if !a.Available {
		return
	}
	base := a.Volume
	if v, held := s.svc.volume.Held(); held {
		base = v
	}
	v := max(0, min(base-float64(n)*volumeStep, 1))
	s.svc.volume.Set(v)
	if c := &s.control; c.open { // held, the slider no longer follows the service
		c.volume.SetValue(v * 100)
		c.volValue.SetText(shell.Percent(v))
	}
}

// tailscaleApp is the Tailscale light app's desktop entry.
const tailscaleApp = "io.github.lyricat.Inua.Tailscale"

// readTailscale reads Tailscale's state for the control center.
func (s *shellUI) readTailscale() {
	ts := s.svc.ts
	if ts == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		st := ts.Read(ctx)
		cancel()
		s.update(func(x *shell.System) { x.TS = st })
	}()
}

// toggleTailscale connects or disconnects Tailscale from its tile; when
// something is in the way (logged out, the daemon stopped, not allowed to
// change it yet) it opens the app, which says what and offers the fix.
func (s *shellUI) toggleTailscale() {
	_, on, toggles := shell.TailscaleTile(s.sys.TS)
	if !toggles {
		s.closeControl()
		s.openLightApp(tailscaleApp, "tailscale")
		return
	}
	ts := s.svc.ts
	// Show the change now; the next read corrects a failure.
	if on {
		s.sys.TS.Backend = "Stopped"
	} else {
		s.sys.TS.Backend = "Starting"
	}
	s.applyControl()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := ts.SetRunning(ctx, !on)
		cancel()
		gtkx.Post(func() {
			if errors.Is(err, tailscale.ErrPermission) {
				log.Printf("tailscale: not allowed to change it yet; opening the app")
				s.closeControl()
				s.openLightApp(tailscaleApp, "tailscale")
			} else {
				logErr("tailscale", err)
			}
			s.readTailscale()
		})
	}()
}
