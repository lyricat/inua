package lightui

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/securitykey"
)

// Security keys (docs/security-key.md): a dialog lists the keys
// the store encrypts to whose secret parts are here, the security keys
// enrolled for each, and adds or removes one. inua-pinentry then unlocks
// the key with the security key.

// enrolTime bounds an enrolment: the passphrase checked and two touches.
const enrolTime = 3 * time.Minute

// passSecKey is the dialog's widgets and state.
type passSecKey struct {
	win               gtk.Window
	keys, form        gtk.Box
	formTitle, status gtk.Label
	err               gtk.Label
	passphrase, pin   gtk.PasswordEntry
	add               gtk.Button
	open, busy        bool
	tools             bool                    // libfido2's tools are installed
	list              []securitykey.Key       // the store's keys with secret parts here
	enrolled          []securitykey.Enrolment // all the enrolments, as listed
	target            string                  // the key a security key is being added to
	gen               int                     // drops a listing that comes after another began
	onChanged         func(uintptr, uintptr)
	onActivate        func(gtk.PasswordEntry)
	onKey             func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onClose           func(gtk.Window) bool
}

// seckeyWidgets are the dialog's widgets in pass.ui, for build to get.
func (a *passApp) seckeyWidgets() map[string]gobject.Ptr {
	s := &a.seckey
	return map[string]gobject.Ptr{
		"seckey_dialog": &s.win, "seckey_keys": &s.keys, "seckey_form": &s.form,
		"seckey_form_title": &s.formTitle, "seckey_status": &s.status, "seckey_error": &s.err,
		"seckey_passphrase": &s.passphrase, "seckey_pin": &s.pin, "seckey_add": &s.add,
	}
}

// bindSecKey connects the dialog, once its widgets are got.
func (a *passApp) bindSecKey(close *gtk.Button) {
	s := &a.seckey
	s.onChanged = func(uintptr, uintptr) { a.checkSecKey() }
	s.onActivate = func(gtk.PasswordEntry) { a.addSecKey() }
	s.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == 0xff1b && !s.busy { // Escape
			a.closeSecKey()
			return true
		}
		return false
	}
	s.onClose = func(gtk.Window) bool {
		if !s.busy {
			a.closeSecKey()
		}
		return true // hidden, kept for the next time
	}
	for _, e := range []*gtk.PasswordEntry{&s.passphrase, &s.pin} {
		gtkx.ConnectChanged(e, &s.onChanged)
		e.ConnectActivate(&s.onActivate)
	}
	for _, b := range []*gtk.Button{&s.add, close} {
		b.ConnectClicked(&a.onButton)
	}
	s.win.SetApplication(a.c.App)
	s.win.SetTitle(i18n.T("Pass"))
	s.win.SetTransientFor(&a.win.Window)
	s.win.ConnectCloseRequest(&s.onClose)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&s.onKey)
	s.win.AddController(&keys.EventController)
}

// seckeyButton answers the dialog's buttons; false for another button.
func (a *passApp) seckeyButton(name string) bool {
	s := &a.seckey
	kind, arg, _ := strings.Cut(name, ":")
	switch kind {
	case "seckey-close":
		if !s.busy {
			a.closeSecKey()
		}
	case "seckey-add":
		a.addSecKey()
	case "seckey-new":
		if !s.busy {
			a.startSecKey(arg)
		}
	case "seckey-remove":
		if i, err := strconv.Atoi(arg); err == nil && !s.busy && i < len(s.enrolled) {
			a.removeSecKey(s.enrolled[i])
		}
	default:
		return false
	}
	return true
}

// openSecKey shows the dialog, listing the store's keys afresh.
func (a *passApp) openSecKey() {
	s := &a.seckey
	s.open = true
	a.resetSecKeyForm()
	a.listSecKeys()
	s.win.Present()
}

func (a *passApp) closeSecKey() {
	s := &a.seckey
	s.open = false
	a.resetSecKeyForm()
	s.win.SetVisible(false)
}

// resetSecKeyForm puts the adding away, its secrets wiped.
func (a *passApp) resetSecKeyForm() {
	s := &a.seckey
	s.target = ""
	s.passphrase.SetText("")
	s.pin.SetText("")
	s.form.SetVisible(false)
	s.add.SetVisible(false)
	a.secKeyStatus("", false)
}

