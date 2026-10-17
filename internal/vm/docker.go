// Package vm contains toolkit-free machine configuration, host checks,
// resource recommendations and the backends that run a guest.
package vm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	dockerComposeFile = "compose.yaml"
	dockerGuestEnv    = ".guest.env"
	dockerPortsFile   = "ports"
	// dockerImage is pinned to a version and to that version's manifest
	// digest. The container gets /dev/kvm, /dev/net/tun and NET_ADMIN, so an
	// image that changed under its tag would run with those rights without
	// anyone choosing the new version; updating it is deliberate.
	dockerImage = "dockurr/windows:6.06@sha256:16fd7c39a3e48bb946d29c59dc6015942e5b900a1fa5d7e5552053bfc48ee2ea"
)

// Docker Compose passes PASSWORD to daemon container metadata. Docker access
// is root-equivalent; users must trust the daemon.

// CommandRunner is injectable so backend behavior can be exercised without Docker.
type CommandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

// GuestSecretStore keeps a guest's password where the rest of the user's
// secrets are kept, never in the machine's folder. It is keyed by the machine,
// so the label the user sees in their keyring is the machine's own name
// (GuestSecretLabel) and a machine renamed or adopted keeps one secret.
type GuestSecretStore interface {
	Store(ctx context.Context, m Machine, value []byte) error
	Load(ctx context.Context, m Machine) ([]byte, error)
	Delete(ctx context.Context, m Machine) error
}

// GuestSecretLabel is the name a machine's password is kept under in the
// Secret Service: what the user sees in their keyring.
func GuestSecretLabel(m Machine) string { return "inua-vm · " + m.Name }

// GuestSecretAttrs is how a machine's password is found again.
func GuestSecretAttrs(m Machine) map[string]string {
	return map[string]string{"application": "inua", "kind": "vm", "machine": m.Name}
}

// LegacyGuestSecretAttrs is where step 1 kept the Windows guest's password.
// A machine installed then must keep the password its Windows was installed
// with, so that one is read once and moved to the machine's own label.
func LegacyGuestSecretAttrs(m Machine) map[string]string {
	return map[string]string{"application": "inua", "kind": "windows-vm", "id": "inua/windows/" + m.Name}
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, msg)
		}
	}
	return stdout.String(), err
}

// DockerBackend runs Windows machines with Docker (Dockur). It holds no
// state of its own: everything it needs is in the machine and in the store,
// so the same backend serves every machine, and two callers never fight over
// one.
type DockerBackend struct {
	Store   Store
	Runner  CommandRunner
	Secrets GuestSecretStore
}

func NewDockerBackend(store Store, runner CommandRunner) *DockerBackend {
	if runner == nil {
		runner = execCommandRunner{}
	}
	return &DockerBackend{Store: store, Runner: runner}
}

// Install writes the machine's Compose project and env file, and reserves the
// ports it publishes. Writing it again is how a changed memory, CPU count or
// disk size takes effect - and how the window's Apply does anything at all.
//
// The guest password is made only the first time and kept afterwards, since
// the installed Windows already has it. The disk is never touched.
func (b *DockerBackend) Install(ctx context.Context, m Machine) error {
	if err := b.check(m); err != nil {
		return err
	}
	dir := b.Store.Dir(m.Name)
	if err := safeOwnedDir(dir); err != nil {
		return vmError("install", ErrorInvalidConfig, err)
	}
	composePath := b.composePath(m)
	if err := rejectPathSymlinks(composePath); err != nil {
		return vmError("install", ErrorInvalidConfig, err)
	}
	if b.Secrets == nil {
		return vmError("install", ErrorUnavailable, errors.New("secure guest password storage is unavailable"))
	}
	// The guest's password: keep the one it was installed with, and make one
	// only when there is none yet.
	password, err := b.Secrets.Load(ctx, m)
	if err != nil {
		password, err = newGuestPassword()
		if err != nil {
			return vmError("install", ErrorBackend, err)
		}
		if err := b.Secrets.Store(ctx, m, password); err != nil {
			clear(password)
			return vmError("install", ErrorUnavailable, err)
		}
	}
	defer clear(password)

	rdpPort, consolePort, err := b.reservePorts(m)
	if err != nil {
		return vmError("install", ErrorConflict, err)
	}
	envPath := filepath.Join(dir, dockerGuestEnv)
	if err := writeAtomic(envPath, []byte("PASSWORD="+string(password)+"\n"), 0600); err != nil {
		return vmError("install", ErrorBackend, err)
	}
	content := fmt.Sprintf("services:\n  windows:\n    image: %s\n    container_name: inua-%s\n    environment:\n      VERSION: \"11\"\n      USERNAME: \"%s\"\n      RAM_SIZE: \"%dG\"\n      CPU_CORES: \"%d\"\n      DISK_SIZE: \"%dG\"\n    env_file:\n      - %s\n    devices:\n      - /dev/kvm\n      - /dev/net/tun\n    cap_add:\n      - NET_ADMIN\n    ports:\n      - \"127.0.0.1:%d:3389/tcp\"\n      - \"127.0.0.1:%d:3389/udp\"\n      - \"127.0.0.1:%d:8006\"\n    volumes:\n      - %s\n    restart: unless-stopped\n    stop_grace_period: 2m\n",
		dockerImage, m.Name, GuestUser, m.MemoryMiB/1024, m.CPUs, m.DiskGiB, yamlQuote(envPath), rdpPort, rdpPort, consolePort, yamlQuote(dir+":/storage"))
	if err := writeAtomic(composePath, []byte(content), 0600); err != nil {
		return vmError("install", ErrorBackend, err)
	}
	return nil
}

