# inua-vm: virtual machines as an app of their own

Status: proposed. It replaces the built-in Windows VM of #18 and keeps
most of its work.

## Goals

The maintainer's three decisions:

1. **An app of its own.** `inua-vm` is a separate program, like
   `inua-files` and `inua-games`: its own binary, window, application ID
   and desktop entry. The shell does not grow a VM manager; it finds the
   app in the launcher and the drawer like any other.
2. **Many operating systems.** Windows, Linux, FreeBSD, Android, and an
   operating system the user wrote: a hobby kernel booted from an ISO, a
   disk image or a kernel file, on the host's architecture or another.
3. **Seamless mode where the guest allows it.** A guest program shows as a
   window of its own on the Linux desktop, not inside the guest's desktop:
   on the dock with its own name and icon, tiled, moved and switched to by
   Hyprland like a native window. Where a guest cannot do this, the whole
   guest desktop is one window, as now.

## What #18 already gives

Keep, moved and generalised:

- `internal/vm`: the host checks (`platform.go`: KVM, Docker, Compose,
  the distribution, why a check failed), the remedies through `pkexec`
  without a shell (`remedy.go`), the lifecycle states, the resource
  profile (`vm.go`), the Dockur backend (`docker.go`), and the tests with
  an injected command runner and secret store.
- The password in the Secret Service, the 0600 environment file written
  atomically, the loopback-only ports, the pinned Compose project name,
  and the guarded disk removal.
- The four pages by state (prerequisites, setup, provisioning, ready) and
  their wording in the six languages.

Drop from the shell: the drawer's SYSTEM entry (`internal/ui/drawer.go`),
the VM window and service in `internal/ui` (`vm.go`, `vm_service.go`), the
`vm` control-socket target in `internal/ui/shell.go`, and the snapshot
hook. The log-out change in `internal/ui/control.go` (Hyprland's Lua
dispatcher) is useful on its own: send it as its own small PR.

## Shape

| Piece | Where |
|---|---|
| Logic, no GTK | `internal/vm` (machines, backends, checks), `internal/vm/qemu`, `internal/vm/dockur`, `internal/vm/waydroid`, `internal/vm/seamless` |
| The window | `internal/vmui`, an `appui` view (`appui.Register("vm", "io.github.lyricat.Inua.VM", …)`) |
| The binary | `cmd/inua-vm`: `inua-vm` opens the window; `inua-vm --doctor`, `--status [--json]`, `--start NAME`, `--stop NAME`, `--run NAME APP` for scripts and for the guest apps' desktop entries |
| Desktop entry and icon | `config/apps/io.github.lyricat.Inua.VM.desktop` (`StartupWMClass=io.github.lyricat.Inua.VM`) and its `.svg` |
| Layout | `ui/vm.blp` and the generated `ui/vm.ui`, read through `appui` |
| Build and release | `Makefile` (`build` and `install`), `.goreleaser.yaml`, the install script's list of binaries |

`inua vm doctor` and `inua vm status` from #18 become `inua-vm --doctor`
and `--status`; `cmd/inua` may keep `inua vm …` as a thin call to them.

The services rule holds: `internal/vm…` imports no GTK; results reach the
window through `gtkx.Post`. One instance runs at a time, as `inua-files`
does (a second start hands its arguments to the running one over D-Bus).

## Machines

A machine is a folder, `~/.local/share/inua/vm/NAME/`:

```
machine.toml      what it is (below)
disk.qcow2        its disk (QEMU); Dockur's /storage; nothing for Waydroid
efivars.fd        its UEFI variables, when it boots UEFI
ssh/              the key inua-vm made for seamless Linux and FreeBSD (0600)
apps.json         the guest programs found for seamless mode
```

Runtime files (QMP and SPICE sockets, the serial console, a pid file) go
in `$XDG_RUNTIME_DIR/inua-vm/NAME/`. Secrets (the Windows password) stay
in the Secret Service, labelled `inua-vm · NAME`, never in the folder.

`machine.toml`:

```toml
name = "fedora"
os = "linux"            # windows | linux | freebsd | android | custom
backend = "qemu"        # qemu | dockur | waydroid
memory_mib = 4096
cpus = 4
disk_gib = 40

[boot]                  # qemu
iso = "~/Downloads/Fedora-Workstation-Live-x86_64-41.iso"   # install media, until installed
disk = "disk.qcow2"
kernel = ""             # a kernel to boot directly: a custom OS
initrd = ""
append = ""             # the kernel's command line
firmware = "uefi"       # uefi | bios
arch = "x86_64"         # x86_64 | aarch64 | riscv64 | i386: another than the host's runs without KVM
machine = "q35"
accel = "auto"          # auto (KVM when it can, else emulation) | kvm (fail without it) | tcg (always emulate)

[display]
kind = "spice"          # spice | gtk | rdp | web | serial | none
gpu = "virgl"           # virgl (3D, OpenGL) | venus (3D, Vulkan; QEMU 9.2 and newer) | basic (2D)

[seamless]
enabled = true          # the backend's way, below
```

