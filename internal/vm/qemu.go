package vm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// The files a QEMU machine keeps in its runtime directory, beside its disk in
// its own folder: the monitor socket QEMU's control protocol speaks on, the
// serial console's socket and its log, what SPICE listens on, and the loopback
// port the guest's ssh is forwarded to. $XDG_RUNTIME_DIR is cleared at log-out,
// which is right: none of this outlives the machine.
const (
	qemuQMPFile     = "qmp.sock"
	qemuSPICESock   = "spice.sock"
	qemuSerialSock  = "serial.sock"
	qemuSerialLog   = "serial.log"
	qemuPortFile    = "ssh-port"
	qemuGDBPortFile = "gdb-port"
	qemuEfivarsFile = "efivars.fd"

	// qemuUnitPrefix names the systemd user unit a machine runs as:
	// `inua-vm-NAME`, so `systemctl --user status inua-vm-fedora` is the whole
	// story of a machine that will not start.
	qemuUnitPrefix = "inua-vm-"

	// qemuSSHPortFrom is where the first machine's forwarded ssh port is looked
	// for. 2222 is where a second ssh on one host is usually put.
	qemuSSHPortFrom = 2222

	// qemuGDBPortFrom is where the first machine's gdb stub port is looked
	// for: 1234 is QEMU's own (`-s`), and each machine reserves its own from
	// there, so two machines held for gdb never ask for the same one.
	qemuGDBPortFrom = 1234

	// qemuShutdownGrace is how long Stop waits for the guest to act on
	// system_powerdown before QEMU is told to quit. A guest doing `shutdown -h`
	// takes seconds; one that is wedged would otherwise hold a window for
	// minutes, which is what QEMU's own default powerdown grace allows.
	qemuShutdownGrace = 90 * time.Second

	// qemuInstallShutdownGrace is that wait while the install image is still
	// attached. Installers commonly ignore the power button (Ubuntu's live
	// server installer does), and stopping one abandons the installation
	// anyway, so the user is not kept waiting the full grace for nothing.
	qemuInstallShutdownGrace = 10 * time.Second

	// qmpDeadline is how long one monitor command may take before the caller is
	// told the machine is not answering.
	qmpDeadline = 5 * time.Second

	// qemuEfivarsSize is the size every OVMF build expects its variable store
	// to be. A qcow2 disk is grown as it is written; this is not, so it is
	// made at its full size and never touched again.
	qemuEfivarsSize = 4 << 20
)

// QEMUOptions is this backend's own settings: what a machine has no field for
// yet.
//
// The debugger is one. The guide gives it as a switch in the window, and a
// switch is not part of what a machine is - a machine.toml written on another
// host should not carry someone's debugging session - so it lives here for
// now, and the window passes what its switch says when it builds the backend.
// The window and the command line build it as:
//
//	b := vm.NewQEMUBackend(store, &vm.QEMUOptions{GDB: gdbSwitch})
type QEMUOptions struct {
	// GDB asks for QEMU's gdb stub: `-s -S`. An OS in the making is debugged
	// from its first instruction, so -S is not separable from -s here.
	GDB bool
}

// QEMUProbes are the host facts this backend needs, injectable so a test does
// not have to be run on a host of a given shape: the machine in CI is not the
// machine on the desk.
type QEMUProbes struct {
	// HostArch is the architecture QEMU runs on, as an Arch. Only a guest of
	// the host's own architecture can use KVM.
	HostArch func() Arch
	// KVMPresent says whether /dev/kvm exists at all.
	KVMPresent func() bool
	// KVMAccessible says whether this user can open /dev/kvm. Access is what
	// matters, not group membership: on many distributions logind grants the
	// session's user an ACL and nothing needs changing.
	KVMAccessible func() bool
	// UEFIFirmware is the OVMF code image to use, or "" when the host has none,
	// which is what makes a UEFI machine fail with a message that says what to
	// install.
	UEFIFirmware func() string
}

// QEMUBackend runs a guest with QEMU, the machine's own disk being its whole
// state: no container, no image, no daemon. Like the Docker backend it holds
// nothing of its own, so the same backend serves every machine and two callers
// never fight over one.
type QEMUBackend struct {
	Store      Store
	Runner     CommandRunner
	Options    QEMUOptions
	Probes     QEMUProbes
	ShutdownBy time.Duration // how long Stop waits for a clean power down
}

// NewQEMUBackend builds the backend with the host probes it needs, so nothing
// else in the file has to know whether they were filled in or injected.
func NewQEMUBackend(store Store, opts *QEMUOptions) *QEMUBackend {
	b := &QEMUBackend{Store: store, ShutdownBy: qemuShutdownGrace, Probes: defaultQEMUProbes()}
	if opts != nil {
		b.Options = *opts
	}
	return b
}

