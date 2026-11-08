package securitykey

import (
	"context"
	"errors"
	"time"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/pinentry"
)

// How long a security key is looked for, and waited on to be touched.
const (
	findTime  = 10 * time.Second
	touchTime = 90 * time.Second
)

// Unlocker answers gpg-agent's questions for a key's passphrase with a
// security key enrolled for the key, in one pinentry's connection.
type Unlocker struct {
	Path  string          // where the enrolments are kept
	tried map[string]bool // keys it answered for
}

// Answer answers q, a question of gpg-agent's, when it asks for the
// passphrase of a key a security key plugged in is enrolled for: ask asks
// for the security key's PIN, the key is touched, and the passphrase it
// opens is the answer. Not handled: the passphrase is to be asked as
// usual, with what went wrong, if anything, in q.Error. Asked again for a
// key it answered for, the passphrase it gave did not open the key.
func (u *Unlocker) Answer(q *pinentry.Request, ask pinentry.Asker) (a pinentry.Answer, handled bool, err error) {
	if q.Mode != pinentry.GetPin || q.KeyGrip == "" || q.Repeat != "" {
		return a, false, nil
	}
	if u.tried[q.KeyGrip] {
		q.Error = i18n.T("The passphrase kept with the security key no longer opens this key. Type it, then add the security key again in Pass.")
		return a, false, nil
	}
	keys, err := Load(u.Path)
	enrolled := ForGrip(keys, q.KeyGrip)
	if err != nil || len(enrolled) == 0 || !Available() {
		return a, false, nil
	}
	dev, e, ok := find(enrolled)
	if !ok {
		return a, false, nil
	}
	name := dev.Name
	if name == "" {
		name = i18n.T("Security key")
	}
	note := ""
	for {
		req := *q
		req.SecurityKey = name
		req.Prompt = i18n.T("PIN")
		req.OK = i18n.T("Unlock")
		req.NotOK = i18n.T("Type Passphrase")
		req.Error = note
		ans, err := ask(req)
		switch {
		case err != nil:
			return ans, true, err
		case ans.NotOK:
			q.Error = ""
			return a, false, nil
		case !ans.OK:
			return ans, true, nil // cancelled
		}
		ctx, cancel := context.WithTimeout(context.Background(), touchTime)
		secret, err := hmacSecret(ctx, dev, ans.Pin, e.Credential, e.Salt)
		cancel()
		switch {
		case errors.Is(err, ErrPIN):
			note = wrongPIN(dev)
			continue
		case errors.Is(err, ErrTouch):
			note = i18n.T("The security key was not touched in time.")
			continue
		case errors.Is(err, ErrPINBlocked):
			q.Error = i18n.T("The security key's PIN is blocked. Type the passphrase.")
			return a, false, nil
		case err != nil:
			q.Error = i18n.Tf("The security key could not unlock the key: %v", err)
			return a, false, nil
		}
		pass, err := e.open(secret)
		if err != nil {
			q.Error = i18n.T("The security key could not open the passphrase kept with it. Type the passphrase.")
			return a, false, nil
		}
		if u.tried == nil {
			u.tried = map[string]bool{}
		}
		u.tried[q.KeyGrip] = true
		return pinentry.Answer{OK: true, Pin: pass}, true, nil
	}
}

// find is the security key plugged in that one of the enrolments was
// made with, asked without a PIN or a touch.
func find(enrolled []Enrolment) (Device, Enrolment, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), findTime)
	defer cancel()
	devs, err := Devices(ctx)
	if err != nil {
		return Device{}, Enrolment{}, false
	}
	for _, dev := range devs {
		for _, e := range enrolled {
			if hasCredential(ctx, dev, e.Credential) {
				return dev, e, true
			}
		}
	}
	return Device{}, Enrolment{}, false
}

// wrongPIN says the PIN was wrong, and how many tries are left when the
// key says.
func wrongPIN(dev Device) string {
	ctx, cancel := context.WithTimeout(context.Background(), findTime)
	defer cancel()
	if info, err := DeviceInfo(ctx, dev); err == nil && info.Retries >= 0 {
		return i18n.Nf("Wrong PIN. %d try left before the security key locks.", "Wrong PIN. %d tries left before the security key locks.", info.Retries, info.Retries)
	}
	return i18n.T("Wrong PIN.")
}
