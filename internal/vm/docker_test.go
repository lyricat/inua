package vm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// fakeRunner stands in for Docker so the lifecycle runs without a daemon.
type fakeRunner struct {
	calls []string
	fail  map[string]error
	out   map[string]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if err, ok := f.fail[call]; ok {
		return "", err
	}
	if f.out != nil {
		if s, ok := f.out[call]; ok {
			return s, nil
		}
		for prefix, s := range f.out {
			if strings.HasPrefix(call, prefix) {
				return s, nil
			}
		}
	}
	return "", nil
}

func (f *fakeRunner) ran(prefix string) bool {
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			return true
		}
	}
	return false
}

type fakeSecrets struct{ items map[string][]byte }

func (f *fakeSecrets) Store(_ context.Context, m Machine, v []byte) error {
	f.items[m.Name] = append([]byte(nil), v...)
	return nil
}
func (f *fakeSecrets) Load(_ context.Context, m Machine) ([]byte, error) {
	v, ok := f.items[m.Name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}
func (f *fakeSecrets) Delete(_ context.Context, m Machine) error {
	delete(f.items, m.Name)
	return nil
}

// newTestBackend puts the store under a temporary root, so nothing touches the
// user's real directories, and hands back a Windows machine of the Dockur
// template's shape.
func newTestBackend(t *testing.T) (*DockerBackend, Machine, *fakeRunner, *fakeSecrets) {
	t.Helper()
	root := t.TempDir()
	store := Store{Root: filepath.Join(root, "vm"), Runtime: filepath.Join(root, "run")}
	m := DockurTemplate("windows")
	m.MemoryMiB, m.CPUs, m.DiskGiB = 4096, 2, 64
	runner := &fakeRunner{fail: map[string]error{}}
	secrets := &fakeSecrets{items: map[string][]byte{}}
	b := NewDockerBackend(store, runner)
	b.Secrets = secrets
	return b, m, runner, secrets
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestInstallWritesComposeAndEnvWithoutLeakingSecret(t *testing.T) {
	b, m, _, secrets := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	dir := b.Store.Dir(m.Name)
	compose, err := os.ReadFile(filepath.Join(dir, dockerComposeFile))
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	envPath := filepath.Join(dir, dockerGuestEnv)
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("env file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("env file mode = %o, want 0600", perm)
	}
	password, err := secrets.Load(ctx, m)
	if err != nil {
		t.Fatalf("stored password: %v", err)
	}
	if len(password) < 32 {
		t.Errorf("generated password is too short: %d", len(password))
	}
	// The password belongs in the 0600 env file alone, never in the project.
	if strings.Contains(string(compose), string(password)) {
		t.Error("the guest password leaked into the Compose project")
	}
	// Ports must stay on the loopback interface, and the console port must be
	// the one this machine reserved, not a fixed 8006 anyone could collide with.
	if !strings.Contains(string(compose), `"127.0.0.1:`) {
		t.Errorf("Compose does not publish ports on the loopback interface:\n%s", compose)
	}
	if strings.Contains(string(compose), `- "8006:8006"`) {
		t.Error("the web console is published on every interface")
	}
	// The image is pinned: the container runs with /dev/kvm, /dev/net/tun and
	// NET_ADMIN, so a tag that moved would change what runs with those rights.
	if !strings.Contains(string(compose), dockerImage) || !strings.Contains(dockerImage, "@sha256:") {
		t.Errorf("the image is not pinned by version and digest: %q", dockerImage)
	}
	if !strings.Contains(string(compose), `USERNAME: "`+GuestUser+`"`) {
		t.Errorf("compose does not set the guest user %q:\n%s", GuestUser, compose)
	}
	// The guest's disk is the machine's own folder.
	if !strings.Contains(string(compose), b.Store.Dir(m.Name)+":/storage") {
		t.Errorf("the guest is not stored in the machine's folder:\n%s", compose)
	}
}

// Applying a machine is what the window's Apply does, and it has to work on a
// project that already exists - that is how a changed memory, CPU count or
// disk size takes effect. The guest's password, though, is the one Windows was
// installed with and must survive.
func TestInstallRewritesTheProjectAndKeepsThePassword(t *testing.T) {
	b, m, _, secrets := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("first install: %v", err)
	}
	first, err := secrets.Load(ctx, m)
	if err != nil {
		t.Fatalf("stored password: %v", err)
	}

	bigger := m
	bigger.MemoryMiB = m.MemoryMiB * 2
	bigger.CPUs = m.CPUs + 2
	if err := b.Install(ctx, bigger); err != nil {
		t.Fatalf("applying a new configuration: %v", err)
	}

	compose, err := os.ReadFile(b.composePath(m))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), fmt.Sprintf("RAM_SIZE: \"%dG\"", bigger.MemoryMiB/1024)) {
		t.Errorf("the new memory is not in the project:\n%s", compose)
	}
	if !strings.Contains(string(compose), fmt.Sprintf("CPU_CORES: \"%d\"", bigger.CPUs)) {
		t.Errorf("the new CPU count is not in the project:\n%s", compose)
	}
	again, err := secrets.Load(ctx, m)
	if err != nil {
		t.Fatalf("password after the rewrite: %v", err)
	}
	if !bytes.Equal(first, again) {
		t.Error("rewriting the project changed the guest's password")
	}
}