// defaultQEMUProbes are the real host's answers.
func defaultQEMUProbes() QEMUProbes {
	return QEMUProbes{
		HostArch: hostArch,
		KVMPresent: func() bool {
			_, err := os.Stat("/dev/kvm")
			return err == nil
		},
		KVMAccessible: func() bool {
			f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
			if err != nil {
				return false
			}
			_ = f.Close()
			return true
		},
		// The one search for firmware on this host: the platform's check and
		// this backend must never disagree about whether there is one.
		UEFIFirmware: UEFIFirmwarePath,
	}
}

// hostArch is this host's architecture as an Arch, or "" when it is one this
// module has no name for. Go's and QEMU's names agree for the four that matter.
func hostArch() Arch {
	switch runtime.GOARCH {
	case "amd64":
		return ArchX8664
	case "arm64":
		return ArchAarch64
	case "riscv64":
		return ArchRiscv64
	case "386":
		return ArchI386
	}
	return ""
}

// Acceleration is what a machine's guest will get, and why, so the window can
// say "Accelerated" or "Emulated (slow)" beside it and word the reason in the
// user's language. Accelerated is true exactly when Reason is empty.
type Acceleration struct {
	Accelerated bool
	Reason      string
}

// Why a guest is not accelerated. These are stable identifiers, not sentences:
// the window says them in the user's language.
const (
	// ReasonRequestedEmulation: the machine is set to accel = "tcg". That is a
	// choice - an OS in the making is easier to watch under an emulator - and
	// not a shortcoming of the host.
	ReasonRequestedEmulation = "requested_emulation"
	// ReasonOtherArchitecture: only the host's own architecture can use KVM,
	// so an aarch64 or riscv64 guest is emulated here by definition.
	ReasonOtherArchitecture = "other_architecture"
	// ReasonNoKVM: there is no /dev/kvm, so virtualisation is off in the
	// firmware or the module is not loaded.
	ReasonNoKVM = "no_kvm"
	// ReasonKVMDenied: /dev/kvm is there but this user cannot open it.
	ReasonKVMDenied = "kvm_denied"
)

// Acceleration is what this host and this machine's settings give its guest.
//
// The answers are ordered by what the user can act on: what they asked for
// first, then the architecture, then whether the host can virtualise at all.
func (b *QEMUBackend) Acceleration(m Machine) Acceleration {
	p := b.probes()
	switch m.Boot.Accel {
	case AccelTCG:
		return Acceleration{Reason: ReasonRequestedEmulation}
	case AccelKVM, AccelAuto:
		// Both need KVM. What they differ in is only what happens without it,
		// and that is Start's business, not this answer's.
	}
	if host, guest := p.HostArch(), m.Boot.Arch; host != "" && guest != "" && host != guest {
		return Acceleration{Reason: ReasonOtherArchitecture}
	}
	switch {
	case !p.KVMPresent():
		return Acceleration{Reason: ReasonNoKVM}
	case !p.KVMAccessible():
		return Acceleration{Reason: ReasonKVMDenied}
	}
	return Acceleration{Accelerated: true}
}

// useKVM says whether this machine's guest runs on the host's processor, and
// refuses - rather than falls back - a machine its owner set to kvm: quietly
// emulating a machine that says "fail without it" is the opposite of what it
// asked for, and it would be slow for no stated reason.
func (b *QEMUBackend) useKVM(m Machine) (bool, error) {
	accel := b.Acceleration(m)
	if accel.Accelerated {
		return true, nil
	}
	if m.Boot.Accel == AccelKVM {
		return false, fmt.Errorf("this machine is set to accel = %q, and KVM is not usable here: %s", string(AccelKVM), accel.Reason)
	}
	return false, nil
}

// Install makes the machine's disk, once.
//
// QEMU needs nothing else of its own: the guest is installed into this disk
// from the machine's install media by the guest itself. An existing disk is
// never touched - it holds the installed guest - which is what makes this safe
// to call again, and it is all the window's Apply has to do for a QEMU
// machine.
func (b *QEMUBackend) Install(ctx context.Context, m Machine) error {
	if err := b.check(m); err != nil {
		return err
	}
	dir := b.Store.Dir(m.Name)
	if err := safeOwnedDir(dir); err != nil {
		return vmError("install", ErrorInvalidConfig, err)
	}
	disk := b.diskPath(m)
	if err := rejectPathSymlinks(disk); err != nil {
		return vmError("install", ErrorInvalidConfig, err)
	}
	if _, err := os.Stat(disk); err == nil {
		return nil // the installed guest: never touched
	} else if !errors.Is(err, os.ErrNotExist) {
		return vmError("install", ErrorBackend, err)
	}
	if _, err := b.runner().Run(ctx, "qemu-img", "create", "-f", "qcow2", disk, strconv.Itoa(m.DiskGiB)+"G"); err != nil {
		return vmError("install", ErrorBackend, err)
	}
	return nil
}