Templates fill it per OS (`internal/vm/templates.go`): Windows 11 (Dockur,
or QEMU with the user's ISO and the virtio drivers), Ubuntu, Fedora, Arch,
FreeBSD, Android (Waydroid), and "Custom" with every boot field open.
`Config.Validate` checks it as #18's does, and the window shows a
machine's problems in its words.

## Backends

The interface from #18, with the connection made general and the seamless
part optional:

```go
type Backend interface {
	Install(ctx context.Context, m Machine) error // make the disk, pull the image, init the container
	State(ctx context.Context, m Machine) (State, error)
	Start(ctx context.Context, m Machine) error
	Stop(ctx context.Context, m Machine) error   // a clean shutdown first, then a forced one after a timeout
	Display(ctx context.Context, m Machine) (Display, error)
	Remove(ctx context.Context, m Machine, keepDisk bool) error
}

// Display is how to see a running machine.
type Display struct {
	Kind    string // spice | gtk | rdp | web | serial
	Address string // a socket path or 127.0.0.1:port
	Console string // the web console's URL, when there is one
}

// Seamless is implemented by the backends that can show a guest program
// as a window of its own.
type Seamless interface {
	Apps(ctx context.Context, m Machine) ([]GuestApp, error)
	Launch(ctx context.Context, m Machine, app GuestApp) error
}

type GuestApp struct {
	ID, Name, Icon string // Icon: a PNG copied out of the guest
	Command        string // the guest's own way to run it
}
```

### QEMU (the default)

For Linux, FreeBSD and custom systems, and for Windows from the user's own
ISO.

- `qemu-system-ARCH -enable-kvm -cpu host -machine q35 -smp N -m M`,
  `-drive file=disk.qcow2,if=virtio`, `-cdrom ISO` while installing,
  `-netdev user,id=n,hostfwd=tcp:127.0.0.1:PORT-:22` (ssh for seamless
  mode) with `-device virtio-net-pci,netdev=n`, `-device
  virtio-vga-gl`, the firmware (OVMF; its path differs by distribution and
  is found the way #18's platform layer finds things), and `-qmp
  unix:$XDG_RUNTIME_DIR/inua-vm/NAME/qmp.sock,server,nowait`.
- The disk: `qemu-img create -f qcow2 disk.qcow2 40G`.
- It runs as the user, outside inua-vm: `systemd-run --user --unit
  inua-vm-NAME --collect …`, so a machine keeps running with the window
  closed and stops with the session. The state comes from QMP
  (`query-status`), Stop sends `system_powerdown` and, after a timeout,
  `quit`.
- No Docker and no `docker` group: only `/dev/kvm`, whose group remedy
  #18 already has. Without KVM, or for another architecture, QEMU emulates
  (TCG): slower, and the window says so.
- The display: SPICE on a unix socket, shown with `remote-viewer`
  (`spice+unix://…`), or QEMU's own GTK window (`-display gtk,gl=on`).
  Either is a Wayland window Hyprland manages.

**Custom systems** (the user's own OS) use the same backend with every
boot field open: an ISO, a raw or qcow2 disk, or `-kernel`, `-initrd` and
`-append` to boot a kernel file directly; BIOS or UEFI; the machine type
and the architecture (`qemu-system-aarch64`, `riscv64` …). Two things for
an OS in the making:

- **The serial console**, `-serial unix:…/serial.sock,server,nowait`, shown
  in the window as a terminal view and kept as a log, often the only
  output a young kernel has.
- **The debugger**, a switch for QEMU's gdb stub (`-s -S`: wait for gdb on
  127.0.0.1:1234), with the command to attach shown and copied.

### Dockur (Windows)

#18's backend, as it is, for the unattended Windows install it does well.
Before it ships:

- **Pin the image** to a version or digest (`dockurr/windows:5.x` or
  `@sha256:…`), updated on purpose: the container gets `/dev/kvm`,
  `/dev/net/tun` and `NET_ADMIN`.
- **The `docker` group.** Membership is as good as root on the host. Do
  not offer it as a one-click fix: explain it, show the command, and
  recommend rootless Docker or Podman (Dockur supports both) or the QEMU
  backend instead.
- **The web console on 8006** has no login: any program the user runs can
  open it. Say so in the window; prefer the QEMU or RDP display.
- **Windows needs a licence.** Say so before Install.

### Waydroid (Android)

Android runs best not in a VM but in Waydroid, a container made for
Wayland desktops.

- Checks: the `waydroid` command, binder in the kernel (`binder_linux`, or
  `/dev/binderfs`), the `waydroid-container` service.
- Install: `pkexec waydroid init` (a one-time image download), with the
  same remedy pattern as #18.
- Start: the system service, then `waydroid session start` as the user;
  Stop: `waydroid session stop`.
- The display: `waydroid show-full-ui`, or seamless (below).
- Without binder, offer Android-x86 or BlissOS on the QEMU backend: the
  whole screen only.

## Hardware acceleration

**The processor: KVM.** QEMU runs the guest's code on the host's processor,
near native speed, with `-accel kvm -cpu host`, when:

- the guest's architecture is the host's (an x86_64 host runs x86_64
  guests: Windows, most Linux and FreeBSD, a custom x86_64 OS);
- the host has virtualisation on (the `vmx` or `svm` CPU flag, the
  `kvm_intel` or `kvm_amd` module) and the user can open `/dev/kvm`.

The check is whether `/dev/kvm` can be opened, not whether the user is in
the `kvm` group: on many distributions logind grants the session's user
access with an ACL, and then no group fix is needed. #18's
`platform.go` already checks access; its group remedy applies only when
access is denied.

Otherwise QEMU emulates (TCG, `-accel tcg`): it works, many times slower.
That is always the case for another architecture: an aarch64 or riscv64
build of a custom OS on an x86_64 host. `accel = "auto"` takes KVM when it
can; the window says **Accelerated** or **Emulated (slow)** beside each
machine, and why (no `/dev/kvm`, no access, another architecture). A
machine set to `kvm` refuses to start without it.

Also, with KVM: `-cpu host` (the host's features), virtio devices for the
disk and the network, and memory and CPU counts from #18's profile.

**Graphics.** Per guest:

| Guest | 3D | How |
|---|---|---|
| Linux | yes | `-device virtio-vga-gl` with `-display gtk,gl=on` or SPICE with `gl=on` (virgl: OpenGL through the host's GPU). `gpu = "venus"` adds Vulkan (`virtio-gpu-gl,venus=on,blob=on,hostmem=…`), on QEMU 9.2 and newer with a host driver that supports it |
| FreeBSD | partly | virgl with its virtio-gpu driver; else 2D |
| Windows | no | no mature driver for virgl: a 2D display (`virtio-vga` with the virtio-win drivers, or Dockur's). Enough for office programs; 3D needs GPU pass-through (VFIO, a second graphics card given to the guest): later, behind a switch, not in these steps |
| Android (Waydroid) | yes | a container, not a VM: the host's GPU directly |
| Custom | if it has a driver | a framebuffer (`-vga std` or `virtio-vga` in 2D) until the OS has a virtio-gpu driver |

The window shows what the machine gets (**3D** or **2D**). A host whose GPU
or QEMU has no virgl falls back to `basic` with a note; `gl=on` needs the
display to be QEMU's GTK window or SPICE on a local socket, never VNC.

## Seamless mode

Each backend's own way; the rest is shared (`internal/vm/seamless`).

| Guest | How | Needs in the guest | Windows on the host |
|---|---|---|---|
| Linux | `waypipe ssh -p PORT user@127.0.0.1 --title-prefix "NAME: " APP` | `waypipe`, an ssh server, the key inua-vm made (it puts it there at setup) | native Wayland windows |
| FreeBSD | the same (waypipe is in the ports) | as for Linux | native Wayland windows |
| Windows | FreeRDP's RemoteApp: `xfreerdp3 /v:127.0.0.1:PORT /u:USER /app:program:"C:\…\app.exe",name:App /wm-class:inua-vm-NAME-APP /cert:tofu /from-stdin` | Windows Pro or Enterprise; RemoteApp allowed (`HKLM\…\Terminal Server\TSAppAllowList` `fDisabledAllowList=1`), set by an OEM script Dockur runs at install | XWayland windows, one per program |
| Android | Waydroid's multi-window mode: `waydroid prop set persist.waydroid.multi_windows true`, then `waydroid app launch PACKAGE` | nothing | native Wayland windows |
| Custom | none, until the OS speaks one of these; the whole screen | — | one window |

Shared:

- **Finding the programs** (`Apps`). Linux and FreeBSD: the guest's
  `.desktop` files and icons, read over ssh. Android: `waydroid app list`.
  Windows: a script from the OEM folder writes the Start menu's programs
  and their icons, as JSON and PNG, to Dockur's shared folder; or the user
  adds a program's path by hand. Kept in `apps.json`.
- **Desktop entries.** Each program chosen gets
  `~/.local/share/applications/inua-vm-NAME-APP.desktop`: its name, its
  icon, `Exec=inua-vm --run NAME APP` and a `StartupWMClass` that matches
  its windows (`/wm-class` for FreeRDP; the title prefix and an app ID
  rule for waypipe). So it is in the launcher and the drawer, pinnable to
  the dock, and its windows group under its icon. Removing the machine,
  or the program, removes the entry.
- **Starting.** `inua-vm --run NAME APP` starts the machine if it is not
  running, waits until the guest answers (ssh, RDP or the Waydroid
  session), then launches the program. A spinner in the window, and the
  launcher's start notification, cover the wait.
- **Secrets.** FreeRDP reads the password on stdin (`/from-stdin`), never
  on its command line; the ssh key stays in the machine's folder, 0600.
- **Hyprland.** Window rules for `^inua-vm-` let FreeRDP's menus and
  pop-ups float and keep focus behaving; waypipe's windows need none.

Limits to state in the window: RemoteApp needs Windows Pro or better;
waypipe needs the guest set up once (inua-vm offers to do it over ssh);
copy and paste and drag and drop work per backend, not everywhere; a
custom OS has no seamless mode until it implements one of these ways.

## The window

As Settings → AI: the machines one a row (the OS's mark, the name, OS ·
state), a row clicked opens its settings in a panel at the right, with
Start, Stop, Connect and, with seamless mode, its programs. **New
machine** picks a template, then the panel shows its fields. Each
machine's state shows #18's pages: prerequisites with their fixes,
setup, provisioning (with the console), ready. All text through
`internal/i18n`, in the six catalogs; Phosphor icons.

## Security

- Every port binds 127.0.0.1; sockets live in `$XDG_RUNTIME_DIR`.
- Privileged steps only through `pkexec`, no shell, and only those a user
  would understand: the `kvm` group, Waydroid's init. Never the `docker`
  group as a one-click fix.
- Downloads (Dockur's image, Waydroid's images, an ISO the app fetches)
  are pinned or checked against their published checksums.
- The disk is removed only by its own, twice confirmed action, with #18's
  path guards.
- Guest programs launched in seamless mode run in the guest: nothing of
  the guest runs on the host but the viewer (waypipe, FreeRDP).

## Tests

As #18's, with the runner and the secret store injected, in temporary
folders:

- `machine.toml` read, written and validated; the templates.
- QEMU's command line per machine (firmware, arch, direct kernel, serial,
  gdb stub, KVM or TCG, virgl, venus or 2D); QMP against a fake on a unix
  socket (state, power down, quit).
- The acceleration check on a fake `/dev/kvm` and `/proc/cpuinfo`: access
  by ACL with no group, access denied, no virtualisation, another
  architecture.
- Dockur's Compose file and environment file (the pinned image, loopback
  ports, the password only in the 0600 file).
- Waydroid's checks and commands against a fake `waydroid`.
- Seamless: the commands per backend, the password on stdin, the desktop
  entries written and removed, their `StartupWMClass`.
- By hand, in a nested session: a Linux guest from an ISO, its program
  seamless on the dock; a custom kernel with its serial console; Android
  multi-window; Windows by Dockur.

## Steps

Each its own PR, each usable on its own:

1. **The app.** Move #18 into `cmd/inua-vm` and `internal/vmui`, take it
   out of the shell, and make its Dockur backend safe as above (pinned
   image, no one-click `docker` group, the console and the licence said).
   Windows only, as today, one machine. The log-out fix separately.
2. **Machines.** `machine.toml`, many machines, the list and the panel,
   templates.
3. **QEMU.** Linux, FreeBSD and custom systems: ISO, disk or kernel, BIOS
   or UEFI, other architectures, the serial console, the gdb stub; KVM with
   emulation as the fallback, and virgl 3D for the guests that have it.
4. **Android.** The Waydroid backend.
5. **Seamless.** Linux and FreeBSD with waypipe, Android multi-window,
   Windows RemoteApp; the desktop entries and window rules.

## Open questions

- Snapshots (`qemu-img snapshot`, `savevm`): in step 3, or later?
- Shared folders: virtiofs for QEMU guests, Dockur's `/shared` for
  Windows; which folder by default?
- USB pass-through for a custom OS on real devices: later, behind a
  switch.
- GPU pass-through (VFIO) for 3D in Windows: worth it for a second graphics
  card only; later, if at all.
