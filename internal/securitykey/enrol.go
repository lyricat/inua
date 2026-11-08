package securitykey

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Step is how far Enrol got, for the app to say what the security key
// waits for.
type Step int

const (
	StepChecking Step = iota // the passphrase is checked with gpg-agent
	StepTouch1               // the key blinks: the credential is made
	StepTouch2               // it blinks again: its secret is read
)

// Enrol enrols the security key dev for key, whose passphrase is
// passphrase: gpg-agent checks the passphrase opens the key's secret
// parts, the security key makes a credential and gives its secret, each
// with the PIN and a touch, and the passphrase is kept at path sealed
// under that secret. step, when not nil, is told each step as it begins.
func Enrol(ctx context.Context, path string, dev Device, pin string, key Key, passphrase string, step func(Step)) (Enrolment, error) {
	tell := func(s Step) {
		if step != nil {
			step(s)
		}
	}
	tell(StepChecking)
	// The parts the passphrase opens; a part with another passphrase of
	// its own is left to be typed. With none, a wrong passphrase is the
	// reason given before any other.
	var grips []string
	var verr error
	for _, grip := range key.Grips {
		err := Verify(ctx, grip, passphrase)
		if err == nil {
			grips = append(grips, grip)
		} else if verr == nil || errors.Is(err, ErrPassphrase) {
			verr = err
		}
	}
	if len(grips) == 0 {
		if verr == nil {
			verr = ErrPassphrase
		}
		return Enrolment{}, verr
	}
	info, err := DeviceInfo(ctx, dev)
	switch {
	case err != nil:
		return Enrolment{}, err
	case !info.HMACSecret:
		return Enrolment{}, ErrUnsupported
	case !info.PIN:
		return Enrolment{}, ErrNoPIN
	}
	tell(StepTouch1)
	cred, err := makeCredential(ctx, dev, pin, key.Fingerprint)
	if err != nil {
		return Enrolment{}, err
	}
	e := Enrolment{
		Fingerprint: key.Fingerprint, Grips: grips, Device: dev.Name, Added: time.Now().UTC().Truncate(time.Second),
		Credential: cred, Salt: random(32),
	}
	tell(StepTouch2)
	secret, err := hmacSecret(ctx, dev, pin, cred, e.Salt)
	if err != nil {
		return Enrolment{}, err
	}
	if err := e.seal(secret, passphrase); err != nil {
		return Enrolment{}, err
	}
	keys, err := Load(path)
	if err != nil {
		return Enrolment{}, err
	}
	if err := save(path, append(keys, e)); err != nil {
		return Enrolment{}, fmt.Errorf("keeping the enrolment: %w", err)
	}
	return e, nil
}
