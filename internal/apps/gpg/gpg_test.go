package gpg

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/sockdir"
)

func TestParseKeys(t *testing.T) {
	listing := `tru::1:1791882018:0:3:1:5
pub:u:255:22:977E5858B6E4C43D:1791882014:::u:::scESC:::::ed25519:::0:
fpr:::::::::E51585CE69B379C636C7D764977E5858B6E4C43D:
uid:r::::1791880000::0000::Old Name <old@example.org>::::::::::0:
uid:u::::1791882014::3EE9::Ada Lovelace (work\x3a laptop) <ada@example.org>::::::::::0:
sub:u:255:18:AFC95841F7D506CF:1791882014::::::e:::::cv25519::
fpr:::::::::B5EE3BE1693E32ACDAFEE470AFC95841F7D506CF:
pub:e:3072:1:1111111111111111:1600000000:1700000000::-:::sc:::::::0:
fpr:::::::::2222222222222222222222221111111111111111:
uid:e::::1600000000::4444::bob@example.org::::::::::0:
uid:-::::1600000000::5555::bob@example.org::::::::::0:
pub:e:255:22:3333333333333333:1600000000:1600000002::-:::sc:::::ed25519:::0:
fpr:::::::::4444444444444444444444443333333333333333:
uid:e::::1600000000::6666::Carol <carol@example.org>::::::::::0:
`
	keys := ParseKeys([]byte(listing))
	if len(keys) != 3 {
		t.Fatalf("%d keys", len(keys))
	}
	a := keys[0]
	if a.Fingerprint != "E51585CE69B379C636C7D764977E5858B6E4C43D" || a.KeyID != "977E5858B6E4C43D" || a.Algo != "ed25519" ||
		a.Validity != "u" || !a.CanEncrypt || !a.CanSign || !a.Usable() || len(a.UIDs) != 1 ||
		a.UIDs[0] != (UID{"Ada Lovelace", "ada@example.org", "work: laptop"}) || a.Name() != "Ada Lovelace" ||
		!slices.Equal(a.SubkeyIDs, []string{"AFC95841F7D506CF"}) {
		t.Errorf("ada: %+v", a)
	}
	if got := a.UIDs[0].String(); got != "Ada Lovelace (work: laptop) <ada@example.org>" {
		t.Errorf("String = %q", got)
	}
	b := keys[1]
	if !b.Expired || b.Usable() || b.CanEncrypt || b.Algo != "RSA 3072" || len(b.UIDs) != 1 || b.Name() != "bob@example.org" {
		t.Errorf("bob: %+v", b)
	}
	if c := keys[2]; !c.Expired || c.Name() != "Carol" || len(c.UIDs) != 1 || c.UIDs[0].Email != "carol@example.org" {
		t.Errorf("carol, expired with her only user ID: %+v", c)
	}
}

func TestParseStatus(t *testing.T) {
	status := `[GNUPG:] ENC_TO AFC95841F7D506CF 18 0
[GNUPG:] KEY_CONSIDERED E51585CE69B379C636C7D764977E5858B6E4C43D 0
[GNUPG:] DECRYPTION_KEY B5EE3BE1693E32ACDAFEE470AFC95841F7D506CF E51585CE69B379C636C7D764977E5858B6E4C43D u
[GNUPG:] ENC_TO AFC95841F7D506CF 18 0
[GNUPG:] BEGIN_DECRYPTION
[GNUPG:] PLAINTEXT 62 1791882014 notes%20for%20ann.txt
[GNUPG:] NEWSIG
[GNUPG:] GOODSIG 977E5858B6E4C43D Ada Lovelace <ada@example.org>
[GNUPG:] VALIDSIG E51585CE69B379C636C7D764977E5858B6E4C43D 2026-10-13 1791882020 0 4 0 22 10 00 E51585CE69B379C636C7D764977E5858B6E4C43D
[GNUPG:] DECRYPTION_OKAY
[GNUPG:] END_DECRYPTION
`
	r := ParseStatus([]byte(status))
	if !r.Encrypted || !slices.Equal(r.To, []string{"AFC95841F7D506CF"}) || r.Signature != "good" ||
		r.Signer != "Ada Lovelace <ada@example.org>" || r.SignerKey != "E51585CE69B379C636C7D764977E5858B6E4C43D" ||
		r.SignedAt.Unix() != 1791882020 || r.FileName != "notes for ann.txt" {
		t.Errorf("result %+v", r)
	}
	r = ParseStatus([]byte("[GNUPG:] ERRSIG 1111111111111111 22 10 00 1791882020 9 -\n[GNUPG:] NO_PUBKEY 1111111111111111\n"))
	if !r.Signed || r.Signature != "unknown" || r.SignerKey != "1111111111111111" || r.Encrypted {
		t.Errorf("unknown signer: %+v", r)
	}
	if r := ParseStatus([]byte("[GNUPG:] BADSIG 977E5858B6E4C43D Ada\n")); r.Signature != "bad" || r.Signer != "Ada" {
		t.Errorf("bad: %+v", r)
	}
}

