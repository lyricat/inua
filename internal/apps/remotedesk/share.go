// Package remotedesk is the Remote Desktop app's logic, without GTK:
// sharing this desktop through Sunshine (its configuration, its unit, its
// pairing API), and the machines the viewer connects to (found on the
// network, on the tailnet, or saved). The streams themselves are
// internal/gamestream.
package remotedesk

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/lyricat/inua/internal/hypr"
)

// Unit is the systemd user unit Sunshine runs in while the desktop is
// shared.
const Unit = "inua-sharing.service"

// APIPort is Sunshine's web API, on this computer only: its HTTP port + 1.
const APIPort = 47990

// Sharing shares this desktop: Sunshine, run with a configuration inua
// writes in Dir, driven through its web API with credentials inua made.
type Sharing struct {
	Dir      string
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, name string, args ...string) ([]byte, error)
	Getenv   func(string) string
	Access   func(path string) error // whether the user can write path
	API      string                  // https://127.0.0.1:47990
	Hostname func() (string, error)
	// Instance is the Hyprland running for this user, for a command run
	// outside the session (over SSH).
	Instance func() (hypr.Instance, error)
	// RenderNode is the GPU Sunshine captures and encodes on, by its render
	// node ("/dev/dri/renderD129"), or "" to let Sunshine pick. Sunshine
	// allocates the buffers of its Wayland capture on that node: a VM's
	// emulated screen (virtio-gpu) cannot allocate renderable ones, so every
	// frame fails there and leaks a descriptor until Sunshine cannot open
	// anything, and the viewer keeps a black screen. A passed-through GPU
	// can, and encodes with it (VA-API).
	RenderNode func() string
}

// NewSharing is the sharing of the user's session.
func NewSharing() *Sharing {
	return &Sharing{
		Dir: filepath.Join(configHome(), "inua", "sharing"), LookPath: exec.LookPath, Run: run, Getenv: os.Getenv,
		Access: func(p string) error { return unix.Access(p, unix.W_OK) },
		API:    fmt.Sprintf("https://127.0.0.1:%d", APIPort), Hostname: os.Hostname,
		Instance:   func() (hypr.Instance, error) { return hypr.FindInstance(os.Getenv("XDG_RUNTIME_DIR")) },
		RenderNode: defaultRenderNode,
	}
}

// emulatedDrivers are the GPUs a machine shows to be a screen, not to
// render with: they have a render node, and cannot allocate the buffers
// Sunshine's capture asks them for. A virtual machine's screen
// reaches sysfs as its bus driver: virtio-gpu's render node is under
// virtio-pci.
var emulatedDrivers = []string{"virtio-pci", "bochs-drm", "cirrus", "qxl", "vmwgfx", "vkms", "vgem"}

// defaultRenderNode is the first render node that is a real GPU, or "".
func defaultRenderNode() string { return renderNodeIn("/sys/class/drm") }

// renderNodeIn finds that node among the render nodes in drm, the sysfs
// class of DRM devices, in name order (renderD128 before renderD129).
func renderNodeIn(drm string) string {
	entries, err := os.ReadDir(drm)
	if err != nil {
		return ""
	}
	var nodes []string
	for _, e := range entries {
		if name := e.Name(); strings.HasPrefix(name, "renderD") {
			nodes = append(nodes, name)
		}
	}
	slices.Sort(nodes)
	for _, node := range nodes {
		if !slices.Contains(emulatedDrivers, driverOf(filepath.Join(drm, node))) {
			return "/dev/dri/" + node
		}
	}
	return ""
}

// driverOf is the kernel driver behind a DRM node, from its device's
// uevent ("DRIVER=i915"), or "" when it cannot be read.
func driverOf(node string) string {
	data, err := os.ReadFile(filepath.Join(node, "device", "uevent"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if driver, ok := strings.CutPrefix(line, "DRIVER="); ok {
			return strings.TrimSpace(driver)
		}
	}
	return ""
}

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, bytes.TrimSpace(out))
	}
	return out, nil
}

// Need is something sharing needs that is not there.
type Need int

const (
	NeedNothing  Need = iota
	NeedSunshine      // Sunshine is not installed
	NeedInput         // /dev/uinput is not the user's: the remote side's keys and mouse cannot reach this desktop
	NeedDisplay       // no graphical session: not in one, and none running
)

