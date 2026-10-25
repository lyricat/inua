package vm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/vm/testutil"
)

// The backend is what the window and the command line hold; the two optional
// interfaces are how the window asks what a machine has made and what it is
// saying while it starts.
var (
	_ Backend  = (*QEMUBackend)(nil)
	_ Prepared = (*QEMUBackend)(nil)
	_ Progress = (*QEMUBackend)(nil)
)

// newQEMUTestBackend puts the store under a temporary root and gives the
// backend host answers that never touch this host: a fast x86_64 machine with
// KVM, and an OVMF image that exists in the test's own directory. Anything else
// is what a case changes.
func newQEMUTestBackend(t *testing.T, opts *QEMUOptions) (*QEMUBackend, Machine, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	// The runtime directory holds sockets: socktest gives one whose paths fit
	// in sun_path whatever TMPDIR says.
	store := Store{Root: filepath.Join(root, "vm"), Runtime: testutil.Dir(t)}
	tmpl, ok := TemplateByID("linux")
	if !ok {
		t.Fatal("no linux template")
	}
	m := tmpl.Machine("ubuntu", Profile{MemoryMiB: 4096, CPUs: 4, DiskGiB: 40})
	firmware := filepath.Join(root, "OVMF_CODE.fd")
	if err := os.WriteFile(firmware, []byte("ovmf"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{fail: map[string]error{}}
	b := NewQEMUBackend(store, opts)
	b.Runner = runner
	b.Probes = QEMUProbes{
		HostArch:      func() Arch { return ArchX8664 },
		KVMPresent:    func() bool { return true },
		KVMAccessible: func() bool { return true },
		UEFIFirmware:  func() string { return firmware },
	}
	b.ShutdownBy = 200 * time.Millisecond
	return b, m, runner
}

// withDisk is the machine as it is after an install: its disk is there.
func withDisk(t *testing.T, b *QEMUBackend, m Machine) {
	t.Helper()
	if err := os.MkdirAll(b.Store.Dir(m.Name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.diskPath(m), []byte("qcow2"), 0600); err != nil {
		t.Fatal(err)
	}
}

// argValue is what follows flag in an argument list. Anything else in the list
// is a value of some other flag, so the pairs are what a test asserts on.
func argValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// argsNamed is every argument that follows one flag, in order: a machine can
// have more than one -drive.
func argsNamed(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

// command runs Command on a machine whose runtime directory the call makes.
func command(t *testing.T, b *QEMUBackend, m Machine) []string {
	t.Helper()
	argv, err := b.Command(m)
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	return argv
}

// A UEFI x86_64 guest with KVM: the whole line the guide describes, asserted
// part by part.
func TestQEMUCommandForAUEFIMachine(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	m.Boot.ISO = "~/Downloads/Fedora-Workstation-Live-x86_64-41.iso"
	argv := command(t, b, m)

	if argv[0] != "qemu-system-x86_64" {
		t.Fatalf("program = %q", argv[0])
	}
	if got, _ := argValue(argv, "-m"); got != "4096" {
		t.Errorf("-m = %q", got)
	}
	if got, _ := argValue(argv, "-smp"); got != "4" {
		t.Errorf("-smp = %q", got)
	}
	if got, _ := argValue(argv, "-machine"); got != "q35" {
		t.Errorf("-machine = %q", got)
	}
	// KVM, with the host's own processor features.
	if got, _ := argValue(argv, "-accel"); got != "kvm" {
		t.Errorf("-accel = %q, want kvm", got)
	}
	if got, _ := argValue(argv, "-cpu"); got != "host" {
		t.Errorf("-cpu = %q, want host", got)
	}
	// The guest's disk, inside the machine's folder, on virtio.
	if got, _ := argValue(argv, "-drive"); got != "file="+filepath.Join(b.Store.Dir(m.Name), "disk.qcow2")+",if=virtio" {
		t.Errorf("the first -drive is %q", got)
	}
	// The install media, with its ~ expanded: QEMU is started by systemd, which
	// has no shell to expand it.
	iso, _ := argValue(argv, "-cdrom")
	if want := ExpandPath(m.Boot.ISO); iso != want || strings.HasPrefix(iso, "~") {
		t.Errorf("-cdrom = %q, want %q", iso, want)
	}
	// UEFI: the read-only code image, and this machine's own writable variables.
	drives := argsNamed(argv, "-drive")
	if len(drives) != 3 {
		t.Fatalf("-drive count = %d: %v", len(drives), drives)
	}
	if !strings.HasPrefix(drives[1], "if=pflash,format=raw,readonly=on,file=") ||
		!strings.HasSuffix(drives[1], "OVMF_CODE.fd") {
		t.Errorf("the firmware drive is %q", drives[1])
	}
	efivars := filepath.Join(b.Store.Dir(m.Name), qemuEfivarsFile)
	if drives[2] != "if=pflash,format=raw,file="+efivars {
		t.Errorf("the variables drive is %q", drives[2])
	}
	if fi, err := os.Stat(efivars); err != nil {
		t.Errorf("no UEFI variable store was made: %v", err)
	} else if fi.Mode().Perm() != 0600 {
		t.Errorf("the UEFI variable store is %v", fi.Mode().Perm())
	}
	// The network: only this host's loopback reaches the guest's ssh.
	netdev, _ := argValue(argv, "-netdev")
	if !strings.HasPrefix(netdev, "user,id=net0,hostfwd=tcp:127.0.0.1:") || !strings.HasSuffix(netdev, "-:22") {
		t.Errorf("-netdev = %q", netdev)
	}
	if got, _ := argValue(argv, "-device"); got != "virtio-net-pci,netdev=net0" {
		t.Errorf("the first -device is %q", got)
	}
	// SPICE on the machine's own runtime directory, with 3D through the host.
	spice, ok := argValue(argv, "-spice")
	if !ok {
		t.Fatal("no -spice")
	}
	for _, want := range []string{
		"unix=on",
		"addr=" + filepath.Join(b.Store.RuntimeDir(m.Name), "spice.sock"),
		"disable-ticketing=on",
		"gl=on",
	} {
		if !strings.Contains(spice, want) {
			t.Errorf("-spice = %q, want %s", spice, want)
		}
	}
	if got, _ := argValue(argv, "-vga"); got != "none" {
		t.Errorf("-vga = %q, want none", got)
	}
	if !hasArg(argv, "virtio-vga-gl") {
		t.Errorf("no virtio-vga-gl: %v", argv)
	}
	// The serial console and the monitor, both in the runtime directory, both
	// started without anybody waiting on them.
	serial, _ := argValue(argv, "-serial")
	if serial != "unix:"+filepath.Join(b.Store.RuntimeDir(m.Name), "serial.sock")+",server,nowait" {
		t.Errorf("-serial = %q", serial)
	}
	qmp, _ := argValue(argv, "-qmp")
	if qmp != "unix:"+filepath.Join(b.Store.RuntimeDir(m.Name), "qmp.sock")+",server,nowait" {
		t.Errorf("-qmp = %q", qmp)
	}
	// No debugger unless it was asked for.
	if hasArg(argv, "-gdb") || hasArg(argv, "-s") || hasArg(argv, "-S") {
		t.Errorf("the gdb stub is in a command that did not ask for it: %v", argv)
	}
	if got := b.GDBCommand(m); got != "" {
		t.Errorf("GDBCommand = %q without the switch", got)
	}
}

// A BIOS machine has no firmware drives at all, and needs no OVMF to exist.
func TestQEMUCommandForABIOSMachine(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	b.Probes.UEFIFirmware = func() string { return "" } // no OVMF, and none needed
	m.Boot.Firmware = FirmwareBIOS
	argv := command(t, b, m)
	for _, drive := range argsNamed(argv, "-drive") {
		if strings.Contains(drive, "pflash") {
			t.Errorf("a BIOS machine has a firmware drive: %q", drive)
		}
	}
	// And a UEFI machine on the same host refuses, saying what to install.
	m.Boot.Firmware = FirmwareUEFI
	_, err := b.Command(m)
	if err == nil {
		t.Fatal("a UEFI machine started without any firmware")
	}
	if !strings.Contains(err.Error(), "ovmf") {
		t.Errorf("the refusal does not say what to install: %v", err)
	}
}

// The guest's architecture decides both the program and whether KVM can be
// used: an aarch64 guest on an x86_64 host is emulated, so no -cpu host either.
func TestQEMUCommandForAForeignArchitecture(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	m.Boot.Arch = ArchAarch64
	m.Boot.Machine = "virt"
	accel := b.Acceleration(m)
	if accel.Accelerated || accel.Reason != ReasonOtherArchitecture {
		t.Fatalf("acceleration = %+v", accel)
	}
	argv := command(t, b, m)
	if argv[0] != "qemu-system-aarch64" {
		t.Errorf("program = %q", argv[0])
	}
	if got, _ := argValue(argv, "-accel"); got != "tcg" {
		t.Errorf("-accel = %q, want tcg", got)
	}
	if got, ok := argValue(argv, "-cpu"); ok {
		t.Errorf("-cpu %q with an emulated guest", got)
	}
	if got, _ := argValue(argv, "-machine"); got != "virt" {
		t.Errorf("-machine = %q", got)
	}
}

// An OS in the making: a kernel and its command line, no install media.
func TestQEMUCommandForADirectKernel(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	m.OS = OSCustom
	m.Boot = Boot{
		Disk:     "disk.qcow2",
		Kernel:   "/usr/lib/kernels/vmlinuz",
		Initrd:   "/usr/lib/kernels/initrd",
		Append:   "console=ttyS0 root=/dev/vda",
		Firmware: FirmwareBIOS,
		Arch:     ArchX8664,
		Machine:  "q35",
		Accel:    AccelAuto,
	}
	m.Display = DisplayConfig{Kind: DisplaySerial, GPU: GPUBasic}
	argv := command(t, b, m)

	if got, _ := argValue(argv, "-kernel"); got != "/usr/lib/kernels/vmlinuz" {
		t.Errorf("-kernel = %q", got)
	}
	if got, _ := argValue(argv, "-initrd"); got != "/usr/lib/kernels/initrd" {
		t.Errorf("-initrd = %q", got)
	}
	if got, _ := argValue(argv, "-append"); got != "console=ttyS0 root=/dev/vda" {
		t.Errorf("-append = %q", got)
	}
	if got, ok := argValue(argv, "-cdrom"); ok {
		t.Errorf("a custom OS with no ISO got -cdrom %q", got)
	}
	// The serial console is this guest's only window, and it is a socket.
	if got, _ := argValue(argv, "-display"); got != "none" {
		t.Errorf("-display = %q, want none", got)
	}
	if got, _ := argValue(argv, "-serial"); got == "" {
		t.Error("no serial console")
	}
	if hasArg(argv, "virtio-vga-gl") {
		t.Error("2D graphics on a machine that asked for basic")
	}
}

// The serial console is served on the machine's own socket, and a machine can
// be held at its first instruction for gdb.
func TestQEMUSerialAndTheGDBStub(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts *QEMUOptions
		// gdb is the machine's own switch, which the window edits and the file
		// keeps: the option is for a caller that has no machine of its own.
		gdb  bool
		want bool
	}{
		{"without the switch", nil, false, false},
		{"with the option", &QEMUOptions{GDB: true}, false, true},
		{"with the machine's own switch", nil, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, m, _ := newQEMUTestBackend(t, tc.opts)
			m.Boot.GDB = tc.gdb
			argv := command(t, b, m)
			// The stub is on this host's loopback and holds the guest stopped:
			// no -s, because -s is `-gdb tcp::1234` and does not pin the host.
			stub, hasStub := argValue(argv, "-gdb")
			got := hasStub && stub == "tcp:127.0.0.1:1234" && hasArg(argv, "-S")
			if got != tc.want {
				t.Errorf("gdb stub present = %v, want %v: %v", got, tc.want, argv)
			}
			if hasArg(argv, "-s") {
				t.Errorf("the stub is not pinned to the loopback interface: %v", argv)
			}
			if hasArg(argv, "-S") != tc.want {
				t.Errorf("-S present = %v, want %v", hasArg(argv, "-S"), tc.want)
			}
			line := b.GDBCommand(m)
			if tc.want {
				// The line the window shows and copies.
				if !strings.Contains(line, "target remote 127.0.0.1:1234") {
					t.Errorf("GDBCommand = %q", line)
				}
			} else if line != "" {
				t.Errorf("GDBCommand = %q without the switch", line)
			}
		})
	}
}

// The graphics: what the machine asked for is what the guest's devices are, and
// gl=on follows the GPU, never the serial console.
func TestQEMUCommandGraphics(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      DisplayKind
		gpu       GPU
		wantDisp  string // the -display value, "" when SPICE is used instead
		spiceGL   string // the gl= in -spice, "" when there is no -spice
		wantDevic string
	}{
		{"spice with virgl (3D)", DisplaySPICE, GPUVirgl, "", "gl=on", "virtio-vga-gl"},
		{"spice with venus (Vulkan)", DisplaySPICE, GPUVenus, "", "gl=on", "virtio-gpu-gl,venus=on,blob=on"},
		{"spice with basic (2D)", DisplaySPICE, GPUBasic, "", "gl=off", "virtio-vga"},
		{"gtk with virgl (3D)", DisplayGTK, GPUVirgl, "gtk,gl=on", "", "virtio-vga-gl"},
		{"gtk with basic (2D)", DisplayGTK, GPUBasic, "gtk,gl=off", "", "virtio-vga"},
		{"the serial console", DisplaySerial, GPUBasic, "none", "", "virtio-vga"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, m, _ := newQEMUTestBackend(t, nil)
			m.Display = DisplayConfig{Kind: tc.kind, GPU: tc.gpu}
			argv := command(t, b, m)

			display, hasDisplay := argValue(argv, "-display")
			if tc.wantDisp == "" {
				if hasDisplay {
					t.Errorf("-display %q on a SPICE machine: %v", display, argv)
				}
				spice, ok := argValue(argv, "-spice")
				if !ok {
					t.Fatalf("no -spice: %v", argv)
				}
				if !strings.Contains(spice, tc.spiceGL) {
					t.Errorf("-spice = %q, want %s", spice, tc.spiceGL)
				}
				if got, _ := argValue(argv, "-vga"); got != "none" {
					t.Errorf("-vga = %q alongside SPICE", got)
				}
			} else if display != tc.wantDisp {
				t.Errorf("-display = %q, want %q", display, tc.wantDisp)
			}
			if !hasArg(argv, tc.wantDevic) {
				t.Errorf("no %s device: %v", tc.wantDevic, argv)
			}
		})
	}
}

