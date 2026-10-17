package vmui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/vm"
)

// fakeBackend stands in for a machine's backend: it records what was asked of
// it and answers what the test tells it to.
type fakeBackend struct {
	prepared bool
	state    vm.State
	calls    []string
}

func (f *fakeBackend) record(call string) { f.calls = append(f.calls, call) }

func (f *fakeBackend) Install(context.Context, vm.Machine) error { f.record("install"); return nil }

func (f *fakeBackend) State(context.Context, vm.Machine) (vm.State, error) {
	f.record("state")
	return f.state, nil
}

func (f *fakeBackend) Start(context.Context, vm.Machine) error { f.record("start"); return nil }
func (f *fakeBackend) Stop(context.Context, vm.Machine) error  { f.record("stop"); return nil }

func (f *fakeBackend) Display(context.Context, vm.Machine) (vm.Display, error) {
	f.record("display")
	return vm.Display{Kind: vm.DisplayRDP, Address: "127.0.0.1:3389"}, nil
}

func (f *fakeBackend) Remove(_ context.Context, _ vm.Machine, keepDisk bool) error {
	if keepDisk {
		f.record("remove-keeping-the-disk")
	} else {
		f.record("remove-with-the-disk")
	}
	return nil
}

func (f *fakeBackend) Prepared(vm.Machine) bool { f.record("prepared"); return f.prepared }

func (f *fakeBackend) RecentOutput(context.Context, vm.Machine) string {
	f.record("progress")
	return "Extracting: 42%"
}

// fakeSecrets stands in for the Secret Service.
type fakeSecrets struct{}

func (fakeSecrets) Store(context.Context, vm.Machine, []byte) error { return nil }
func (fakeSecrets) Load(context.Context, vm.Machine) ([]byte, error) {
	return []byte("guest-password"), nil
}
func (fakeSecrets) Delete(context.Context, vm.Machine) error { return nil }

// readyHost is the host a machine's backend needs, all present, so that a test
// looks at the pages that follow from what is on disk rather than from the
// machine the tests happen to run on.
func readyHost() vm.Platform {
	device := vm.DeviceCapability{Available: true, Accessible: true}
	return vm.Platform{
		OS: "linux", Distribution: "debian", Supported: true,
		KVM: device, DockerSocket: device, TUN: device,
		ComposeV2: vm.Capability{Available: true},
		FreeRDP:   vm.Capability{Available: true},
	}
}

// testService is a service over a store of its own, with a backend for the
// machines of one kind: no D-Bus, no GTK and nothing of the user's own.
func testService(t *testing.T) (*vmService, vm.Store, *fakeBackend) {
	t.Helper()
	root := t.TempDir()
	store := vm.Store{Root: filepath.Join(root, "data"), Runtime: filepath.Join(root, "run")}
	backend := &fakeBackend{prepared: true, state: vm.StateRunning}
	s := &vmService{
		store:    store,
		platform: readyHost(),
		secrets:  fakeSecrets{},
		backends: map[vm.BackendKind]vm.Backend{vm.BackendDockur: backend},
		seen:     map[string]observed{},
		busy:     map[string]bool{},
	}
	return s, store, backend
}

// fromTemplate is a machine of a template's shape, as the window makes one.
func fromTemplate(t *testing.T, id, name string, p vm.Profile) vm.Machine {
	t.Helper()
	tpl, ok := vm.TemplateByID(id)
	if !ok {
		t.Fatalf("there is no %s template", id)
	}
	return tpl.Machine(name, p)
}

// write stores a machine, and a guest image in its folder when it has one.
func write(t *testing.T, store vm.Store, m vm.Machine, guest bool) {
	t.Helper()
	if err := store.Save(m); err != nil {
		t.Fatalf("save %s: %v", m.Name, err)
	}
	if guest {
		if err := os.WriteFile(filepath.Join(store.Dir(m.Name), "data.img"), []byte("disk"), 0600); err != nil {
			t.Fatalf("guest image: %v", err)
		}
	}
}

// The list comes from the store alone: no backend is read, so opening the
// window never waits for Docker - or for a machine that is not even running.
func TestTheListReadsNoBackend(t *testing.T) {
	s, store, backend := testService(t)
	write(t, store, vm.DockurTemplate("windows"), true)
	write(t, store, vm.DockurTemplate("alpha"), false)

	views := s.Machines()
	if len(views) != 2 {
		t.Fatalf("%d machines, want 2", len(views))
	}
	if views[0].Name != "alpha" || views[1].Name != "windows" {
		t.Errorf("machines = %q, %q; want them by name", views[0].Name, views[1].Name)
	}
	if len(backend.calls) != 0 {
		t.Errorf("listing the machines called the backend: %v", backend.calls)
	}
}

// A folder whose machine.toml does not hold is not a machine: the list leaves
// it out, and --doctor is where it is reported.
func TestAFolderThatIsNotAMachineIsLeftOut(t *testing.T) {
	s, store, _ := testService(t)
	write(t, store, vm.DockurTemplate("windows"), false)
	broken := filepath.Join(store.Root, "broken")
	if err := os.MkdirAll(broken, 0700); err != nil {
		t.Fatal(err)
	}
	half := "name = \"broken\"\nos = \"windows\"\n"
	if err := os.WriteFile(filepath.Join(broken, vm.MachineFileName), []byte(half), 0600); err != nil {
		t.Fatal(err)
	}
	if names := s.Names(); len(names) != 1 || names[0] != "windows" {
		t.Errorf("names = %v, want only windows", names)
	}
	faults := store.Faults()
	if len(faults) != 1 || faults[0].Name != "broken" {
		t.Errorf("faults = %v, want the broken folder", faults)
	}
}