// Command is the whole QEMU command line for a machine: the program to run and
// its arguments, in QEMU's own order. It is exported because it is the one
// thing worth reading when a machine will not start: the window shows it,
// `--status` prints it, and a test asserts every part of it.
//
// It starts nothing. Start puts it under systemd-run.
func (b *QEMUBackend) Command(m Machine) ([]string, error) {
	if err := b.check(m); err != nil {
		return nil, err
	}
	runtimeDir, err := b.Store.EnsureRuntime(m.Name)
	if err != nil {
		return nil, vmError("command", ErrorBackend, err)
	}
	kvm, err := b.useKVM(m)
	if err != nil {
		return nil, vmError("command", ErrorUnsupported, err)
	}
	sshPort, err := b.reserveSSHPort(m)
	if err != nil {
		return nil, vmError("command", ErrorConflict, err)
	}

	args := []string{"-m", strconv.Itoa(m.MemoryMiB), "-smp", strconv.Itoa(m.CPUs)}
	machineType := strings.TrimSpace(m.Boot.Machine)
	if machineType == "" {
		machineType = "q35"
	}
	args = append(args, "-machine", machineType)

	if kvm {
		// The guest's own code on the host's processor, with the host's
		// features: what -cpu host means, and why it is not used with TCG.
		args = append(args, "-accel", "kvm", "-cpu", "host")
	} else {
		args = append(args, "-accel", "tcg")
	}

	// The guest's own disk, inside its own folder.
	args = append(args, "-drive", "file="+qemuOpt(b.diskPath(m))+",if=virtio")

	// Install media, while there is any: the guest stops needing it by
	// installing itself, and the window's Finish clears the field then.
	if iso := strings.TrimSpace(m.Boot.ISO); iso != "" {
		// -cdrom is a drive's options underneath (file=…,media=cdrom), so
		// its path is quoted as one.
		args = append(args, "-cdrom", qemuOpt(ExpandPath(iso)))
	}

	// A kernel booted directly, which is what an OS in the making is: no
	// firmware and no boot loader, the kernel and its command line.
	if kernel := strings.TrimSpace(m.Boot.Kernel); kernel != "" {
		args = append(args, "-kernel", ExpandPath(kernel))
		if initrd := strings.TrimSpace(m.Boot.Initrd); initrd != "" {
			args = append(args, "-initrd", ExpandPath(initrd))
		}
		if line := strings.TrimSpace(m.Boot.Append); line != "" {
			args = append(args, "-append", line)
		}
	}

	switch m.Boot.Firmware {
	case FirmwareBIOS:
		// SeaBIOS is QEMU's own: nothing to add.
	case FirmwareUEFI:
		firmware := b.probes().UEFIFirmware()
		if firmware == "" {
			return nil, vmError("command", ErrorUnavailable, errors.New(
				"no UEFI firmware is installed: install ovmf (Debian, Ubuntu: `ovmf`; Fedora: `edk2-ovmf`) or set the machine's boot.firmware to \"bios\""))
		}
		// The code image is read-only and shared between machines; the
		// variables are this machine's own writable file, because its boot
		// entries live in them.
		efivars := filepath.Join(b.Store.Dir(m.Name), qemuEfivarsFile)
		if err := b.ensureEfivars(efivars); err != nil {
			return nil, vmError("command", ErrorBackend, err)
		}
		args = append(args,
			"-drive", "if=pflash,format=raw,readonly=on,file="+qemuOpt(firmware),
			"-drive", "if=pflash,format=raw,file="+qemuOpt(efivars))
	}

	// The network: QEMU's own user-mode stack, with the guest's ssh reachable
	// from this host's loopback alone.
	args = append(args,
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp:127.0.0.1:%d-:22", sshPort),
		"-device", "virtio-net-pci,netdev=net0")

	args = append(args, b.displayArgs(m, runtimeDir)...)

	// The serial console, on a unix socket: a guest whose only output is a
	// kernel log has one even when it is watched in QEMU's own window, and it
	// is what DisplaySerial hands the window. `nowait` is deliberate: QEMU must
	// never hold a boot back until somebody reads the console.
	//
	// The log file is a second reader of this same socket (TeeSerial), not a
	// second serial port: QEMU can put a chardev in a file but cannot write one
	// device's output to a socket and a file at once, and `socat`/`tee` beside
	// QEMU would be a process nobody asked for. The socket is the console; the
	// log is what a viewer mirrors into.
	args = append(args, "-serial", "unix:"+qemuOpt(filepath.Join(runtimeDir, qemuSerialSock))+",server,nowait")

	// QMP, always: it is the only way to ask a QEMU guest what it is doing, and
	// the only way to stop it without killing it.
	args = append(args, "-qmp", "unix:"+qemuOpt(filepath.Join(runtimeDir, qemuQMPFile))+",server,nowait")

	if b.debugged(m) {
		// The gdb stub, on this host's loopback alone, plus -S, which holds the
		// guest at its first instruction - the whole point of a debugger here.
		// `-s` is QEMU's shorthand for this, but it is `-gdb tcp::1234`, which
		// does not pin the interface the way `tcp:HOST:PORT` does; the manual
		// says only that it listens on port 1234. Everything inua opens is
		// reachable from this host alone, so the host is spelled out, and the
		// port is this machine's own (reserveGDBPort).
		gdbPort, err := b.reserveGDBPort(m)
		if err != nil {
			return nil, vmError("command", ErrorConflict, err)
		}
		args = append(args, "-gdb", "tcp:"+qemuGDBAddr(gdbPort), "-S")
	}
	return append([]string{b.program(m)}, args...), nil
}

