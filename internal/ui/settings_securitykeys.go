package ui

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/securitykey"
)

// The Security Keys page (Settings → Devices → Security Keys), for each
// FIDO2 security key plugged in (docs/security-key.md):
//
//	■ [SECURITY KEY]   Yubico YubiKey OTP+FIDO+CCID
//	PIN        Set · 8 tries left                     [Change PIN]
//	Unlocks    Ada Lovelace <ada@example.org> · added Nov 8, 2026
//	           …or none                                [Open Pass]
//	Firmware   5.8.0
//
// Setting or changing the PIN opens its fields under the PIN's row. The
// page reads the keys through libfido2's tools when it is shown, and
// looks again every few seconds while it is, for a key plugged in or out.
// Which gpg keys it unlocks are Pass's enrolments (FT83) whose
// credential the key knows, asked without a PIN or a touch.

// secKeyPollMS is how often the page looks for keys plugged in or out.
const secKeyPollMS = 3000

type securityKeySettings struct {
	page    gtk.Box
	read    secKeyRead // what was last read
	gen     int        // the newest reading
	reading bool
	polling bool
	looking bool // a poll's listing runs

	// The PIN's fields, open for the key at path form.
	form            string
	busy            bool
	current, pin    *gtk.PasswordEntry
	again           *gtk.PasswordEntry
	formErr, status *gtk.Label // the form's error; the last change's word
	done            string     // the key whose PIN was just changed

	onButton   func(gtk.Button)
	onActivate func(gtk.PasswordEntry)
	onPoll     glib.SourceFunc
}

// secKeyRead is what the page shows.
type secKeyRead struct {
	tools   bool
	err     error
	devices []secKeyDevice
}

type secKeyDevice struct {
	dev   securitykey.Device
	info  securitykey.Info
	err   error
	holds []secKeyHold
}

// secKeyHold is a gpg key a security key unlocks.
type secKeyHold struct {
	name  string // its user ID, or its fingerprint
	added time.Time
}

// getSecurityKeySettings gets the page's box and connects what it makes.
func (s *shellUI) getSecurityKeySettings(ui *gtkx.Builder) {
	k := &s.settings.securityKeys
	ui.Get("securitykeys_page", &k.page)
	k.read, k.form, k.busy, k.done = secKeyRead{}, "", false, ""
	k.current, k.pin, k.again, k.formErr, k.status = nil, nil, nil, nil, nil
	k.onButton = func(b gtk.Button) {
		name := b.GetName()
		gtkx.Post(func() { s.secKeyButton(name) })
	}
	k.onActivate = func(gtk.PasswordEntry) { gtkx.Post(s.saveSecKeyPIN) }
	k.onPoll = func(uintptr) bool { return s.pollSecurityKeys() }
}

// securityKeysShown is whether the page is in sight.
func (s *shellUI) securityKeysShown() bool {
	st := &s.settings
	return st.win != nil && st.pages.GetVisibleChildName() == "securitykeys"
}

// showSecurityKeysPage reads the keys as the page comes into sight, and
// starts looking for keys plugged in or out.
func (s *shellUI) showSecurityKeysPage() {
	k := &s.settings.securityKeys
	s.readSecurityKeys()
	if !k.polling {
		k.polling = true
		glib.TimeoutAdd(secKeyPollMS, &k.onPoll, 0)
	}
}

// pollSecurityKeys lists the keys plugged in, off the main thread, and
// reads them again when they changed; it stops with the page out of
// sight. An open PIN form is kept unless its key went.
func (s *shellUI) pollSecurityKeys() bool {
	k := &s.settings.securityKeys
	if !s.securityKeysShown() {
		k.polling = false
		return false
	}
	if k.looking || k.reading || k.busy || !k.read.tools {
		return true
	}
	k.looking = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		devs, err := securitykey.Devices(ctx)
		gtkx.Post(func() {
			k.looking = false
			if err != nil {
				return
			}
			var had []securitykey.Device
			for _, d := range k.read.devices {
				had = append(had, d.dev)
			}
			if slices.Equal(devs, had) {
				return
			}
			if k.form != "" && slices.ContainsFunc(devs, func(d securitykey.Device) bool { return d.Path == k.form }) {
				return // the next change when the form closes
			}
			s.readSecurityKeys()
		})
	}()
	return true
}

