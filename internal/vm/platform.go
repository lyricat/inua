package vm

import (
	"bufio"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Platform describes host facilities needed to run and connect to a Windows VM.
type Platform struct {
	OS            string
	Distribution  string
	Supported     bool
	KVM           DeviceCapability
	DockerSocket  DeviceCapability
	ComposeV2     Capability
	FreeRDP       Capability
	BrowserOpener Capability
	Disk          Capability
	Filesystem    string
	TUN           DeviceCapability
	Doctor        []DoctorItem
}

type Capability struct {
	Available bool
	Detail    string
}
type DeviceCapability struct {
	Available  bool
	Accessible bool
	Path       string
	Detail     string
}

// DoctorReason says why a check failed, so the caller can say it in the
// user's language. A check that passed has none. The set is small and stable.
type DoctorReason string

const (
	ReasonMissing DoctorReason = "missing" // the device, tool or service is not there
	ReasonDenied  DoctorReason = "denied"  // it is there, but this user cannot use it
	ReasonStopped DoctorReason = "stopped" // the service is there but not running
	ReasonTooOld  DoctorReason = "too_old" // a version older than the one required
	ReasonUnclear DoctorReason = "unclear" // could not be told apart
)

// Remedy is what fixes a failing check, when Inua can do it at all. A check
// the user must fix by hand (installing a package) carries none, and the
// window shows the platform's instructions instead.
//
// A privileged remedy runs through pkexec, so the session's polkit agent
// decides whether to ask for a password; it never runs with the caller's
// rights.
type Remedy struct {
	// What is the fix, in the imperative: "Add your user to the docker group".
	What string
	// Group is the account group the fix adds the user to, when it does.
	Group string
	// Privileged means the command needs root, and runs through pkexec.
	Privileged bool
	// Argv is the command, run without a shell. {User} and {Group} are
	// replaced with this session's user and Group.
	Argv []string
	// Relogin says the fix only takes effect after the user logs out and in
	// again - a process's group list cannot be changed from inside it.
	Relogin bool
}

// DoctorItem is one host check: what it needs, whether it holds, when it does
// not why, and how to fix it when Inua can. The text is a stable identifier,
// so the window can word all of it in the user's language.
type DoctorItem struct {
	Name   string
	OK     bool
	Reason DoctorReason
	Remedy *Remedy
}

type PlatformProbes struct {
	Executable    func(string) bool
	Accessible    func(string) bool
	Filesystem    func(string) string
	Exists        func(string) bool
	CommandOutput func(string, ...string) (string, error)
}

// DetectPlatform inspects host support without modifying system state. Root maps absolute
// paths into a fixture root; probe functions make host-dependent checks deterministic in tests.
func DetectPlatform(root string, probes PlatformProbes) Platform {
	if root == "" {
		root = "/"
	}
	if probes.Exists == nil {
		probes.Exists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	}
	if probes.Accessible == nil {
		probes.Accessible = func(p string) bool {
			fi, err := os.Stat(p)
			if err != nil {
				return false
			}
			if fi.Mode()&os.ModeSocket != 0 {
				// A Unix socket cannot be opened like a file: connecting is
				// what checks the permission bits.
				c, err := net.DialTimeout("unix", p, 250*time.Millisecond)
				if err != nil {
					return false
				}
				_ = c.Close()
				return true
			}
			f, err := os.OpenFile(p, os.O_RDWR, 0)
			if err != nil {
				return false
			}
			_ = f.Close()
			return true
		}
	}
	if probes.Executable == nil {
		probes.Executable = func(name string) bool { _, err := exec.LookPath(name); return err == nil }
	}
	if probes.CommandOutput == nil {
		probes.CommandOutput = func(name string, args ...string) (string, error) {
			out, err := exec.Command(name, args...).CombinedOutput()
			return string(out), err
		}
	}
	mapPath := func(p string) string {
		return filepath.Join(root, strings.TrimPrefix(filepath.Clean(p), string(filepath.Separator)))
	}
	osName, distro := hostOS(root)
	p := Platform{OS: osName, Distribution: distro, Supported: osName == "linux" && supportedDistribution(distro)}
	kvm := mapPath("/dev/kvm")
	p.KVM = DeviceCapability{Path: kvm, Available: probes.Exists(kvm), Accessible: probes.Accessible(kvm)}
	check := func(name string, ok bool, reason DoctorReason) DoctorItem {
		return DoctorItem{Name: name, OK: ok, Reason: reason}
	}

	// The remedies Inua can carry out itself: adding the user to a device's
	// group. A process's group list cannot be changed from inside it, so the
	// fix only takes hold at the next login.
	groupRemedy := func(group string) *Remedy {
		return &Remedy{
			What:       "Add your user to the " + group + " group",
			Group:      group,
			Privileged: true,
			Argv:       []string{"gpasswd", "-a", "{User}", group},
			Relogin:    true,
		}
	}

	kvmCheck := check("KVM", p.KVM.Available && p.KVM.Accessible, ReasonMissing)
	if p.KVM.Available && !p.KVM.Accessible {
		// The device is there; this user is not in the group it belongs to.
		kvmCheck.Reason = ReasonDenied
		kvmCheck.Remedy = groupRemedy("kvm")
	}

	socket := mapPath("/var/run/docker.sock")
	p.DockerSocket = DeviceCapability{Path: socket, Available: probes.Exists(socket), Accessible: probes.Accessible(socket)}
	socketCheck := check("Docker", p.DockerSocket.Available && p.DockerSocket.Accessible, ReasonMissing)
	if p.DockerSocket.Available && !p.DockerSocket.Accessible {
		// The socket is there; either this user cannot talk to it, or the
		// daemon is not running. Dial once more to tell the two apart, so
		// the window does not say Docker is down when it is only this user.
		if err := dockerDial(socket); err != nil && isConnRefused(err) {
			socketCheck.Reason = ReasonStopped
			p.DockerSocket.Detail = "Docker is not running"
		} else {
			// No remedy: membership in the docker group is as good as root
			// on this host, so it is never Inua's one-click fix. The window
			// explains the risk and points at rootless Docker or Podman;
			// the kvm group, which grants one device, keeps its remedy.
			socketCheck.Reason = ReasonDenied
		}
	}

	composeVersion, composeErr := composeVersion(probes)
	compose := composeErr == nil && composeVersion != ""
	p.ComposeV2 = Capability{Available: compose, Detail: "Docker Compose v2 plugin unavailable"}
	if compose {
		p.ComposeV2.Detail = "Docker Compose " + composeVersion + " available"
	}
	p.FreeRDP = executableCapability(probes, []string{"xfreerdp", "xfreerdp3"}, "FreeRDP unavailable")
	p.BrowserOpener = executableCapability(probes, []string{"xdg-open"}, "browser opener xdg-open unavailable")
	tunPath := mapPath("/dev/net/tun")
	p.TUN = DeviceCapability{Path: tunPath, Available: probes.Exists(tunPath), Accessible: probes.Accessible(tunPath)}
	tunCheck := check("TUN", p.TUN.Available && p.TUN.Accessible, ReasonMissing)
	if p.TUN.Available && !p.TUN.Accessible {
		tunCheck.Reason = ReasonDenied
	}
	p.Filesystem = "unknown"
	if probes.Filesystem != nil {
		p.Filesystem = probes.Filesystem(root)
	} else {
		p.Filesystem = detectFilesystem(root)
	}
	p.Disk = Capability{Available: p.Filesystem != "" && p.Filesystem != "unknown", Detail: "filesystem: " + p.Filesystem}

	osCheck := check("Operating system", p.Supported, ReasonUnclear)
	if osName == "linux" && distro != "" {
		osCheck.Reason = ReasonTooOld // an unsupported distribution, said plainly
	}

	// A check that passes says nothing about why; a failing one carries the
	// reason, and a remedy when Inua can apply one.
	p.Doctor = []DoctorItem{
		osCheck,
		kvmCheck,
		socketCheck,
		check("Docker Compose v2", p.ComposeV2.Available, ReasonMissing),
		check("FreeRDP", p.FreeRDP.Available, ReasonMissing),
		check("Browser opener", p.BrowserOpener.Available, ReasonMissing),
		check("Disk", p.Disk.Available, ReasonUnclear),
		tunCheck,
	}
	return p
}

// composeVersion reads the Compose v2 plugin's version. Docker prints it bare
// ("2.39.1") or with a leading "v", depending on the release.
func composeVersion(p PlatformProbes) (string, error) {
	out, err := p.CommandOutput("docker", "compose", "version", "--short")
	if err != nil {
		return "", err
	}
	v := strings.TrimPrefix(strings.TrimSpace(out), "v")
	if v == "" || strings.HasPrefix(v, "1.") {
		return "", errors.New("Docker Compose v2 is not available")
	}
	return v, nil
}

// detectFilesystem names the filesystem holding the user's home, where the VM
// disk lives; "unknown" when /proc/mounts cannot be read.
func detectFilesystem(root string) string {
	if root != "/" {
		return "unknown"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "unknown"
	}
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "unknown"
	}
	best, fs := "", ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		mount := strings.ReplaceAll(fields[1], `\040`, " ")
		if mount == "/" || home == mount || strings.HasPrefix(home, mount+"/") {
			if len(mount) > len(best) {
				best, fs = mount, fields[2]
			}
		}
	}
	if fs == "" {
		return "unknown"
	}
	return fs
}

