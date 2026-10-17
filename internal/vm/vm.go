// Package vm contains toolkit-free machine configuration, host
// recommendations, and the backends that run a guest.
package vm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	DefaultMemoryMiB = 4096
	DefaultCPUs      = 2
	DefaultDiskGiB   = 64
	MinMemoryMiB     = 2048
	MinCPUs          = 1
	MinDiskGiB       = 32
	ConfigFileName   = "windows-vm.json"
	// GuestUser is the Windows account Dockur creates. Connect uses it.
	GuestUser = "Inua"
)

// Config describes an isolated Windows VM managed by Inua.
type Config struct {
	Name      string `json:"name"`
	MemoryMiB int    `json:"memory_mib"`
	CPUs      int    `json:"cpus"`
	DiskGiB   int    `json:"disk_gib"`
	DataDir   string `json:"data_dir"`
	ISOPath   string `json:"iso_path,omitempty"`
	RDPPort   int    `json:"rdp_port"`
}

func DefaultConfig() Config {
	return Config{Name: "windows", MemoryMiB: DefaultMemoryMiB, CPUs: DefaultCPUs, DiskGiB: DefaultDiskGiB, DataDir: DefaultDataDir(), RDPPort: 3389}
}

// DefaultDataDir stores VM images under XDG_DATA_HOME.
func DefaultDataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "inua", "vm", "windows")
}

// ConfigPath returns Inua's VM configuration path under XDG_CONFIG_HOME.
func ConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "inua", "vm", "windows.json")
}

// Persisted is the on-disk record of a VM: its configuration and whether
// Windows has come up at least once.
type Persisted struct {
	Config    Config `json:"config"`
	Installed bool   `json:"installed"`
}

// ParsePersisted reads a configuration file. The current form wraps the
// guest in a Config field; a bare Config is still accepted so an older
// file is not treated as missing.
func ParsePersisted(data []byte) (Persisted, error) {
	var saved Persisted
	if json.Unmarshal(data, &saved) == nil && saved.Config.Validate() == nil {
		return saved, nil
	}
	var cfg Config
	if json.Unmarshal(data, &cfg) == nil && cfg.Validate() == nil {
		return Persisted{Config: cfg}, nil
	}
	return Persisted{}, errors.New("invalid VM configuration")
}

// GuestDiskPresent reports whether DataDir holds a real guest image.
// A download cache (tmp/, an ISO) is not a Windows install.
func GuestDiskPresent(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range []string{"data.img", "data.qcow2", "windows.boot"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return true
		}
	}
	return false
}

// GuestDataPresent reports whether DataDir has leftover files (an ISO, a
// tmp download, a disk), so the window can offer to delete them.
func GuestDataPresent(dir string) bool {
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		return true
	}
	return false
}

// Validate checks resource limits and rejects paths that could escape the configured VM directory.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Name) == "" || c.Name == "." || c.Name == ".." || strings.ContainsAny(c.Name, `/\\`) {
		return &Error{Kind: ErrorInvalidConfig, Op: "validate", Err: errors.New("invalid VM name")}
	}
	if c.MemoryMiB < MinMemoryMiB || c.CPUs < MinCPUs || c.DiskGiB < MinDiskGiB {
		return &Error{Kind: ErrorInvalidConfig, Op: "validate", Err: errors.New("VM resources are below minimums")}
	}
	if c.RDPPort < 1 || c.RDPPort > 65535 {
		return &Error{Kind: ErrorInvalidConfig, Op: "validate", Err: errors.New("RDP port must be between 1 and 65535")}
	}
	if !filepath.IsAbs(c.DataDir) || filepath.Clean(c.DataDir) == string(filepath.Separator) {
		return &Error{Kind: ErrorInvalidConfig, Op: "validate", Err: errors.New("VM data directory must be a non-root absolute path")}
	}
	if c.ISOPath != "" && (!filepath.IsAbs(c.ISOPath) || filepath.Clean(c.ISOPath) != c.ISOPath) {
		return &Error{Kind: ErrorInvalidConfig, Op: "validate", Err: errors.New("ISO path must be clean and absolute")}
	}
	return nil
}

// State is the lifecycle state reported by a VM backend.
type State string

const (
	StateUnknown  State = "unknown"
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateFailed   State = "failed"
)

// ErrorKind classifies lifecycle failures for callers.
type ErrorKind string

const (
	ErrorInvalidConfig ErrorKind = "invalid_config"
	ErrorUnsupported   ErrorKind = "unsupported"
	ErrorUnavailable   ErrorKind = "unavailable"
	ErrorConflict      ErrorKind = "conflict"
	ErrorBackend       ErrorKind = "backend"
)

// Error carries a stable machine-readable kind and operation.
type Error struct {
	Kind ErrorKind
	Op   string
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("vm %s: %s: %v", e.Op, e.Kind, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// Host describes the resources available to a VM.
type Host struct {
	MemoryBytes   uint64
	CPUs          int
	FreeDiskBytes uint64
	OS            string
}

// Profile is a conservative resource allocation recommendation.
type Profile struct {
	MemoryMiB int
	CPUs      int
	DiskGiB   int
}

// RecommendProfile leaves host resources available and caps the VM at a modest desktop size.
func RecommendProfile(h Host) Profile {
	cpus := h.CPUs - 2
	if cpus < 2 {
		cpus = 2
	}
	if cpus > 8 {
		cpus = 8
	}
	memoryMiB := int(h.MemoryBytes / (1024 * 1024) / 2)
	if memoryMiB > 16384 {
		memoryMiB = 16384
	}
	if memoryMiB < 4096 {
		memoryMiB = 4096
	}
	diskGiB := int(h.FreeDiskBytes / (1024 * 1024 * 1024) / 4)
	if diskGiB > 128 {
		diskGiB = 128
	}
	if diskGiB < 64 {
		diskGiB = 64
	}
	return Profile{MemoryMiB: memoryMiB, CPUs: cpus, DiskGiB: diskGiB}
}

// DetectHost reads Linux host resources from procfs/sysfs. Root is injectable for fixture tests.
func DetectHost(root string) Host {
	if root == "" {
		root = "/"
	}
	mem := uint64(0)
	if data, err := os.ReadFile(filepath.Join(root, "proc/meminfo")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				var kib uint64
				if _, err := fmt.Sscanf(line, "MemTotal: %d kB", &kib); err == nil {
					mem = kib * 1024
				}
				break
			}
		}
	}
	return Host{MemoryBytes: mem, CPUs: runtime.NumCPU(), FreeDiskBytes: freeSpace(filepath.Join(root, "var")), OS: runtime.GOOS}
}

func freeSpace(path string) uint64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0
	}
	return stat.Bfree * uint64(stat.Bsize)
}