// secKeyStatus shows a line of progress, or an error.
func (a *passApp) secKeyStatus(text string, bad bool) {
	s := &a.seckey
	s.status.SetText(text)
	s.status.SetVisible(text != "" && !bad)
	s.err.SetText(text)
	s.err.SetVisible(text != "" && bad)
}

// storeIDs are the keys the store encrypts to, as its .gpg-id names them.
func storeIDs(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, ".gpg-id"))
	if err != nil {
		return nil
	}
	var ids []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			ids = append(ids, line)
		}
	}
	return ids
}

// listSecKeys reads the store's keys and the enrolments, off the main
// thread, and lists them.
func (a *passApp) listSecKeys() {
	s := &a.seckey
	s.gen++
	gen, dir := s.gen, a.store.Dir
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tools := securitykey.Available()
		keys, kerr := securitykey.SecretKeys(ctx, storeIDs(dir))
		enrolled, eerr := securitykey.Load(securitykey.Path())
		gtkx.Post(func() {
			if s.gen != gen || !s.open {
				return
			}
			s.tools, s.list, s.enrolled = tools, keys, enrolled
			a.showSecKeys(errors.Join(kerr, eerr))
		})
	}()
}

// showSecKeys fills the list: for each key, the security keys that unlock
// it and a button to add one.
func (a *passApp) showSecKeys(err error) {
	s := &a.seckey
	gtkx.Clear(&s.keys)
	note := func(text string) { gtkx.Append(&s.keys, &passNote(text).Widget) }
	switch {
	case !s.tools:
		note(i18n.T("Install libfido2's tools (fido2-tools) to use a security key."))
		return
	case err != nil:
		note(i18n.Tf("The keys could not be read: %v", err))
		return
	case len(s.list) == 0:
		note(i18n.T("None of the keys this store encrypts to has its secret part on this computer."))
		return
	}
	for _, k := range s.list {
		mine := securitykey.ForKey(s.enrolled, k.Fingerprint)
		head := passSection(i18n.T("SECURITY KEYS"), cmp.Or(k.UserID, pinentry.Grouped(k.Fingerprint)))
		head.SetTooltipText(pinentry.Grouped(k.Fingerprint))
		gtkx.Append(&s.keys, &head.Widget)
		if len(mine) == 0 {
			note(i18n.T("None: the passphrase is typed."))
		}
		for i, e := range s.enrolled {
			if e.Fingerprint != k.Fingerprint {
				continue
			}
			device := cmp.Or(e.Device, i18n.T("Security key"))
			row := passInfoRow(device, i18n.Tf("Added %s", i18n.Time(e.Added.Local(), "Jan 2, 2006")))
			rm := gtk.NewButtonWithLabel(i18n.T("Remove"))
			rm.AddCssClass("outlined")
			rm.SetValign(gtk.AlignCenterValue)
			rm.SetName("seckey-remove:" + strconv.Itoa(i))
			rm.SetTooltipText(i18n.T("It no longer unlocks the key; the passphrase is typed again"))
			rm.ConnectClicked(&a.onButton)
			gtkx.Append(row, &rm.Widget)
			gtkx.Append(&s.keys, &row.Widget)
		}
		add := gtk.NewButtonWithLabel(i18n.T("Add a security key"))
		add.AddCssClass("outlined")
		add.SetHalign(gtk.AlignStartValue)
		add.SetMarginTop(10)
		add.SetName("seckey-new:" + k.Fingerprint)
		add.ConnectClicked(&a.onButton)
		gtkx.Append(&s.keys, &add.Widget)
	}
}

// startSecKey opens the adding for the key fpr.
func (a *passApp) startSecKey(fpr string) {
	s := &a.seckey
	for _, k := range s.list {
		if k.Fingerprint != fpr {
			continue
		}
		s.target = fpr
		s.formTitle.SetText(i18n.Tf("Add a security key to %s. Type the key's passphrase, which is kept sealed under the security key, and the security key's PIN; then touch the security key twice when it blinks.", cmp.Or(k.UserID, pinentry.Grouped(fpr))))
		s.form.SetVisible(true)
		s.add.SetVisible(true)
		a.secKeyStatus("", false)
		a.checkSecKey()
		s.passphrase.GrabFocus()
		return
	}
}