func dockerDial(path string) error {
	c, err := net.DialTimeout("unix", path, 250*time.Millisecond)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}

func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "connect: no such file")
}

func executableCapability(p PlatformProbes, names []string, missing string) Capability {
	for _, name := range names {
		if p.Executable(name) {
			return Capability{true, name + " available"}
		}
	}
	return Capability{false, missing}
}
func hostOS(root string) (string, string) {
	if runtime.GOOS != "linux" {
		return runtime.GOOS, ""
	}
	f, err := os.Open(filepath.Join(root, "etc", "os-release"))
	if err != nil {
		return "linux", ""
	}
	defer f.Close()
	values := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = strings.Trim(strings.TrimSpace(v), "\"'")
		}
	}
	id := strings.ToLower(values["ID"])
	if id == "" {
		return "linux", ""
	}
	return "linux", id
}
func supportedDistribution(d string) bool { return d == "nixos" || d == "ubuntu" || d == "debian" }
func distroDetail(osName, distro string) string {
	if osName != "linux" {
		return "Windows VM host support requires Linux"
	}
	if !supportedDistribution(distro) {
		return "unsupported Linux distribution: " + distro
	}
	return distro + " supported"
}

type PackagePlan struct {
	Distribution             string
	PackageManager           string
	Packages                 []string
	RequiresUserConfirmation bool
	Confirmation             string
}

// PlanPackages returns recommendations only; it never runs a package manager or changes groups.
func PlanPackages(distribution string) PackagePlan {
	d := strings.ToLower(distribution)
	p := PackagePlan{Distribution: d, RequiresUserConfirmation: true, Confirmation: "Review and confirm before installing packages or changing system configuration."}
	switch d {
	case "nixos":
		p.PackageManager = "nix"
		p.Packages = []string{"docker", "freerdp", "xdg-utils", "qemu_kvm"}
	case "ubuntu", "debian":
		p.PackageManager = "apt"
		p.Packages = []string{"docker.io", "docker-compose-v2", "freerdp3-x11", "xdg-utils", "qemu-system-x86", "qemu-utils"}
	}
	return p
}
