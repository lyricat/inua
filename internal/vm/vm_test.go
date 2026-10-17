package vm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPageFor(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		available, hasProject, installed bool
		state                            State
		busy                             bool
		want                             Page
	}{
		{"host missing", false, false, false, StateStopped, false, PagePrereq},
		{"host missing even with a guest", false, true, true, StateRunning, false, PagePrereq},
		{"fresh setup", true, false, false, StateStopped, false, PageSetup},
		{"install just started", true, false, false, StateStopped, true, PageProvisioning},
		{"paused install", true, true, false, StateStopped, false, PageProvisioning},
		{"windows installing", true, true, false, StateStarting, true, PageProvisioning},
		{"installed stopped", true, true, true, StateStopped, false, PageStopped},
		{"installed starting", true, true, true, StateStarting, false, PageStarting},
		{"installed start clicked", true, true, true, StateStopped, true, PageStarting},
		{"installed running", true, true, true, StateRunning, false, PageRunning},
		{"installed stopping", true, true, true, StateStopping, false, PageStarting},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PageFor(tc.available, tc.hasProject, tc.installed, tc.state, tc.busy); got != tc.want {
				t.Fatalf("PageFor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGuestDiskPresent(t *testing.T) {
	dir := t.TempDir()
	if GuestDiskPresent(dir) || GuestDataPresent(dir) {
		t.Fatal("empty directory should not count as a disk or leftover data")
	}
	if GuestDiskPresent(filepath.Join(dir, "missing")) {
		t.Fatal("missing directory should not count as a disk")
	}
	if err := os.WriteFile(filepath.Join(dir, ".keep"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if GuestDiskPresent(dir) {
		t.Fatal("dotfiles alone should not count as a disk")
	}
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmp", "win11x64.iso"), []byte("iso"), 0600); err != nil {
		t.Fatal(err)
	}
	if GuestDiskPresent(dir) {
		t.Fatal("a download cache should not count as a guest disk")
	}
	if !GuestDataPresent(dir) {
		t.Fatal("a download cache should count as leftover data")
	}
	if err := os.WriteFile(filepath.Join(dir, "data.img"), []byte("windows"), 0600); err != nil {
		t.Fatal(err)
	}
	if !GuestDiskPresent(dir) {
		t.Fatal("a guest image should count as a disk")
	}
}

func TestParsePersisted(t *testing.T) {
	cfg := DefaultConfig()
	wrapped, err := json.Marshal(Persisted{Config: cfg, Installed: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePersisted(wrapped)
	if err != nil {
		t.Fatalf("wrapped: %v", err)
	}
	if got.Config.Name != cfg.Name || !got.Installed {
		t.Fatalf("wrapped = %+v", got)
	}
	bare, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err = ParsePersisted(bare)
	if err != nil {
		t.Fatalf("bare: %v", err)
	}
	if got.Config.Name != cfg.Name || got.Installed {
		t.Fatalf("bare = %+v", got)
	}
	if _, err := ParsePersisted([]byte(`{"nope":true}`)); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"relative data":  func(c *Config) { c.DataDir = "vm" },
		"root data":      func(c *Config) { c.DataDir = string(filepath.Separator) },
		"traversal name": func(c *Config) { c.Name = "../vm" },
		"invalid port":   func(c *Config) { c.RDPPort = 65536 },
		"low memory":     func(c *Config) { c.MemoryMiB = MinMemoryMiB - 1 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := cfg
			mutate(&bad)
			err := bad.Validate()
			var typed *Error
			if !errors.As(err, &typed) || typed.Kind != ErrorInvalidConfig {
				t.Fatalf("got %v, want invalid config error", err)
			}
		})
	}
}

func TestRecommendProfile(t *testing.T) {
	p := RecommendProfile(Host{MemoryBytes: 8 << 30, CPUs: 8, FreeDiskBytes: 200 << 30})
	if p.MemoryMiB != 4096 || p.CPUs != 6 || p.DiskGiB != 64 {
		t.Fatalf("unexpected profile: %+v", p)
	}
	p = RecommendProfile(Host{})
	if p.MemoryMiB < MinMemoryMiB || p.CPUs < MinCPUs || p.DiskGiB < MinDiskGiB {
		t.Fatalf("profile below minimum: %+v", p)
	}
	// The caps keep the guest modest on a large host.
	p = RecommendProfile(Host{MemoryBytes: 1 << 50, CPUs: 128, FreeDiskBytes: 1 << 50})
	if p.MemoryMiB != 16384 || p.CPUs != 8 || p.DiskGiB != 128 {
		t.Fatalf("profile exceeds caps: %+v", p)
	}
}
func TestDetectHostRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "var"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proc", "meminfo"), []byte("MemTotal:       8192000 kB\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := DetectHost(root)
	if h.MemoryBytes != 8192000*1024 {
		t.Fatalf("memory = %d", h.MemoryBytes)
	}
	if h.CPUs < 1 || h.OS == "" {
		t.Fatalf("incomplete host: %+v", h)
	}
}