// readSecurityKeys reads, off the main thread, each key plugged in, what
// it can do, and which enrolled gpg keys it unlocks; then shows them.
func (s *shellUI) readSecurityKeys() {
	k := &s.settings.securityKeys
	k.gen++
	gen := k.gen
	k.reading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		r := secKeyRead{tools: securitykey.Available()}
		if r.tools {
			var devs []securitykey.Device
			devs, r.err = securitykey.Devices(ctx)
			enrolled, _ := securitykey.Load(securitykey.Path())
			for _, d := range devs {
				v := secKeyDevice{dev: d}
				v.info, v.err = securitykey.DeviceInfo(ctx, d)
				for _, e := range securitykey.Holds(ctx, d, enrolled) {
					h := secKeyHold{name: pinentry.Grouped(e.Fingerprint), added: e.Added}
					if len(e.Grips) > 0 {
						if key, ok := pinentry.LookupKey(e.Grips[0]); ok {
							h.name = keyUserID(key, h.name)
						}
					}
					v.holds = append(v.holds, h)
				}
				r.devices = append(r.devices, v)
			}
		}
		gtkx.Post(func() {
			if k.gen != gen {
				return
			}
			k.reading = false
			k.read = r
			if k.form != "" && !slices.ContainsFunc(r.devices, func(d secKeyDevice) bool { return d.dev.Path == k.form }) {
				k.form = "" // its key went
			}
			s.fillSecurityKeys()
		})
	}()
}

// keyUserID writes a key's user ID as gpg would, or def without one.
func keyUserID(k pinentry.Key, def string) string {
	id := k.Name
	if k.Comment != "" {
		id += " (" + k.Comment + ")"
	}
	if k.Email != "" {
		id = strings.TrimSpace(id + " <" + k.Email + ">")
	}
	if id == "" {
		return def
	}
	return id
}

// fillSecurityKeys lays the page out from what was read.
func (s *shellUI) fillSecurityKeys() {
	k := &s.settings.securityKeys
	gtkx.Clear(&k.page)
	k.current, k.pin, k.again, k.formErr, k.status = nil, nil, nil, nil, nil
	note := func(body *gtk.Box, text string) {
		row, _ := displayRow(text, "")
		gtkx.Append(body, &row.Widget)
	}
	r := k.read
	switch {
	case !r.tools:
		sec, body := displaySection(i18n.T("SECURITY KEY"), "")
		note(body, i18n.T("Install libfido2's tools (fido2-tools) to manage a security key here."))
		gtkx.Append(&k.page, &sec.Widget)
		return
	case r.err != nil:
		sec, body := displaySection(i18n.T("SECURITY KEY"), "")
		note(body, i18n.Tf("The security keys could not be listed: %v", r.err))
		gtkx.Append(&k.page, &sec.Widget)
		return
	case len(r.devices) == 0:
		sec, body := displaySection(i18n.T("SECURITY KEY"), i18n.T("FIDO2, such as a YubiKey"))
		note(body, i18n.T("Plug in a security key."))
		gtkx.Append(&k.page, &sec.Widget)
		return
	}
	for _, d := range r.devices {
		gtkx.Append(&k.page, &s.secKeySection(d).Widget)
	}
}