// TestParseStatusPassphrase reads what gpg says of a message encrypted
// with a passphrase: opened, to a key as well, not opened by the
// passphrase given, and its dialog closed.
func TestParseStatusPassphrase(t *testing.T) {
	failed := errors.New("decryption failed: Bad session key")
	r := ParseStatus([]byte(`[GNUPG:] NEED_PASSPHRASE_SYM 9 3 10
[GNUPG:] BEGIN_DECRYPTION
[GNUPG:] DECRYPTION_INFO 2 9 0
[GNUPG:] PLAINTEXT 62 1793881195 
[GNUPG:] DECRYPTION_OKAY
[GNUPG:] END_DECRYPTION
`))
	if !r.Passphrase || !r.Encrypted || len(r.To) != 0 || r.Signed || r.failure(nil) != nil || r.failure(failed) != failed {
		t.Errorf("opened: %+v", r)
	}
	r = ParseStatus([]byte(`[GNUPG:] ENC_TO AFC95841F7D506CF 18 0
[GNUPG:] NEED_PASSPHRASE_SYM 9 3 10
[GNUPG:] DECRYPTION_KEY B5EE3BE1693E32ACDAFEE470AFC95841F7D506CF E51585CE69B379C636C7D764977E5858B6E4C43D u
[GNUPG:] DECRYPTION_OKAY
`))
	if !r.Passphrase || !r.Encrypted || !slices.Equal(r.To, []string{"AFC95841F7D506CF"}) {
		t.Errorf("to a key and with a passphrase: %+v", r)
	}
	r = ParseStatus([]byte(`[GNUPG:] NEED_PASSPHRASE_SYM 9 3 10
[GNUPG:] BEGIN_DECRYPTION
[GNUPG:] ERROR symkey_decrypt.maybe_error 11_BAD_PASSPHRASE
[GNUPG:] DECRYPTION_FAILED
[GNUPG:] FAILURE gpg-exit 33554433
`))
	if !r.Passphrase || r.Encrypted || r.failure(failed) != ErrPassphrase {
		t.Errorf("wrong passphrase: %+v, %v", r, r.failure(failed))
	}
	// A dialog closed leaves gpg without a passphrase, which it then
	// calls a bad one.
	r = ParseStatus([]byte(`[GNUPG:] NEED_PASSPHRASE_SYM 9 3 10
[GNUPG:] CANCELED_BY_USER
[GNUPG:] BEGIN_DECRYPTION
[GNUPG:] ERROR symkey_decrypt.maybe_error 11_BAD_PASSPHRASE
[GNUPG:] DECRYPTION_FAILED
`))
	if r.failure(failed) != ErrCanceled {
		t.Errorf("cancelled: %+v, %v", r, r.failure(failed))
	}
	if r := ParseStatus([]byte("[GNUPG:] ERROR pkdecrypt_failed 11\n[GNUPG:] DECRYPTION_FAILED\n")); r.failure(failed) != failed {
		t.Errorf("a key that did not open it: %v", r.failure(failed))
	}
}

