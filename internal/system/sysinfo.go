package system

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// SysInfo is what the About page shows of this computer, in the manner of
// fastfetch: read from /proc, /sys and /etc, no program run.
type SysInfo struct {
	Host     string // the host name
	OS       string // os-release's PRETTY_NAME
	OSID     string // its ID: "ubuntu", "arch" …
	Arch     string // "x86_64", "aarch64" …
	Kernel   string
	Model    string // the machine's maker and model, from DMI
	Uptime   time.Duration
	Packages []Packages // by package manager, those found
	CPU      string     // the model, without the maker's marks
	Cores    int        // logical processors
	MHz      int        // the highest clock, 0 when unknown
	GPUs     []string   // each display adapter's maker and name
	Memory   Usage
	Swap     Usage
	Disk     Usage  // the root file system
	DiskFS   string // its type: "ext4", "btrfs" …
}

// Distribution is what /etc/os-release says this system is: its name, its
// version and the icon it names.
type Distribution struct {
	Name    string
	Version string
	Logo    string
}

// ReadDistribution reads the distribution's identity below root ("/" on
// the computer itself; a test's folder in tests).
func ReadDistribution(root string) Distribution {
	values := keyValues(filepath.Join(root, "/etc/os-release"), "=")
	name := values["NAME"]
	if name == "" {
		name = values["PRETTY_NAME"]
	}
	return Distribution{Name: name, Version: values["VERSION_ID"], Logo: values["LOGO"]}
}

// Packages is how many packages one manager has installed.
type Packages struct {
	Manager string
	Count   int
}

// Usage is the room used and in all, in bytes.
type Usage struct{ Used, Total uint64 }

// ReadSysInfo reads the facts below root ("/" on the computer itself; a
// test's folder in tests). The root file system's room is read with
// statfs, of root.
func ReadSysInfo(root string) SysInfo {
	p := func(path string) string { return filepath.Join(root, path) }
	var s SysInfo
	s.Host = firstLine(p("/proc/sys/kernel/hostname"))
	osr := keyValues(p("/etc/os-release"), "=")
	s.OS, s.OSID = osr["PRETTY_NAME"], osr["ID"]
	if s.OS == "" {
		s.OS = osr["NAME"]
	}
	s.Kernel = firstLine(p("/proc/sys/kernel/osrelease"))
	s.Arch = firstLine(p("/proc/sys/kernel/arch"))
	if s.Arch == "" {
		s.Arch = map[string]string{"amd64": "x86_64", "arm64": "aarch64", "386": "i686"}[runtime.GOARCH]
	}
	s.Model = machineModel(p("/sys/class/dmi/id"))
	if up := strings.Fields(firstLine(p("/proc/uptime"))); len(up) > 0 {
		if secs, err := strconv.ParseFloat(up[0], 64); err == nil {
			s.Uptime = time.Duration(secs) * time.Second
		}
	}
	s.Packages = countPackages(root)
	s.CPU, s.Cores = readCPU(p("/proc/cpuinfo"))
	if khz, err := strconv.Atoi(firstLine(p("/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq"))); err == nil {
		s.MHz = khz / 1000
	}
	s.GPUs = readGPUs(root)
	mem := keyValues(p("/proc/meminfo"), ":")
	kb := func(name string) uint64 {
		n, _ := strconv.ParseUint(strings.TrimSuffix(mem[name], " kB"), 10, 64)
		return n * 1024
	}
	s.Memory = Usage{Total: kb("MemTotal"), Used: kb("MemTotal") - min(kb("MemAvailable"), kb("MemTotal"))}
	s.Swap = Usage{Total: kb("SwapTotal"), Used: kb("SwapTotal") - min(kb("SwapFree"), kb("SwapTotal"))}
	var st unix.Statfs_t
	if unix.Statfs(root, &st) == nil {
		bs := uint64(st.Bsize)
		s.Disk = Usage{Total: st.Blocks * bs, Used: (st.Blocks - st.Bfree) * bs}
	}
	s.DiskFS = rootFS(p("/proc/mounts"))
	return s
}

func firstLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(line)
}