// displayArgs is how a machine is seen and what its graphics are: the machine's
// display kind says through what, its gpu says how far that goes. The two are
// tied together - gl=on needs a display that can reach the host's GPU, which is
// QEMU's own window or SPICE on a local socket and never VNC.
func (b *QEMUBackend) displayArgs(m Machine, runtimeDir string) []string {
	gpu := m.Display.GPU
	if gpu == "" {
		gpu = GPUBasic
	}
	gl := onOff(gpu == GPUVirgl || gpu == GPUVenus)

	var out []string
	switch m.Display.Kind {
	case DisplayGTK:
		out = append(out, "-display", "gtk,gl="+gl)
	case DisplaySerial, DisplayNone:
		// Nothing to draw into: the serial console, or the guest draws itself.
		out = append(out, "-display", "none")
	case DisplayRDP, DisplayWeb:
		// RemoteApp and a web console are Dockur's ways to reach Windows; a
		// QEMU machine that asks for one still has to be watched somewhere, so
		// it gets QEMU's own window.
		out = append(out, "-display", "gtk,gl="+gl)
	default: // DisplaySPICE, and whatever else a hand-edited file says
		// SPICE on a unix socket, which remote-viewer reaches as
		// `spice+unix://<path>`: nothing is published on any interface.
		// disable-ticketing because the socket is this user's alone, in
		// $XDG_RUNTIME_DIR; a password over it would be theatre.
		out = append(out,
			"-spice", fmt.Sprintf("unix=on,addr=%s,disable-ticketing=on,gl=%s", qemuOpt(filepath.Join(runtimeDir, qemuSPICESock)), gl),
			"-vga", "none")
	}

	switch gpu {
	case GPUVirgl:
		// 3D through the host's GPU, OpenGL: virgl on the guest's virtio-gpu
		// driver, which Linux and FreeBSD have and Windows does not.
		out = append(out, "-device", "virtio-vga-gl")
	case GPUVenus:
		// Vulkan (venus) and blob resources on the same device: QEMU 9.2 and
		// newer, with a guest driver and a host driver that has it. An older
		// QEMU refuses the machine rather than ignoring the options, which is
		// the honest failure.
		out = append(out, "-device", "virtio-gpu-gl,venus=on,blob=on")
	default: // GPUBasic
		// 2D: the framebuffer every guest has a driver for - Windows, and a
		// custom OS until it has a virtio-gpu driver.
		out = append(out, "-device", "virtio-vga")
	}
	return out
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// qemuOverheadMiB is what QEMU takes besides the guest's RAM: its devices,
// and with virgl the GL buffers, often hundreds of MB.
const qemuOverheadMiB = 1024

// memoryFence is the unit's memory on the host, for a guest of memMiB:
// slowed past its RAM and QEMU's overhead (MemoryHigh, the kernel
// reclaims), killed at another overhead's more (MemoryMax, the unit alone,
// OOMPolicy=kill), and no more host swap than one overhead. Without it a
// guest that fills its RAM pushes the host into swap thrashing, where
// nothing answers, not even a TTY (#44).
func memoryFence(memMiB int) []string {
	return []string{
		fmt.Sprintf("--property=MemoryHigh=%dM", memMiB+qemuOverheadMiB),
		fmt.Sprintf("--property=MemoryMax=%dM", memMiB+2*qemuOverheadMiB),
		fmt.Sprintf("--property=MemorySwapMax=%dM", qemuOverheadMiB),
		"--property=OOMPolicy=kill",
	}
}

// Start puts the machine under a transient systemd user unit, so it outlives
// the window that started it and dies with the session, and returns as soon as
// the unit is handed over: a first install is tens of minutes, and readiness
// is State's to report.
func (b *QEMUBackend) Start(ctx context.Context, m Machine) error {
	if err := b.check(m); err != nil {
		return err
	}
	if err := b.installed(m); err != nil {
		return vmError("start", ErrorConflict, err)
	}
	argv, err := b.Command(m)
	if err != nil {
		return err
	}
	// --collect takes the unit away when the guest exits, so a machine starts
	// again without anyone running `systemctl reset-failed`; --unit pins the
	// name, so a second Start cannot make a second machine. The memory fence
	// keeps a guest that fills its RAM from dragging the host into swap.
	args := append([]string{"--user", "--unit", b.UnitName(m), "--collect"}, memoryFence(m.MemoryMiB)...)
	args = append(args, argv...)
	if _, err := b.runner().Run(ctx, "systemd-run", args...); err != nil {
		return vmError("start", ErrorBackend, err)
	}
	return nil
}

// State is what the guest is doing, read from QEMU's own monitor: the one
// answer that is about the guest and not about a process on the host.
func (b *QEMUBackend) State(ctx context.Context, m Machine) (State, error) {
	if err := b.check(m); err != nil {
		return StateUnknown, err
	}
	reply, err := b.qmp(ctx, m, "query-status")
	if err != nil {
		switch {
		case errors.Is(err, qmpNoSocket), isConnRefused(err):
			// No monitor, or a socket nobody is listening on: QEMU has
			// stopped. QEMU makes the socket at start and removes it as it
			// exits, so there is nothing to clean up here either.
			return StateStopped, nil
		case ctx.Err() != nil:
			return StateUnknown, vmError("state", ErrorUnavailable, err)
		}
		// The socket is there and does not answer: a QEMU on its way up or
		// down, which is not a state anyone can name.
		return StateUnknown, vmError("state", ErrorBackend, err)
	}
	var status struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(reply, &status); err != nil {
		return StateUnknown, vmError("state", ErrorBackend, err)
	}
	return qemuState(status.Status)
}

// qemuState maps QEMU's run state onto this package's:
//
//	running, paused     the guest is there. A paused guest is QEMU's, not the
//	                    user's - `stop` in the monitor, a savevm - and both
//	                    mean "do not start it again".
//	shutdown            the guest is going away: stopped.
//	prelaunch, debug    the guest is held before its first instruction (-S,
//	                    the gdb switch) or by a debugger: QEMU is there and
//	                    waiting, so it is starting, and Start must not be
//	                    offered again (its unit is still there).
//	inmigrate, postmigrate, finish-migrate
//	                    a guest that is arriving somewhere: starting.
//	anything else       no state a window can offer Start or Connect on, which
//	                    is worth telling apart from both.
func qemuState(status string) (State, error) {
	switch status {
	case "running", "paused":
		return StateRunning, nil
	case "shutdown":
		return StateStopped, nil
	case "prelaunch", "debug", "inmigrate", "postmigrate", "finish-migrate":
		return StateStarting, nil
	}
	return StateUnknown, vmError("state", ErrorBackend, fmt.Errorf("qemu reports %q", status))
}

// Stop asks the guest to shut itself down properly and waits for it: a disk
// being written to must not be pulled out from under it, and the guest is what
// knows its own disks. Only when that has not happened in time does QEMU get
// `quit`, which is QEMU giving up rather than the kernel being killed.
func (b *QEMUBackend) Stop(ctx context.Context, m Machine) error {
	if err := b.check(m); err != nil {
		return err
	}
	return b.stopQEMU(ctx, m)
}

// stopQEMU is Stop, shared with Remove: everything the two have to do about a
// running machine.
func (b *QEMUBackend) stopQEMU(ctx context.Context, m Machine) error {
	if _, err := os.Stat(b.QMPPath(m)); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return vmError("stop", ErrorBackend, err)
		}
		return nil // no monitor: nothing of this machine is running
	}
	if _, err := b.qmp(ctx, m, "system_powerdown"); err != nil {
		if errors.Is(err, qmpNoSocket) || isConnRefused(err) {
			return nil // it went away between the stat and the dial
		}
		return vmError("stop", ErrorBackend, err)
	}
	if b.waitForExit(ctx, m) {
		return nil // the guest shut down and QEMU left
	}
	if ctx.Err() != nil {
		return vmError("stop", ErrorUnavailable, ctx.Err())
	}
	// The guest has not gone in time. `quit` is QEMU abandoning the guest,
	// which is the last thing to try, not the first.
	if _, err := b.qmpOnce(m, "quit"); err != nil && !errors.Is(err, qmpNoSocket) && !isConnRefused(err) {
		return vmError("stop", ErrorBackend, err)
	}
	return nil
}