func TestDecryptedName(t *testing.T) {
	for in, want := range map[string]string{
		"/a/notes.txt.gpg": "/a/notes.txt",
		"/a/notes.txt.asc": "/a/notes.txt",
		"/a/key.pgp":       "/a/key",
		"/a/blob":          "/a/blob.decrypted",
		"/a/.gpg":          "/a/.gpg.decrypted",
	} {
		if got := DecryptedName(in); got != want {
			t.Errorf("DecryptedName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArmored(t *testing.T) {
	msg := "-----BEGIN PGP MESSAGE-----\n\nhQEMA\n=abcd\n-----END PGP MESSAGE-----"
	if kind, block, ok := Armored("Hi,\n" + msg + "\nbye"); !ok || kind != "MESSAGE" || block != msg+"\n" {
		t.Errorf("message: %v %q %q", ok, kind, block)
	}
	signed := "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA512\n\nhello\n-----BEGIN PGP SIGNATURE-----\n\niHUE\n-----END PGP SIGNATURE-----"
	if kind, block, ok := Armored(signed); !ok || kind != "SIGNED MESSAGE" || block != signed+"\n" {
		t.Errorf("signed: %v %q %q", ok, kind, block)
	}
	if kind, _, ok := Armored("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nmDMEZ\n-----END PGP PUBLIC KEY BLOCK-----\n"); !ok || kind != "PUBLIC KEY BLOCK" {
		t.Errorf("key: %v %q", ok, kind)
	}
	for _, half := range []string{"", "hello", "-----BEGIN PGP MESSAGE-----\n\nhQEMA", "-----BEGIN PGP MESSAGE-----\n-----END PGP SIGNATURE-----"} {
		if _, _, ok := Armored(half); ok {
			t.Errorf("Armored(%q) found a block", half)
		}
	}
}

// TestRoundTrip runs the real gpg in a temporary home with two keys
// without passphrases: a message encrypted to Bob and signed by Ada, and
// a file, both decrypted again; the keyring listed and a key exported.
func TestRoundTrip(t *testing.T) {
	for _, p := range []string{"gpg", "gpgconf"} {
		if _, err := exec.LookPath(p); err != nil {
			t.Skip("no " + p)
		}
	}
	home := t.TempDir()
	gnupg := filepath.Join(home, "gnupg")
	os.MkdirAll(gnupg, 0o700)
	t.Setenv("GNUPGHOME", gnupg)
	t.Cleanup(func() { exec.Command("gpgconf", "--homedir", gnupg, "--kill", "all").Run() })
	for _, uid := range []string{"Ada Lovelace <ada@example.org>", "Bob <bob@example.org>"} {
		gen := exec.Command("gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", "",
			"--quick-generate-key", uid, "default", "default", "never")
		if out, err := gen.CombinedOutput(); err != nil {
			t.Skipf("gpg cannot make a key here: %v %s", err, out)
		}
	}
	keys, err := Keys()
	if err != nil || len(keys) != 2 {
		t.Fatalf("keys %+v %v", keys, err)
	}
	if keys[0].Name() != "Ada Lovelace" || !keys[0].Secret || !keys[0].CanEncrypt || keys[0].Validity != "u" {
		t.Errorf("ada %+v", keys[0])
	}
	ada, bob := keys[0], keys[1]

	msg := []byte("meet at noon\n")
	armored, err := Encrypt(msg, Options{Recipients: []string{bob.Fingerprint}, Signer: ada.Fingerprint, Armor: true})
	if err != nil || !strings.HasPrefix(string(armored), "-----BEGIN PGP MESSAGE-----") {
		t.Fatalf("encrypt: %v %q", err, armored)
	}
	plain, r, err := Decrypt(armored)
	if err != nil || string(plain) != string(msg) {
		t.Fatalf("decrypt: %v %q", err, plain)
	}
	if !r.Encrypted || r.Signature != "good" || r.SignerKey != ada.Fingerprint || len(r.To) != 1 || !strings.Contains(r.Signer, "Ada") {
		t.Errorf("result %+v", r)
	}
	if _, err := Encrypt(msg, Options{}); err == nil {
		t.Error("no recipient accepted")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	os.WriteFile(path, []byte("file body\n"), 0o600)
	out, err := EncryptFile(path, Options{Recipients: []string{ada.Fingerprint}})
	if err != nil || out != path+".gpg" {
		t.Fatalf("encrypt file: %q %v", out, err)
	}
	if _, err := EncryptFile(path, Options{Recipients: []string{ada.Fingerprint}}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("encrypting over a file: %v", err)
	}
	os.Rename(path, path+".orig")
	dec, r, err := DecryptFile(out)
	if err != nil || dec != path {
		t.Fatalf("decrypt file: %q %v", dec, err)
	}
	if b, _ := os.ReadFile(dec); string(b) != "file body\n" || !r.Encrypted || r.Signed || r.FileName != "notes.txt" {
		t.Errorf("decrypted %q, %+v", b, r)
	}
	if _, _, err := DecryptFile(out); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("decrypting over a file: %v", err)
	}
	if _, _, err := Decrypt([]byte("not a message")); err == nil {
		t.Error("garbage decrypted")
	}
	pub, err := Export(bob.Fingerprint)
	if err != nil || !strings.HasPrefix(pub, "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
		t.Errorf("export: %v", err)
	}
}

// TestPassphrase encrypts a message and a file with a passphrase alone
// and decrypts them: real gpg in a home without a key, given the
// passphrase to decrypt with instead of asking.
func TestPassphrase(t *testing.T) {
	for _, p := range []string{"gpg", "gpgconf"} {
		if _, err := exec.LookPath(p); err != nil {
			t.Skip("no " + p)
		}
	}
	gnupg := sockdir.New(t) // gpg-agent's socket may be made in it
	t.Setenv("GNUPGHOME", gnupg)
	t.Cleanup(func() { exec.Command("gpgconf", "--homedir", gnupg, "--kill", "all").Run() })
	asked := func(passphrase string) {
		testArgs = []string{"--pinentry-mode", "loopback", "--passphrase", passphrase}
	}
	t.Cleanup(func() { testArgs = nil })

	msg := []byte("meet at noon\n")
	armored, err := Encrypt(msg, Options{Passphrase: "correct horse", Armor: true})
	if err != nil && strings.Contains(err.Error(), "loopback") {
		t.Skipf("gpg takes no passphrase here: %v", err)
	}
	if err != nil || !strings.HasPrefix(string(armored), "-----BEGIN PGP MESSAGE-----") {
		t.Fatalf("encrypt: %v %q", err, armored)
	}
	asked("wrong horse")
	if plain, r, err := Decrypt(armored); !errors.Is(err, ErrPassphrase) || len(plain) != 0 || !r.Passphrase || r.Encrypted {
		t.Errorf("a wrong passphrase: %v %q %+v", err, plain, r)
	}
	asked("correct horse")
	plain, r, err := Decrypt(armored)
	if err != nil || string(plain) != string(msg) {
		t.Fatalf("decrypt: %v %q", err, plain)
	}
	if !r.Encrypted || !r.Passphrase || len(r.To) != 0 || r.Signed {
		t.Errorf("result %+v", r)
	}
	for _, o := range []Options{
		{Passphrase: "correct horse", Recipients: []string{"E51585CE69B379C636C7D764977E5858B6E4C43D"}},
		{Passphrase: "correct horse", Signer: "E51585CE69B379C636C7D764977E5858B6E4C43D"},
		{Passphrase: "two\nlines"},
	} {
		if out, err := Encrypt(msg, o); err == nil {
			t.Errorf("encrypted with %+v: %q", o, out)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	os.WriteFile(path, []byte("file body\n"), 0o600)
	out, err := EncryptFile(path, Options{Passphrase: "correct horse"})
	if err != nil || out != path+".gpg" {
		t.Fatalf("encrypt file: %q %v", out, err)
	}
	if b, _ := os.ReadFile(out); len(b) == 0 || strings.Contains(string(b), "file body") {
		t.Errorf("the file written: %q", b)
	}
	os.Rename(path, path+".orig")
	asked("wrong horse")
	if _, _, err := DecryptFile(out); !errors.Is(err, ErrPassphrase) {
		t.Errorf("a wrong passphrase for the file: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a wrong passphrase wrote a file")
	}
	asked("correct horse")
	dec, r, err := DecryptFile(out)
	if err != nil || dec != path {
		t.Fatalf("decrypt file: %q %v", dec, err)
	}
	if b, _ := os.ReadFile(dec); string(b) != "file body\n" || !r.Encrypted || !r.Passphrase || r.FileName != "notes.txt" {
		t.Errorf("decrypted %q, %+v", b, r)
	}
}

// TestKeyring makes a key with Generate, extends it, and carries its
// public key to a second keyring with ShowKeys and Import; real gpg, an
// empty passphrase given instead of asking.
func TestKeyring(t *testing.T) {
	for _, p := range []string{"gpg", "gpgconf"} {
		if _, err := exec.LookPath(p); err != nil {
			t.Skip("no " + p)
		}
	}
	testArgs = []string{"--pinentry-mode", "loopback", "--passphrase", ""}
	t.Cleanup(func() { testArgs = nil })
	home := func() string {
		dir := filepath.Join(t.TempDir(), "gnupg")
		os.MkdirAll(dir, 0o700)
		t.Cleanup(func() { exec.Command("gpgconf", "--homedir", dir, "--kill", "all").Run() })
		t.Setenv("GNUPGHOME", dir)
		return dir
	}
	home()
	if _, err := Generate(" ", "", "2y"); err == nil {
		t.Error("a key without a name")
	}
	if _, err := Generate("Ada", "", "3w"); err == nil {
		t.Error("an unknown expiry")
	}
	fpr, err := Generate("Ada Lovelace", "ada@example.org", "1y")
	if err != nil {
		t.Skipf("gpg cannot make a key here: %v", err)
	}
	keys, err := Keys()
	if err != nil || len(keys) != 1 || keys[0].Fingerprint != fpr || !keys[0].Secret ||
		keys[0].UIDs[0] != (UID{Name: "Ada Lovelace", Email: "ada@example.org"}) || !keys[0].CanEncrypt {
		t.Fatalf("keys %+v %v", keys, err)
	}
	before := keys[0].Expires
	if before.IsZero() {
		t.Fatal("no expiry")
	}
	if err := Extend(fpr, "5y"); err != nil {
		t.Fatalf("extend: %v", err)
	}
	keys, _ = Keys()
	if !keys[0].Expires.After(before.AddDate(3, 0, 0)) {
		t.Errorf("expires %v, was %v", keys[0].Expires, before)
	}
	pub, err := Export(fpr)
	if err != nil {
		t.Fatal(err)
	}

	home() // another person's keyring
	shown, err := ShowKeys([]byte(pub))
	if err != nil || len(shown) != 1 || shown[0].Fingerprint != fpr || shown[0].Secret || shown[0].Name() != "Ada Lovelace" {
		t.Fatalf("show %+v %v", shown, err)
	}
	if keys, _ := Keys(); len(keys) != 0 {
		t.Errorf("ShowKeys imported: %+v", keys)
	}
	if _, err := ShowKeys([]byte("not a key")); err == nil {
		t.Error("garbage shown")
	}
	got, err := Import([]byte(pub))
	if err != nil || !slices.Equal(got, []string{fpr}) {
		t.Fatalf("import %v %v", got, err)
	}
	if keys, _ := Keys(); len(keys) != 1 || keys[0].Secret || keys[0].Fingerprint != fpr {
		t.Errorf("after import %+v", keys)
	}
	if _, err := Import([]byte("not a key")); err == nil {
		t.Error("garbage imported")
	}
	if err := Delete(fpr); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if keys, _ := Keys(); len(keys) != 0 {
		t.Errorf("after delete %+v", keys)
	}
	mine, err := Generate("Bob", "bob@example.org", "1y")
	if err != nil {
		t.Fatal(err)
	}
	if err := Delete(mine); err == nil || !strings.Contains(err.Error(), "your own") {
		t.Errorf("deleting the user's own key: %v", err)
	}
	if keys, _ := Keys(); len(keys) != 1 || !keys[0].Secret {
		t.Errorf("the user's key went: %+v", keys)
	}
}
