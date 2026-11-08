package securitykey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Enrolment is one security key enrolled for one gpg key: the credential
// it made, and the key's passphrase sealed under the secret it gives for
// that credential and the salt. Nothing in it opens the passphrase
// without the security key and its PIN.
type Enrolment struct {
	Fingerprint string    `json:"fingerprint"` // the gpg key's
	Grips       []string  `json:"grips"`       // its keys (primary, subkeys) the passphrase opens
	Device      string    `json:"device"`      // the security key's name, to show
	Added       time.Time `json:"added"`
	Credential  []byte    `json:"credential"`
	Salt        []byte    `json:"salt"`
	Nonce       []byte    `json:"nonce"`
	Sealed      []byte    `json:"sealed"`
}

// file is what Path holds.
type file struct {
	Version int         `json:"version"`
	Keys    []Enrolment `json:"keys"`
}

// Path is where the enrolments are kept: $XDG_DATA_HOME/inua/security-keys.json.
func Path() string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "inua", "security-keys.json")
}

// Load reads the enrolments at path; none when it does not exist.
func Load(path string) ([]Enrolment, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f.Keys, nil
}

// save writes the enrolments to path, readable by the user alone, in
// place at once.
func save(path string, keys []Enrolment) error {
	data, err := json.MarshalIndent(file{Version: 1, Keys: keys}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".security-keys-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ForGrip is the enrolments whose passphrase opens the key with keygrip
// grip.
func ForGrip(keys []Enrolment, grip string) []Enrolment {
	var out []Enrolment
	for _, k := range keys {
		if slices.Contains(k.Grips, grip) {
			out = append(out, k)
		}
	}
	return out
}

// ForKey is the enrolments for the gpg key with fingerprint fpr.
func ForKey(keys []Enrolment, fpr string) []Enrolment {
	var out []Enrolment
	for _, k := range keys {
		if k.Fingerprint == fpr {
			out = append(out, k)
		}
	}
	return out
}

// Remove forgets the enrolment of credential cred for the gpg key fpr. The
// credential kept nothing on the security key, so nothing is left there.
func Remove(path, fpr string, cred []byte) error {
	keys, err := Load(path)
	if err != nil {
		return err
	}
	kept := slices.DeleteFunc(keys, func(k Enrolment) bool {
		return k.Fingerprint == fpr && string(k.Credential) == string(cred)
	})
	return save(path, kept)
}

// sealInfo binds the sealing key to its use.
const sealInfo = "inua security key: gpg passphrase"

// aead is AES-256-GCM under a key drawn from the security key's secret.
func aead(secret []byte) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, secret, nil, sealInfo, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// additional ties a sealed passphrase to its gpg key and credential: one
// copied to another enrolment does not open.
func (e Enrolment) additional() []byte {
	return append([]byte(e.Fingerprint+"\x00"), e.Credential...)
}

// seal seals passphrase in e under secret.
func (e *Enrolment) seal(secret []byte, passphrase string) error {
	a, err := aead(secret)
	if err != nil {
		return err
	}
	e.Nonce = random(a.NonceSize())
	e.Sealed = a.Seal(nil, e.Nonce, []byte(passphrase), e.additional())
	return nil
}

// ErrSealed is a sealed passphrase the secret given does not open.
var ErrSealed = errors.New("the security key's secret does not open the passphrase")

// open opens the passphrase sealed in e with secret.
func (e Enrolment) open(secret []byte) (string, error) {
	a, err := aead(secret)
	if err != nil {
		return "", err
	}
	plain, err := a.Open(nil, e.Nonce, e.Sealed, e.additional())
	if err != nil {
		return "", ErrSealed
	}
	return string(plain), nil
}