// Status is what sharing stands at.
type Status struct {
	Need    Need
	Version string // Sunshine's
	Running bool
}

// Uinput is the device Sunshine makes its keyboard and mouse with.
const Uinput = "/dev/uinput"

// UinputRule is the udev rule Sunshine's packages install: the device is
// the logged-in user's.
const UinputRule = `KERNEL=="uinput", SUBSYSTEM=="misc", OPTIONS+="static_node=uinput", TAG+="uaccess"`

// Status checks what sharing needs, and whether it runs.
func (s *Sharing) Status(ctx context.Context) Status {
	var st Status
	bin, err := s.LookPath("sunshine")
	if err != nil {
		st.Need = NeedSunshine
		return st
	}
	if out, err := s.Run(ctx, bin, "--version"); err == nil {
		st.Version = version(string(out))
	}
	if s.Access(Uinput) != nil {
		st.Need = NeedInput
	} else if _, err := s.session(); err != nil {
		st.Need = NeedDisplay
	}
	st.Running = s.unitState(ctx) == "active"
	return st
}

// version finds the version in what `sunshine --version` prints: the
// word after "version:" in its log line.
func version(out string) string {
	fields := strings.Fields(out)
	for i, f := range fields {
		if strings.EqualFold(f, "version:") && i+1 < len(fields) {
			return strings.TrimPrefix(fields[i+1], "v")
		}
	}
	return ""
}

// The files inua keeps Sunshine's state in.
func (s *Sharing) file(name string) string { return filepath.Join(s.Dir, name) }

// credentials are the API's user and password, inua's alone.
type credentials struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

// prepare writes Sunshine's configuration for the session env, its apps
// (the desktop only), the API's credentials as Sunshine reads them, and a
// certificate for its API that inua pins. What is there already is kept:
// paired devices live in Sunshine's state file.
func (s *Sharing) prepare(env map[string]string) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	name, err := s.Hostname()
	if err != nil || name == "" {
		name = "inua"
	}
	var capture []string
	if env["HYPRLAND_INSTANCE_SIGNATURE"] != "" {
		// Hyprland's screencopy needs no one to answer. Left to choose,
		// Sunshine asks the desktop portal, whose picker waits for a click
		// that a computer without its user never gets.
		capture = []string{"capture = wlr"}
	}
	var adapter []string
	if s.RenderNode != nil {
		if node := s.RenderNode(); node != "" {
			adapter = []string{"adapter_name = " + node}
		}
	}
	conf := strings.Join(slices.Concat([]string{
		"sunshine_name = " + name,
		"origin_web_ui_allowed = pc", // the API answers this computer only
		"file_apps = " + s.file("apps.json"),
		"file_state = " + s.file("state.json"),
		"credentials_file = " + s.file("credentials.json"),
		"pkey = " + s.file("key.pem"),
		"cert = " + s.file("cert.pem"),
		"log_path = " + s.file("sunshine.log"),
		"lan_encryption_mode = 0", // the viewer encrypts input, not video, on the local network
		"wan_encryption_mode = 1",
	}, capture, adapter, []string{""}), "\n")
	if err := os.WriteFile(s.file("sunshine.conf"), []byte(conf), 0600); err != nil {
		return err
	}
	apps := `{"env": {}, "apps": [{"name": "Desktop", "image-path": "desktop.png"}]}` + "\n"
	if err := os.WriteFile(s.file("apps.json"), []byte(apps), 0600); err != nil {
		return err
	}
	if _, err := os.Stat(s.file("cert.pem")); err != nil {
		if err := writeCert(s.file("cert.pem"), s.file("key.pem")); err != nil {
			return err
		}
	}
	if _, err := s.creds(); err != nil {
		return s.newCreds()
	}
	return nil
}

func (s *Sharing) creds() (credentials, error) {
	var c credentials
	data, err := os.ReadFile(s.file("api.json"))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil || c.User == "" || c.Password == "" {
		return c, errors.New("sharing: no API credentials")
	}
	return c, nil
}

