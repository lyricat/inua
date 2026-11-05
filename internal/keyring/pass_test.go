package keyring

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lyricat/inua/internal/sockdir"
)

// TestPass runs the store on the real pass, with a key made for the test
// in a GNUPGHOME of its own.
func TestPass(t *testing.T) {
	for _, bin := range []string{"pass", "gpg", "gpgconf"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	if testing.Short() {
		t.Skip("makes a gpg key")
	}
	dir := t.TempDir()
	home := sockdir.New(t) // gpg-agent's socket is in it
	t.Setenv("GNUPGHOME", home)
	store := filepath.Join(dir, "store")
	t.Setenv("PASSWORD_STORE_DIR", store)
	t.Cleanup(func() { exec.Command("gpgconf", "--kill", "gpg-agent").Run() })
	gen := exec.Command("gpg", "--batch", "--passphrase", "", "--quick-gen-key", "inua-test@example.invalid", "default", "default", "never")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("gpg: %v %s", err, out)
	}
	if out, err := exec.Command("pass", "init", "inua-test@example.invalid").CombinedOutput(); err != nil {
		t.Fatalf("pass init: %v %s", err, out)
	}

	s, err := OpenPass(Pass{Dir: store, Folder: "inua-keyring"})
	if err != nil {
		t.Fatal(err)
	}
	it, _, err := s.CreateItem(Login, "API key: work", map[string]string{"profile": "work"}, []byte("sk-λ\nsecond line"), "text/plain", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store, "inua-keyring", "login", "api-key-work.gpg")); err != nil {
		t.Fatalf("no entry: %v", err)
	}
	// pass shows it as it is, the first line first.
	out, err := exec.Command("pass", "show", "inua-keyring/login/api-key-work").Output()
	if err != nil || string(out) != "sk-λ\nsecond line" {
		t.Errorf("pass show: %q %v", out, err)
	}
	got, _, err := s.Secret(Login, it.ID, "")
	if err != nil || string(got) != "sk-λ\nsecond line" {
		t.Errorf("secret %q %v", got, err)
	}
	// Read without a pinentry (the key has no passphrase), and the
	// entry's stamp changes when it is written.
	p := Pass{Dir: store, Folder: "inua-keyring"}
	if got, err := p.ShowQuiet("login/api-key-work"); err != nil || string(got) != "sk-λ\nsecond line" {
		t.Errorf("show quietly: %q %v", got, err)
	}
	st := p.Stamp("login/api-key-work")
	if st == "" || p.Stamp("login/none") != "" {
		t.Errorf("stamps %q, %q", st, p.Stamp("login/none"))
	}
	s.SetSecret(Login, it.ID, []byte("sk-λ\nsecond line\nthird"), "text/plain")
	if p.Stamp("login/api-key-work") == st {
		t.Error("the stamp stayed after a write")
	}
	s.SetSecret(Login, it.ID, []byte("sk-λ\nsecond line"), "text/plain")
	bin := []byte{0xff, 0, 7}
	bi, _, _ := s.CreateItem(Login, "binary", nil, bin, "", false)
	if got, _, err := s.Secret(Login, bi.ID, ""); err != nil || !bytes.Equal(got, bin) {
		t.Errorf("binary %v %v", got, err)
	}
	if err := s.DeleteItem(Login, it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store, "inua-keyring", "login", "api-key-work.gpg")); err == nil {
		t.Error("the entry stayed")
	}
}