// waitForExit waits for a machine's monitor socket to go away, which is what
// QEMU exiting does: it removes its own sockets. The wait ends at whichever
// comes first, the context or the shutdown grace.
func (b *QEMUBackend) waitForExit(ctx context.Context, m Machine) bool {
	deadline := time.Now().Add(b.shutdownGrace(m))
	for {
		if _, err := os.Stat(b.QMPPath(m)); errors.Is(err, os.ErrNotExist) {
			return true
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (b *QEMUBackend) shutdownGrace(m Machine) time.Duration {
	grace := b.ShutdownBy
	if grace <= 0 {
		grace = qemuShutdownGrace
	}
	if strings.TrimSpace(m.Boot.ISO) != "" {
		grace = min(grace, qemuInstallShutdownGrace)
	}
	return grace
}

// Display is how a running machine is seen: the viewer, and where it is.
func (b *QEMUBackend) Display(ctx context.Context, m Machine) (Display, error) {
	if err := b.check(m); err != nil {
		return Display{}, err
	}
	switch m.Display.Kind {
	case DisplayGTK:
		return Display{Kind: DisplayGTK}, nil
	case DisplaySerial, DisplayNone:
		return Display{Kind: DisplaySerial, Address: b.SerialPath(m)}, nil
	case DisplayRDP, DisplayWeb:
		// A QEMU machine has neither; the window opens this one instead.
		return Display{Kind: DisplayGTK}, nil
	}
	return Display{Kind: DisplaySPICE, Address: filepath.Join(b.Store.RuntimeDir(m.Name), qemuSPICESock)}, nil
}

// Remove takes the machine's process and its runtime files apart. With keepDisk
// the guest's own folder stays exactly as it is - the disk, its UEFI variables,
// its install media - and without it the folder goes, through the same guards
// the Docker backend's removal uses. The machine's definition stays either way,
// so it can be installed again; the disk is the caller's second, confirmed
// step.
func (b *QEMUBackend) Remove(ctx context.Context, m Machine, keepDisk bool) error {
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
	if err := b.stopQEMU(ctx, m); err != nil {
		return err
	}
	if runtimeDir := b.Store.RuntimeDir(m.Name); runtimeDir != "" {
		if err := os.RemoveAll(runtimeDir); err != nil {
			return vmError("remove", ErrorBackend, err)
		}
	}
	if keepDisk {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return vmError("remove", ErrorBackend, err)
	}
	if err := b.Store.Save(m); err != nil {
		return vmError("remove", ErrorBackend, err)
	}
	return nil
}

// check says whether this backend runs this machine at all, and whether the
// machine is one QEMU can start: the same first question the Docker backend
// asks, with QEMU's own boot section read.
func (b *QEMUBackend) check(m Machine) error {
	if m.Backend != BackendQEMU {
		return vmError("backend", ErrorUnsupported, fmt.Errorf("%s: %w", m.Backend, ErrNotThisBackend))
	}
	return m.Validate()
}

// installed reports whether the machine has a disk to boot: a QEMU machine
// without one has nothing to install into and nothing to run.
func (b *QEMUBackend) installed(m Machine) error {
	disk := b.diskPath(m)
	if err := rejectPathSymlinks(disk); err != nil {
		return err
	}
	fi, err := os.Stat(disk)
	if err != nil {
		return fmt.Errorf("%s is not there yet: install the machine first", disk)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", disk)
	}
	return nil
}

// Prepared reports whether this machine has been made: its disk is there. A
// QEMU machine without one has nothing for an installer to write into, so it
// is what the window shows as "not installed yet" rather than "installed, and
// off".
func (b *QEMUBackend) Prepared(m Machine) bool { return b.installed(m) == nil }

// RecentOutput is the last line of the machine's serial console, for the
// window to show while a guest boots: the only output an OS in the making has
// is the kernel log on its serial port, and it is what TeeSerial keeps.
func (b *QEMUBackend) RecentOutput(ctx context.Context, m Machine) string {
	f, err := os.Open(b.SerialLogPath(m))
	if err != nil {
		return ""
	}
	defer f.Close()
	// The tail alone: a boot that has been running for hours is not worth
	// reading whole for one line.
	const tail = 8 << 10
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	offset := int64(0)
	if fi.Size() > tail {
		offset = fi.Size() - tail
	}
	buf := make([]byte, fi.Size()-offset)
	if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	return lastLogLine(string(buf))
}

// diskPath is the machine's disk: the name its boot section gives, inside its
// own folder. An empty or escaping name falls back to the guide's own
// `disk.qcow2`; the machine's validation refuses both before this is reached,
// and this is the backstop under it.
func (b *QEMUBackend) diskPath(m Machine) string {
	name := strings.TrimSpace(m.Boot.Disk)
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		name = "disk.qcow2"
	}
	return filepath.Join(b.Store.Dir(m.Name), name)
}

// ensureEfivars makes a machine's UEFI variable store the first time it boots:
// a fresh 4 MiB of zeros, which is an empty variable store to the firmware. An
// existing one is never touched - it holds this guest's boot entries.
func (b *QEMUBackend) ensureEfivars(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := safeOwnedDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := rejectPathSymlinks(path); err != nil {
		return err
	}
	return writeAtomic(path, make([]byte, qemuEfivarsSize), 0600)
}

// reserveSSHPort is the loopback port this machine's guest ssh is forwarded to.
// It is chosen once and recorded in the machine's runtime directory, 0600, so
// two machines never forward the same one - and a port another machine
// recorded is skipped before it ever runs, which is the same per-machine
// reservation the Docker backend makes for its ports.
func (b *QEMUBackend) reserveSSHPort(m Machine) (int, error) {
	return b.reservePort(m, qemuPortFile, qemuSSHPortFrom)
}

// reservePort is a loopback port of this machine's, recorded in file in its
// runtime directory: the one recorded when it is still free, else the first
// free one from `from` that no other machine has recorded under that file.
func (b *QEMUBackend) reservePort(m Machine, file string, from int) (int, error) {
	runtimeDir, err := b.Store.EnsureRuntime(m.Name)
	if err != nil {
		return 0, err
	}
	taken := b.portsReservedByOthers(m.Name, file)
	path := filepath.Join(runtimeDir, file)
	if data, err := os.ReadFile(path); err == nil {
		var have int
		if n, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &have); n == 1 && err == nil &&
			portFree(have) && !taken[have] {
			return have, nil
		}
	}
	port, err := freePortFrom(from, taken)
	if err != nil {
		return 0, err
	}
	if err := writeAtomic(path, []byte(strconv.Itoa(port)+"\n"), 0600); err != nil {
		return 0, err
	}
	return port, nil
}

// SSHPort is the machine's forwarded ssh port: what seamless mode reaches the
// guest's own ssh on.
func (b *QEMUBackend) SSHPort(m Machine) (int, error) { return b.reserveSSHPort(m) }

// portsReservedByOthers is every port another machine has recorded in file.
func (b *QEMUBackend) portsReservedByOthers(name, file string) map[int]bool {
	out := map[int]bool{}
	entries, err := os.ReadDir(b.Store.Runtime)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == name {
			continue
		}
		data, err := os.ReadFile(filepath.Join(b.Store.RuntimeDir(e.Name()), file))
		if err != nil {
			continue
		}
		var port int
		if n, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &port); n == 1 && err == nil {
			out[port] = true
		}
	}
	return out
}

