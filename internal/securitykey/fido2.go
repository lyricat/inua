// Package securitykey unlocks a gpg key with a FIDO2 security key
// (docs/security-key.md). FIDO2 cannot decrypt OpenPGP, so the
// key's passphrase is kept sealed under a secret only the security key
// gives, with its PIN and a touch (the hmac-secret extension). The Pass
// app enrols a security key; inua-pinentry answers gpg-agent with the
// passphrase it opens, and the typed passphrase keeps working.
//
// The security key is driven through libfido2's tools (fido2-token,
// fido2-cred, fido2-assert), so the programs stay free of cgo; without
// them there is nothing to enrol or unlock with.
package securitykey

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// RelyingParty names inua's credentials to the security key.
const RelyingParty = "inua"

// The tools' names, found on PATH.
const (
	toolToken  = "fido2-token"
	toolCred   = "fido2-cred"
	toolAssert = "fido2-assert"
)

// What the security key answered, as the tools report it.
var (
	ErrPIN          = errors.New("wrong PIN")
	ErrPINPolicy    = errors.New("PIN too short or too long")
	ErrPINBlocked   = errors.New("PIN blocked")
	ErrNoPIN        = errors.New("no PIN set")
	ErrTouch        = errors.New("not touched in time")
	ErrNoCredential = errors.New("not this security key's credential")
	ErrUnsupported  = errors.New("security key without hmac-secret")
)

// fidoErrors maps libfido2's error names, which the tools print last on
// stderr, to the errors above.
var fidoErrors = map[string]error{
	"FIDO_ERR_PIN_INVALID":           ErrPIN,
	"FIDO_ERR_PIN_POLICY_VIOLATION":  ErrPINPolicy,
	"FIDO_ERR_PIN_BLOCKED":           ErrPINBlocked,
	"FIDO_ERR_PIN_AUTH_BLOCKED":      ErrPINBlocked,
	"FIDO_ERR_PIN_NOT_SET":           ErrNoPIN,
	"FIDO_ERR_PIN_REQUIRED":          ErrNoPIN,
	"FIDO_ERR_ACTION_TIMEOUT":        ErrTouch,
	"FIDO_ERR_USER_ACTION_TIMEOUT":   ErrTouch,
	"FIDO_ERR_OPERATION_DENIED":      ErrTouch,
	"FIDO_ERR_NO_CREDENTIALS":        ErrNoCredential,
	"FIDO_ERR_INVALID_CREDENTIAL":    ErrNoCredential,
	"FIDO_ERR_UNSUPPORTED_EXTENSION": ErrUnsupported,
	"FIDO_ERR_UNSUPPORTED_OPTION":    ErrUnsupported,
}

// Available reports whether libfido2's tools are installed.
func Available() bool {
	for _, t := range []string{toolToken, toolCred, toolAssert} {
		if _, err := exec.LookPath(t); err != nil {
			return false
		}
	}
	return true
}

// Device is a security key plugged in: its hidraw node and its name, as
// fido2-token lists them.
type Device struct {
	Path string
	Name string // "Yubico YubiKey OTP+FIDO+CCID"
}

// Devices lists the security keys plugged in.
func Devices(ctx context.Context) ([]Device, error) {
	out, err := run(ctx, nil, "", toolToken, "-L")
	if err != nil {
		return nil, err
	}
	return parseDevices(out), nil
}

// parseDevices reads fido2-token -L:
// "/dev/hidraw8: vendor=0x1050, product=0x0407 (Yubico YubiKey OTP+FIDO+CCID)".
func parseDevices(out []byte) []Device {
	var devs []Device
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		path, rest, ok := strings.Cut(sc.Text(), ": ")
		if !ok || !strings.HasPrefix(path, "/") {
			continue
		}
		name := ""
		if i := strings.IndexByte(rest, '('); i >= 0 && strings.HasSuffix(rest, ")") {
			name = strings.TrimSpace(rest[i+1 : len(rest)-1])
		}
		devs = append(devs, Device{Path: path, Name: name})
	}
	return devs
}

// Info is what a security key can do.
type Info struct {
	PIN        bool   // a PIN is set
	HMACSecret bool   // it has the hmac-secret extension
	Retries    int    // PIN tries left; -1 when it does not say
	MinPIN     int    // the shortest PIN it takes, in characters
	Firmware   string // "5.8.0"; "" when it does not say
}

// maxPIN is the longest PIN in bytes (CTAP2).
const maxPIN = 63

// DeviceInfo asks dev what it can do.
func DeviceInfo(ctx context.Context, dev Device) (Info, error) {
	out, err := run(ctx, nil, "", toolToken, "-I", dev.Path)
	if err != nil {
		return Info{}, err
	}
	return parseInfo(out), nil
}

// parseInfo reads fido2-token -I.
func parseInfo(out []byte) Info {
	info := Info{Retries: -1, MinPIN: 4}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ": ")
		if !ok {
			continue
		}
		words := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
		switch key {
		case "options":
			for _, w := range words {
				if w == "clientPin" {
					info.PIN = true
				}
			}
		case "extension strings":
			for _, w := range words {
				if w == "hmac-secret" {
					info.HMACSecret = true
				}
			}
		case "pin retries":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				info.Retries = n
			}
		case "minpinlen":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
				info.MinPIN = n
			}
		case "fwversion":
			// 0xMMmmpp, as Yubico and others number it.
			if n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(value), "0x"), 16, 32); err == nil && n > 0 {
				info.Firmware = fmt.Sprintf("%d.%d.%d", n>>16, n>>8&0xff, n&0xff)
			}
		}
	}
	return info
}