// newCreds makes the API's credentials: inua's copy, and Sunshine's
// (its user, a salt, and the hash of the password and salt, written as
// Sunshine writes it: hex, upper case, the hash's bytes last to first).
// Writing them here keeps the password off any command line.
func (s *Sharing) newCreds() error {
	c := credentials{User: "inua", Password: randomString(24)}
	salt := randomString(16)
	sum := sha256.Sum256([]byte(c.Password + salt))
	slices.Reverse(sum[:])
	sunshine, _ := json.Marshal(map[string]string{
		"username": c.User, "salt": salt, "password": strings.ToUpper(hex.EncodeToString(sum[:])),
	})
	mine, _ := json.Marshal(c)
	if err := os.WriteFile(s.file("credentials.json"), sunshine, 0600); err != nil {
		return err
	}
	return os.WriteFile(s.file("api.json"), mine, 0600)
}

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// writeCert makes the certificate Sunshine serves its API and GameStream
// with.
func writeCert(certPath, keyPath string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "Sunshine Gamestream Host"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(20, 0, 0),
		SignatureAlgorithm: x509.SHA256WithRSA,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		return err
	}
	return os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
}

// ErrNoSession is no graphical session to share: the command runs
// outside one (over SSH), and no Hyprland is running for this user.
var ErrNoSession = errors.New("sharing: no desktop session is running on this computer; log in to it first")

// sessionEnv are the variables Sunshine takes from the session: its
// display, and its bus for the screen and the sound.
var sessionEnv = []string{"WAYLAND_DISPLAY", "DISPLAY", "XDG_RUNTIME_DIR", "XDG_SESSION_TYPE", "XDG_CURRENT_DESKTOP",
	"HYPRLAND_INSTANCE_SIGNATURE", "DBUS_SESSION_BUS_ADDRESS", "PULSE_SERVER", "PATH"}

// session is the environment of the session to share: this process's,
// when it runs in one, or else the running Hyprland's. Without either,
// Sunshine would start with nothing to capture.
func (s *Sharing) session() (map[string]string, error) {
	env := map[string]string{}
	for _, k := range sessionEnv {
		if v := s.Getenv(k); v != "" {
			env[k] = v
		}
	}
	if env["WAYLAND_DISPLAY"] != "" || env["DISPLAY"] != "" {
		return env, nil
	}
	i, err := s.Instance()
	if err != nil {
		return nil, ErrNoSession
	}
	env["WAYLAND_DISPLAY"] = i.Wayland
	env["XDG_RUNTIME_DIR"] = i.Runtime
	env["HYPRLAND_INSTANCE_SIGNATURE"] = i.Signature
	env["XDG_SESSION_TYPE"] = "wayland"
	env["XDG_CURRENT_DESKTOP"] = "Hyprland"
	if env["DBUS_SESSION_BUS_ADDRESS"] == "" {
		env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=" + filepath.Join(i.Runtime, "bus")
	}
	return env, nil
}

// StartTimeout is how long Sunshine may take to start: it tries the
// encoders on the screen first, which took most of a minute on one
// computer.
const StartTimeout = 120 * time.Second

// unitState is the unit's state as systemctl names it: active,
// activating, failed, inactive…
func (s *Sharing) unitState(ctx context.Context) string {
	out, _ := s.Run(ctx, "systemctl", "--user", "is-active", Unit)
	return strings.TrimSpace(string(out))
}

// running says whether Sunshine runs or is starting: it tries the
// encoders first, which can take most of a minute.
func running(state string) bool {
	return state == "active" || state == "activating" || state == "reloading"
}