// UnitName is the transient systemd unit a machine runs as, so a window or a
// command line can ask systemctl about it. A machine's name may hold what a
// unit's may not (a space, a comma), so it is escaped as systemd-escape
// escapes it: `my fedora` is inua-vm-my\x20fedora.
func (b *QEMUBackend) UnitName(m Machine) string { return qemuUnitPrefix + systemdEscape(m.Name) }

// systemdEscape is a string as systemd-escape writes it for a unit name:
// letters, digits, ':', '_' and '.' (but not a leading one) as they are, '/'
// as '-', and every other byte as \xNN.
func systemdEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '/':
			b.WriteByte('-')
		case c == '.' && i == 0:
			fmt.Fprintf(&b, `\x%02x`, c)
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == ':', c == '_', c == '.':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	return b.String()
}

// qemuOpt is a value as it goes into one of QEMU's option strings
// (key=value,key=value): a comma is doubled, which QEMU reads as one comma in
// the value rather than the start of another option. A path with a comma in it
// - a machine's name, an image's - is then that path and nothing more.
func qemuOpt(s string) string { return strings.ReplaceAll(s, ",", ",,") }

// QMPPath is the machine's control socket: what State and Stop speak to, and
// what a window's own queries would.
func (b *QEMUBackend) QMPPath(m Machine) string {
	return filepath.Join(b.Store.RuntimeDir(m.Name), qemuQMPFile)
}

