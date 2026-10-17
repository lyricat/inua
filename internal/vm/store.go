package vm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Store is where the machines are. Each is one folder under Root
// (`$XDG_DATA_HOME/inua/vm/NAME`), holding its machine.toml and the guest's
// own files; its runtime files (sockets, a pid, the serial console) live in
// Runtime (`$XDG_RUNTIME_DIR/inua-vm/NAME`), which no one keeps across a
// reboot.
type Store struct {
	Root    string
	Runtime string
}

// DefaultStore is the store in the user's own directories.
func DefaultStore() Store {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		data = filepath.Join(home, ".local", "share")
	}
	run := os.Getenv("XDG_RUNTIME_DIR")
	if run == "" {
		run = filepath.Join(os.TempDir(), "inua-vm-"+strconv.Itoa(os.Getuid()))
	}
	return Store{Root: filepath.Join(data, "inua", "vm"), Runtime: filepath.Join(run, "inua-vm")}
}

// Dir is a machine's folder.
func (s Store) Dir(name string) string { return filepath.Join(s.Root, name) }

// RuntimeDir is a machine's runtime directory.
func (s Store) RuntimeDir(name string) string { return filepath.Join(s.Runtime, name) }

// Dir returns a machine's own folder.
func (m Machine) Dir(s Store) string { return s.Dir(m.Name) }

// File is a machine's machine.toml.
func (s Store) File(name string) string { return filepath.Join(s.Dir(name), MachineFileName) }

// DiskPath is where a machine's disk goes: its own folder, under the name its
// boot section gives it.
func (m Machine) DiskPath(s Store) string { return filepath.Join(s.Dir(m.Name), m.Boot.Disk) }

// EnsureRuntime makes a machine's runtime directory, 0700, and returns it.
func (s Store) EnsureRuntime(name string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	dir := s.RuntimeDir(name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// Load reads a machine. A folder that holds a guest but no machine.toml yet is
// a machine from before this file existed: step 1's Windows guest, which the
// folder already named and kept its disk in. It is written out as what it is,
// so an existing installation keeps working.
func (s Store) Load(name string) (Machine, error) {
	if err := checkName(name); err != nil {
		return Machine{}, err
	}
	data, err := os.ReadFile(s.File(name))
	if err == nil {
		m, err := ParseMachine(data)
		if err != nil {
			return Machine{}, err
		}
		if problems := m.Problems(); len(problems) > 0 {
			p := problems[0]
			return m, &Error{Kind: ErrorInvalidConfig, Op: "load", Err: fmt.Errorf("%s: %s: %s", name, p.Field, p.Reason)}
		}
		return m, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Machine{}, err
	}
	if m, ok := s.adopt(name); ok {
		if err := s.Save(m); err != nil {
			return m, err
		}
		return m, nil
	}
	return Machine{}, fmt.Errorf("%s: %w", name, ErrNoMachine)
}

// adopt recognises a machine from before machine.toml: step 1 kept one Windows
// guest in a folder of its own, with the Dockur files in it and its password
// in the Secret Service under the same name.
func (s Store) adopt(name string) (Machine, bool) {
	dir := s.Dir(name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Machine{}, false
	}
	dockur := false
	for _, e := range entries {
		switch {
		case e.IsDir():
			continue
		case e.Name() == "data.img", e.Name() == "setup.img", e.Name() == "windows.boot", e.Name() == "data.qcow2":
			dockur = true
		case strings.HasSuffix(e.Name(), ".iso"):
			dockur = true
		}
	}
	if !dockur {
		return Machine{}, false
	}
	m := DockurTemplate(name)
	// Step 1's profile, when it is still on disk, says what the guest was
	// given; older files without it keep the template's.
	if legacy, ok := stepOneConfig(name); ok {
		m.MemoryMiB, m.CPUs, m.DiskGiB = legacy.MemoryMiB, legacy.CPUs, legacy.DiskGiB
	}
	return m, true
}

// stepOneConfig is the single Windows guest step 1 wrote, if it is still
// there: `~/.config/inua/vm/windows.json`.
func stepOneConfig(name string) (Config, bool) {
	if name != "windows" {
		return Config{}, false
	}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		return Config{}, false
	}
	saved, err := ParsePersisted(data)
	if err != nil {
		return Config{}, false
	}
	return saved.Config, true
}

// Save writes a machine's machine.toml, atomically, 0600 like the rest of a
// machine's folder.
func (s Store) Save(m Machine) error {
	if err := checkName(m.Name); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	dir := s.Dir(m.Name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := rejectPathSymlinks(filepath.Join(dir, MachineFileName)); err != nil {
		return vmError("save", ErrorInvalidConfig, err)
	}
	return writeAtomic(filepath.Join(dir, MachineFileName), m.TOML(), 0600)
}

// List is every machine, by name. A folder with no machine.toml and no guest
// in it is not a machine and is left alone.
func (s Store) List() ([]Machine, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Machine
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if err := checkName(name); err != nil {
			continue // not something inua made, or not something it will touch
		}
		m, err := s.Load(name)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Fault is a folder in the store that is not a machine: a machine.toml that
// does not hold, or a folder that is not one of ours. The window's list leaves
// them out, so the diagnostics are where they are said.
type Fault struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Error string `json:"error"`
}

// Faults reads the store's own folders and reports the ones that are not
// machines.
func (s Store) Faults() []Fault {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil
	}
	var out []Fault
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if err := checkName(name); err != nil {
			continue // not something inua made, or not something it will touch
		}
		if _, err := s.Load(name); err != nil {
			out = append(out, Fault{Name: name, Path: s.Dir(name), Error: err.Error()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Delete removes a machine's own folder: its machine.toml and its guest's
// files, the disk included. Callers ask first; this is the destructive one.
func (s Store) Delete(name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	dir := s.Dir(name)
	if err := validateOwnedDataDir(dir); err != nil {
		return vmError("delete", ErrorInvalidConfig, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return vmError("delete", ErrorBackend, err)
	}
	return nil
}

// checkName refuses anything that is not one folder's name, so a machine can
// never be read or written outside the store.
func checkName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return &Error{Kind: ErrorInvalidConfig, Op: "name", Err: errors.New("a machine needs a name")}
	case name == "." || name == ".." || strings.HasPrefix(name, "."):
		return &Error{Kind: ErrorInvalidConfig, Op: "name", Err: fmt.Errorf("%q cannot name a machine", name)}
	case strings.ContainsAny(name, `/\`):
		return &Error{Kind: ErrorInvalidConfig, Op: "name", Err: fmt.Errorf("%q cannot name a machine", name)}
	case len(name) > 64:
		return &Error{Kind: ErrorInvalidConfig, Op: "name", Err: errors.New("a machine's name is too long")}
	}
	return nil
}