// The acceleration matrix: what the guest gets and why, on a host that is
// whatever the case says it is.
func TestQEMUAccelerationMatrix(t *testing.T) {
	for _, tc := range []struct {
		name       string
		host       Arch
		guest      Arch
		present    bool
		accessible bool
		accel      Accel
		want       bool
		wantReason string
	}{
		{"KVM, auto", ArchX8664, ArchX8664, true, true, AccelAuto, true, ""},
		{"KVM, asked for", ArchX8664, ArchX8664, true, true, AccelKVM, true, ""},
		{"asked for emulation", ArchX8664, ArchX8664, true, true, AccelTCG, false, ReasonRequestedEmulation},
		{"asked for emulation, even without KVM", ArchX8664, ArchX8664, false, false, AccelTCG, false, ReasonRequestedEmulation},
		{"another architecture", ArchX8664, ArchAarch64, true, true, AccelAuto, false, ReasonOtherArchitecture},
		{"no /dev/kvm", ArchX8664, ArchX8664, false, false, AccelAuto, false, ReasonNoKVM},
		{"no access to /dev/kvm", ArchX8664, ArchX8664, true, false, AccelAuto, false, ReasonKVMDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, m, _ := newQEMUTestBackend(t, nil)
			b.Probes.HostArch = func() Arch { return tc.host }
			b.Probes.KVMPresent = func() bool { return tc.present }
			b.Probes.KVMAccessible = func() bool { return tc.accessible }
			m.Boot.Arch, m.Boot.Accel = tc.guest, tc.accel

			got := b.Acceleration(m)
			if got.Accelerated != tc.want || got.Reason != tc.wantReason {
				t.Fatalf("acceleration = %+v, want accelerated=%v reason=%q", got, tc.want, tc.wantReason)
			}
			if got.Accelerated && got.Reason != "" {
				t.Errorf("an accelerated guest carries a reason: %+v", got)
			}
			if !got.Accelerated && got.Reason == "" {
				t.Errorf("an emulated guest carries no reason: %+v", got)
			}
			// And the command line agrees with the answer.
			argv, err := b.Command(m)
			if err != nil {
				t.Fatalf("command: %v", err)
			}
			accel, _ := argValue(argv, "-accel")
			if tc.want && accel != "kvm" {
				t.Errorf("-accel = %q for an accelerated guest", accel)
			}
			if !tc.want && accel != "tcg" {
				t.Errorf("-accel = %q for an emulated guest", accel)
			}
		})
	}
}