// SerialPath is the machine's serial console: the socket QEMU serves it on.
func (b *QEMUBackend) SerialPath(m Machine) string {
	return filepath.Join(b.Store.RuntimeDir(m.Name), qemuSerialSock)
}

// SerialLogPath is where the serial console can be kept on disk, for a window
// that shows the boot of an OS whose only output is a kernel log. TeeSerial
// writes it.
func (b *QEMUBackend) SerialLogPath(m Machine) string {
	return filepath.Join(b.Store.RuntimeDir(m.Name), qemuSerialLog)
}

// debugged says whether a machine starts under the debugger: the machine's own
// gdb switch, or the option a caller set for a machine that has none.
func (b *QEMUBackend) debugged(m Machine) bool { return m.Boot.GDB || b.Options.GDB }

// GDBCommand is the line to attach to a machine started with the debugger
// switch, or "" when this backend was built without it.
func (b *QEMUBackend) GDBCommand(m Machine) string {
	if !b.debugged(m) {
		return ""
	}
	port, err := b.reserveGDBPort(m)
	if err != nil {
		return ""
	}
	return `gdb -ex "target remote ` + qemuGDBAddr(port) + `"`
}

// qemuGDBAddr is a gdb stub's address: this host's loopback alone.
func qemuGDBAddr(port int) string { return "127.0.0.1:" + strconv.Itoa(port) }

// reserveGDBPort is the loopback port this machine's gdb stub listens on,
// chosen once and recorded beside its ssh port, as that one is.
func (b *QEMUBackend) reserveGDBPort(m Machine) (int, error) {
	return b.reservePort(m, qemuGDBPortFile, qemuGDBPortFrom)
}

// TeeSerial mirrors a machine's serial console into its log file for as long as
// ctx lives: `socat` and `tee` in one, without either, and without a process
// QEMU does not know about. It is deliberately not part of the command line: a
// machine nobody is watching should not pay for a file nobody reads, so a
// window that shows the console starts this with the console's own lifetime.
func (b *QEMUBackend) TeeSerial(ctx context.Context, m Machine) error {
	socket := b.SerialPath(m)
	if err := b.waitForSerial(ctx, socket); err != nil {
		return err
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return vmError("serial", ErrorBackend, err)
	}
	defer conn.Close()
	if err := safeOwnedDir(b.Store.RuntimeDir(m.Name)); err != nil {
		return vmError("serial", ErrorInvalidConfig, err)
	}
	f, err := os.OpenFile(b.SerialLogPath(m), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return vmError("serial", ErrorBackend, err)
	}
	defer f.Close()
	go func() {
		<-ctx.Done()
		_ = conn.Close() // unblocks the copy below
	}()
	if _, err := io.Copy(f, conn); err != nil && ctx.Err() == nil {
		return vmError("serial", ErrorBackend, err)
	}
	return nil
}

