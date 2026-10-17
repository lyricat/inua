package vm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// MachineFileName is the file that says what a machine is: one folder per
// machine holds it, beside the guest's own files (docs/inua-vm.md).
const MachineFileName = "machine.toml"

// OS is the system a machine runs.
type OS string

const (
	OSWindows OS = "windows"
	OSLinux   OS = "linux"
	OSFreeBSD OS = "freebsd"
	OSAndroid OS = "android"
	OSCustom  OS = "custom"
)

// BackendKind is how a machine is run.
type BackendKind string

const (
	BackendQEMU     BackendKind = "qemu"     // the guest runs on QEMU, with or without KVM
	BackendDockur   BackendKind = "dockur"   // Windows, installed unattended in a container
	BackendWaydroid BackendKind = "waydroid" // Android, in Waydroid's container
)

// Firmware is what a machine boots with.
type Firmware string

const (
	FirmwareUEFI Firmware = "uefi"
	FirmwareBIOS Firmware = "bios"
)

// Arch is the architecture a guest runs as; another than the host's runs
// without KVM.
type Arch string

const (
	ArchX8664   Arch = "x86_64"
	ArchAarch64 Arch = "aarch64"
	ArchRiscv64 Arch = "riscv64"
	ArchI386    Arch = "i386"
)

// Accel is how QEMU runs the guest's code.
type Accel string

const (
	AccelAuto Accel = "auto" // KVM when it can, emulation otherwise
	AccelKVM  Accel = "kvm"  // refuse to start without it
	AccelTCG  Accel = "tcg"  // always emulate: slow, and works anywhere
)

// DisplayKind is how a running machine is seen.
type DisplayKind string

const (
	DisplaySPICE  DisplayKind = "spice"  // SPICE on a unix socket, shown with remote-viewer
	DisplayGTK    DisplayKind = "gtk"    // QEMU's own window
	DisplayRDP    DisplayKind = "rdp"    // FreeRDP, the way Windows is reached
	DisplayWeb    DisplayKind = "web"    // the backend's web console
	DisplaySerial DisplayKind = "serial" // the serial console, for an OS in the making
	DisplayNone   DisplayKind = "none"   // the guest draws itself (Waydroid)
)

// GPU is what a guest's graphics get.
type GPU string

const (
	GPUVirgl GPU = "virgl" // 3D through the host's GPU (OpenGL)
	GPUVenus GPU = "venus" // 3D with Vulkan; QEMU 9.2 and newer, and a host driver that has it
	GPUBasic GPU = "basic" // 2D
)

// Boot is how a machine boots: the install media until it is installed, then
// its disk; a kernel can be booted directly, which is what a custom OS wants.
type Boot struct {
	ISO      string   `toml:"iso"`      // install media, until installed
	Disk     string   `toml:"disk"`     // the disk's name inside the machine's folder
	Kernel   string   `toml:"kernel"`   // a kernel to boot directly
	Initrd   string   `toml:"initrd"`   //
	Append   string   `toml:"append"`   // the kernel's command line
	Firmware Firmware `toml:"firmware"` // uefi | bios
	Arch     Arch     `toml:"arch"`     // x86_64 | aarch64 | riscv64 | i386
	Machine  string   `toml:"machine"`  // the machine type, q35 for most
	Accel    Accel    `toml:"accel"`    // auto | kvm | tcg
}

// DisplayConfig is how a machine wants to be seen. The backend reports what it
// gives it (Display); this is the wish.
type DisplayConfig struct {
	Kind DisplayKind `toml:"kind"`
	GPU  GPU         `toml:"gpu"`
}

// SeamlessConfig is whether a machine wants its programs as windows of their
// own on this desktop, where the guest allows it.
type SeamlessConfig struct {
	Enabled bool `toml:"enabled"`
}

// Machine is one guest: what it is, in machine.toml. Its files live in
// ~/.local/share/inua/vm/NAME/, its runtime files in the runtime directory,
// and its secrets (the Windows password) in the Secret Service, never here.
type Machine struct {
	Name      string         `toml:"name"`
	OS        OS             `toml:"os"`
	Backend   BackendKind    `toml:"backend"`
	MemoryMiB int            `toml:"memory_mib"`
	CPUs      int            `toml:"cpus"`
	DiskGiB   int            `toml:"disk_gib"`
	Boot      Boot           `toml:"boot"`
	Display   DisplayConfig  `toml:"display"`
	Seamless  SeamlessConfig `toml:"seamless"`
}

// Reason says why a machine does not hold, so the window can word it in the
// user's language. A machine that holds has no problems.
type Reason string

const (
	ReasonEmpty     Reason = "empty"      // a value the machine needs is missing
	ReasonUnknown   Reason = "unknown"    // a value outside the set the field takes
	ReasonTooSmall  Reason = "too_small"  // a resource below what a guest needs
	ReasonBadName   Reason = "bad_name"   // a name that cannot be one folder
	ReasonNotAPath  Reason = "not_a_path" // an image or kernel that is not an absolute path
	ReasonNotInside Reason = "not_inside" // a file that must be the machine's own
	ReasonMismatch  Reason = "mismatch"   // two fields that cannot go together
	ReasonOneLine   Reason = "one_line"   // a value that must fit on one line
)

