package vm

import (
	"fmt"
	"strings"
)

// Report is what the diagnostics answer: what this host provides for a guest
// and which machines there are. `inua vm doctor` and `inua-vm --doctor` print
// it; nothing here contacts Docker or changes the host.
type Report struct {
	Platform    Platform        `json:"platform"`
	Host        Host            `json:"host"`
	Recommended Profile         `json:"recommended_profile"`
	Dir         string          `json:"machines_dir"`
	Machines    []MachineReport `json:"machines"`
	Faults      []Fault         `json:"faults,omitempty"`
}

// MachineReport is one machine as the diagnostics report it: its definition,
// where it is, and whether that definition holds.
type MachineReport struct {
	Name      string      `json:"name"`
	OS        OS          `json:"os"`
	Backend   BackendKind `json:"backend"`
	MemoryMiB int         `json:"memory_mib"`
	CPUs      int         `json:"cpus"`
	DiskGiB   int         `json:"disk_gib"`
	Path      string      `json:"path"`
	Valid     bool        `json:"valid"`
	Problems  []Problem   `json:"problems,omitempty"`
}

// Diagnose reads the host's facilities and the machines in store. A host with
// no machine yet is not an error: the checks can be run before anything is
// installed. Root maps absolute paths into a fixture root, for tests.
func Diagnose(root string, store Store) (Report, error) {
	if root == "" {
		root = "/"
	}
	host := DetectHost(root)
	platform := DetectPlatform(root, PlatformProbes{})
	r := Report{
		Platform:    platform,
		Host:        host,
		Recommended: RecommendProfile(host),
		Dir:         store.Root,
	}
	machines, err := store.List()
	if err != nil {
		return r, fmt.Errorf("read the machines in %s: %w", store.Root, err)
	}
	for _, m := range machines {
		problems := m.Problems()
		r.Machines = append(r.Machines, MachineReport{
			Name: m.Name, OS: m.OS, Backend: m.Backend,
			MemoryMiB: m.MemoryMiB, CPUs: m.CPUs, DiskGiB: m.DiskGiB,
			Path: store.Dir(m.Name), Valid: len(problems) == 0, Problems: problems,
		})
	}
	// A folder that is not a machine is worth saying out loud: a machine.toml
	// edited by hand is the usual reason, and it is why the window's list is
	// shorter than the folder.
	r.Faults = store.Faults()
	return r, nil
}

// Text is the report as both commands print it: one line per check, in the
// order the window shows them, then the machines.
func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Host: %s\n", r.Platform.OS)
	if r.Platform.Distribution != "" {
		fmt.Fprintf(&b, "Distribution: %s\n", r.Platform.Distribution)
	}
	fmt.Fprintf(&b, "Supported: %t\n", r.Platform.Supported)
	for _, item := range r.Platform.Doctor {
		if item.OK {
			fmt.Fprintf(&b, "OK: %s\n", item.Name)
			continue
		}
		fmt.Fprintf(&b, "FAIL: %s (%s)\n", item.Name, item.Reason)
	}
	fmt.Fprintf(&b, "Recommended resources: %d MiB RAM, %d CPUs, %d GiB disk\n",
		r.Recommended.MemoryMiB, r.Recommended.CPUs, r.Recommended.DiskGiB)
	fmt.Fprintf(&b, "Machines: %s\n", r.Dir)
	if len(r.Machines) == 0 {
		fmt.Fprintln(&b, "  (none)")
	}
	for _, m := range r.Machines {
		fmt.Fprintf(&b, "  %s: %s, %s, %d MiB RAM, %d CPUs, %d GiB disk", m.Name, m.OS, m.Backend,
			m.MemoryMiB, m.CPUs, m.DiskGiB)
		if !m.Valid {
			parts := make([]string, 0, len(m.Problems))
			for _, p := range m.Problems {
				parts = append(parts, fmt.Sprintf("%s: %s", p.Field, p.Reason))
			}
			fmt.Fprintf(&b, " — %s", strings.Join(parts, "; "))
		}
		fmt.Fprintln(&b)
	}
	for _, f := range r.Faults {
		fmt.Fprintf(&b, "  %s: %s\n", f.Name, f.Error)
	}
	return b.String()
}