// Start brings the container up. It does not wait for Windows: on a first
// install that is tens of minutes of downloading and setup, and the caller
// needs to show progress and offer Stop meanwhile. Readiness is State's to
// report - it is what the running container's RDP port answers to.
func (b *DockerBackend) Start(ctx context.Context, m Machine) error {
	if err := b.check(m); err != nil {
		return err
	}
	if err := b.installed(m); err != nil {
		return vmError("start", ErrorConflict, err)
	}
	if b.Secrets == nil {
		return vmError("start", ErrorUnavailable, errors.New("secure guest password storage is unavailable"))
	}
	password, err := b.Secrets.Load(ctx, m)
	if err != nil {
		return vmError("start", ErrorUnavailable, err)
	}
	defer clear(password)
	envPath := filepath.Join(b.Store.Dir(m.Name), dockerGuestEnv)
	if err := writeAtomic(envPath, []byte("PASSWORD="+string(password)+"\n"), 0600); err != nil {
		return vmError("start", ErrorBackend, err)
	}
	if _, err := b.compose(ctx, m, "up", "-d"); err != nil {
		return vmError("start", ErrorBackend, err)
	}
	return nil
}

// State reports what the guest is doing.
//
// Running means Windows itself is up, which is not the same as the container
// being up: the container's RDP port answers long before Windows has
// installed, so a container that is running but whose guest is still
// installing is StateStarting. The distinction is what tells the window
// whether to offer Connect or to keep showing the installation.
func (b *DockerBackend) State(ctx context.Context, m Machine) (State, error) {
	if err := b.installed(m); err != nil {
		return StateUnknown, vmError("state", ErrorConflict, err)
	}
	out, err := b.compose(ctx, m, "ps", "--status", "running", "--services")
	if err != nil {
		return StateUnknown, vmError("state", ErrorBackend, err)
	}
	if !strings.Contains(out, "windows") {
		return StateStopped, nil
	}
	ready, err := b.guestReady(ctx, m)
	if err != nil {
		// The container is up; Windows is still installing or still booting.
		return StateStarting, nil
	}
	if ready {
		return StateRunning, nil
	}
	return StateStarting, nil
}

// Stop shuts the container down, keeping the disk and the project.
func (b *DockerBackend) Stop(ctx context.Context, m Machine) error {
	if err := b.check(m); err != nil {
		return err
	}
	if err := b.installed(m); err != nil {
		return vmError("stop", ErrorConflict, err)
	}
	if _, err := b.compose(ctx, m, "stop", "-t", "120"); err != nil {
		return vmError("stop", ErrorBackend, err)
	}
	return nil
}

// Display is how this machine is seen: over RDP once Windows is up, and in
// the container's web console before that - which is the only view there is
// while it installs.
func (b *DockerBackend) Display(ctx context.Context, m Machine) (Display, error) {
	rdpPort, consolePort, err := b.reservePorts(m)
	if err != nil {
		return Display{}, vmError("display", ErrorConflict, err)
	}
	d := Display{
		Kind:    DisplayWeb,
		Address: fmt.Sprintf("127.0.0.1:%d", rdpPort),
		Console: fmt.Sprintf("http://127.0.0.1:%d/", consolePort),
	}
	state, err := b.State(ctx, m)
	if err == nil && state == StateRunning {
		d.Kind = DisplayRDP
	}
	return d, nil
}