// A machine set to kvm refuses to start without it: falling back to emulation
// would be the opposite of what it asked for.
func TestQEMUCommandRefusesAKVMMachineWithoutKVM(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	b.Probes.KVMPresent = func() bool { return false }
	m.Boot.Accel = AccelKVM
	_, err := b.Command(m)
	if err == nil {
		t.Fatal("a machine set to kvm started without KVM")
	}
	for _, want := range []string{"kvm", ReasonNoKVM} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	// The same machine set to auto starts, emulated.
	m.Boot.Accel = AccelAuto
	argv := command(t, b, m)
	if got, _ := argValue(argv, "-accel"); got != "tcg" {
		t.Errorf("-accel = %q", got)
	}
}

// The reasons are stable identifiers: the window words them in the user's
// language, so they cannot drift.
func TestQEMUAccelerationReasonsAreStable(t *testing.T) {
	for got, want := range map[string]string{
		ReasonRequestedEmulation: "requested_emulation",
		ReasonOtherArchitecture:  "other_architecture",
		ReasonNoKVM:              "no_kvm",
		ReasonKVMDenied:          "kvm_denied",
	} {
		if got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	}
}

// Install makes the disk once, and never touches one that is already there:
// that file is the installed guest.
func TestQEMUInstallMakesTheDiskOnce(t *testing.T) {
	b, m, runner := newQEMUTestBackend(t, nil)
	ctx := ctxT(t)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install: %v", err)
	}
	want := "qemu-img create -f qcow2 " + filepath.Join(b.Store.Dir(m.Name), "disk.qcow2") + " 40G"
	if !runner.ran(want) {
		t.Fatalf("no %q:\n%v", want, runner.calls)
	}
	if n := countCalls(runner, "qemu-img"); n != 1 {
		t.Errorf("qemu-img ran %d times", n)
	}
	// A machine whose disk is there is left alone: applying the same machine
	// again (or a bigger memory) never rewrites its disk.
	withDisk(t, b, m)
	if err := b.Install(ctx, m); err != nil {
		t.Fatalf("install over an existing disk: %v", err)
	}
	if n := countCalls(runner, "qemu-img"); n != 1 {
		t.Errorf("qemu-img ran %d times over an existing disk", n)
	}
}