// What a machine's page shows follows from what is on disk: a machine whose
// backend has made what it needs and whose guest is installed is stopped, one
// that only has the guest's files is not installed yet, and a machine this
// build has no backend for says so.
func TestThePageFollowsWhatIsOnDisk(t *testing.T) {
	s, store, backend := testService(t)
	write(t, store, vm.DockurTemplate("installed"), true)
	write(t, store, vm.DockurTemplate("leftover"), true)
	write(t, store, fromTemplate(t, "fedora", "fedora", vm.Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 40}), false)

	backend.prepared = true
	installed := s.Refresh("installed")
	if !installed.Installed || installed.Page != vm.PageRunning {
		t.Errorf("installed machine: installed=%t page=%s, want installed on the running page",
			installed.Installed, installed.Page)
	}
	if installed.State != vm.StateRunning || installed.Progress == 0 {
		t.Errorf("state = %q, progress = %v; want what the backend reported", installed.State, installed.Progress)
	}
	// The same machine, once its guest is off, is the stopped page.
	backend.state = vm.StateStopped
	if off := s.Refresh("installed"); off.Page != vm.PageStopped || !off.Installed {
		t.Errorf("installed and off: page=%s installed=%t, want the stopped page", off.Page, off.Installed)
	}

	backend.prepared = false
	leftover := s.Refresh("leftover")
	if !leftover.Leftovers || leftover.Installed || leftover.Page != vm.PageSetup {
		t.Errorf("leftover files: leftovers=%t installed=%t page=%s, want them on the setup page",
			leftover.Leftovers, leftover.Installed, leftover.Page)
	}

	qemu := s.Refresh("fedora")
	if qemu.Ready {
		t.Error("a QEMU machine is ready in a build with no QEMU backend")
	}
	if !strings.Contains(vmListSub(qemu), "no backend") {
		t.Errorf("the list's line for it = %q; it should say there is no backend", vmListSub(qemu))
	}
}

// Making a machine writes its own file, from a template, under a name that is
// nobody else's - and starts nothing.
func TestCreateWritesTheMachineAndStartsNothing(t *testing.T) {
	s, store, backend := testService(t)
	m, err := s.Create("ubuntu", "test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.OS != vm.OSLinux || m.Backend != vm.BackendQEMU {
		t.Errorf("created %s/%s, want a Linux machine on QEMU", m.OS, m.Backend)
	}
	if _, err := os.Stat(store.File("test")); err != nil {
		t.Errorf("the machine's file is not there: %v", err)
	}
	if len(backend.calls) != 0 {
		t.Errorf("making a machine called the backend: %v", backend.calls)
	}
	// The same name again is not the same folder.
	again, err := s.Create("ubuntu", "test")
	if err != nil {
		t.Fatalf("create again: %v", err)
	}
	if again.Name == m.Name {
		t.Errorf("a second machine took the name %q", again.Name)
	}
	// And a template that does not exist is refused.
	if _, err := s.Create("no-such-template", "x"); err == nil {
		t.Error("Create accepted a template that does not exist")
	}
}

// Deleting a machine takes its files away and asks its backend to let go of
// the guest.
func TestDeleteTakesTheMachineAway(t *testing.T) {
	s, store, backend := testService(t)
	write(t, store, vm.DockurTemplate("windows"), true)
	if err := s.Delete("windows"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(store.Dir("windows")); !os.IsNotExist(err) {
		t.Errorf("the machine's folder is still there: %v", err)
	}
	if len(backend.calls) != 1 || backend.calls[0] != "remove-with-the-disk" {
		t.Errorf("backend calls = %v, want the guest's files removed", backend.calls)
	}
	if names := s.Names(); len(names) != 0 {
		t.Errorf("names = %v, want none", names)
	}
}

// Removing a machine's container keeps its disk: the same guest starts again.
func TestRemoveKeepsTheDisk(t *testing.T) {
	s, store, backend := testService(t)
	write(t, store, vm.DockurTemplate("windows"), true)
	if err := s.Remove("windows"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(backend.calls) != 1 || backend.calls[0] != "remove-keeping-the-disk" {
		t.Errorf("backend calls = %v, want the disk kept", backend.calls)
	}
	if _, err := os.Stat(store.Dir("windows")); err != nil {
		t.Errorf("the machine's folder is gone: %v", err)
	}
}

// The command line's report says what each machine is, and which host checks
// failed.
func TestStatusTextReportsMachinesAndChecks(t *testing.T) {
	text := StatusText(StatusReport{
		Dir:    "/somewhere",
		Checks: []StatusCheck{{Name: "KVM", OK: true}, {Name: "Docker", OK: false, Reason: "denied"}},
		Machines: []StatusMachine{
			{Name: "windows", OS: "windows", Backend: "dockur", State: "running", Ready: true, Installed: true},
			{Name: "fedora", OS: "linux", Backend: "qemu", State: "stopped"},
		},
	})
	for _, want := range []string{
		"/somewhere",
		"OK: KVM",
		"FAIL: Docker (denied)",
		"windows: windows, dockur, running",
		"fedora: linux, qemu, stopped (no qemu backend in this build)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report does not say %q:\n%s", want, text)
		}
	}
	if got := StatusText(StatusReport{Dir: "/x"}); !strings.Contains(got, "No machines.") {
		t.Errorf("an empty report = %q", got)
	}
}