// makeCredential makes a credential for inua on dev, with the hmac-secret
// extension and the PIN: the key blinks to be touched. It keeps nothing
// on the key (not a resident credential); the ID returned names it.
func makeCredential(ctx context.Context, dev Device, pin, user string) ([]byte, error) {
	input := lines(b64(random(32)), RelyingParty, user, b64(random(16)))
	out, err := run(ctx, input, pin, toolCred, "-M", "-h", "-v", "-i", "/dev/fd/3", dev.Path, "es256")
	if err != nil {
		return nil, err
	}
	// client data hash, relying party, format, authenticator data,
	// credential ID, …
	f := outLines(out)
	if len(f) < 5 {
		return nil, fmt.Errorf("%s: %d lines of output", toolCred, len(f))
	}
	return unb64(f[4])
}

// hmacSecret asks dev, with the PIN, for the secret of credential cred and
// salt: the key blinks to be touched. The same three always give the
// same 32 bytes, and only this key gives them.
func hmacSecret(ctx context.Context, dev Device, pin string, cred, salt []byte) ([]byte, error) {
	input := lines(b64(random(32)), RelyingParty, b64(cred), b64(salt))
	out, err := run(ctx, input, pin, toolAssert, "-G", "-h", "-v", "-i", "/dev/fd/3", dev.Path)
	if err != nil {
		return nil, err
	}
	// client data hash, relying party, authenticator data, signature,
	// hmac secret.
	f := outLines(out)
	if len(f) < 5 {
		return nil, fmt.Errorf("%s: %d lines of output", toolAssert, len(f))
	}
	secret, err := unb64(f[4])
	if err == nil && len(secret) != 32 {
		err = fmt.Errorf("%s: a secret of %d bytes", toolAssert, len(secret))
	}
	return secret, err
}

// hasCredential reports whether dev knows cred, asked without a PIN or a
// touch: of several keys plugged in, or several enrolled, the one to ask.
func hasCredential(ctx context.Context, dev Device, cred []byte) bool {
	input := lines(b64(random(32)), RelyingParty, b64(cred))
	_, err := run(ctx, input, "", toolAssert, "-G", "-t", "up=false", "-i", "/dev/fd/3", dev.Path)
	return err == nil
}

// run runs a tool with input on file descriptor 3 and pin, when given,
// on stdin. It runs in a session of its own: with no terminal to read
// it from, the tools read the PIN from stdin.
func run(ctx context.Context, input []byte, pin, tool string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if pin != "" {
		cmd.Stdin = strings.NewReader(pin + "\n")
	}
	if input != nil {
		r, w, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		// It fits the pipe's buffer: written before the tool starts.
		_, err = w.Write(input)
		w.Close()
		if err != nil {
			return nil, err
		}
		cmd.ExtraFiles = []*os.File{r}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, toolError(tool, stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

// toolError names what went wrong: libfido2's error, when the tool's
// message ends with one, as one of the errors above.
func toolError(tool, stderr string, err error) error {
	msg := strings.TrimSpace(stderr)
	for name, known := range fidoErrors {
		if strings.Contains(msg, name) {
			return fmt.Errorf("%s: %w", tool, known)
		}
	}
	if msg == "" {
		return fmt.Errorf("%s: %w", tool, err)
	}
	if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
		msg = msg[i+1:]
	}
	return fmt.Errorf("%s", msg)
}

func lines(fields ...string) []byte { return []byte(strings.Join(fields, "\n") + "\n") }

func outLines(out []byte) []string {
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n")
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func unb64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(strings.TrimSpace(s)) }

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// CheckPIN says why pin cannot be a new PIN for a key that takes no
// shorter than min characters; nil when it can.
func CheckPIN(pin string, min int) error {
	if len([]rune(pin)) < min || len(pin) > maxPIN || strings.ContainsAny(pin, "\n\r") {
		return ErrPINPolicy
	}
	return nil
}

// SetPIN sets the PIN of dev, which has none.
func SetPIN(ctx context.Context, dev Device, pin string) error {
	if err := CheckPIN(pin, 4); err != nil {
		return err
	}
	// The tool asks for the new PIN, then again.
	_, err := run(ctx, nil, pin+"\n"+pin, toolToken, "-S", dev.Path)
	return err
}

// ChangePIN changes the PIN of dev from old to pin.
func ChangePIN(ctx context.Context, dev Device, old, pin string) error {
	if err := CheckPIN(pin, 4); err != nil {
		return err
	}
	if strings.ContainsAny(old, "\n\r") {
		return ErrPIN
	}
	// The tool asks for the current PIN, the new one, then it again.
	_, err := run(ctx, nil, old+"\n"+pin+"\n"+pin, toolToken, "-C", dev.Path)
	return err
}

// Holds is the enrolments whose credential dev knows, asked without a
// PIN or a touch.
func Holds(ctx context.Context, dev Device, enrolled []Enrolment) []Enrolment {
	var out []Enrolment
	for _, e := range enrolled {
		if hasCredential(ctx, dev, e.Credential) {
			out = append(out, e)
		}
	}
	return out
}