func TestInstallRefusesWithoutSecretStore(t *testing.T) {
	b, m, _, _ := newTestBackend(t)
	b.Secrets = nil
	if err := b.Install(ctxT(t), m); err == nil {
		t.Fatal("install succeeded without a secure password store")
	}
	if _, err := os.Stat(filepath.Join(b.Store.Dir(m.Name), dockerComposeFile)); !os.IsNotExist(err) {
		t.Error("a refused install left a Compose project behind")
	}
}

// A machine that is not the Dockur backend's - a Linux guest, say - is refused
// before anything is written.
func TestInstallRefusesAMachineOfAnotherBackend(t *testing.T) {
	b, m, _, _ := newTestBackend(t)
	m.Backend = BackendQEMU
	if err := b.Install(ctxT(t), m); err == nil {
		t.Fatal("the Docker backend accepted a QEMU machine")
	} else if !strings.Contains(err.Error(), ErrNotThisBackend.Error()) {
		t.Errorf("the refusal does not say what happened: %v", err)
	}
}

func TestRemoveDeletesProjectAndKeepsThePassword(t *testing.T) {
	b, m, runner, secrets := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	first, err := secrets.Load(ctx, m)
	if err != nil {
		t.Fatalf("stored password: %v", err)
	}
	if err := b.Remove(ctx, m, true); err != nil {
		t.Fatalf("remove: %v", err)
	}
	dir := b.Store.Dir(m.Name)
	for _, name := range []string{dockerComposeFile, dockerGuestEnv, dockerPortsFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s survived removal", name)
		}
	}
	again, err := secrets.Load(ctx, m)
	if err != nil {
		t.Fatalf("password after remove: %v", err)
	}
	if !bytes.Equal(first, again) {
		t.Error("remove dropped the password of the disk it kept")
	}
	if !runner.ran("docker compose -p inua-vm-" + m.Name + " -f " + filepath.Join(dir, dockerComposeFile) + " down") {
		t.Errorf("the container was not taken down: %v", runner.calls)
	}
}

// Two machines publish different loopback ports: a second Windows guest cannot
// take the first one's, and the ports it chose are the ones recorded in its
// runtime directory.
func TestPortsAreReservedPerMachine(t *testing.T) {
	b, m, _, _ := newTestBackend(t)
	ctx := ctxT(t)
	second := m
	second.Name = "windows-2"
	for _, machine := range []Machine{m, second} {
		if err := b.Install(ctx, machine); err != nil {
			t.Fatalf("install %s: %v", machine.Name, err)
		}
	}
	ports := func(machine Machine) (rdp, console int) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(b.Store.RuntimeDir(machine.Name), dockerPortsFile))
		if err != nil {
			t.Fatalf("ports of %s: %v", machine.Name, err)
		}
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d %d", &rdp, &console); err != nil {
			t.Fatalf("ports of %s: %v", machine.Name, err)
		}
		info, err := os.Stat(filepath.Join(b.Store.RuntimeDir(machine.Name), dockerPortsFile))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Errorf("the ports file is not 0600: %v %v", info, err)
		}
		return rdp, console
	}
	rdp1, console1 := ports(m)
	rdp2, console2 := ports(second)
	if rdp1 == rdp2 || console1 == console2 {
		t.Errorf("two machines share a port: %d/%d and %d/%d", rdp1, console1, rdp2, console2)
	}
	// And the project publishes exactly what was recorded.
	compose, err := os.ReadFile(b.composePath(second))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{fmt.Sprintf(`"127.0.0.1:%d:3389/tcp"`, rdp2), fmt.Sprintf(`"127.0.0.1:%d:8006"`, console2)} {
		if !strings.Contains(string(compose), want) {
			t.Errorf("the project does not publish %s:\n%s", want, compose)
		}
	}
}

