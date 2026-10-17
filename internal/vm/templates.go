package vm

import (
	"errors"
	"fmt"
	"os"
)

// Template is one of the machines "New machine" offers: what it runs, how, and
// what a fresh machine of it looks like. The IDs are stable, so the window
// words each one in the user's language.
type Template struct {
	ID      string // "windows-dockur", "windows-qemu", "ubuntu", …
	OS      OS
	Backend BackendKind
	Name    string // the name a new machine of this template starts with
}

// Templates are the machines the window offers, in the order it shows them.
func Templates() []Template {
	return []Template{
		{ID: "windows-dockur", OS: OSWindows, Backend: BackendDockur, Name: "windows"},
		{ID: "windows-qemu", OS: OSWindows, Backend: BackendQEMU, Name: "windows"},
		{ID: "ubuntu", OS: OSLinux, Backend: BackendQEMU, Name: "ubuntu"},
		{ID: "fedora", OS: OSLinux, Backend: BackendQEMU, Name: "fedora"},
		{ID: "arch", OS: OSLinux, Backend: BackendQEMU, Name: "arch"},
		{ID: "freebsd", OS: OSFreeBSD, Backend: BackendQEMU, Name: "freebsd"},
		{ID: "android", OS: OSAndroid, Backend: BackendWaydroid, Name: "android"},
		{ID: "custom", OS: OSCustom, Backend: BackendQEMU, Name: "custom"},
	}
}

// Template returns the template with this ID.
func TemplateByID(id string) (Template, bool) {
	for _, t := range Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// Machine is a fresh machine from a template: the guest it runs, the resources
// it is given (the host's profile, so a new machine leaves the system room),
// and the boot, display and seamless settings that guest wants.
//
// What the guide says of each, in short: Windows is installed unattended by
// Dockur, or from the user's own ISO with QEMU and the virtio drivers; the
// Linux systems and FreeBSD boot their install media and give 3D through the
// host's GPU; Android runs in Waydroid, which draws itself; and a custom
// system is an OS in the making, whose serial console is often the only thing
// it can say yet.
func (t Template) Machine(name string, p Profile) Machine {
	m := Machine{
		Name:      name,
		OS:        t.OS,
		Backend:   t.Backend,
		MemoryMiB: p.MemoryMiB,
		CPUs:      p.CPUs,
		DiskGiB:   p.DiskGiB,
	}
	if m.MemoryMiB < MinMemoryMiB {
		m.MemoryMiB = DefaultMemoryMiB
	}
	if m.CPUs < MinCPUs {
		m.CPUs = DefaultCPUs
	}
	if m.DiskGiB < MinDiskGiB {
		m.DiskGiB = DefaultDiskGiB
	}
	switch t.ID {
	case "windows-dockur":
		// Dockur boots nothing itself: its container installs Windows and runs
		// it. Windows has no virgl driver, so its graphics are 2D and it is
		// reached over RDP (or its web console, while it installs).
		m.Display = DisplayConfig{Kind: DisplayRDP, GPU: GPUBasic}
	case "android":
		// Waydroid draws the guest itself: inua opens no viewer of its own.
		m.Display = DisplayConfig{Kind: DisplayNone, GPU: GPUBasic}
		m.Seamless.Enabled = true
	case "custom":
		m.Boot = Boot{Disk: "disk.qcow2", Firmware: FirmwareUEFI, Arch: ArchX8664, Machine: "q35", Accel: AccelAuto}
		m.Display = DisplayConfig{Kind: DisplaySerial, GPU: GPUBasic}
	default:
		// Windows from the user's ISO, the Linux systems and FreeBSD: their
		// own install media, a disk of their own, and 3D where the guest has a
		// driver for it.
		m.Boot = Boot{Disk: "disk.qcow2", Firmware: FirmwareUEFI, Arch: ArchX8664, Machine: "q35", Accel: AccelAuto}
		m.Display = DisplayConfig{Kind: DisplaySPICE, GPU: GPUVirgl}
		if t.OS == OSLinux || t.OS == OSFreeBSD {
			m.Seamless.Enabled = true
		}
	}
	return m
}

// DockurTemplate is the machine a Windows guest with Dockur has: what the
// template makes, and what a folder with a Dockur guest in it is recognised as.
func DockurTemplate(name string) Machine {
	t, ok := TemplateByID("windows-dockur")
	if !ok {
		return Machine{Name: name, OS: OSWindows, Backend: BackendDockur}
	}
	return t.Machine(name, Profile{MemoryMiB: DefaultMemoryMiB, CPUs: DefaultCPUs, DiskGiB: DefaultDiskGiB})
}

// UniqueName is want, or the first of its numbered variants that is free yet,
// so a new machine never lands on another's folder. A name is taken when
// there is a machine.toml under it, or anything at all in its folder: what is
// there is not inua's to write over.
func UniqueName(s Store, want string) (string, error) {
	if want == "" {
		want = "machine"
	}
	if err := checkName(want); err != nil {
		return "", err
	}
	for i := 1; i <= 1000; i++ {
		name := want
		if i > 1 {
			name = fmt.Sprintf("%s-%d", want, i)
		}
		if _, err := os.Stat(s.File(name)); err == nil {
			continue
		}
		if entries, err := os.ReadDir(s.Dir(name)); err == nil && len(entries) > 0 {
			continue
		}
		return name, nil
	}
	return "", &Error{Kind: ErrorConflict, Op: "name", Err: fmt.Errorf("no free name near %q", want)}
}

// IsNoMachine says whether an error is a machine that is not there.
func IsNoMachine(err error) bool { return err != nil && errors.Is(err, ErrNoMachine) }