// Problem is one thing wrong with a machine: which field, and why. Both are
// stable identifiers, so every window can say them in the user's language.
type Problem struct {
	Field  string
	Reason Reason
}

// Problems lists everything wrong with a machine, in the order the fields
// appear in its file, so a window can show them all at once.
func (m Machine) Problems() []Problem {
	var out []Problem
	bad := func(field string, reason Reason) { out = append(out, Problem{Field: field, Reason: reason}) }

	switch {
	case strings.TrimSpace(m.Name) == "":
		bad("name", ReasonEmpty)
	case m.Name == "." || m.Name == ".." || strings.HasPrefix(m.Name, ".") ||
		strings.ContainsAny(m.Name, `/\`) || len(m.Name) > 64:
		// A name is one folder: nothing that walks out of the directory, and
		// nothing hidden, which a file manager would rather not show.
		bad("name", ReasonBadName)
	}

	if !m.OS.valid() {
		bad("os", ReasonEmpty)
	}
	if !m.Backend.valid() {
		bad("backend", ReasonEmpty)
	}
	switch m.Backend {
	case BackendDockur:
		// Dockur's unattended install is Windows'; Linux and FreeBSD have
		// QEMU, Android has Waydroid.
		if m.OS != OSWindows {
			bad("backend", ReasonMismatch)
		}
	case BackendWaydroid:
		if m.OS != OSAndroid {
			bad("backend", ReasonMismatch)
		}
	}

	if m.MemoryMiB < MinMemoryMiB {
		bad("memory_mib", ReasonTooSmall)
	}
	if m.CPUs < MinCPUs {
		bad("cpus", ReasonTooSmall)
	}
	if m.DiskGiB < MinDiskGiB {
		bad("disk_gib", ReasonTooSmall)
	}

	if m.Backend == BackendQEMU {
		out = append(out, m.Boot.problems()...)
	}

	if !m.Display.Kind.valid() {
		bad("display.kind", ReasonEmpty)
	}
	if !m.Display.GPU.valid() {
		bad("display.gpu", ReasonEmpty)
	}
	switch m.Backend {
	case BackendDockur:
		// Windows has no virgl driver, and Dockur is reached over RDP or its
		// web console.
		if m.Display.Kind != DisplayRDP && m.Display.Kind != DisplayWeb {
			bad("display.kind", ReasonMismatch)
		}
		if m.Display.GPU != GPUBasic {
			bad("display.gpu", ReasonMismatch)
		}
	case BackendWaydroid:
		// Waydroid draws itself; there is no viewer of inua's.
		if m.Display.Kind != DisplayNone {
			bad("display.kind", ReasonMismatch)
		}
		if m.Display.GPU != GPUBasic {
			bad("display.gpu", ReasonMismatch)
		}
	case BackendQEMU:
		// 3D needs QEMU's own window or SPICE with gl=on, never the serial
		// console and never nothing.
		if m.Display.GPU != GPUBasic && (m.Display.Kind == DisplaySerial || m.Display.Kind == DisplayNone) {
			bad("display.gpu", ReasonMismatch)
		}
	}
	return out
}

// problems lists what QEMU needs from the boot section. A Dockur machine
// boots nothing itself: its container does.
func (b Boot) problems() []Problem {
	var out []Problem
	bad := func(field string, reason Reason) { out = append(out, Problem{Field: field, Reason: reason}) }
	switch {
	case !b.Firmware.valid():
		bad("boot.firmware", ReasonEmpty)
	}
	switch {
	case !b.Arch.valid():
		bad("boot.arch", ReasonEmpty)
	}
	switch {
	case !b.Accel.valid():
		bad("boot.accel", ReasonEmpty)
	}
	if strings.TrimSpace(b.Machine) == "" {
		bad("boot.machine", ReasonEmpty)
	}
	switch {
	case strings.TrimSpace(b.Disk) == "":
		bad("boot.disk", ReasonEmpty)
	case b.Disk != filepath.Base(b.Disk) || b.Disk == "." || b.Disk == "..":
		// The disk belongs to the machine, inside its folder.
		bad("boot.disk", ReasonNotInside)
	}
	for _, p := range []struct{ field, path string }{
		{"boot.iso", b.ISO}, {"boot.kernel", b.Kernel}, {"boot.initrd", b.Initrd},
	} {
		if p.path == "" {
			continue
		}
		if !isAbsolutePath(ExpandPath(p.path)) {
			bad(p.field, ReasonNotAPath)
		}
	}
	if strings.ContainsAny(b.Append, "\n\r\x00") {
		bad("boot.append", ReasonOneLine)
	}
	return out
}

// Validate is the first thing wrong with a machine, as the value a caller
// already handles (ErrorInvalidConfig).
func (m Machine) Validate() error {
	if problems := m.Problems(); len(problems) > 0 {
		p := problems[0]
		return &Error{
			Kind: ErrorInvalidConfig,
			Op:   "validate",
			Err:  fmt.Errorf("%s: %s", p.Field, p.Reason),
		}
	}
	return nil
}

func (o OS) valid() bool {
	switch o {
	case OSWindows, OSLinux, OSFreeBSD, OSAndroid, OSCustom:
		return true
	}
	return false
}

func (b BackendKind) valid() bool {
	switch b {
	case BackendQEMU, BackendDockur, BackendWaydroid:
		return true
	}
	return false
}

func (f Firmware) valid() bool { return f == FirmwareUEFI || f == FirmwareBIOS }

func (a Arch) valid() bool {
	switch a {
	case ArchX8664, ArchAarch64, ArchRiscv64, ArchI386:
		return true
	}
	return false
}

func (a Accel) valid() bool {
	switch a {
	case AccelAuto, AccelKVM, AccelTCG:
		return true
	}
	return false
}

func (d DisplayKind) valid() bool {
	switch d {
	case DisplaySPICE, DisplayGTK, DisplayRDP, DisplayWeb, DisplaySerial, DisplayNone:
		return true
	}
	return false
}

func (g GPU) valid() bool { return g == GPUVirgl || g == GPUVenus || g == GPUBasic }

// isAbsolutePath says whether a path is one a machine can use: absolute, and
// already clean, so nothing resolves somewhere else than it reads.
func isAbsolutePath(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p
}

// ExpandPath turns a machine's ~ path into an absolute one and leaves every
// other path as it is.
func ExpandPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// ParseMachine reads a machine.toml.
func ParseMachine(data []byte) (Machine, error) {
	var m Machine
	if err := toml.Unmarshal(data, &m); err != nil {
		return Machine{}, &Error{Kind: ErrorInvalidConfig, Op: "parse", Err: err}
	}
	return m, nil
}

// TOML is a machine as its file: every field the guide documents, with the
// lines that say what each one takes. Written by hand so a person editing
// machine.toml finds the same comments as in the guide.
func (m Machine) TOML() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "name = %s\n", tomlString(m.Name))
	fmt.Fprintf(&b, "os = %s            # %s\n", tomlString(string(m.OS)), "windows | linux | freebsd | android | custom")
	fmt.Fprintf(&b, "backend = %s     # %s\n", tomlString(string(m.Backend)), "qemu | dockur | waydroid")
	fmt.Fprintf(&b, "memory_mib = %d\n", m.MemoryMiB)
	fmt.Fprintf(&b, "cpus = %d\n", m.CPUs)
	fmt.Fprintf(&b, "disk_gib = %d\n", m.DiskGiB)
	b.WriteString("\n[boot]                 # qemu\n")
	fmt.Fprintf(&b, "iso = %s   # %s\n", tomlString(m.Boot.ISO), "install media, until installed")
	fmt.Fprintf(&b, "disk = %s\n", tomlString(m.Boot.Disk))
	fmt.Fprintf(&b, "kernel = %s             # %s\n", tomlString(m.Boot.Kernel), "a kernel to boot directly: a custom OS")
	fmt.Fprintf(&b, "initrd = %s\n", tomlString(m.Boot.Initrd))
	fmt.Fprintf(&b, "append = %s             # %s\n", tomlString(m.Boot.Append), "the kernel's command line")
	fmt.Fprintf(&b, "firmware = %s     # %s\n", tomlString(string(m.Boot.Firmware)), "uefi | bios")
	fmt.Fprintf(&b, "arch = %s       # %s\n", tomlString(string(m.Boot.Arch)), "x86_64 | aarch64 | riscv64 | i386: another than the host's runs without KVM")
	fmt.Fprintf(&b, "machine = %s\n", tomlString(m.Boot.Machine))
	fmt.Fprintf(&b, "accel = %s          # %s\n", tomlString(string(m.Boot.Accel)), "auto (KVM when it can, else emulation) | kvm (fail without it) | tcg (always emulate)")
	b.WriteString("\n[display]\n")
	fmt.Fprintf(&b, "kind = %s          # %s\n", tomlString(string(m.Display.Kind)), "spice | gtk | rdp | web | serial | none")
	fmt.Fprintf(&b, "gpu = %s           # %s\n", tomlString(string(m.Display.GPU)), "virgl (3D, OpenGL) | venus (3D, Vulkan; QEMU 9.2 and newer) | basic (2D)")
	b.WriteString("\n[seamless]\n")
	fmt.Fprintf(&b, "enabled = %t           # %s\n", m.Seamless.Enabled, "the backend's way: waypipe, RemoteApp, multi-window")
	return []byte(b.String())
}

// tomlString writes a TOML basic string, escaping what a machine's fields can
// hold (a Windows path's backslashes, a kernel command line's quotes).
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ErrNoMachine is what a lookup answers when there is no such machine.
var ErrNoMachine = errors.New("no such machine")
