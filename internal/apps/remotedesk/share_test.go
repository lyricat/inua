package remotedesk

import (
	"cmp"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/hypr"
)

// fakeSunshine is Sunshine's API, as it answers: it checks the basic
// credentials against its credentials file as Sunshine does, and keeps
// the paired devices, with their certificates, in its state file.
type fakeSunshine struct {
	t       *testing.T
	dir     string
	mu      sync.Mutex
	pinned  map[string]string // pairing ID → the PIN typed
	devices []device
	cert    string // the certificate of the device pairing next
	unit    string // the unit's state, when not active or inactive
	paired  int    // devices paired so far, for their IDs
}

// save writes the state file as Sunshine does.
func (f *fakeSunshine) save() {
	var st struct {
		Root struct {
			Devices []device `json:"named_devices"`
		} `json:"root"`
	}
	st.Root.Devices = f.devices
	data, _ := json.Marshal(st)
	os.WriteFile(filepath.Join(f.dir, "state.json"), data, 0600)
}

// clientCert is a device's certificate, as Sunshine keeps it.
func clientCert(t *testing.T) string {
	dir := t.TempDir()
	if err := writeCert(filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "c.pem"))
	return string(data)
}

func (f *fakeSunshine) authorized(r *http.Request) bool {
	data, err := os.ReadFile(filepath.Join(f.dir, "credentials.json"))
	if err != nil {
		return false
	}
	var c struct{ Username, Salt, Password string }
	json.Unmarshal(data, &c)
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(r.Header.Get("Authorization"), "Basic "))
	user, pass, _ := strings.Cut(string(raw), ":")
	// Sunshine's check: util::hex(sha256(password + salt)), its bytes last to first, upper case.
	sum := sha256.Sum256([]byte(pass + c.Salt))
	slices.Reverse(sum[:])
	return strings.EqualFold(user, c.Username) && strings.ToUpper(hex.EncodeToString(sum[:])) == c.Password
}

func (f *fakeSunshine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method + " " + r.URL.Path {
	case "GET /api/pin":
		json.NewEncoder(w).Encode(map[string]any{"pairings": []Pairing{{ID: "0123456789abcdef0123456789abcdef", Name: "laptop", Address: "192.168.1.9"}}})
	case "POST /api/pin":
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		f.pinned[in["pairing_id"]] = in["pin"]
		ok := in["pin"] == "4821"
		json.NewEncoder(w).Encode(map[string]bool{"status": ok})
		if ok {
			// The device is recorded once it has answered its challenges.
			cert := cmp.Or(f.cert, clientCert(f.t))
			name := in["name"]
			go func() {
				time.Sleep(30 * time.Millisecond)
				f.mu.Lock()
				defer f.mu.Unlock()
				f.paired++
				f.devices = append(f.devices, device{Name: name, UUID: fmt.Sprintf("u%d", f.paired), Cert: cert})
				f.save()
			}()
		}
	case "GET /api/clients/list":
		clients := []Client{}
		for _, d := range f.devices {
			clients = append(clients, Client{Name: d.Name, UUID: d.UUID, Enabled: true})
		}
		json.NewEncoder(w).Encode(map[string]any{"named_certs": clients, "status": true})
	case "POST /api/clients/unpair":
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		n := len(f.devices)
		f.devices = slices.DeleteFunc(f.devices, func(d device) bool { return d.UUID == in["uuid"] })
		f.save()
		json.NewEncoder(w).Encode(map[string]bool{"status": len(f.devices) < n})
	default: // POST /api/unpair among them: Sunshine has no such path
		w.WriteHeader(http.StatusNotFound)
	}
}