// Before Windows is up, the display is the web console - the only view there
// is while it installs; once it is up, it is RDP.
func TestDisplayIsLoopbackOnlyAndFollowsTheGuest(t *testing.T) {
	b, m, runner, _ := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	prefix := "docker compose -p inua-vm-" + m.Name + " -f " + b.composePath(m)
	runner.out = map[string]string{
		prefix:        "windows\n",
		"docker logs": "Windows started successfully, visit http://127.0.0.1:8006/\n",
	}
	d, err := b.Display(ctx, m)
	if err != nil {
		t.Fatalf("display: %v", err)
	}
	if d.Kind != DisplayRDP {
		t.Errorf("a running guest is reached over RDP: %+v", d)
	}
	if !strings.HasPrefix(d.Address, "127.0.0.1:") || !strings.HasPrefix(d.Console, "http://127.0.0.1:") {
		t.Errorf("the display is not loopback: %+v", d)
	}

	// The same machine before its guest is ready: the console, not RDP.
	runner.out = map[string]string{prefix: "windows\n"}
	d, err = b.Display(ctx, m)
	if err != nil {
		t.Fatalf("display while installing: %v", err)
	}
	if d.Kind != DisplayWeb {
		t.Errorf("an installing guest is watched in the console: %+v", d)
	}
}

func TestInstalledReportsTheProject(t *testing.T) {
	b, m, _, _ := newTestBackend(t)
	if err := b.installed(m); err == nil {
		t.Error("installed accepted a project that was never written")
	}
	if err := b.Install(ctxT(t), m); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := b.installed(m); err != nil {
		t.Errorf("installed rejected the project it wrote: %v", err)
	}
	// The password is found again by the machine's own name, and the label
	// the user sees in their keyring names the machine.
	if got := GuestSecretLabel(m); got != "inua-vm · windows" {
		t.Errorf("secret label = %q, want %q", got, "inua-vm · windows")
	}
	if got := GuestSecretAttrs(m)["machine"]; got != m.Name {
		t.Errorf("secret attributes name %q, want %q", got, m.Name)
	}
	if got := LegacyGuestSecretAttrs(m)["id"]; got != "inua/windows/windows" {
		t.Errorf("the step 1 secret id changed: %q", got)
	}
}

// The Compose project must be valid YAML: a quoted volume source followed by
// a bare ": /storage" made Docker refuse the project outright.
func TestInstallWritesParsableCompose(t *testing.T) {
	b, m, _, _ := newTestBackend(t)
	if err := b.Install(ctxT(t), m); err != nil {
		t.Fatalf("install: %v", err)
	}
	data, err := os.ReadFile(b.composePath(m))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("compose is not valid YAML: %v\n%s", err, data)
	}
	services, _ := doc["services"].(map[string]any)
	if _, ok := services["windows"]; !ok {
		t.Fatalf("compose has no windows service: %s", data)
	}
}