// And an install refuses a machine of another backend outright.
func TestQEMUInstallRefusesAMachineOfAnotherBackend(t *testing.T) {
	b, m, runner := newQEMUTestBackend(t, nil)
	m.Backend = BackendDockur
	m.Display = DisplayConfig{Kind: DisplayRDP, GPU: GPUBasic}
	err := b.Install(ctxT(t), m)
	if err == nil {
		t.Fatal("the QEMU backend accepted a Dockur machine")
	}
	if !strings.Contains(err.Error(), ErrNotThisBackend.Error()) {
		t.Errorf("the refusal does not say what happened: %v", err)
	}
	if n := countCalls(runner, "qemu-img"); n != 0 {
		t.Errorf("a refused install ran qemu-img %d times", n)
	}
}

// Two machines forward their guest's ssh to different loopback ports, and each
// one's command line carries its own.
func TestQEMUSSHPortsAreReservedPerMachine(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	second := m
	second.Name = "ubuntu-2"

	port := func(machine Machine) int {
		t.Helper()
		path := filepath.Join(b.Store.RuntimeDir(machine.Name), qemuPortFile)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("the port of %s: %v", machine.Name, err)
		}
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0600 {
			t.Errorf("the port file of %s is not 0600: %v %v", machine.Name, fi, err)
		}
		var got int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &got); err != nil {
			t.Fatalf("the port file of %s: %v", machine.Name, err)
		}
		return got
	}

	first := command(t, b, m)
	secondArgv := command(t, b, second)
	firstPort, secondPort := port(m), port(second)
	if firstPort == secondPort {
		t.Errorf("two machines share the ssh port %d", firstPort)
	}
	for _, one := range []struct {
		machine Machine
		argv    []string
	}{{m, first}, {second, secondArgv}} {
		got, _ := argValue(one.argv, "-netdev")
		if want := fmt.Sprintf("user,id=net0,hostfwd=tcp:127.0.0.1:%d-:22", port(one.machine)); got != want {
			t.Errorf("%s forwards %q, want %q", one.machine.Name, got, want)
		}
	}
	// A second Command keeps the port the machine already recorded, and SSHPort
	// is what the seamless launcher reaches the guest's ssh on.
	again := command(t, b, m)
	if got, _ := argValue(again, "-netdev"); !strings.Contains(got, fmt.Sprintf(":%d-:22", firstPort)) {
		t.Errorf("the reserved port changed under the machine: %q", got)
	}
	if got, err := b.SSHPort(m); err != nil || got != firstPort {
		t.Errorf("SSHPort = %d (%v), want %d", got, err, firstPort)
	}
}