// newSharing is a sharing in a temporary folder, with programs that
// answer as systemd and Sunshine do, and Sunshine's API served with the
// certificate inua writes for it.
func newSharing(t *testing.T) (*Sharing, *fakeSunshine, *[]string) {
	dir := t.TempDir()
	var calls []string
	var mu sync.Mutex
	active := false
	s := &Sharing{
		Dir:      dir,
		LookPath: func(b string) (string, error) { return "/usr/bin/" + b, nil },
		Getenv:   func(k string) string { return map[string]string{"WAYLAND_DISPLAY": "wayland-1", "PATH": "/usr/bin"}[k] },
		Access:   func(string) error { return nil },
		Hostname: func() (string, error) { return "desk", nil },
		Instance: func() (hypr.Instance, error) { return hypr.Instance{}, hypr.ErrNoInstance },
		// No GPU here: what the test machine has does not reach the config.
		RenderNode: func() string { return "" },
	}
	f := &fakeSunshine{t: t, dir: dir, pinned: map[string]string{}}
	var srv *httptest.Server
	s.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		mu.Lock()
		calls = append(calls, name+" "+strings.Join(args, " "))
		mu.Unlock()
		switch {
		case strings.HasSuffix(name, "sunshine") && args[0] == "--version":
			return []byte("Sunshine version: v2026.914.233613\n"), nil
		case name == "systemd-run":
			// Sunshine starts: its API, with the certificate inua made.
			cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
			if err != nil {
				t.Fatal(err)
			}
			srv = httptest.NewUnstartedServer(f)
			srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
			srv.StartTLS()
			t.Cleanup(srv.Close)
			s.API = srv.URL
			active = true
		case name == "systemctl" && args[1] == "is-active":
			if f.unit != "" {
				return []byte(f.unit + "\n"), nil
			}
			if active {
				return []byte("active\n"), nil
			}
			return []byte("inactive\n"), errors.New("exit status 3")
		case name == "systemctl" && args[1] == "stop":
			active = false
		}
		return nil, nil
	}
	return s, f, &calls
}

