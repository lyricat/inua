package vm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func (p Persisted) mustJSON(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestMachineRoundTrip(t *testing.T) {
	want := Machine{
		Name:      "fedora",
		OS:        OSLinux,
		Backend:   BackendQEMU,
		MemoryMiB: 4096,
		CPUs:      4,
		DiskGiB:   40,
		Boot: Boot{
			ISO:      "~/Downloads/Fedora-Workstation-Live-x86_64-41.iso",
			Disk:     "disk.qcow2",
			Kernel:   "/usr/lib/kernels/vmlinuz",
			Initrd:   "/usr/lib/kernels/initrd",
			Append:   `console=ttyS0 root=/dev/vda "quiet"`,
			Firmware: FirmwareUEFI,
			Arch:     ArchAarch64,
			Machine:  "virt",
			Accel:    AccelAuto,
		},
		Display:  DisplayConfig{Kind: DisplaySPICE, GPU: GPUVenus},
		Seamless: SeamlessConfig{Enabled: true},
	}
	got, err := ParseMachine(want.TOML())
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, want.TOML())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, want)
	}
}

func TestMachineProblems(t *testing.T) {
	base := func() Machine {
		tmpl, _ := TemplateByID("ubuntu")
		return tmpl.Machine("ubuntu", Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 64})
	}
	for _, tc := range []struct {
		name   string
		change func(*Machine)
		want   []Problem
	}{
		{"fine", func(*Machine) {}, nil},
		{"no name", func(m *Machine) { m.Name = "  " }, []Problem{{"name", ReasonEmpty}}},
		{"name escapes", func(m *Machine) { m.Name = "../windows" }, []Problem{{"name", ReasonBadName}}},
		{"hidden name", func(m *Machine) { m.Name = ".ssh" }, []Problem{{"name", ReasonBadName}}},
		{"unknown os", func(m *Machine) { m.OS = "plan9" }, []Problem{{"os", ReasonEmpty}}},
		{"dockur is windows only", func(m *Machine) {
			m.Backend = BackendDockur
			m.Display.Kind = DisplayRDP
			m.Display.GPU = GPUBasic
		}, []Problem{{"backend", ReasonMismatch}}},
		{"waydroid is android only", func(m *Machine) {
			m.Backend = BackendWaydroid
			m.Display.Kind = DisplayNone
			m.Display.GPU = GPUBasic
		}, []Problem{{"backend", ReasonMismatch}}},
		{"small memory", func(m *Machine) { m.MemoryMiB = MinMemoryMiB - 1 }, []Problem{{"memory_mib", ReasonTooSmall}}},
		{"no cpus", func(m *Machine) { m.CPUs = 0 }, []Problem{{"cpus", ReasonTooSmall}}},
		{"small disk", func(m *Machine) { m.DiskGiB = MinDiskGiB - 1 }, []Problem{{"disk_gib", ReasonTooSmall}}},
		{"unknown firmware", func(m *Machine) { m.Boot.Firmware = "coreboot" }, []Problem{{"boot.firmware", ReasonEmpty}}},
		{"unknown arch", func(m *Machine) { m.Boot.Arch = "sparc" }, []Problem{{"boot.arch", ReasonEmpty}}},
		{"unknown accel", func(m *Machine) { m.Boot.Accel = "hvf" }, []Problem{{"boot.accel", ReasonEmpty}}},
		{"no machine type", func(m *Machine) { m.Boot.Machine = "" }, []Problem{{"boot.machine", ReasonEmpty}}},
		{"disk walks out", func(m *Machine) { m.Boot.Disk = "./disk.qcow2" }, []Problem{{"boot.disk", ReasonNotInside}}},
		{"iso is relative", func(m *Machine) { m.Boot.ISO = "downloads/x.iso" }, []Problem{{"boot.iso", ReasonNotAPath}}},
		{"kernel is not clean", func(m *Machine) { m.Boot.Kernel = "/usr/lib/../vmlinuz" }, []Problem{{"boot.kernel", ReasonNotAPath}}},
		{"append has a newline", func(m *Machine) { m.Boot.Append = "quiet\nroot=/dev/vda" }, []Problem{{"boot.append", ReasonOneLine}}},
		{"unknown display", func(m *Machine) { m.Display.Kind = "vnc" }, []Problem{{"display.kind", ReasonEmpty}}},
		{"3D over the serial console", func(m *Machine) { m.Display.Kind = DisplaySerial }, []Problem{{"display.gpu", ReasonMismatch}}},
		{"dockur needs rdp or web", func(m *Machine) {
			tmpl, _ := TemplateByID("windows-dockur")
			*m = tmpl.Machine("w", Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 64})
			m.Display.Kind = DisplaySPICE
		}, []Problem{{"display.kind", ReasonMismatch}}},
		{"windows has no virgl", func(m *Machine) {
			tmpl, _ := TemplateByID("windows-dockur")
			*m = tmpl.Machine("w", Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 64})
			m.Display.GPU = GPUVirgl
		}, []Problem{{"display.gpu", ReasonMismatch}}},
		{"waydroid draws itself", func(m *Machine) {
			tmpl, _ := TemplateByID("android")
			*m = tmpl.Machine("a", Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 64})
			m.Display.Kind = DisplaySPICE
		}, []Problem{{"display.kind", ReasonMismatch}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			tc.change(&m)
			if got := m.Problems(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems = %+v, want %+v", got, tc.want)
			}
			if err := m.Validate(); (err != nil) != (len(tc.want) > 0) {
				t.Errorf("Validate = %v, want problems: %v", err, tc.want)
			}
		})
	}
}