// checkSecKey lets Add go once both are typed.
func (a *passApp) checkSecKey() {
	s := &a.seckey
	s.add.SetSensitive(!s.busy && s.target != "" && s.passphrase.GetText() != "" && s.pin.GetText() != "")
}

// addSecKey enrols the security key plugged in for the key chosen, off
// the main thread, saying what it waits for.
func (a *passApp) addSecKey() {
	s := &a.seckey
	if s.busy || s.target == "" || s.passphrase.GetText() == "" || s.pin.GetText() == "" {
		return
	}
	var key securitykey.Key
	for _, k := range s.list {
		if k.Fingerprint == s.target {
			key = k
		}
	}
	if key.Fingerprint == "" {
		return
	}
	passphrase, pin := s.passphrase.GetText(), s.pin.GetText()
	s.busy = true
	a.checkSecKey()
	s.passphrase.SetSensitive(false)
	s.pin.SetSensitive(false)
	a.secKeyStatus(i18n.T("Looking for the security key…"), false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), enrolTime)
		defer cancel()
		var err error
		devs, derr := securitykey.Devices(ctx)
		switch {
		case derr != nil:
			err = derr
		case len(devs) == 0:
			err = errNoDevice
		default:
			_, err = securitykey.Enrol(ctx, securitykey.Path(), devs[0], pin, key, passphrase, func(step securitykey.Step) {
				gtkx.Post(func() { a.secKeyStatus(secKeyStepText(step, devs[0]), false) })
			})
		}
		gtkx.Post(func() {
			s.busy = false
			s.passphrase.SetSensitive(true)
			s.pin.SetSensitive(true)
			if err != nil {
				s.pin.SetText("")
				if errors.Is(err, securitykey.ErrPassphrase) {
					s.passphrase.SetText("")
					s.passphrase.GrabFocus()
				} else {
					s.pin.GrabFocus()
				}
				a.checkSecKey()
				a.secKeyStatus(secKeyErrorText(err), true)
				return
			}
			a.resetSecKeyForm()
			a.listSecKeys()
		})
	}()
}

var errNoDevice = errors.New("no security key")

// secKeyStepText says what an enrolment waits for.
func secKeyStepText(step securitykey.Step, dev securitykey.Device) string {
	switch step {
	case securitykey.StepChecking:
		return i18n.T("Checking the passphrase…")
	case securitykey.StepTouch1:
		return i18n.Tf("Touch %s when it blinks.", cmp.Or(dev.Name, i18n.T("the security key")))
	case securitykey.StepTouch2:
		return i18n.T("Touch it once more.")
	}
	return ""
}

// secKeyErrorText says why an enrolment failed.
func secKeyErrorText(err error) string {
	switch {
	case errors.Is(err, errNoDevice):
		return i18n.T("No security key is plugged in. Plug one in, then try again.")
	case errors.Is(err, securitykey.ErrPassphrase):
		return i18n.T("The passphrase does not open the key.")
	case errors.Is(err, securitykey.ErrNoPassphrase):
		return i18n.T("The key has no passphrase, so there is nothing for a security key to unlock.")
	case errors.Is(err, securitykey.ErrPIN):
		return i18n.T("Wrong PIN.")
	case errors.Is(err, securitykey.ErrPINBlocked):
		return i18n.T("The security key's PIN is blocked.")
	case errors.Is(err, securitykey.ErrNoPIN):
		return i18n.T("The security key has no PIN. Set one first, e.g. with fido2-token -S.")
	case errors.Is(err, securitykey.ErrTouch):
		return i18n.T("The security key was not touched in time.")
	case errors.Is(err, securitykey.ErrUnsupported):
		return i18n.T("This security key cannot keep a secret (it has no hmac-secret).")
	}
	return err.Error()
}

// removeSecKey forgets an enrolment, and lists again.
func (a *passApp) removeSecKey(e securitykey.Enrolment) {
	if err := securitykey.Remove(securitykey.Path(), e.Fingerprint, e.Credential); err != nil {
		a.secKeyStatus(err.Error(), true)
		return
	}
	a.listSecKeys()
}