// Removing the machine keeps its disk until the disk is explicitly deleted:
// losing a Windows installation is a second, deliberate step.
func TestRemoveKeepsDataUntilExplicitlyDeleted(t *testing.T) {
	b, m, _, _ := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	disk := filepath.Join(b.Store.Dir(m.Name), "data.img")
	if err := os.WriteFile(disk, []byte("guest"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove(ctx, m, true); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(disk); err != nil {
		t.Fatalf("remove deleted the guest's disk: %v", err)
	}

	// And with keepDisk false, the guest's files go while the machine's own
	// definition stays: it can be installed again.
	if err := os.WriteFile(filepath.Join(b.Store.Dir(m.Name), "win11x64.iso"), []byte("iso"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove(ctx, m, false); err != nil {
		t.Fatalf("removing the data: %v", err)
	}
	if _, err := os.Stat(disk); !os.IsNotExist(err) {
		t.Error("the disk survived the deletion it was asked for")
	}
	if _, err := os.Stat(filepath.Join(b.Store.Dir(m.Name), "win11x64.iso")); !os.IsNotExist(err) {
		t.Error("the install media survived the deletion")
	}
	if _, err := os.Stat(b.Store.File(m.Name)); err != nil {
		t.Errorf("the machine's own definition went with its data: %v", err)
	}
	if got, err := b.Store.Load(m.Name); err != nil || got.Name != m.Name {
		t.Errorf("the machine no longer loads: %+v (%v)", got, err)
	}
}

// A machine folder is only ever removed through the guards: never the
// filesystem root, never the home directory, never a path that is not clean
// and absolute.
func TestRemovalRefusesDangerousPaths(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"", "/", "relative/path", "//double//slash", home, home + "/"} {
		if dir == home+"/" {
			// A trailing separator is dirty, and dirty is refused as well.
			if err := validateOwnedDataDir(dir); err == nil {
				t.Errorf("validateOwnedDataDir(%q) was allowed", dir)
			}
			continue
		}
		if err := validateOwnedDataDir(dir); err == nil {
			t.Errorf("validateOwnedDataDir(%q) was allowed", dir)
		}
	}
	if err := validateOwnedDataDir("/var/lib/inua/vm/windows"); err != nil {
		t.Errorf("a plain machine folder was refused: %v", err)
	}
}

// Compose derives its project name from the compose file's folder. Two
// projects whose folder is "windows" and whose service is "windows" would be
// the same project to Docker, so `up -d` would tear down the other one's
// container. The backend must pin a namespaced project name instead.
func TestComposePinsNamespacedProject(t *testing.T) {
	b, m, runner, _ := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := b.State(ctx, m); err != nil {
		t.Fatalf("state: %v", err)
	}
	want := "docker compose -p inua-vm-" + m.Name + " -f " + b.composePath(m)
	if !runner.ran(want) {
		t.Errorf("no call pinned the project:\n%v", runner.calls)
	}
}

func TestComposeProjectNamesAreNamespaced(t *testing.T) {
	if got := composeProject("windows"); got != "inua-vm-windows" {
		t.Errorf("composeProject = %q", got)
	}
}

func TestStateReportsStartingUntilGuestReady(t *testing.T) {
	b, m, runner, _ := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	// The container is up, so its RDP port answers; Windows is still installing.
	prefix := "docker compose -p inua-vm-" + m.Name + " -f " + b.composePath(m)
	runner.out = map[string]string{
		prefix + " ps --status running --services": "windows\n",
		"docker logs": "Waiting for the system to become ready",
	}
	state, err := b.State(ctx, m)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state != StateStarting {
		t.Errorf("state = %q, want %q", state, StateStarting)
	}
}

func TestStateReportsRunningWhenLogsSayStarted(t *testing.T) {
	b, m, runner, _ := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	prefix := "docker compose -p inua-vm-" + m.Name + " -f " + b.composePath(m)
	runner.out = map[string]string{
		prefix + " ps --status running --services": "windows\n",
		prefix + " ps -q windows":                  "abc123\n",
		"docker logs":                              "Windows started successfully, visit http://127.0.0.1:8006/\n",
	}
	state, err := b.State(ctx, m)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state != StateRunning {
		t.Errorf("state = %q, want %q", state, StateRunning)
	}
	// With the container down, the same machine is stopped.
	runner.out = map[string]string{prefix + " ps --status running --services": ""}
	if state, err = b.State(ctx, m); err != nil || state != StateStopped {
		t.Errorf("state = %q (%v), want %q", state, err, StateStopped)
	}
}

func TestRecentOutputIsLastLogLine(t *testing.T) {
	b, m, runner, _ := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	prefix := "docker compose -p inua-vm-" + m.Name + " -f " + b.composePath(m)
	runner.out = map[string]string{
		prefix + " ps -q windows": "abc123\n",
		"docker logs":             "\x1b[36mDownloading Windows...\x1b[0m\nExtracting: 42%\n\n",
	}
	if got := b.RecentOutput(ctx, m); got != "Extracting: 42%" {
		t.Errorf("RecentOutput = %q", got)
	}
	if p := ParsePercent("Extracting: 42%"); p < 0.41 || p > 0.43 {
		t.Errorf("ParsePercent = %v", p)
	}
}

// Removing a machine's data takes its container down first, so the disk is
// never pulled out from under a running guest.
func TestRemovingTheDataGoesThroughAContainerThatIsUp(t *testing.T) {
	b, m, runner, _ := newTestBackend(t)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := b.Remove(ctx, m, false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	down := "docker compose -p inua-vm-" + m.Name + " -f " + b.composePath(m) + " down"
	if !runner.ran(down) {
		t.Errorf("the container was not stopped first:\n%v", runner.calls)
	}

	// And a folder with no project at all is still cleaned.
	second := m
	second.Name = "windows-2"
	if err := os.MkdirAll(b.Store.Dir(second.Name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := b.Store.Save(second); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Store.Dir(second.Name), "data.img"), []byte("guest"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove(ctx, second, false); err != nil {
		t.Fatalf("remove without a project: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.Store.Dir(second.Name), "data.img")); !os.IsNotExist(err) {
		t.Error("a leftover disk survived the deletion")
	}
}