// secKeySection is one key's section.
func (s *shellUI) secKeySection(d secKeyDevice) *gtk.Box {
	k := &s.settings.securityKeys
	sec, body := displaySection(i18n.T("SECURITY KEY"), cmp.Or(d.dev.Name, d.dev.Path))
	sep := func() {
		if body.GetFirstChild() != nil {
			gtkx.Append(body, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		}
	}
	button := func(row *gtk.Box, label, name string, suggested bool) *gtk.Button {
		b := gtk.NewButtonWithLabel(label)
		b.AddCssClass("outlined")
		if suggested {
			b.RemoveCssClass("outlined")
			b.AddCssClass("suggested-action")
		}
		b.SetValign(gtk.AlignCenterValue)
		b.SetName(name)
		b.ConnectClicked(&k.onButton)
		gtkx.Append(row, &b.Widget)
		return b
	}
	if d.err != nil {
		row, _ := displayRow(i18n.T("It could not be read"), d.err.Error())
		gtkx.Append(body, &row.Widget)
		return sec
	}

	// The PIN: whether it is set, the tries left, and a way to set or
	// change it.
	pinState := i18n.T("Not set: unlocking a gpg key with it needs one")
	if d.info.PIN {
		pinState = i18n.T("Set")
		if d.info.Retries >= 0 {
			pinState = i18n.Nf("Set · %d try left before it locks", "Set · %d tries left before it locks", d.info.Retries, d.info.Retries)
		}
		if d.info.Retries == 0 {
			pinState = i18n.T("Blocked: too many wrong tries. Only a reset of the key unlocks it, which erases its credentials.")
		}
	}
	row, _ := displayRow(i18n.T("PIN"), pinState)
	if k.form != d.dev.Path && d.info.Retries != 0 {
		label := i18n.T("Set PIN")
		if d.info.PIN {
			label = i18n.T("Change PIN")
		}
		button(row, label, "seckey-pin:"+d.dev.Path, false)
	}
	gtkx.Append(body, &row.Widget)
	if k.done == d.dev.Path {
		k.status = gtk.NewLabel(i18n.T("The PIN was changed. The gpg keys it unlocks still open with it."))
		k.status.AddCssClass("sub")
		k.status.SetXalign(0)
		k.status.SetWrap(true)
		gtkx.Append(body, &k.status.Widget)
	}
	if k.form == d.dev.Path {
		field := func(title string) *gtk.PasswordEntry {
			sep()
			row, _ := displayRow(title, "")
			row.AddCssClass("compact")
			e := gtk.NewPasswordEntry()
			e.SetShowPeekIcon(true)
			e.SetValign(gtk.AlignCenterValue)
			e.SetSizeRequest(240, -1)
			e.ConnectActivate(&k.onActivate)
			gtkx.Append(row, &e.Widget)
			gtkx.Append(body, &row.Widget)
			return e
		}
		if d.info.PIN {
			k.current = field(i18n.T("Current PIN"))
		}
		k.pin = field(i18n.T("New PIN"))
		k.again = field(i18n.T("New PIN again"))
		sep()
		row, _ := displayRow(i18n.Tf("From %d to 63 characters", max(d.info.MinPIN, 4)), "")
		row.AddCssClass("compact")
		button(row, i18n.T("Cancel"), "seckey-pin-cancel", false)
		save := button(row, i18n.T("Save"), "seckey-pin-save", true)
		save.SetSensitive(!k.busy)
		gtkx.Append(body, &row.Widget)
		k.formErr = gtk.NewLabel("")
		k.formErr.AddCssClass("row-error")
		k.formErr.AddCssClass("flush")
		k.formErr.SetXalign(0)
		k.formErr.SetWrap(true)
		k.formErr.SetVisible(false)
		gtkx.Append(body, &k.formErr.Widget)
	}

	// The gpg keys it unlocks, from Pass's enrolments.
	sep()
	if len(d.holds) == 0 {
		row, _ := displayRow(i18n.T("Unlocks no gpg key"), i18n.T("Add it to a password store's key in Pass: File → Security keys"))
		button(row, i18n.T("Open Pass"), "seckey-pass", false)
		gtkx.Append(body, &row.Widget)
	}
	for i, h := range d.holds {
		if i > 0 {
			sep()
		}
		row, _ := displayRow(i18n.Tf("Unlocks %s", h.name), i18n.Tf("Added in Pass on %s", i18n.Time(h.added.Local(), "Jan 2, 2006")))
		if i == len(d.holds)-1 {
			button(row, i18n.T("Open Pass"), "seckey-pass", false)
		}
		gtkx.Append(body, &row.Widget)
	}
	if d.info.Firmware != "" {
		sep()
		row, _ := displayRow(i18n.T("Firmware"), d.info.Firmware)
		gtkx.Append(body, &row.Widget)
	}
	return sec
}

// secKeyButton answers the page's buttons.
func (s *shellUI) secKeyButton(name string) {
	k := &s.settings.securityKeys
	kind, arg, _ := strings.Cut(name, ":")
	switch kind {
	case "seckey-pin":
		if k.busy {
			return
		}
		k.form, k.done = arg, ""
		s.fillSecurityKeys()
		if k.current != nil {
			k.current.GrabFocus()
		} else if k.pin != nil {
			k.pin.GrabFocus()
		}
	case "seckey-pin-cancel":
		if !k.busy {
			k.form = ""
			s.fillSecurityKeys()
		}
	case "seckey-pin-save":
		s.saveSecKeyPIN()
	case "seckey-pass":
		s.openLightApp(passApp, "pass")
	}
}

// passApp is Pass's desktop entry.
const passApp = "io.github.lyricat.Inua.Pass"

// secKeyFormError shows why the PIN was not changed, in the form.
func (s *shellUI) secKeyFormError(text string) {
	k := &s.settings.securityKeys
	if k.formErr != nil {
		k.formErr.SetText(text)
		k.formErr.SetVisible(text != "")
	}
}

// saveSecKeyPIN sets or changes the PIN of the key whose form is open,
// off the main thread.
func (s *shellUI) saveSecKeyPIN() {
	k := &s.settings.securityKeys
	if k.busy || k.form == "" || k.pin == nil {
		return
	}
	i := slices.IndexFunc(k.read.devices, func(d secKeyDevice) bool { return d.dev.Path == k.form })
	if i < 0 {
		return
	}
	d := k.read.devices[i]
	old, pin, again := "", k.pin.GetText(), k.again.GetText()
	if k.current != nil {
		old = k.current.GetText()
		if old == "" {
			s.secKeyFormError(i18n.T("Type the current PIN."))
			k.current.GrabFocus()
			return
		}
	}
	min := max(d.info.MinPIN, 4)
	switch {
	case securitykey.CheckPIN(pin, min) != nil:
		s.secKeyFormError(i18n.Tf("A PIN has %d to 63 characters, and is on one line.", min))
		k.pin.GrabFocus()
		return
	case pin != again:
		s.secKeyFormError(i18n.T("The new PINs do not match."))
		k.again.SetText("")
		k.again.GrabFocus()
		return
	}
	k.busy = true
	s.secKeyFormError("")
	for _, e := range []*gtk.PasswordEntry{k.current, k.pin, k.again} {
		if e != nil {
			e.SetSensitive(false)
		}
	}
	gen := k.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var err error
		if d.info.PIN {
			err = securitykey.ChangePIN(ctx, d.dev, old, pin)
		} else {
			err = securitykey.SetPIN(ctx, d.dev, pin)
		}
		var info securitykey.Info
		if errors.Is(err, securitykey.ErrPIN) {
			info, _ = securitykey.DeviceInfo(ctx, d.dev)
		}
		gtkx.Post(func() {
			k.busy = false
			if k.gen != gen && k.form == "" {
				return // the page was laid out again without the form
			}
			if err == nil {
				k.form, k.done = "", d.dev.Path
				s.readSecurityKeys()
				return
			}
			for _, e := range []*gtk.PasswordEntry{k.current, k.pin, k.again} {
				if e != nil {
					e.SetSensitive(true)
				}
			}
			switch {
			case errors.Is(err, securitykey.ErrPIN):
				text := i18n.T("The current PIN is wrong.")
				if info.Retries > 0 {
					text = i18n.Nf("The current PIN is wrong. %d try left before the key locks.", "The current PIN is wrong. %d tries left before the key locks.", info.Retries, info.Retries)
				}
				s.secKeyFormError(text)
				k.current.SetText("")
				k.current.GrabFocus()
			case errors.Is(err, securitykey.ErrPINPolicy):
				s.secKeyFormError(i18n.T("The key does not take this PIN: it is too short, too long, or too simple."))
				k.pin.GrabFocus()
			case errors.Is(err, securitykey.ErrPINBlocked):
				s.secKeyFormError(i18n.T("The PIN is blocked: too many wrong tries. Unplug the key and plug it in again; after that, only a reset unlocks it."))
			default:
				s.secKeyFormError(i18n.Tf("The PIN was not changed: %v", err))
			}
		})
	}()
}