// Every template has to make a machine the window can offer as it stands: a
// template that needs a person to fill a field first is a bug in the template.
func TestTemplatesAreValid(t *testing.T) {
	templates := Templates()
	if len(templates) != 8 {
		t.Fatalf("templates = %d, want the guide's eight", len(templates))
	}
	seen := map[string]bool{}
	for _, tmpl := range templates {
		if seen[tmpl.ID] {
			t.Errorf("two templates with the id %q", tmpl.ID)
		}
		seen[tmpl.ID] = true
		if tmpl.Name == "" {
			t.Errorf("%s: no name to start from", tmpl.ID)
		}
		got, ok := TemplateByID(tmpl.ID)
		if !ok || got.OS != tmpl.OS || got.Backend != tmpl.Backend {
			t.Errorf("%s: TemplateByID answered %+v", tmpl.ID, got)
		}
		m := tmpl.Machine(tmpl.Name, Profile{MemoryMiB: 8192, CPUs: 4, DiskGiB: 128})
		if problems := m.Problems(); len(problems) > 0 {
			t.Errorf("%s: %+v", tmpl.ID, problems)
		}
		if m.OS != tmpl.OS || m.Backend != tmpl.Backend {
			t.Errorf("%s: machine is %s/%s", tmpl.ID, m.OS, m.Backend)
		}
	}
	// A profile below the minimums still makes a machine that holds.
	custom, _ := TemplateByID("custom")
	if problems := custom.Machine("c", Profile{}).Problems(); len(problems) > 0 {
		t.Errorf("a machine from an empty profile: %+v", problems)
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home to expand")
	}
	for _, tc := range []struct{ in, want string }{
		{"~/Downloads/x.iso", filepath.Join(home, "Downloads", "x.iso")},
		{"~", home},
		{"/var/lib/x.iso", "/var/lib/x.iso"},
		{"relative.iso", "relative.iso"},
		{"", ""},
	} {
		if got := ExpandPath(tc.in); got != tc.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	store := Store{Root: filepath.Join(root, "vm"), Runtime: filepath.Join(root, "run")}

	tmpl, _ := TemplateByID("freebsd")
	m := tmpl.Machine("freebsd", Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 64})
	if err := store.Save(m); err != nil {
		t.Fatalf("save: %v", err)
	}
	if info, err := os.Stat(store.File("freebsd")); err != nil {
		t.Fatalf("machine.toml: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("machine.toml is %o, want 0600", perm)
	}
	got, err := store.Load("freebsd")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Errorf("load:\n got %+v\nwant %+v", got, m)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 || list[0].Name != "freebsd" {
		t.Fatalf("list = %+v (%v)", list, err)
	}

	// A folder that is not a machine is left alone by List.
	if err := os.MkdirAll(store.Dir("photos"), 0700); err != nil {
		t.Fatal(err)
	}
	if list, err := store.List(); err != nil || len(list) != 1 {
		t.Errorf("list after a stray folder = %+v (%v)", list, err)
	}

	if _, err := store.EnsureRuntime("freebsd"); err != nil {
		t.Fatalf("runtime dir: %v", err)
	}
	if info, err := os.Stat(store.RuntimeDir("freebsd")); err != nil {
		t.Fatalf("runtime dir: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("runtime dir is %o, want 0700", perm)
	}

	if err := store.Delete("freebsd"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(store.Dir("freebsd")); !os.IsNotExist(err) {
		t.Errorf("the machine's folder is still there: %v", err)
	}
	if err := store.Delete("freebsd"); err != nil {
		t.Errorf("deleting what is gone: %v", err)
	}
}

// A machine's name is one folder: nothing that reads or writes outside the
// store, whatever a caller passes.
func TestStoreRefusesNamesThatAreNotOneFolder(t *testing.T) {
	root := t.TempDir()
	store := Store{Root: filepath.Join(root, "vm"), Runtime: filepath.Join(root, "run")}
	if err := os.MkdirAll(store.Root, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside", MachineFileName)
	if err := os.MkdirAll(filepath.Dir(outside), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("name = \"x\"\nos = \"linux\"\nbackend = \"qemu\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", " ", ".", "..", "../outside", "a/b", `a\b`, ".hidden"} {
		if _, err := store.Load(name); err == nil {
			t.Errorf("Load(%q) was allowed", name)
		}
		if err := store.Save(Machine{Name: name}); err == nil {
			t.Errorf("Save(%q) was allowed", name)
		}
		if err := store.Delete(name); err == nil {
			t.Errorf("Delete(%q) was allowed", name)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("a machine outside the store was touched: %v", err)
	}
}

func TestUniqueName(t *testing.T) {
	root := t.TempDir()
	store := Store{Root: filepath.Join(root, "vm"), Runtime: filepath.Join(root, "run")}
	tmpl, _ := TemplateByID("ubuntu")
	for _, name := range []string{"ubuntu", "ubuntu-2", "ubuntu-3"} {
		m := tmpl.Machine(name, Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 64})
		if err := store.Save(m); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	if name, err := UniqueName(store, "ubuntu"); err != nil || name != "ubuntu-4" {
		t.Fatalf("after three machines, the next is %q (%v), want ubuntu-4", name, err)
	}
	// A folder with something in it is taken even without a machine.toml.
	if err := os.MkdirAll(filepath.Join(store.Dir("custom"), "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if name, err := UniqueName(store, "custom"); err != nil || name != "custom-2" {
		t.Errorf("UniqueName(custom) = %q (%v), want custom-2", name, err)
	}
	// And so is a folder that holds no machine.toml and nothing else? No: that
	// is an empty folder, which is free.
	if err := os.MkdirAll(store.Dir("fedora"), 0700); err != nil {
		t.Fatal(err)
	}
	if name, err := UniqueName(store, "fedora"); err != nil || name != "fedora" {
		t.Errorf("UniqueName(fedora) = %q (%v), want fedora", name, err)
	}
}

// Step 1 kept one Windows guest in a folder named after it, its Dockur files
// inside and its profile in ~/.config. A store finds it, writes what it is,
// and keeps the resources that guest was given.
func TestStoreAdoptsTheStepOneGuest(t *testing.T) {
	root := t.TempDir()
	store := Store{Root: filepath.Join(root, "vm"), Runtime: filepath.Join(root, "run")}
	data := filepath.Join(store.Dir("windows"))
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"data.img", "setup.img", "win11x64.iso"} {
		if err := os.WriteFile(filepath.Join(data, f), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfgHome := filepath.Join(root, "config")
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	if err := os.MkdirAll(filepath.Join(cfgHome, "inua", "vm"), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := Persisted{Config: Config{Name: "windows", MemoryMiB: 16384, CPUs: 8, DiskGiB: 128, DataDir: data, RDPPort: 3389}, Installed: true}
	if err := os.WriteFile(ConfigPath(), legacy.mustJSON(t), 0600); err != nil {
		t.Fatal(err)
	}

	m, err := store.Load("windows")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.OS != OSWindows || m.Backend != BackendDockur {
		t.Errorf("the adopted machine is %s/%s", m.OS, m.Backend)
	}
	if m.MemoryMiB != 16384 || m.CPUs != 8 || m.DiskGiB != 128 {
		t.Errorf("the guest's own resources were lost: %+v", m)
	}
	if m.Display.Kind != DisplayRDP || m.Display.GPU != GPUBasic {
		t.Errorf("Windows is reached over RDP: %+v", m.Display)
	}
	if _, err := os.Stat(store.File("windows")); err != nil {
		t.Errorf("machine.toml was not written: %v", err)
	}
	// The guest's files are still there: adopting reads the folder, it does
	// not touch it.
	for _, f := range []string{"data.img", "setup.img", "win11x64.iso"} {
		if _, err := os.Stat(filepath.Join(data, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	// A folder with no guest in it is not adopted.
	if err := os.MkdirAll(store.Dir("scratch"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("scratch"); !IsNoMachine(err) {
		t.Errorf("Load(scratch) = %v, want no such machine", err)
	}
}

func TestMachineTOMLIsTheGuide(t *testing.T) {
	tmpl, _ := TemplateByID("windows-dockur")
	m := tmpl.Machine("windows", Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 64})
	text := string(m.TOML())
	for _, want := range []string{
		`name = "windows"`, `os = "windows"`, `backend = "dockur"`, "memory_mib = 4096", "[boot]", "[display]",
		`kind = "rdp"`, `gpu = "basic"`, "[seamless]", "enabled = false",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("machine.toml has no %q:\n%s", want, text)
		}
	}
}