// Start hands the whole line to a transient systemd user unit, so the machine
// outlives the window and dies with the session, and returns at once; the
// unit's memory is fenced, so a full guest slows or is killed, not the host.
func TestQEMUStartRunsUnderASystemdUnit(t *testing.T) {
	b, m, runner := newQEMUTestBackend(t, nil)
	withDisk(t, b, m)
	if err := b.Start(ctxT(t), m); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Fenced on the host: the guest's 4 GiB and QEMU's overhead, then killed.
	want := "systemd-run --user --unit inua-vm-ubuntu --collect --property=MemoryHigh=5120M --property=MemoryMax=6144M --property=MemorySwapMax=1024M --property=OOMPolicy=kill qemu-system-x86_64 -m 4096 -smp 4"
	if !runner.ran(want) {
		t.Fatalf("no %q:\n%v", want, runner.calls)
	}
	if got := b.UnitName(m); got != "inua-vm-ubuntu" {
		t.Errorf("UnitName = %q", got)
	}
	if !b.Prepared(m) {
		t.Error("a machine with a disk is not prepared")
	}

	// A machine that was never installed has nothing to run.
	fresh := m
	fresh.Name = "ubuntu-3"
	if err := b.Start(ctxT(t), fresh); err == nil {
		t.Error("a machine without a disk started")
	}
	if runner.ran("systemd-run --user --unit inua-vm-ubuntu-3") {
		t.Error("a machine without a disk was handed to systemd")
	}
}