func TestSharingStartsSunshineWithInuasConfiguration(t *testing.T) {
	s, _, calls := newSharing(t)
	if err := s.SetOn(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if !s.On() {
		t.Error("not remembered as on")
	}
	conf, _ := os.ReadFile(filepath.Join(s.Dir, "sunshine.conf"))
	for _, want := range []string{"sunshine_name = desk", "origin_web_ui_allowed = pc", "credentials_file = " + filepath.Join(s.Dir, "credentials.json"),
		"lan_encryption_mode = 0", "file_apps = " + filepath.Join(s.Dir, "apps.json")} {
		if !strings.Contains(string(conf), want+"\n") {
			t.Errorf("config lacks %q:\n%s", want, conf)
		}
	}
	var run string
	for _, c := range *calls {
		if strings.HasPrefix(c, "systemd-run") {
			run = c
		}
	}
	for _, want := range []string{"--unit=" + Unit, "--setenv=WAYLAND_DISPLAY=wayland-1", "-- /usr/bin/sunshine " + filepath.Join(s.Dir, "sunshine.conf")} {
		if !strings.Contains(run, want) {
			t.Errorf("no %q in %q", want, run)
		}
	}
	for _, c := range *calls {
		if strings.Contains(c, "--creds") {
			t.Errorf("credentials on a command line: %q", c)
		}
	}
	for _, f := range []string{"api.json", "credentials.json", "key.pem"} {
		if st, err := os.Stat(filepath.Join(s.Dir, f)); err != nil || st.Mode().Perm() != 0600 {
			t.Errorf("%s: %v %v", f, st.Mode(), err)
		}
	}
	if st := s.Status(t.Context()); st.Need != NeedNothing || !st.Running || st.Version != "2026.914.233613" {
		t.Errorf("status = %+v", st)
	}
	if err := s.SetOn(t.Context(), false); err != nil || s.On() {
		t.Errorf("off: %v, on %v", err, s.On())
	}
}

func TestPairingADeviceByItsPIN(t *testing.T) {
	s, f, _ := newSharing(t)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending, err := s.Pending(t.Context())
	if err != nil || len(pending) != 1 || pending[0].Name != "laptop" {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	if err := s.Pair(t.Context(), pending[0], "1111", "laptop"); !errors.Is(err, ErrPIN) {
		t.Errorf("a wrong PIN: %v", err)
	}
	if err := s.Pair(t.Context(), pending[0], "4821", "Ann's laptop"); err != nil {
		t.Fatal(err)
	}
	clients, err := s.Clients(t.Context())
	if err != nil || len(clients) != 1 || clients[0].Name != "Ann's laptop" {
		t.Fatalf("clients = %+v, %v", clients, err)
	}
	if err := s.Unpair(t.Context(), clients[0].UUID); err != nil {
		t.Fatal(err)
	}
	if clients, _ := s.Clients(t.Context()); len(clients) != 0 {
		t.Errorf("unpaired, still %+v", clients)
	}
	_ = f
}

// Sunshine refuses a certificate it holds twice: a device paired again,
// even under another name, could never connect. Every copy goes, and the
// device pairs again.
func TestPairingAgainRemovesEveryCopy(t *testing.T) {
	s, f, _ := newSharing(t)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	p := Pairing{ID: "0123456789abcdef0123456789abcdef", Name: "lyric-osx"}
	f.cert = clientCert(t)
	if err := s.Pair(t.Context(), p, "4821", "nest"); err != nil {
		t.Fatal(err)
	}
	if twice := s.Twice(); len(twice) != 0 {
		t.Errorf("one pairing, yet twice: %q", twice)
	}
	err := s.Pair(t.Context(), p, "4821", "lyric-osx")
	var before *PairedBeforeError
	if !errors.As(err, &before) || before.Name != "lyric-osx" || !slices.Equal(before.Before, []string{"nest"}) {
		t.Fatalf("paired again: %v", err)
	}
	if clients, _ := s.Clients(t.Context()); len(clients) != 0 {
		t.Errorf("copies left: %+v", clients)
	}
	if err := s.Pair(t.Context(), p, "4821", "lyric-osx"); err != nil {
		t.Fatalf("pairing once more: %v", err)
	}
	if clients, _ := s.Clients(t.Context()); len(clients) != 1 || clients[0].Name != "lyric-osx" {
		t.Errorf("after pairing once more: %+v", clients)
	}

	// Another device is left alone.
	f.cert = clientCert(t)
	if err := s.Pair(t.Context(), p, "4821", "phone"); err != nil {
		t.Fatal(err)
	}
	if clients, _ := s.Clients(t.Context()); len(clients) != 2 {
		t.Errorf("two devices: %+v", clients)
	}
}

func TestTwiceFindsADevicePairedTwice(t *testing.T) {
	s, f, _ := newSharing(t)
	cert, other := clientCert(t), clientCert(t)
	f.devices = []device{{Name: "nest", UUID: "a", Cert: cert}, {Name: "phone", UUID: "b", Cert: other}, {Name: "lyric-osx", UUID: "c", Cert: cert}}
	f.save()
	if twice := s.Twice(); !slices.Equal(twice, []string{"nest", "lyric-osx"}) {
		t.Errorf("twice = %q", twice)
	}
}

func TestUnpairByName(t *testing.T) {
	s, f, _ := newSharing(t)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.devices = []device{{Name: "laptop", UUID: "a"}, {Name: "Laptop", UUID: "b"}, {Name: "phone", UUID: "c"}}
	if n, err := s.UnpairNamed(t.Context(), "LAPTOP"); err != nil || n != 2 {
		t.Errorf("unpair laptop: %d, %v", n, err)
	}
	if n, err := s.UnpairNamed(t.Context(), "tv"); err != nil || n != 0 {
		t.Errorf("unpair a name none has: %d, %v", n, err)
	}
	if n, err := s.UnpairNamed(t.Context(), "all"); err != nil || n != 1 {
		t.Errorf("unpair all: %d, %v", n, err)
	}
	if err := s.Unpair(t.Context(), "gone"); err == nil {
		t.Error("unpairing a device not paired said nothing")
	}
}

// A Sunshine already running, or still trying its encoders, is waited for:
// starting it again would make a second unit of the same name.
func TestStartingWhileSunshineStartsWaitsForIt(t *testing.T) {
	s, f, calls := newSharing(t)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"active", "activating"} {
		f.unit = state
		if err := s.SetOn(t.Context(), true); err != nil {
			t.Errorf("%s: %v", state, err)
		}
		if err := s.AtLogin(t.Context()); err != nil {
			t.Errorf("at login, %s: %v", state, err)
		}
	}
	n := 0
	for _, c := range *calls {
		if strings.HasPrefix(c, "systemd-run") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("Sunshine started %d times", n)
	}
}

// On Hyprland, Sunshine captures the screen through screencopy: the
// desktop portal's picker would wait for a click on a computer nobody is
// at.
func TestSunshineCapturesHyprlandWithoutThePortal(t *testing.T) {
	s, _, _ := newSharing(t)
	s.Getenv = func(k string) string {
		return map[string]string{"WAYLAND_DISPLAY": "wayland-1", "HYPRLAND_INSTANCE_SIGNATURE": "abc"}[k]
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	conf, _ := os.ReadFile(filepath.Join(s.Dir, "sunshine.conf"))
	if !strings.Contains(string(conf), "\ncapture = wlr\n") {
		t.Errorf("Hyprland, no capture = wlr:\n%s", conf)
	}

	s, _, _ = newSharing(t) // another desktop: Sunshine chooses
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if conf, _ := os.ReadFile(filepath.Join(s.Dir, "sunshine.conf")); strings.Contains(string(conf), "capture") {
		t.Errorf("not Hyprland, yet a capture:\n%s", conf)
	}
}

func TestSunshineCapturesOnARealGPU(t *testing.T) {
	s, _, _ := newSharing(t)
	s.RenderNode = func() string { return "/dev/dri/renderD129" }
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	conf, _ := os.ReadFile(filepath.Join(s.Dir, "sunshine.conf"))
	if !strings.Contains(string(conf), "\nadapter_name = /dev/dri/renderD129\n") {
		t.Errorf("a real GPU, no adapter_name:\n%s", conf)
	}

	s, _, _ = newSharing(t) // an emulated screen only: Sunshine picks
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if conf, _ := os.ReadFile(filepath.Join(s.Dir, "sunshine.conf")); strings.Contains(string(conf), "adapter_name") {
		t.Errorf("no real GPU, yet an adapter_name:\n%s", conf)
	}
}

func TestTheRenderNodeIsTheFirstRealGPU(t *testing.T) {
	drm := t.TempDir()
	for _, dev := range []struct{ node, driver string }{
		{"renderD128", "virtio-pci"}, {"renderD129", "i915"}, {"card1-Virtual-1", ""}, {"version", ""},
	} {
		dir := filepath.Join(drm, dev.node, "device")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "uevent"), []byte("DRIVER="+dev.driver+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := renderNodeIn(drm); got != "/dev/dri/renderD129" {
		t.Errorf("render node = %q, want the passed-through GPU", got)
	}

	emu := t.TempDir() // a machine with an emulated screen and nothing else
	dir := filepath.Join(emu, "renderD128", "device")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "uevent"), []byte("DRIVER=virtio-pci\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := renderNodeIn(emu); got != "" {
		t.Errorf("render node = %q, want none", got)
	}
	if got := renderNodeIn(filepath.Join(emu, "nowhere")); got != "" {
		t.Errorf("render node = %q without sysfs, want none", got)
	}
}

func TestTheAPIIsTrustedByInuasCertificateOnly(t *testing.T) {
	s, _, _ := newSharing(t)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Another certificate in inua's place: the server is not the Sunshine inua set up.
	os.Remove(filepath.Join(s.Dir, "cert.pem"))
	writeCert(filepath.Join(s.Dir, "cert.pem"), filepath.Join(s.Dir, "other.key"))
	if _, err := s.Clients(t.Context()); err == nil {
		t.Error("trusted a server with another certificate")
	}
}

func TestStatusSaysWhatIsMissing(t *testing.T) {
	s, _, _ := newSharing(t)
	s.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if st := s.Status(t.Context()); st.Need != NeedSunshine {
		t.Errorf("no Sunshine: %+v", st)
	}
	s, _, _ = newSharing(t)
	s.Access = func(string) error { return os.ErrPermission }
	if st := s.Status(t.Context()); st.Need != NeedInput {
		t.Errorf("no uinput: %+v", st)
	}
	for out, want := range map[string]string{
		"Sunshine version: v0.23.1\n": "0.23.1",
		"[2026-10-18 08:54:27.214]: Info: Sunshine version: 2026.914.233613 commit: 63d35f7\n": "2026.914.233613",
		"no version here": "",
	} {
		if v := version(out); v != want {
			t.Errorf("version(%q) = %q, want %q", out, v, want)
		}
	}
}

func TestTheDeviceToPairIsChosen(t *testing.T) {
	laptop := Pairing{ID: "aa", Name: "laptop", Address: "192.168.1.9"}
	phone := Pairing{ID: "bb", Name: "Phone", Address: "192.168.1.7"}
	if _, err := Choose(nil, ""); !errors.Is(err, ErrNoDevice) {
		t.Errorf("none waiting: %v", err)
	}
	if p, err := Choose([]Pairing{laptop}, ""); err != nil || p != laptop {
		t.Errorf("the one waiting: %+v %v", p, err)
	}
	var many *ManyError
	if _, err := Choose([]Pairing{laptop, phone}, ""); !errors.As(err, &many) || len(many.Waiting) != 2 {
		t.Errorf("two waiting, none named: %v", err)
	}
	for _, which := range []string{"phone", "192.168.1.7", "BB"} {
		if p, err := Choose([]Pairing{laptop, phone}, which); err != nil || p != phone {
			t.Errorf("%q: %+v %v", which, p, err)
		}
	}
	if _, err := Choose([]Pairing{laptop}, "tv"); !errors.Is(err, ErrNoDevice) {
		t.Errorf("a name no device has: %v", err)
	}
}

func TestAwaitWaitsForTheDevice(t *testing.T) {
	s, _, _ := newSharing(t)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	p, err := s.Await(t.Context(), "laptop")
	if err != nil || p.Name != "laptop" {
		t.Fatalf("await: %+v %v", p, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 700*time.Millisecond)
	defer cancel()
	if _, err := s.Await(ctx, "tv"); !errors.Is(err, ErrNoDevice) {
		t.Errorf("a device that never comes: %v", err)
	}
}
