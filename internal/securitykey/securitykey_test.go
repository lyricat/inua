package securitykey

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/sockdir"
)

func TestParseDevices(t *testing.T) {
	out := []byte("/dev/hidraw8: vendor=0x1050, product=0x0407 (Yubico YubiKey OTP+FIDO+CCID)\n" +
		"ioctl: nothing here\n/dev/hidraw2: vendor=0x20a0, product=0x42b1 ()\n")
	got := parseDevices(out)
	want := []Device{{"/dev/hidraw8", "Yubico YubiKey OTP+FIDO+CCID"}, {"/dev/hidraw2", ""}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseInfo(t *testing.T) {
	out := []byte("proto: 0x02\nextension strings: credProtect, hmac-secret, largeBlobKey\n" +
		"options: rk, up, noplat, noalwaysUv, credMgmt, clientPin, largeBlobs\nfwversion: 0x50800\n" +
		"minpinlen: 6\npin retries: 8\n")
	if got := parseInfo(out); !got.PIN || !got.HMACSecret || got.Retries != 8 || got.MinPIN != 6 || got.Firmware != "5.8.0" {
		t.Fatalf("got %+v", got)
	}
	out = []byte("extension strings: credProtect\noptions: rk, up, noclientPin\n")
	if got := parseInfo(out); got.PIN || got.HMACSecret || got.Retries != -1 || got.MinPIN != 4 || got.Firmware != "" {
		t.Fatalf("got %+v", got)
	}
}

// A key with its subkey here, one with only stubs (its secret on a card),
// and a revoked part are told apart.
func TestParseSecretKeys(t *testing.T) {
	listing := []byte(`sec:u:255:22:AAAA:1700000000:::u:::scESC:::+:::ed25519:::0:
fpr:::::::::FPR1:
grp:::::::::GRIP1:
uid:u::::1700000000::H::Ada Lovelace \x3cada@example.org\x3e::::::::::0:
ssb:u:255:18:BBBB:1700000000::::::e:::+:::cv25519::
fpr:::::::::FPR1SUB:
grp:::::::::GRIP2:
ssb:r:255:18:CCCC:1700000000::::::e:::+:::cv25519::
fpr:::::::::FPR1OLD:
grp:::::::::GRIP3:
sec:u:255:22:DDDD:1700000000:::u:::scESC:::#:::ed25519:::0:
fpr:::::::::FPR2:
grp:::::::::GRIP4:
uid:u::::1700000000::H::Card User::::::::::0:
ssb:u:255:18:EEEE:1700000000::::::e:::D2760001240100000006000012340000:::cv25519::
fpr:::::::::FPR2SUB:
grp:::::::::GRIP5:
`)
	got := parseSecretKeys(listing)
	if len(got) != 1 || got[0].Fingerprint != "FPR1" || got[0].UserID != "Ada Lovelace <ada@example.org>" ||
		!slices.Equal(got[0].Grips, []string{"GRIP1", "GRIP2"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestSealOpen(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, 32)
	e := Enrolment{Fingerprint: "FPR", Credential: []byte("cred")}
	if err := e.seal(secret, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(e.Sealed, []byte("correct horse")) {
		t.Fatal("the passphrase is in the clear")
	}
	if got, err := e.open(secret); err != nil || got != "correct horse" {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := e.open(bytes.Repeat([]byte{8}, 32)); !errors.Is(err, ErrSealed) {
		t.Fatalf("another secret opened it: %v", err)
	}
	damaged := e
	damaged.Nonce = nil
	if _, err := damaged.open(secret); !errors.Is(err, ErrSealed) {
		t.Fatalf("a missing nonce: %v", err)
	}
	moved := e
	moved.Fingerprint = "OTHER"
	if _, err := moved.open(secret); !errors.Is(err, ErrSealed) {
		t.Fatalf("a copy for another key opened: %v", err)
	}
}

func TestStoreRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inua", "security-keys.json")
	if keys, err := Load(path); err != nil || len(keys) != 0 {
		t.Fatalf("missing file: %v %v", keys, err)
	}
	a := Enrolment{Fingerprint: "F1", Grips: []string{"G1", "G2"}, Credential: []byte("a")}
	b := Enrolment{Fingerprint: "F1", Grips: []string{"G1"}, Credential: []byte("b")}
	if err := save(path, []Enrolment{a, b}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", st, err)
	}
	keys, _ := Load(path)
	if len(ForGrip(keys, "G1")) != 2 || len(ForGrip(keys, "G2")) != 1 || len(ForKey(keys, "F2")) != 0 {
		t.Fatalf("keys %+v", keys)
	}
	if err := Remove(path, "F1", []byte("a")); err != nil {
		t.Fatal(err)
	}
	keys, _ = Load(path)
	if len(keys) != 1 || string(keys[0].Credential) != "b" {
		t.Fatalf("after remove: %+v", keys)
	}
}

// fakeTools puts fido2-token, fido2-cred and fido2-assert on PATH: a key
// at /dev/hidraw9 with PIN 1234 and the credential "credential-1", whose
// secret for a salt is the salt itself. A file "timeout" in the returned
// directory makes the next assertion time out.
func fakeTools(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	scripts := map[string]string{
		"fido2-token": `
case "$1" in
-L) echo "/dev/hidraw9: vendor=0x1050, product=0x0407 (Test Key)";;
-I) printf 'extension strings: hmac-secret\noptions: rk, up, clientPin\npin retries: 5\n';;
-S) read -r a; read -r b; echo "set $a $b $2" > "$(dirname "$0")/pin";;
-C) read -r old; read -r a; read -r b
    [ "$old" = 1234 ] || { echo "fido2-token: fido_dev_set_pin: FIDO_ERR_PIN_INVALID" >&2; exit 1; }
    echo "change $old $a $b $2" > "$(dirname "$0")/pin";;
esac`,
		"fido2-cred": `
read -r pin
[ "$pin" = 1234 ] || { echo "fido2-cred: fido_dev_make_cred: FIDO_ERR_PIN_INVALID" >&2; exit 1; }
{ read -r cdh; read -r rp; read -r user; read -r uid; } < /dev/fd/3
[ "$rp" = inua ] || exit 1
printf '%s\n%s\npacked\nYXV0aA==\n%s\nc2ln\n' "$cdh" "$rp" "$(printf credential-1 | base64)"`,
		"fido2-assert": `
up=true; pin=false
for a; do case "$a" in up=false) up=false;; -v) pin=true;; esac; done
{ read -r cdh; read -r rp; read -r cred; read -r salt; } < /dev/fd/3
[ "$cred" = "$(printf credential-1 | base64)" ] || { echo "fido2-assert: fido_dev_get_assert: FIDO_ERR_NO_CREDENTIALS" >&2; exit 1; }
[ "$up" = false ] && { printf '%s\n%s\nYXV0aA==\nc2ln\n' "$cdh" "$rp"; exit 0; }
if $pin; then
  read -r p
  [ "$p" = 1234 ] || { echo "fido2-assert: fido_dev_get_assert: FIDO_ERR_PIN_INVALID" >&2; exit 1; }
fi
if [ -e "` + "$(dirname \"$0\")" + `/timeout" ]; then
  rm "` + "$(dirname \"$0\")" + `/timeout"
  echo "fido2-assert: fido_dev_get_assert: FIDO_ERR_ACTION_TIMEOUT" >&2; exit 1
fi
printf '%s\n%s\nYXV0aA==\nc2ln\n%s\n' "$cdh" "$rp" "$salt"`,
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// gpgHome makes a throwaway gpg home with a key whose passphrase is
// passphrase ("" for none), and returns the key.
func gpgHome(t *testing.T, passphrase string) Key {
	t.Helper()
	for _, p := range []string{"gpg", "gpgconf", "gpg-agent"} {
		if _, err := exec.LookPath(p); err != nil {
			t.Skip("no " + p)
		}
	}
	home := sockdir.New(t) // the agent's socket may be made in it
	t.Setenv("GNUPGHOME", home)
	t.Cleanup(func() { exec.Command("gpgconf", "--homedir", home, "--kill", "all").Run() })
	gen := exec.Command("gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", passphrase,
		"--quick-generate-key", "Ada Lovelace <ada@example.org>", "default", "default", "never")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("gpg cannot make a key here: %v %s", err, out)
	}
	keys, err := SecretKeys(t.Context(), []string{"ada@example.org"})
	if err != nil || len(keys) != 1 || len(keys[0].Grips) != 2 {
		t.Fatalf("keys %+v %v", keys, err)
	}
	return keys[0]
}

// The agent checks the passphrase given, even with the right one
// remembered from a use just before.
func TestVerify(t *testing.T) {
	key := gpgHome(t, "right horse")
	ctx := t.Context()
	for _, grip := range key.Grips {
		if err := Verify(ctx, grip, "right horse"); err != nil {
			t.Fatalf("right passphrase: %v", err)
		}
	}
	sign := exec.Command("gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", "right horse",
		"--clearsign", "-u", key.Fingerprint)
	sign.Stdin = strings.NewReader("hello\n")
	if out, err := sign.CombinedOutput(); err != nil {
		t.Fatalf("sign: %v %s", err, out)
	}
	if err := Verify(ctx, key.Grips[0], "wrong horse"); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("wrong passphrase, the right one remembered: %v", err)
	}
	if err := Verify(ctx, key.Grips[0], "100% \nnewline"); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("escaped passphrase: %v", err)
	}
}

func TestVerifyWithoutPassphrase(t *testing.T) {
	key := gpgHome(t, "")
	if err := Verify(t.Context(), key.Grips[0], "anything"); !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("got %v", err)
	}
}

// A security key is enrolled, then answers gpg-agent: a wrong PIN and a
// missed touch are asked again, Type Passphrase gives way, and a second
// ask for the same key in the connection is left to the passphrase.
func TestEnrolAndUnlock(t *testing.T) {
	key := gpgHome(t, "right horse")
	tools := fakeTools(t)
	path := filepath.Join(t.TempDir(), "security-keys.json")
	devs, err := Devices(t.Context())
	if err != nil || len(devs) != 1 {
		t.Fatalf("devices %+v %v", devs, err)
	}
	if _, err := Enrol(t.Context(), path, devs[0], "1234", key, "wrong horse", nil); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("enrolled with a wrong passphrase: %v", err)
	}
	if _, err := Enrol(t.Context(), path, devs[0], "4321", key, "right horse", nil); !errors.Is(err, ErrPIN) {
		t.Fatalf("enrolled with a wrong PIN: %v", err)
	}
	var steps []Step
	e, err := Enrol(t.Context(), path, devs[0], "1234", key, "right horse", func(s Step) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(steps, []Step{StepChecking, StepTouch1, StepTouch2}) || e.Device != "Test Key" ||
		!slices.Equal(e.Grips, key.Grips) || string(e.Credential) != "credential-1" {
		t.Fatalf("enrolment %+v steps %v", e, steps)
	}
	data, _ := os.ReadFile(path)
	if bytes.Contains(data, []byte("right horse")) {
		t.Fatal("the passphrase is kept in the clear")
	}

	// The PIN is asked; a wrong one and a missed touch are asked again.
	var asked []pinentry.Request
	pins := []string{"0000", "1234", "1234"}
	ask := func(q pinentry.Request) (pinentry.Answer, error) {
		asked = append(asked, q)
		pin := pins[0]
		pins = pins[1:]
		if len(asked) == 2 {
			os.WriteFile(filepath.Join(tools, "timeout"), nil, 0o600)
		}
		return pinentry.Answer{OK: true, Pin: pin}, nil
	}
	u := &Unlocker{Path: path}
	q := pinentry.Request{Mode: pinentry.GetPin, KeyGrip: key.Grips[1], Desc: "Please enter the passphrase"}
	a, handled, err := u.Answer(&q, ask)
	if err != nil || !handled || !a.OK || a.Pin != "right horse" {
		t.Fatalf("answer %+v handled=%v err=%v", a, handled, err)
	}
	if len(asked) != 3 || asked[0].SecurityKey != "Test Key" || asked[0].NotOK == "" || asked[0].Error != "" ||
		!strings.Contains(asked[1].Error, "5") || asked[2].Error == "" {
		t.Fatalf("asked %+v", asked)
	}

	// gpg-agent asks again: what the security key gave did not open it.
	q = pinentry.Request{Mode: pinentry.GetPin, KeyGrip: key.Grips[1], Error: "Bad Passphrase"}
	if _, handled, _ := u.Answer(&q, ask); handled || q.Error == "Bad Passphrase" {
		t.Fatalf("asked again: handled=%v error=%q", handled, q.Error)
	}

	// Type Passphrase, in another connection.
	u = &Unlocker{Path: path}
	q = pinentry.Request{Mode: pinentry.GetPin, KeyGrip: key.Grips[0]}
	notOK := func(pinentry.Request) (pinentry.Answer, error) { return pinentry.Answer{NotOK: true}, nil }
	if _, handled, _ := u.Answer(&q, notOK); handled {
		t.Fatal("Type Passphrase was not given way to")
	}
	// Cancel cancels.
	cancel := func(pinentry.Request) (pinentry.Answer, error) { return pinentry.Answer{}, nil }
	if a, handled, _ := u.Answer(&q, cancel); !handled || a.OK {
		t.Fatalf("cancel: %+v handled=%v", a, handled)
	}
	// A new passphrase, another key, or a question that is no passphrase
	// is not the security key's.
	for _, q := range []pinentry.Request{
		{Mode: pinentry.GetPin, KeyGrip: key.Grips[0], Repeat: "Repeat:"},
		{Mode: pinentry.GetPin, KeyGrip: "OTHER"},
		{Mode: pinentry.Confirm, KeyGrip: key.Grips[0]},
	} {
		if _, handled, _ := u.Answer(&q, ask); handled {
			t.Fatalf("handled %+v", q)
		}
	}
	// Without the tools, the passphrase is asked.
	t.Setenv("PATH", "/nonexistent")
	q = pinentry.Request{Mode: pinentry.GetPin, KeyGrip: key.Grips[0]}
	if _, handled, _ := u.Answer(&q, ask); handled {
		t.Fatal("handled without the tools")
	}
}

// Setting and changing a PIN give the tool its PINs on stdin, in the
// order it asks; a wrong current PIN or a PIN the key cannot take fail.
func TestSetChangePIN(t *testing.T) {
	tools := fakeTools(t)
	dev := Device{Path: "/dev/hidraw9"}
	got := func() string {
		t.Helper()
		data, _ := os.ReadFile(filepath.Join(tools, "pin"))
		os.Remove(filepath.Join(tools, "pin"))
		return strings.TrimSpace(string(data))
	}
	if err := SetPIN(t.Context(), dev, "246810"); err != nil || got() != "set 246810 246810 /dev/hidraw9" {
		t.Fatalf("set: %v", err)
	}
	if err := ChangePIN(t.Context(), dev, "1234", "135790"); err != nil || got() != "change 1234 135790 135790 /dev/hidraw9" {
		t.Fatalf("change: %v", err)
	}
	if err := ChangePIN(t.Context(), dev, "0000", "135790"); !errors.Is(err, ErrPIN) || got() != "" {
		t.Fatalf("wrong current PIN: %v", err)
	}
	for _, pin := range []string{"123", strings.Repeat("9", 64), "1234\n5678"} {
		if err := SetPIN(t.Context(), dev, pin); !errors.Is(err, ErrPINPolicy) || got() != "" {
			t.Fatalf("set %q: %v", pin, err)
		}
	}
	if err := CheckPIN("12345", 6); !errors.Is(err, ErrPINPolicy) {
		t.Fatalf("shorter than the key takes: %v", err)
	}
	if err := CheckPIN("密码密码", 4); err != nil {
		t.Fatalf("four characters, more bytes: %v", err)
	}
}

// Holds finds the enrolments a key knows the credential of.
func TestHolds(t *testing.T) {
	fakeTools(t)
	dev := Device{Path: "/dev/hidraw9"}
	mine := Enrolment{Fingerprint: "F1", Credential: []byte("credential-1")}
	other := Enrolment{Fingerprint: "F2", Credential: []byte("credential-2")}
	got := Holds(t.Context(), dev, []Enrolment{other, mine})
	if len(got) != 1 || got[0].Fingerprint != "F1" {
		t.Fatalf("got %+v", got)
	}
}