// Display, and the paths the window needs beside it: the console's socket and
// log, the monitor, and the unit.
func TestQEMUDisplayAndPaths(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	runtime := b.Store.RuntimeDir(m.Name)
	for _, tc := range []struct {
		kind DisplayKind
		gpu  GPU
		want Display
	}{
		{DisplaySPICE, GPUVirgl, Display{Kind: DisplaySPICE, Address: filepath.Join(runtime, "spice.sock")}},
		{DisplayGTK, GPUVirgl, Display{Kind: DisplayGTK}},
		{DisplaySerial, GPUBasic, Display{Kind: DisplaySerial, Address: filepath.Join(runtime, "serial.sock")}},
	} {
		m.Display = DisplayConfig{Kind: tc.kind, GPU: tc.gpu}
		got, err := b.Display(ctxT(t), m)
		if err != nil {
			t.Fatalf("display: %v", err)
		}
		if got != tc.want {
			t.Errorf("display = %+v, want %+v", got, tc.want)
		}
		if strings.Contains(got.Address, "0.0.0.0") {
			t.Errorf("the display is not loopback: %+v", got)
		}
	}
	if b.QMPPath(m) != filepath.Join(runtime, "qmp.sock") {
		t.Errorf("QMPPath = %q", b.QMPPath(m))
	}
	if b.SerialPath(m) != filepath.Join(runtime, "serial.sock") {
		t.Errorf("SerialPath = %q", b.SerialPath(m))
	}
	if b.SerialLogPath(m) != filepath.Join(runtime, "serial.log") {
		t.Errorf("SerialLogPath = %q", b.SerialLogPath(m))
	}
}

// The serial console is mirrored into the machine's log, and its last line is
// what the window shows while a guest boots.
func TestQEMUTeeSerialMirrorsTheConsole(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	if _, err := b.Store.EnsureRuntime(m.Name); err != nil {
		t.Fatal(err)
	}
	// The console's socket is the backend's own path.
	ln, err := net.Listen("unix", b.SerialPath(m))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	const line = "[    0.418376] booting Linux\n"
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("noise\n" + line))
		_ = conn.Close()
	}()
	if err := b.TeeSerial(ctxT(t), m); err != nil {
		t.Fatalf("tee: %v", err)
	}
	log, err := os.ReadFile(b.SerialLogPath(m))
	if err != nil {
		t.Fatalf("the serial log: %v", err)
	}
	if string(log) != "noise\n"+line {
		t.Errorf("the serial log is %q", log)
	}
	if fi, err := os.Stat(b.SerialLogPath(m)); err != nil || fi.Mode().Perm() != 0600 {
		t.Errorf("the serial log is not 0600: %v %v", fi, err)
	}
	if got := b.RecentOutput(ctxT(t), m); got != strings.TrimSpace(line) {
		t.Errorf("RecentOutput = %q", got)
	}
}

// Remove takes the process and the runtime files apart, and the disk only when
// it is asked to: losing an installation is a second, deliberate step.
func TestQEMURemoveKeepsTheDiskUntilAsked(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	ctx := ctxT(t)
	if err := b.Store.Save(m); err != nil {
		t.Fatal(err)
	}
	withDisk(t, b, m)
	disk := b.diskPath(m)
	if err := b.Remove(ctx, m, true); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(disk); err != nil {
		t.Fatalf("remove deleted the guest's disk: %v", err)
	}
	if _, err := os.Stat(b.Store.RuntimeDir(m.Name)); !os.IsNotExist(err) {
		t.Error("the machine's runtime directory survived removal")
	}
	if err := b.Remove(ctx, m, false); err != nil {
		t.Fatalf("removing the disk: %v", err)
	}
	if _, err := os.Stat(disk); !os.IsNotExist(err) {
		t.Error("the disk survived the deletion it was asked for")
	}
	if _, err := os.Stat(b.Store.File(m.Name)); err != nil {
		t.Errorf("the machine's own definition went with its disk: %v", err)
	}
	if got, err := b.Store.Load(m.Name); err != nil || got.Name != m.Name {
		t.Errorf("the machine no longer loads: %+v (%v)", got, err)
	}
}

// ---- the monitor, on a socket the test serves ----------------------------

// fakeQMP is a QEMU monitor on a real unix socket in the test's own temporary
// directory: the greeting, the capabilities, query-status, and the two ways of
// stopping a guest.
type fakeQMP struct {
	t      *testing.T
	path   string
	ln     net.Listener
	status string
	// holdOpen is a guest that ignores system_powerdown: QEMU is left running
	// for Stop's timeout to run out.
	holdOpen bool

	mu       sync.Mutex
	commands []string
	once     sync.Once
}