// Start shares the desktop: Sunshine runs as a user unit with the
// session's display, and Start returns once its API answers. A Sunshine
// already running or starting is waited for.
func (s *Sharing) Start(ctx context.Context) error {
	bin, err := s.LookPath("sunshine")
	if err != nil {
		return err
	}
	env, err := s.session()
	if err != nil {
		return err
	}
	if !running(s.unitState(ctx)) {
		if err := s.prepare(env); err != nil {
			return err
		}
		s.Run(ctx, "systemctl", "--user", "reset-failed", Unit)
		args := []string{"--user", "--unit=" + Unit, "--description=inua desktop sharing (Sunshine)", "--collect", "--quiet",
			"--property=Restart=on-failure", "--property=RestartSec=3"}
		// The unit starts in the user manager's environment, not the session's.
		for _, k := range sessionEnv {
			if v := env[k]; v != "" {
				args = append(args, "--setenv="+k+"="+v)
			}
		}
		args = append(args, "--", bin, s.file("sunshine.conf"))
		if _, err := s.Run(ctx, "systemd-run", args...); err != nil {
			return err
		}
	}
	for {
		if _, err := s.Clients(ctx); err == nil {
			return nil
		}
		if s.unitState(ctx) == "failed" {
			return fmt.Errorf("sharing: Sunshine stopped: %s", s.LastLog())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Stop ends the sharing: Sunshine stops, and what is connected with it.
func (s *Sharing) Stop(ctx context.Context) error {
	out, err := s.Run(ctx, "systemctl", "--user", "stop", Unit)
	if err != nil && !strings.Contains(string(out), "not loaded") {
		return err
	}
	return nil
}

// LastLog is the last line of Sunshine's log that says something went
// wrong, or its last line.
func (s *Sharing) LastLog() string {
	data, _ := os.ReadFile(s.file("sunshine.log"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-40; i-- {
		if strings.Contains(lines[i], "Error:") || strings.Contains(lines[i], "Fatal:") {
			return strings.TrimSpace(lines[i])
		}
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// api asks Sunshine's API, trusting only the certificate inua made for it.
func (s *Sharing) api(ctx context.Context, method, path string, body any, out any) error {
	c, err := s.creds()
	if err != nil {
		return err
	}
	certPEM, err := os.ReadFile(s.file("cert.pem"))
	if err != nil {
		return err
	}
	b, _ := pem.Decode(certPEM)
	if b == nil {
		return errors.New("sharing: the API's certificate is not PEM")
	}
	pinned := b.Bytes
	var payload io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.API+path, payload)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.User, c.Password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true, // a self-signed certificate, checked below against inua's
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 || !bytes.Equal(raw[0], pinned) {
				return errors.New("sharing: the API's certificate is not the one inua made")
			}
			return nil
		},
	}}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("sharing: Sunshine refused inua's credentials")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sharing: %s %s: %d %s", method, path, resp.StatusCode, bytes.TrimSpace(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Pairing is a device waiting to pair: its user typed this computer's
// address, and their screen shows a PIN to type here.
type Pairing struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

// Pending are the devices waiting to pair.
func (s *Sharing) Pending(ctx context.Context) ([]Pairing, error) {
	var r struct {
		Pairings []Pairing `json:"pairings"`
	}
	err := s.api(ctx, http.MethodGet, "/api/pin", nil, &r)
	return r.Pairings, err
}

// ErrPIN is a PIN the pairing device did not show.
var ErrPIN = errors.New("sharing: that is not the PIN the device shows")

// PairedBeforeError is a device that was paired here already, under the
// names Before. Sunshine refuses a certificate it holds twice, so the
// pairing failed on the device; every copy is unpaired, and the device
// pairs again.
type PairedBeforeError struct {
	Name   string
	Before []string
}

func (e *PairedBeforeError) Error() string {
	return fmt.Sprintf("sharing: %s was paired here before, as %s; that pairing is removed: pair again from %s",
		e.Name, strings.Join(e.Before, ", "), e.Name)
}

// recordWait is how long Sunshine may take to record a device after its
// PIN: the device answers its challenges first, in milliseconds.
var recordWait = 5 * time.Second

// Pair gives a waiting device the PIN its screen shows, and the name it
// is to be known by here. A device paired here before is a
// *PairedBeforeError.
func (s *Sharing) Pair(ctx context.Context, p Pairing, pin, name string) error {
	known := map[string]bool{}
	for _, d := range s.devices() {
		known[d.UUID] = true
	}
	var r struct {
		Status bool `json:"status"`
	}
	if err := s.api(ctx, http.MethodPost, "/api/pin", map[string]string{"pairing_id": p.ID, "pin": pin, "name": name}, &r); err != nil {
		return err
	}
	if !r.Status {
		return ErrPIN
	}
	// The device is recorded once it has answered; then its certificate
	// tells whether it was here before.
	deadline := time.Now().Add(recordWait)
	for time.Now().Before(deadline) {
		ds := s.devices()
		i := slices.IndexFunc(ds, func(d device) bool { return !known[d.UUID] })
		if i < 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		added := ds[i]
		var before []string
		for _, d := range ds {
			if d.UUID != added.UUID && sameCert(d.Cert, added.Cert) {
				before = append(before, d.Name)
				if err := s.Unpair(ctx, d.UUID); err != nil {
					return err
				}
			}
		}
		if len(before) == 0 {
			return nil
		}
		if err := s.Unpair(ctx, added.UUID); err != nil {
			return err
		}
		return &PairedBeforeError{Name: name, Before: before}
	}
	return nil
}

// Client is a paired device.
type Client struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	Enabled bool   `json:"enabled"`
}

// Clients are the paired devices.
func (s *Sharing) Clients(ctx context.Context) ([]Client, error) {
	var r struct {
		Clients []Client `json:"named_certs"`
	}
	err := s.api(ctx, http.MethodGet, "/api/clients/list", nil, &r)
	return r.Clients, err
}

// Unpair forgets a paired device: it must pair again to connect. It
// takes effect at once.
func (s *Sharing) Unpair(ctx context.Context, uuid string) error {
	var r struct {
		Status bool `json:"status"`
	}
	if err := s.api(ctx, http.MethodPost, "/api/clients/unpair", map[string]string{"uuid": uuid}, &r); err != nil {
		return err
	}
	if !r.Status {
		return fmt.Errorf("sharing: Sunshine did not unpair %s", uuid)
	}
	return nil
}

// UnpairNamed forgets the paired devices called name (in any case), or
// every one for "all", and says how many went.
func (s *Sharing) UnpairNamed(ctx context.Context, name string) (int, error) {
	clients, err := s.Clients(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range clients {
		if name != "all" && !strings.EqualFold(c.Name, name) {
			continue
		}
		if err := s.Unpair(ctx, c.UUID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// device is a paired device as Sunshine keeps it in its state file, with
// its certificate.
type device struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	Cert string `json:"cert"`
}

// devices are the paired devices in Sunshine's state file. Its API does
// not give their certificates.
func (s *Sharing) devices() []device {
	var st struct {
		Root struct {
			Devices []device `json:"named_devices"`
		} `json:"root"`
	}
	data, err := os.ReadFile(s.file("state.json"))
	if err != nil || json.Unmarshal(data, &st) != nil {
		return nil
	}
	return st.Root.Devices
}

// sameCert says whether two PEM certificates are the same one.
func sameCert(a, b string) bool {
	da, _ := pem.Decode([]byte(a))
	db, _ := pem.Decode([]byte(b))
	return da != nil && db != nil && bytes.Equal(da.Bytes, db.Bytes)
}

// Twice are the names of the devices paired more than once with the same
// certificate (perhaps under other names). Sunshine refuses a certificate
// it holds twice, so they cannot connect until every copy is unpaired and
// the device pairs again.
func (s *Sharing) Twice() []string {
	ds := s.devices()
	var out []string
	for i, d := range ds {
		for j, e := range ds {
			if i != j && sameCert(d.Cert, e.Cert) {
				out = append(out, d.Name)
				break
			}
		}
	}
	return out
}

// On says whether sharing is on: it starts again at login.
func (s *Sharing) On() bool {
	data, err := os.ReadFile(s.file("on"))
	return err == nil && strings.TrimSpace(string(data)) == "1"
}

// SetOn turns sharing on or off, now and at the next login.
func (s *Sharing) SetOn(ctx context.Context, on bool) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	flag := "0"
	if on {
		flag = "1"
	}
	if err := os.WriteFile(s.file("on"), []byte(flag+"\n"), 0600); err != nil {
		return err
	}
	if on {
		return s.Start(ctx)
	}
	return s.Stop(ctx)
}

// AtLogin starts sharing when it is on and not running: the shell calls
// it once the session's display is there.
func (s *Sharing) AtLogin(ctx context.Context) error {
	if !s.On() || running(s.unitState(ctx)) {
		return nil
	}
	return s.Start(ctx)
}

// VirtualModes are the virtual screen's sizes to choose from.
var VirtualModes = []string{"1280x720@60", "1920x1080@60", "2560x1440@60", "3840x2160@60"}

// DefaultVirtualMode is the virtual screen's size unless another is chosen.
const DefaultVirtualMode = "1920x1080@60"

// Virtual is the virtual screen's mode ("1920x1080@60"), or "" when there
// is none: the user turns it on for a computer without a monitor, so it
// has a screen to share.
func (s *Sharing) Virtual() string {
	data, err := os.ReadFile(s.file("virtual"))
	if err != nil {
		return ""
	}
	mode := strings.TrimSpace(string(data))
	if _, _, _, ok := hypr.ParseMode(mode); !ok {
		return ""
	}
	return mode
}

// SetVirtual turns the virtual screen on at mode, or off with "". It is
// kept for the next login; SyncVirtual makes it so now.
func (s *Sharing) SetVirtual(mode string) error {
	if mode != "" {
		if _, _, _, ok := hypr.ParseMode(mode); !ok {
			return fmt.Errorf("sharing: virtual screen %q: want WIDTHxHEIGHT@HZ, like %s", mode, DefaultVirtualMode)
		}
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(s.file("virtual"), []byte(mode+"\n"), 0600)
}

// SyncVirtual makes the session's outputs follow the setting: the virtual
// screen is there while it is on and no monitor is connected, and gone
// otherwise (hypr.SyncVirtualScreen). Sunshine looks for screens and
// encoders as it starts, so a running Sunshine is started again when the
// screen comes or goes. The error is ErrNoSession without a session.
func (s *Sharing) SyncVirtual(ctx context.Context) error {
	env, err := s.session()
	if err != nil {
		return err
	}
	sig := env["HYPRLAND_INSTANCE_SIGNATURE"]
	if sig == "" {
		if s.Virtual() == "" {
			return nil // another desktop, and nothing asked of it
		}
		return errors.New("sharing: a virtual screen needs Hyprland")
	}
	runtime := cmp.Or(env["XDG_RUNTIME_DIR"], fmt.Sprintf("/run/user/%d", os.Getuid()))
	client := hypr.NewClient(hypr.Instance{Signature: sig, Runtime: runtime}.Paths())
	changed, err := client.SyncVirtualScreen(ctx, s.Virtual())
	if err != nil || !changed {
		return err
	}
	if !running(s.unitState(ctx)) {
		return nil
	}
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx)
}

// VirtualAtStart makes the virtual screen, when it is on, in the Hyprland
// that the process parent starts: inua-session, become start-hyprland.
// Hyprland runs its exec-once lines only once a monitor shows a frame,
// so without a monitor nothing would start inua-shell, which makes the
// screen. It waits for Hyprland to answer, and gives up when parent is
// gone.
func (s *Sharing) VirtualAtStart(ctx context.Context, parent int) error {
	mode := s.Virtual()
	if mode == "" {
		return nil
	}
	for {
		i, err := hypr.InstanceOf(s.Getenv("XDG_RUNTIME_DIR"), parent)
		if err == nil {
			if _, err = hypr.NewClient(i.Paths()).SyncVirtualScreen(ctx, mode); err == nil {
				return nil
			}
		}
		if os.Getppid() != parent {
			return errors.New("sharing: the session ended before Hyprland started")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("sharing: virtual screen: %w", err)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// ErrNoDevice is no device waiting to pair, or none of the one named.
var ErrNoDevice = errors.New("sharing: no device is waiting to pair")

// ManyError is more than one device waiting, and none chosen.
type ManyError struct{ Waiting []Pairing }

func (e *ManyError) Error() string {
	var names []string
	for _, p := range e.Waiting {
		names = append(names, p.Name+" ("+p.Address+")")
	}
	return "sharing: several devices are waiting to pair: " + strings.Join(names, ", ")
}

// Choose is the device to pair from those waiting: the one there is, or
// the one which names (its name, address or ID, in any case).
func Choose(waiting []Pairing, which string) (Pairing, error) {
	if which == "" {
		switch len(waiting) {
		case 0:
			return Pairing{}, ErrNoDevice
		case 1:
			return waiting[0], nil
		}
		return Pairing{}, &ManyError{Waiting: waiting}
	}
	var found []Pairing
	for _, p := range waiting {
		if strings.EqualFold(p.Name, which) || p.Address == which || strings.EqualFold(p.ID, which) {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		return Pairing{}, ErrNoDevice
	case 1:
		return found[0], nil
	}
	return Pairing{}, &ManyError{Waiting: found}
}

// Await is the device to pair, as Choose finds it, once it is waiting:
// it asks every half second until ctx ends.
func (s *Sharing) Await(ctx context.Context, which string) (Pairing, error) {
	for {
		waiting, err := s.Pending(ctx)
		if err != nil {
			return Pairing{}, err
		}
		p, err := Choose(waiting, which)
		if !errors.Is(err, ErrNoDevice) {
			return p, err
		}
		select {
		case <-ctx.Done():
			return Pairing{}, ErrNoDevice
		case <-time.After(500 * time.Millisecond):
		}
	}
}