// keyValues reads "KEY=value" (os-release) or "Key: value" (meminfo)
// lines; quotes around a value go.
func keyValues(path, sep string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		k, v, ok := strings.Cut(line, sep)
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if uq, err := strconv.Unquote(v); err == nil {
			v = uq
		} else {
			v = strings.Trim(v, `"'`)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// machineModel is the maker and model from DMI, without the placeholders
// makers leave there.
func machineModel(dir string) string {
	junk := func(s string) bool {
		l := strings.ToLower(s)
		return s == "" || strings.Contains(l, "to be filled") || strings.Contains(l, "default string") ||
			strings.Contains(l, "system product name") || strings.Contains(l, "not specified") || l == "none"
	}
	vendor := firstLine(filepath.Join(dir, "sys_vendor"))
	name := firstLine(filepath.Join(dir, "product_name"))
	version := firstLine(filepath.Join(dir, "product_version"))
	if junk(name) {
		// A desktop's board says more than its case.
		vendor, name = firstLine(filepath.Join(dir, "board_vendor")), firstLine(filepath.Join(dir, "board_name"))
		version = ""
	}
	if junk(name) {
		return ""
	}
	parts := []string{}
	if !junk(vendor) && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(strings.Fields(vendor + " x")[0])) {
		parts = append(parts, vendor)
	}
	parts = append(parts, name)
	// ThinkPads keep the name in the version, and the code in the name.
	if !junk(version) && !strings.Contains(name, version) && len(version) > 3 {
		parts = append(parts, "("+version+")")
	}
	return strings.Join(parts, " ")
}

// countPackages counts what dpkg and pacman installed, and Flatpak's and
// Snap's apps. rpm's database is not read: that takes rpm itself.
func countPackages(root string) []Packages {
	var out []Packages
	add := func(manager string, n int) {
		if n > 0 {
			out = append(out, Packages{manager, n})
		}
	}
	if f, err := os.Open(filepath.Join(root, "/var/lib/dpkg/status")); err == nil {
		n := 0
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			if sc.Text() == "Status: install ok installed" {
				n++
			}
		}
		f.Close()
		add("dpkg", n)
	}
	add("pacman", countDirs(filepath.Join(root, "/var/lib/pacman/local"), func(name string) bool { return name != "ALPM_DB_VERSION" }))
	flatpak := countDirs(filepath.Join(root, "/var/lib/flatpak/app"), nil)
	if home, err := os.UserHomeDir(); err == nil && root == "/" {
		flatpak += countDirs(filepath.Join(home, ".local/share/flatpak/app"), nil)
	}
	add("flatpak", flatpak)
	add("snap", countDirs(filepath.Join(root, "/snap"), func(name string) bool { return name != "bin" && name != "README" }))
	return out
}

func countDirs(dir string, keep func(string) bool) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && (keep == nil || keep(e.Name())) {
			n++
		}
	}
	return n
}

// cpuMarks are what makers put in a CPU's name that says nothing.
var cpuMarks = regexp.MustCompile(`\((R|TM|tm|r)\)|\s+CPU|\s+Processor|\s+@\s+[\d.]+\s*GHz|\s+\d+-Core|\s+(with|w/)\s+Radeon.*$`)

func readCPU(path string) (model string, cores int) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "processor":
			cores++
		case "model name", "Model", "cpu model":
			if model == "" {
				model = strings.TrimSpace(v)
			}
		}
	}
	model = strings.Join(strings.Fields(cpuMarks.ReplaceAllString(model, "")), " ")
	return model, cores
}

// gpuVendors are short names for the display adapters' makers.
var gpuVendors = map[string]string{"1002": "AMD", "10de": "NVIDIA", "8086": "Intel", "1af4": "Virtio", "15ad": "VMware", "1234": "QEMU", "5143": "Qualcomm"}

// readGPUs names each display adapter (a DRM card with a PCI device),
// from pci.ids.
func readGPUs(root string) []string {
	cards, _ := filepath.Glob(filepath.Join(root, "/sys/class/drm/card[0-9]*"))
	type id struct{ vendor, device string }
	var ids []id
	for _, c := range cards {
		if strings.Contains(filepath.Base(c), "-") {
			continue // a connector
		}
		v := strings.TrimPrefix(firstLine(filepath.Join(c, "device/vendor")), "0x")
		d := strings.TrimPrefix(firstLine(filepath.Join(c, "device/device")), "0x")
		if v != "" && d != "" && !slices.Contains(ids, id{v, d}) {
			ids = append(ids, id{v, d})
		}
	}
	if len(ids) == 0 {
		return nil
	}
	names := map[id]string{}
	for _, f := range []string{"/usr/share/hwdata/pci.ids", "/usr/share/misc/pci.ids", "/usr/share/pci.ids"} {
		file, err := os.Open(filepath.Join(root, f))
		if err != nil {
			continue
		}
		vendor := ""
		sc := bufio.NewScanner(file)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "" || line[0] == '#':
			case line[0] != '\t':
				vendor, _, _ = strings.Cut(line, " ")
			case len(line) > 6 && line[1] != '\t':
				dev, name, _ := strings.Cut(line[1:], "  ")
				if i := slices.Index(ids, id{vendor, dev}); i >= 0 {
					names[ids[i]] = strings.TrimSpace(name)
				}
			}
		}
		file.Close()
		break
	}
	var out []string
	for _, i := range ids {
		maker := gpuVendors[i.vendor]
		name := names[i]
		// "Phoenix1", or the bracketed marketing name when there is one:
		// "Navi 31 [Radeon RX 7900 XT]" is a Radeon RX 7900 XT.
		if _, b, ok := strings.Cut(name, "["); ok && strings.HasSuffix(b, "]") {
			name = strings.TrimSuffix(b, "]")
		}
		switch {
		case name == "":
			name = i.vendor + ":" + i.device
		case maker != "" && !strings.HasPrefix(name, maker):
			name = maker + " " + name
		}
		out = append(out, name)
	}
	return out
}

// rootFS is the root file system's type.
func rootFS(mounts string) string {
	b, err := os.ReadFile(mounts)
	if err != nil {
		return ""
	}
	fs := ""
	for line := range strings.SplitSeq(string(b), "\n") {
		if f := strings.Fields(line); len(f) > 2 && f[1] == "/" {
			fs = f[2] // the last mounted on / wins
		}
	}
	return fs
}