// RecentOutput is the last useful line of the container's logs, for the
// window to show while Windows is installing.
func (b *DockerBackend) RecentOutput(ctx context.Context, m Machine) string {
	logs, err := b.containerLogs(ctx, m, "12")
	if err != nil {
		return ""
	}
	return lastLogLine(logs)
}

// Remove takes the container and the project apart. With keepDisk the guest's
// disk and its password stay, and a later install reuses that Windows;
// without it the machine's own files go, and its definition stays so it can be
// installed again. That one is the caller's second, confirmed step.
func (b *DockerBackend) Remove(ctx context.Context, m Machine, keepDisk bool) error {
	if err := b.check(m); err != nil {
		return err
	}
	dir := b.Store.Dir(m.Name)
	if err := validateOwnedDataDir(dir); err != nil {
		return vmError("remove", ErrorInvalidConfig, err)
	}
	if err := rejectPathSymlinks(dir); err != nil {
		return vmError("remove", ErrorInvalidConfig, err)
	}
	if err := b.installed(m); err == nil {
		if _, err := b.compose(ctx, m, "down"); err != nil {
			return vmError("remove", ErrorBackend, err)
		}
	}
	for _, name := range []string{dockerComposeFile, dockerGuestEnv, dockerPortsFile} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return vmError("remove", ErrorBackend, err)
		}
	}
	if keepDisk {
		// The guest password stays too: this Windows still uses it.
		return nil
	}
	// The guest's files go, the machine's definition stays: the disk, the
	// install media it was made from and what Dockur keeps beside them.
	if err := os.RemoveAll(dir); err != nil {
		return vmError("remove", ErrorBackend, err)
	}
	if err := b.Store.Save(m); err != nil {
		return vmError("remove", ErrorBackend, err)
	}
	return nil
}

// reservePorts returns the loopback ports this machine publishes: RDP for
// Windows itself, and the container's web console. They are reserved when the
// machine is installed and recorded in its runtime directory, so a second
// machine - or a second Inua - cannot publish the same port by accident.
func (b *DockerBackend) reservePorts(m Machine) (rdp, console int, err error) {
	runtimeDir, err := b.Store.EnsureRuntime(m.Name)
	if err != nil {
		return 0, 0, err
	}
	taken := b.reservedByOthers(m.Name)
	path := filepath.Join(runtimeDir, dockerPortsFile)
	if data, err := os.ReadFile(path); err == nil {
		var haveRDP, haveConsole int
		if n, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d %d", &haveRDP, &haveConsole); n == 2 && err == nil &&
			portFree(haveRDP) && portFree(haveConsole) && !taken[haveRDP] && !taken[haveConsole] {
			return haveRDP, haveConsole, nil
		}
	}
	rdp, err = freePortFrom(3389, taken)
	if err != nil {
		return 0, 0, err
	}
	console, err = freePortFrom(8006, taken)
	if err != nil {
		return 0, 0, err
	}
	if err := writeAtomic(path, []byte(strconv.Itoa(rdp)+" "+strconv.Itoa(console)+"\n"), 0600); err != nil {
		return 0, 0, err
	}
	return rdp, console, nil
}

// reservedByOthers is every port another machine has recorded, so the machine
// being installed picks one no one else uses even before that one runs.
func (b *DockerBackend) reservedByOthers(name string) map[int]bool {
	out := map[int]bool{}
	entries, err := os.ReadDir(b.Store.Runtime)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == name {
			continue
		}
		data, err := os.ReadFile(filepath.Join(b.Store.RuntimeDir(e.Name()), dockerPortsFile))
		if err != nil {
			continue
		}
		var rdp, console int
		if n, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d %d", &rdp, &console); n == 2 && err == nil {
			out[rdp], out[console] = true, true
		}
	}
	return out
}