// waitForSerial turns the race between Start and TeeSerial into a wait: a
// window may ask for the console the moment it starts the machine.
func (b *QEMUBackend) waitForSerial(ctx context.Context, socket string) error {
	deadline := time.Now().Add(qmpDeadline)
	for {
		if _, err := os.Stat(socket); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return vmError("serial", ErrorUnavailable, ctx.Err())
		}
		if !time.Now().Before(deadline) {
			return vmError("serial", ErrorUnavailable, fmt.Errorf("%s never appeared: is that machine running?", socket))
		}
		select {
		case <-ctx.Done():
			return vmError("serial", ErrorUnavailable, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// probes is the injected host answers, or the real ones when a backend was
// built by hand - which a test does, and nothing else should.
func (b *QEMUBackend) probes() QEMUProbes {
	p := b.Probes
	if p.HostArch == nil {
		p.HostArch = func() Arch { return "" } // an unknown host never claims KVM
	}
	if p.KVMPresent == nil {
		p.KVMPresent = func() bool { return false }
	}
	if p.KVMAccessible == nil {
		p.KVMAccessible = func() bool { return false }
	}
	if p.UEFIFirmware == nil {
		p.UEFIFirmware = func() string { return "" }
	}
	return p
}

func (b *QEMUBackend) runner() CommandRunner {
	if b.Runner == nil {
		b.Runner = execCommandRunner{}
	}
	return b.Runner
}

func (b *QEMUBackend) program(m Machine) string { return "qemu-system-" + string(m.Boot.Arch) }

// ---- the QEMU monitor protocol -------------------------------------------
//
// QMP is a few JSON objects in a line-oriented stream over a unix socket: QEMU
// greets, `qmp_capabilities` turns the rest on, and every command is answered
// by one object with either a `return` or an `error`. That is all this backend
// needs, so it is written here rather than pulled in: a library for two
// commands, on a socket that only ever has one client, would be more to go
// wrong rather than less.

// qmpNoSocket says the monitor is not there at all: QEMU is not running.
var qmpNoSocket = errors.New("the QEMU monitor socket is not there")

// qmp runs one command against a machine's monitor, on its own connection: a
// connection that died mid-command is never handed to the next caller.
func (b *QEMUBackend) qmp(ctx context.Context, m Machine, command string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, qmpDeadline)
	defer cancel()
	return b.qmpCtx(ctx, b.QMPPath(m), command)
}

// qmpOnce is qmp without the caller's context: for `quit`, which may be the
// last thing that happens on that connection, and which must still be sent
// when the wait for a clean shutdown has already run out.
func (b *QEMUBackend) qmpOnce(m Machine, command string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), qmpDeadline)
	defer cancel()
	return b.qmpCtx(ctx, b.QMPPath(m), command)
}

// qmpCtx is the whole client: connect, read the greeting, take the
// capabilities, send the command, read its answer.
func (b *QEMUBackend) qmpCtx(ctx context.Context, path, command string) (json.RawMessage, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, qmpNoSocket
		}
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	r := bufio.NewReader(conn)

	// The greeting, {"QMP":{"version":…,"capabilities":[]}}, read and dropped:
	// none of what this backend does needs a capability it could not already
	// grant.
	if _, err := readQMPLine(r); err != nil {
		return nil, err
	}
	if _, err := b.qmpSend(r, conn, `{"execute":"qmp_capabilities"}`); err != nil {
		return nil, err
	}
	return b.qmpSend(r, conn, commandJSON(command))
}

// qmpSend writes one command and reads its answer, skipping the asynchronous
// `event` objects QEMU interleaves: a POWERDOWN event can arrive between a
// command and its reply.
func (b *QEMUBackend) qmpSend(r *bufio.Reader, conn net.Conn, line string) (json.RawMessage, error) {
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		return nil, err
	}
	for {
		raw, err := readQMPLine(r)
		if err != nil {
			return nil, err
		}
		var msg struct {
			Event  string          `json:"event"` // an asynchronous event, not an answer
			Return json.RawMessage `json:"return"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			return nil, fmt.Errorf("the QEMU monitor said something unexpected: %s", raw)
		}
		if msg.Event != "" {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("the QEMU monitor refused the command: %s", msg.Error)
		}
		if msg.Return == nil {
			return nil, fmt.Errorf("the QEMU monitor answered without a result: %s", raw)
		}
		return msg.Return, nil
	}
}

// readQMPLine reads one JSON object; the protocol separates them with newlines
// and terminates them with CRLF, and blank lines are skipped.
func readQMPLine(r *bufio.Reader) (json.RawMessage, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return readQMPLine(r)
	}
	return json.RawMessage(line), nil
}

// commandJSON is one command as the monitor's JSON, quoted by the encoder so a
// command name with a quote in it could never make a malformed request.
func commandJSON(command string) string {
	name, _ := json.Marshal(command)
	return `{"execute":` + string(name) + `}`
}