func newFakeQMP(t *testing.T, b *QEMUBackend, m Machine) *fakeQMP {
	t.Helper()
	if _, err := b.Store.EnsureRuntime(m.Name); err != nil {
		t.Fatal(err)
	}
	f := &fakeQMP{t: t, path: b.QMPPath(m), status: "running"}
	ln, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.ln = ln
	go f.serve()
	t.Cleanup(f.exit)
	return f
}

func (f *fakeQMP) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeQMP) handle(conn net.Conn) {
	defer conn.Close()
	fmt.Fprint(conn, "{\"QMP\":{\"version\":{\"qemu\":{\"major\":9,\"minor\":2,\"micro\":0},\"package\":\"\"},\"capabilities\":[]}}\r\n")
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		var req struct {
			Execute string `json:"execute"`
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		f.record(req.Execute)
		switch req.Execute {
		case "qmp_capabilities":
			fmt.Fprint(conn, "{\"return\":{}}\r\n")
		case "query-status":
			fmt.Fprintf(conn, "{\"return\":{\"status\":%q}}\r\n", f.Status())
		case "system_powerdown":
			// An event arrives in the middle of a command and its answer, which
			// is what the client has to skip.
			fmt.Fprint(conn, "{\"event\":\"POWERDOWN\",\"data\":{},\"timestamp\":{\"seconds\":0,\"microseconds\":0}}\r\n")
			fmt.Fprint(conn, "{\"return\":{}}\r\n")
			if !f.holdOpen {
				f.exit() // the guest did shut down
			}
		case "quit":
			fmt.Fprint(conn, "{\"return\":{}}\r\n")
			f.exit()
		default:
			fmt.Fprint(conn, "{\"error\":{\"class\":\"CommandNotFound\"}}\r\n")
		}
	}
}

// exit is QEMU leaving: its sockets go with it.
func (f *fakeQMP) exit() {
	f.once.Do(func() {
		_ = f.ln.Close()
		_ = os.Remove(f.path)
	})
}

func (f *fakeQMP) record(command string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, command)
}

func (f *fakeQMP) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

func (f *fakeQMP) Status() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeQMP) setStatus(status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

// State is QEMU's own answer about the guest, mapped onto this package's.
func TestQEMUStateReadsTheMonitor(t *testing.T) {
	for _, tc := range []struct {
		qemu  string
		want  State
		error bool
	}{
		{"running", StateRunning, false},
		{"paused", StateRunning, false},
		{"shutdown", StateStopped, false},
		{"prelaunch", StateStarting, false}, // held at -S for gdb: its unit is still there
		{"debug", StateStarting, false},
		{"guest-panicked", StateUnknown, true},
	} {
		t.Run(tc.qemu, func(t *testing.T) {
			b, m, _ := newQEMUTestBackend(t, nil)
			monitor := newFakeQMP(t, b, m)
			monitor.setStatus(tc.qemu)
			state, err := b.State(ctxT(t), m)
			if (err != nil) != tc.error {
				t.Fatalf("state %q: state = %q, error = %v", tc.qemu, state, err)
			}
			if state != tc.want {
				t.Errorf("state %q = %q, want %q", tc.qemu, state, tc.want)
			}
			// The client took the capabilities first, as the protocol says.
			if got := monitor.got(); len(got) < 2 || got[0] != "qmp_capabilities" || got[1] != "query-status" {
				t.Errorf("the monitor was asked %v", got)
			}
		})
	}
}

// No monitor socket at all is a machine that is not running - not an error.
func TestQEMUStateWithoutAMonitor(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	state, err := b.State(ctxT(t), m)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state != StateStopped {
		t.Errorf("state = %q, want %q", state, StateStopped)
	}
	if err := b.Stop(ctxT(t), m); err != nil {
		t.Errorf("stopping a machine that is not running: %v", err)
	}
}

// A guest that shuts down when it is asked is never quit: the disk is not
// pulled out from under a kernel that is still writing to it.
func TestQEMUStopAsksTheGuestToPowerDown(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	monitor := newFakeQMP(t, b, m)
	if err := b.Stop(ctxT(t), m); err != nil {
		t.Fatalf("stop: %v", err)
	}
	got := monitor.got()
	if len(got) != 2 || got[0] != "qmp_capabilities" || got[1] != "system_powerdown" {
		t.Fatalf("the monitor was asked %v", got)
	}
	for _, command := range got {
		if command == "quit" {
			t.Error("a guest that shut down was quit anyway")
		}
	}
	if _, err := os.Stat(b.QMPPath(m)); !os.IsNotExist(err) {
		t.Error("the monitor socket outlived the machine")
	}
}

// A guest that never shuts down: after the grace runs out QEMU is told to quit,
// and the call still returns.
func TestQEMUStopQuitsWhenTheGuestDoesNotShutDown(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	b.ShutdownBy = 50 * time.Millisecond
	monitor := newFakeQMP(t, b, m)
	monitor.holdOpen = true

	started := time.Now()
	if err := b.Stop(ctxT(t), m); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("stop took %v", elapsed)
	}
	var sawPowerdown, sawQuit bool
	for _, command := range monitor.got() {
		switch command {
		case "system_powerdown":
			sawPowerdown = true
		case "quit":
			if !sawPowerdown {
				t.Fatal("the guest was quit before it was asked to shut down")
			}
			sawQuit = true
		}
	}
	if !sawPowerdown || !sawQuit {
		t.Errorf("the monitor was asked %v", monitor.got())
	}
}