// freePortFrom is the first port at or above from that is free here and that
// no other machine has reserved.
func freePortFrom(from int, taken map[int]bool) (int, error) {
	for port := from; port < from+200 && port <= 65535; port++ {
		if taken[port] {
			continue
		}
		if portFree(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port at or above %d", from)
}

// Prepared reports whether this machine has been made: its Compose project is
// written. A machine without one has nothing to start yet.
func (b *DockerBackend) Prepared(m Machine) bool {
	return b.installed(m) == nil
}

// check says whether this backend runs this machine at all.
func (b *DockerBackend) check(m Machine) error {
	if m.Backend != BackendDockur {
		return vmError("backend", ErrorUnsupported, fmt.Errorf("%s: %w", m.Backend, ErrNotThisBackend))
	}
	if err := m.Validate(); err != nil {
		return err
	}
	return nil
}

// installed reports whether the machine's Compose project is actually there:
// a machine whose project was removed is not an installed guest.
func (b *DockerBackend) installed(m Machine) error {
	path := b.composePath(m)
	if err := rejectPathSymlinks(path); err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("the machine's Compose file is not a regular file")
	}
	return nil
}

func (b *DockerBackend) composePath(m Machine) string {
	return filepath.Join(b.Store.Dir(m.Name), dockerComposeFile)
}

func (b *DockerBackend) compose(ctx context.Context, m Machine, args ...string) (string, error) {
	if b.Runner == nil {
		b.Runner = execCommandRunner{}
	}
	// Pin the project name: Docker Compose otherwise derives it from the
	// directory the file sits in, so any other Compose project whose folder
	// and service are both named like this machine would be treated as this
	// one - and `up -d` would tear its container down.
	argv := []string{"compose", "-p", composeProject(m.Name), "-f", b.composePath(m)}
	argv = append(argv, args...)
	return b.Runner.Run(ctx, "docker", argv...)
}

// composeProject is the Compose project name for a machine, namespaced so it
// can never collide with a user's own Compose files.
func composeProject(name string) string {
	return "inua-vm-" + name
}

// guestReady reports whether Windows has said it started.
//
// The container's RDP port is open as soon as it starts, so it says nothing
// about the guest; Dockur prints "started successfully" when the system in it
// actually comes up, which is the moment worth reporting. The last log lines
// are enough: that message is written at the end of each boot.
func (b *DockerBackend) guestReady(ctx context.Context, m Machine) (bool, error) {
	logs, err := b.containerLogs(ctx, m, "200")
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToLower(logs), "started successfully"), nil
}

func (b *DockerBackend) containerLogs(ctx context.Context, m Machine, tail string) (string, error) {
	started, err := b.compose(ctx, m, "ps", "-q", "windows")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(started)
	if id == "" {
		return "", nil
	}
	return b.Runner.Run(ctx, "docker", "logs", "--tail", tail, id)
}

func lastLogLine(s string) string {
	s = stripANSI(s)
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ParsePercent reads a 0-100 figure out of a log line, when it has one.
func ParsePercent(line string) float64 {
	best := -1.0
	for i := 0; i < len(line); i++ {
		if line[i] < '0' || line[i] > '9' {
			continue
		}
		n := 0
		j := i
		for j < len(line) && line[j] >= '0' && line[j] <= '9' {
			n = n*10 + int(line[j]-'0')
			j++
		}
		if j < len(line) && line[j] == '%' && n <= 100 {
			best = float64(n) / 100
		}
		i = j
	}
	if best < 0 {
		return 0
	}
	return best
}

// portFree says whether this host can publish a port on the loopback
// interface, which is the only interface a machine uses.
func portFree(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	free, err := FreePort(port)
	return err == nil && free == port
}

// validateOwnedDataDir rejects anything that is not a plainly-owned machine
// folder, so a misconfigured path cannot turn a removal into a wide rm.
func validateOwnedDataDir(dir string) error {
	clean := filepath.Clean(dir)
	switch {
	case dir == "" || !filepath.IsAbs(clean):
		return errors.New("VM data directory must be absolute")
	case clean == string(filepath.Separator):
		return errors.New("refusing to remove the filesystem root")
	case clean == filepath.Clean(os.Getenv("HOME")) && os.Getenv("HOME") != "":
		return errors.New("refusing to remove the home directory")
	case dir != clean:
		return errors.New("VM data directory must be a clean path")
	}
	return nil
}

func safeOwnedDir(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Clean(path) == string(filepath.Separator) {
		return errors.New("unsafe directory path")
	}
	if err := rejectPathSymlinks(path); err != nil {
		return err
	}
	return os.MkdirAll(path, 0700)
}

func rejectPathSymlinks(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	vol := filepath.VolumeName(abs)
	cur := vol + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, cur), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path component rejected: %s", cur)
		}
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".compose-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func yamlQuote(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(s) + "\""
}

func vmError(op string, kind ErrorKind, err error) error {
	return &Error{Op: op, Kind: kind, Err: err}
}

func newGuestPassword() ([]byte, error) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, err
	}
	password := make([]byte, hex.EncodedLen(len(entropy)))
	hex.Encode(password, entropy[:])
	return password, nil
}