// An installer often ignores the power button, so while the install image is
// attached Stop waits only a short while before QEMU is told to quit.
func TestQEMUStopWaitsLessWhileInstalling(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	b.ShutdownBy = 0
	m.Boot.ISO = ""
	if got := b.shutdownGrace(m); got != qemuShutdownGrace {
		t.Errorf("installed: grace = %v, want %v", got, qemuShutdownGrace)
	}
	m.Boot.ISO = "/srv/ubuntu.iso"
	if got := b.shutdownGrace(m); got != qemuInstallShutdownGrace {
		t.Errorf("installing: grace = %v, want %v", got, qemuInstallShutdownGrace)
	}
	b.ShutdownBy = time.Second
	if got := b.shutdownGrace(m); got != time.Second {
		t.Errorf("installing, shorter grace set: grace = %v, want 1s", got)
	}
}

// countCalls is how often the runner was asked for one program.
func countCalls(r *fakeRunner, program string) int {
	n := 0
	for _, call := range r.calls {
		if call == program || strings.HasPrefix(call, program+" ") {
			n++
		}
	}
	return n
}

// A context that is already over is what a window closing is: Stop says so
// rather than waiting for a guest nobody is watching any more.
func TestQEMUStopHonoursTheContext(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	b.ShutdownBy = 30 * time.Second
	monitor := newFakeQMP(t, b, m)
	monitor.holdOpen = true
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := b.Stop(ctx, m)
	if err == nil {
		t.Fatal("stopping a wedged guest succeeded without a deadline")
	}
	if !strings.Contains(err.Error(), string(ErrorUnavailable)) {
		t.Errorf("the error does not say the caller gave up: %v", err)
	}
}

// A machine's name may hold a space or a comma; its unit name is escaped as
// systemd-escape escapes it, or systemd-run would refuse the unit.
func TestQEMUUnitNameEscaped(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	for name, want := range map[string]string{
		"ubuntu":    "inua-vm-ubuntu",
		"my fedora": `inua-vm-my\x20fedora`,
		"a,b-c":     `inua-vm-a\x2cb\x2dc`,
		"v1.2_x:y":  "inua-vm-v1.2_x:y",
		"日本":        `inua-vm-\xe6\x97\xa5\xe6\x9c\xac`,
	} {
		m.Name = name
		if got := b.UnitName(m); got != want {
			t.Errorf("UnitName(%q) = %q, want %q", name, got, want)
		}
	}
}

// A comma in a path is doubled where the path goes into QEMU's options, so it
// stays the path: a machine's name, its disk's and its image's.
func TestQEMUCommandQuotesCommas(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, nil)
	m.Name = "a,b"
	iso := filepath.Join(t.TempDir(), "x,if=none.iso")
	m.Boot.ISO = iso
	argv := command(t, b, m)
	drive, _ := argValue(argv, "-drive")
	if want := "file=" + strings.ReplaceAll(b.diskPath(m), ",", ",,") + ",if=virtio"; drive != want {
		t.Errorf("-drive = %q, want %q", drive, want)
	}
	if cd, _ := argValue(argv, "-cdrom"); cd != strings.ReplaceAll(iso, ",", ",,") {
		t.Errorf("-cdrom = %q", cd)
	}
	if qmp, _ := argValue(argv, "-qmp"); !strings.Contains(qmp, "a,,b") || !strings.HasSuffix(qmp, ",server,nowait") {
		t.Errorf("-qmp = %q", qmp)
	}
}

// Two machines held for gdb each have a stub port of their own.
func TestQEMUGDBPortPerMachine(t *testing.T) {
	b, m, _ := newQEMUTestBackend(t, &QEMUOptions{GDB: true})
	other := m
	other.Name = "other"
	first, _ := argValue(command(t, b, m), "-gdb")
	second, _ := argValue(command(t, b, other), "-gdb")
	if first == "" || second == "" || first == second {
		t.Errorf("gdb stubs %q and %q", first, second)
	}
	if !strings.HasPrefix(first, "tcp:127.0.0.1:") || !strings.Contains(b.GDBCommand(m), strings.TrimPrefix(first, "tcp:")) {
		t.Errorf("stub %q, GDBCommand %q", first, b.GDBCommand(m))
	}
}
