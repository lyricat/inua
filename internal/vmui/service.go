package vmui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/vm"
)

// vmService owns the machines, the backends and the host checks. It touches no
// GTK: the command line uses it for --list, --start and --stop, the window for
// everything else.
//
// Reading what a guest is doing is slow (a Docker call, a QMP round trip on a
// Unix socket), so it never happens on the GTK thread: Refresh reads a machine
// off-thread and keeps the result here, and the window paints from that.
type vmService struct {
	mu       sync.Mutex
	store    vm.Store
	host     vm.Host
	platform vm.Platform
	backends map[vm.BackendKind]vm.Backend
	seen     map[string]observed // what each machine was last seen doing
	busy     map[string]bool     // a machine with an operation in flight

	// The Secret Service session, once it is there, and whether this service
	// has been closed. The connection is made in the background (see
	// connectSecrets), so both are read under mu.
	secretConn *dbus.Conn
	secrets    vm.GuestSecretStore
	closed     bool
}

// observed is what a machine was last seen doing. Everything the window shows
// about a machine comes from here plus its machine.toml.
type observed struct {
	state    vm.State
	err      error
	progress string
	prepared bool // the backend has made what it needs
	guest    bool // the guest's own files are on disk
	display  vm.Display
}

// MachineView is one machine as the window shows it. Building one never touches
// a backend: it is the machine's own definition plus the last observation.
type MachineView struct {
	Name     string
	Machine  vm.Machine
	Problems []vm.Problem

	State        vm.State
	Prepared     bool // the backend has made what it needs (the project, the disk)
	Installed    bool // the guest has been installed: its own files are there
	Leftovers    bool // guest files are there without a project: a removal left them
	Busy         bool
	Progress     float64
	ProgressText string
	Err          error
	Page         vm.Page

	// Ready: this build has a backend for the machine's kind. Available: the
	// host gives that backend what it needs (the same checks as the prereq
	// page). Checks are the host checks, in the window's words.
	Ready        bool
	Available    bool
	Checks       []vm.DoctorItem
	Display      vm.Display
	Recommended  vm.Profile
	Distribution string
}

// vmSecretStore keeps a guest's password in the Secret Service where the rest
// of the user's secrets are: labelled with the machine's name, found by its
// attributes, never in the machine's folder.
type vmSecretStore struct{ client *keyring.Client }

func (s vmSecretStore) Store(ctx context.Context, m vm.Machine, value []byte) error {
	return s.client.Store(ctx, vm.GuestSecretLabel(m), vm.GuestSecretAttrs(m), value)
}

// Load reads a machine's password. A machine installed by step 1 kept its
// password under the old label; it is read there and moved to the machine's
// own, so an installed Windows keeps the password it was installed with.
func (s vmSecretStore) Load(ctx context.Context, m vm.Machine) ([]byte, error) {
	v, ok, err := s.client.Lookup(ctx, vm.GuestSecretAttrs(m))
	if err != nil {
		return nil, err
	}
	if ok {
		return v, nil
	}
	legacy, ok, err := s.client.Lookup(ctx, vm.LegacyGuestSecretAttrs(m))
	if err != nil || !ok {
		return nil, errors.New("the guest password is missing from the Secret Service")
	}
	if err := s.Store(ctx, m, legacy); err != nil {
		return legacy, nil // it is usable where it is
	}
	if err := s.client.Delete(vm.LegacyGuestSecretAttrs(m)); err != nil {
		logErr("vm secret", err)
	}
	return legacy, nil
}

func (s vmSecretStore) Delete(_ context.Context, m vm.Machine) error {
	if err := s.client.Delete(vm.GuestSecretAttrs(m)); err != nil {
		return err
	}
	return s.client.Delete(vm.LegacyGuestSecretAttrs(m))
}

// newService reads the machines and wires the backends this build has. The
// Secret Service is found in the background: it is where a guest's password
// lives, and a keyring that wants a password nobody can be asked for must not
// keep the window - or `inua-vm --list` - waiting.
func newService() *vmService {
	s := &vmService{
		store:    vm.DefaultStore(),
		host:     vm.DetectHost(""),
		platform: vm.DetectPlatform("", vm.PlatformProbes{}),
		backends: map[vm.BackendKind]vm.Backend{},
		seen:     map[string]observed{},
		busy:     map[string]bool{},
	}
	s.backends[vm.BackendDockur] = vm.NewDockerBackend(s.store, nil)
	go s.connectSecrets()
	return s
}

// connectSecrets finds the Secret Service once, in the background, and keeps
// the session bus open for as long as this service lives: a password read must
// not find the bus closed under it.
func (s *vmService) connectSecrets() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	client, err := keyring.Connect(conn, keyring.AlgDH)
	if err != nil {
		_ = conn.Close()
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close()
		return
	}
	s.secretConn, s.secrets = conn, vmSecretStore{client: client}
}

// secretStore is the Secret Service, when it is there.
func (s *vmService) secretStore() (vm.GuestSecretStore, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets, s.secrets != nil
}

// close drops the Secret Service session. The window keeps it for the app's
// life; a run from the command line has nobody else to do it.
func (s *vmService) close() {
	s.mu.Lock()
	conn := s.secretConn
	s.secretConn, s.secrets, s.closed = nil, nil, true
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func logErr(what string, err error) {
	if err != nil {
		log.Printf("%s: %v", what, err)
	}
}

// secretsReady reports whether guest passwords can be kept safely. Without the
// Secret Service nothing may be installed: the password would have nowhere to
// go but the machine's folder.
func (s *vmService) secretsReady() bool {
	_, ok := s.secretStore()
	return ok
}

// checks is the host, in the window's words: one line per check. The Secret
// Service is one of them: it is where a machine's password lives.
func (s *vmService) checks() []vm.DoctorItem {
	s.mu.Lock()
	platform := s.platform
	s.mu.Unlock()
	out := make([]vm.DoctorItem, 0, len(platform.Doctor)+1)
	out = append(out, platform.Doctor...)
	if !s.secretsReady() {
		out = append(out, vm.DoctorItem{Name: "Secret Service", OK: false, Reason: vm.ReasonMissing})
	}
	return out
}

// availability is whether the host gives this machine's backend what it needs.
// Only the checks that backend uses count: a missing Docker does not keep a
// QEMU machine from running.
func (s *vmService) availability(m vm.Machine) bool {
	s.mu.Lock()
	p := s.platform
	s.mu.Unlock()
	switch m.Backend {
	case vm.BackendDockur:
		return p.Supported && p.KVM.Available && p.KVM.Accessible &&
			p.DockerSocket.Available && p.DockerSocket.Accessible &&
			p.ComposeV2.Available && p.TUN.Available && p.TUN.Accessible &&
			s.secretsReady()
	default:
		// The QEMU and Waydroid checks come with their backends.
		return false
	}
}

// backendOf is the backend that runs this machine, when this build has one.
// A backend that keeps a guest's password is handed the Secret Service here, so
// an install or a start that needs it has it.
func (s *vmService) backendOf(m vm.Machine) (vm.Backend, bool) {
	s.mu.Lock()
	be, ok := s.backends[m.Backend]
	s.mu.Unlock()
	if ok && m.Backend == vm.BackendDockur {
		if docker, isDocker := be.(*vm.DockerBackend); isDocker && docker.Secrets == nil {
			if store, ready := s.secretStore(); ready {
				docker.Secrets = store
			}
		}
	}
	return be, ok
}

// machines reads the store. A machine whose machine.toml does not hold is left
// out: the window's list is what can be run.
func (s *vmService) machines() []vm.Machine {
	list, err := s.store.List()
	if err != nil {
		logErr("vm machines", err)
		return nil
	}
	return list
}

// Machines is the list, for the window's rows: no backend is read here.
func (s *vmService) Machines() []MachineView {
	list := s.machines()
	out := make([]MachineView, 0, len(list))
	for _, m := range list {
		out = append(out, s.view(m))
	}
	return out
}

// Names is the machines' names, for the command line.
func (s *vmService) Names() []string {
	list := s.machines()
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, m.Name)
	}
	return out
}

// Lookup reads one machine.
func (s *vmService) Lookup(name string) (vm.Machine, error) {
	return s.store.Load(name)
}

// Snapshot is one machine's view as last observed.
func (s *vmService) Snapshot(name string) MachineView {
	m, err := s.store.Load(name)
	if err != nil {
		// A machine whose machine.toml does not hold is shown with what is
		// wrong with it, on the page that explains a host problem.
		return MachineView{Name: name, Err: err, Page: vm.PagePrereq, Checks: s.checks()}
	}
	return s.view(m)
}

// view builds a machine's view from its definition and the last observation.
func (s *vmService) view(m vm.Machine) MachineView {
	_, ready := s.backendOf(m)
	available := ready && s.availability(m)
	s.mu.Lock()
	o, busy := s.seen[m.Name], s.busy[m.Name]
	s.mu.Unlock()
	if !o.prepared {
		o.state = vm.StateStopped
	}
	s.mu.Lock()
	recommended, distribution := vm.RecommendProfile(s.host), s.platform.Distribution
	s.mu.Unlock()
	// Installed is the guest's own files *and* the backend's project: a removal
	// that kept the disk leaves the files, and its Install makes the project
	// again - so that machine is set up, not stopped.
	installed := o.prepared && o.guest
	v := MachineView{
		Name: m.Name, Machine: m, Problems: m.Problems(),
		State: o.state, Prepared: o.prepared, Installed: installed, Leftovers: o.guest,
		Busy: busy, Progress: vm.ParsePercent(o.progress), ProgressText: o.progress,
		Err: o.err, Ready: ready, Available: available, Checks: s.checks(),
		Display: o.display, Recommended: recommended, Distribution: distribution,
	}
	v.Page = vm.PageFor(v.Available, v.Prepared, v.Installed, v.State, v.Busy)
	return v
}

// Refresh reads one machine's backend off the GTK thread and returns its view.
// It is what the window's watch calls, and what --status uses.
func (s *vmService) Refresh(name string) MachineView {
	m, err := s.store.Load(name)
	if err != nil {
		s.mu.Lock()
		s.seen[name] = observed{}
		s.mu.Unlock()
		return MachineView{Name: name, Err: err, Page: vm.PagePrereq, Checks: s.checks()}
	}
	o := observed{guest: vm.GuestDiskPresent(m.Dir(s.store))}
	if be, ok := s.backendOf(m); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if p, ok := be.(vm.Prepared); ok {
			o.prepared = p.Prepared(m)
		}
		if o.prepared {
			o.state, o.err = be.State(ctx, m)
			if o.err == nil {
				if prog, ok := be.(vm.Progress); ok {
					o.progress = prog.RecentOutput(ctx, m)
				}
				if d, err := be.Display(ctx, m); err == nil {
					o.display = d
				}
			}
		}
		cancel()
	}
	s.mu.Lock()
	s.seen[name] = o
	s.mu.Unlock()
	return s.view(m)
}

// RefreshAll reads every machine, for the window's slower sweep. The host is
// re-read too: the user may have installed Docker or joined a group since the
// shell started, and the prereq page has to notice without a restart.
func (s *vmService) RefreshAll() []MachineView {
	host := vm.DetectHost("")
	platform := vm.DetectPlatform("", vm.PlatformProbes{})
	s.mu.Lock()
	s.host, s.platform = host, platform
	s.mu.Unlock()
	list := s.machines()
	out := make([]MachineView, 0, len(list))
	for _, m := range list {
		out = append(out, s.Refresh(m.Name))
	}
	return out
}

// Templates are what a new machine can be made from.
func (s *vmService) Templates() []vm.Template { return vm.Templates() }

// Create writes a new machine from a template. It makes nothing and starts
// nothing: which page the machine shows follows from what is on disk.
func (s *vmService) Create(templateID, name string) (vm.Machine, error) {
	t, ok := vm.TemplateByID(templateID)
	if !ok {
		return vm.Machine{}, fmt.Errorf("there is no %q template", templateID)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = t.Name
	}
	unique, err := vm.UniqueName(s.store, name)
	if err != nil {
		return vm.Machine{}, err
	}
	m := t.Machine(unique, vm.RecommendProfile(s.host))
	if err := s.store.Save(m); err != nil {
		return vm.Machine{}, err
	}
	return m, nil
}

// Save writes a machine's definition and asks its backend to make what it
// needs: the Compose project for Windows, the disk for QEMU. It does not start
// anything, and it never touches a disk that is already there - writing the
// definition again is how a changed memory, CPU count or disk size takes
// effect.
func (s *vmService) Save(m vm.Machine) error {
	if err := s.begin(m.Name); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(m.Name, opErr) }()
	if err := s.store.Save(m); err != nil {
		opErr = err
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		opErr = s.noBackend(m)
		return opErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	opErr = be.Install(ctx, m)
	return opErr
}

// noBackend is what a machine whose backend this build does not have reports.
func (s *vmService) noBackend(m vm.Machine) error {
	return fmt.Errorf("this build has no %s backend", m.Backend)
}

// Install writes the machine and starts it: for Windows that is the image
// download and the unattended setup, which the window then follows on the
// provisioning page.
func (s *vmService) Install(m vm.Machine) error {
	if err := s.Save(m); err != nil {
		return err
	}
	return s.Start(m.Name)
}

// Start brings a machine up and returns before the guest is ready: a first
// Windows install is tens of minutes, and the window shows the progress.
func (s *vmService) Start(name string) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	if m.Backend == vm.BackendDockur && !s.secretsReady() {
		return errors.New("the Secret Service is unavailable: the guest password cannot be kept safely")
	}
	if err := s.begin(name); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(name, opErr) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	opErr = be.Start(ctx, m)
	return opErr
}

// Stop shuts a machine down, keeping its disk and its password.
func (s *vmService) Stop(name string) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	if err := s.begin(name); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(name, opErr) }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	opErr = be.Stop(ctx, m)
	return opErr
}

// Connect opens what shows the running machine: FreeRDP for Windows on a
// loopback port, the web console while it installs, the guest's own window
// when the backend draws it.
func (s *vmService) Connect(name string, display *rdpDisplay) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := be.Display(ctx, m)
	if err != nil {
		return err
	}
	switch d.Kind {
	case vm.DisplayRDP:
		host, port, err := splitHostPort(d.Address)
		if err != nil {
			return err
		}
		password, err := s.password(ctx, m)
		if err != nil {
			return err
		}
		defer clear(password)
		return startRDPClient(host, port, vm.GuestUser, string(password), display)
	case vm.DisplayWeb:
		if d.Console == "" {
			return errors.New("this machine has no web console")
		}
		return openBrowser(d.Console)
	case vm.DisplaySPICE:
		return openSpice(d.Address)
	case vm.DisplayGTK:
		// The guest's own window is already on screen.
		return nil
	case vm.DisplaySerial:
		return errors.New("this machine is seen through its serial console")
	}
	return fmt.Errorf("this machine has no viewer (%s)", d.Kind)
}

// Console opens the machine's web console in the browser: during a first
// Windows installation it is the only usable view of the guest.
func (s *vmService) Console(name string) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := be.Display(ctx, m)
	if err != nil {
		return err
	}
	if d.Console == "" {
		return errors.New("this machine has no web console")
	}
	return openBrowser(d.Console)
}

// password reads a machine's guest password, for the viewer that needs it.
func (s *vmService) password(ctx context.Context, m vm.Machine) ([]byte, error) {
	store, ok := s.secretStore()
	if !ok {
		return nil, errors.New("the Secret Service is unavailable: the guest password cannot be read")
	}
	return store.Load(ctx, m)
}

// Remove takes the machine's container or process apart and leaves the guest's
// own files alone: its disk, its password and its definition stay, so the same
// Windows can be started again.
func (s *vmService) Remove(name string) error {
	return s.remove(name, true, false)
}

// DeleteDisk is the destructive one: it removes the guest's own files, so the
// machine has nothing installed and can be installed from scratch. The
// machine's definition stays. Callers ask first.
func (s *vmService) DeleteDisk(name string) error {
	return s.remove(name, false, false)
}

// Delete takes the machine away entirely: its guest's files and its definition,
// so it is gone from the list. Callers ask first.
func (s *vmService) Delete(name string) error {
	return s.remove(name, false, true)
}

func (s *vmService) remove(name string, keepDisk, forget bool) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	if err := s.begin(name); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(name, opErr) }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := be.Remove(ctx, m, keepDisk); err != nil {
		opErr = err
		return err
	}
	if forget {
		if err := s.store.Delete(name); err != nil {
			opErr = err
			return err
		}
		s.mu.Lock()
		delete(s.seen, name)
		s.mu.Unlock()
	}
	return nil
}

// Fix applies a failing check's remedy. A privileged one goes through pkexec,
// so the shell's own polkit dialog takes the password; this runs on a
// goroutine, because that dialog is on the main thread and this call waits.
func (s *vmService) Fix(r vm.Remedy) (string, bool, error) {
	done, relogin, err := vm.ApplyRemedy(r)
	s.mu.Lock()
	s.platform = vm.DetectPlatform("", vm.PlatformProbes{})
	s.mu.Unlock()
	return done, relogin, err
}

// Doctor is the host checks as plain lines, for `inua-vm --doctor`.
func (s *vmService) Doctor() string {
	var b strings.Builder
	for _, c := range s.checks() {
		if c.OK {
			fmt.Fprintf(&b, "OK   %s\n", c.Name)
			continue
		}
		fmt.Fprintf(&b, "FAIL %s (%s)\n", c.Name, c.Reason)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Status is what the machines are doing now, for scripts: the host's checks
// and one entry per machine, each read from its backend.
type StatusReport struct {
	Checks   []StatusCheck   `json:"checks"`
	Machines []StatusMachine `json:"machines"`
	Dir      string          `json:"machines_dir"`
}

// StatusCheck is one host check, as --status reports it.
type StatusCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// StatusMachine is one machine, as --status reports it.
type StatusMachine struct {
	Name      string `json:"name"`
	OS        string `json:"os"`
	Backend   string `json:"backend"`
	State     string `json:"state"`
	Installed bool   `json:"installed"`
	Ready     bool   `json:"ready"`
	Problem   string `json:"problem,omitempty"`
}

// Status reads every machine, for `inua-vm --status`.
func (s *vmService) Status() StatusReport {
	st := StatusReport{Dir: s.store.Root}
	for _, c := range s.checks() {
		st.Checks = append(st.Checks, StatusCheck{Name: c.Name, OK: c.OK, Reason: string(c.Reason)})
	}
	for _, v := range s.RefreshAll() {
		e := StatusMachine{
			Name: v.Name, OS: string(v.Machine.OS), Backend: string(v.Machine.Backend),
			State: string(v.State), Installed: v.Installed, Ready: v.Ready,
		}
		switch {
		case v.Err != nil:
			e.Problem = v.Err.Error()
		case len(v.Problems) > 0:
			e.Problem = v.Problems[0].Field + ": " + string(v.Problems[0].Reason)
		}
		st.Machines = append(st.Machines, e)
	}
	return st
}

// StatusText is the status as both the command line and the shell print it.
func StatusText(st StatusReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Machines: %s\n", st.Dir)
	for _, c := range st.Checks {
		if c.OK {
			fmt.Fprintf(&b, "OK: %s\n", c.Name)
			continue
		}
		fmt.Fprintf(&b, "FAIL: %s (%s)\n", c.Name, c.Reason)
	}
	if len(st.Machines) == 0 {
		fmt.Fprintln(&b, "No machines.")
		return b.String()
	}
	for _, m := range st.Machines {
		fmt.Fprintf(&b, "%s: %s, %s, %s", m.Name, m.OS, m.Backend, m.State)
		if !m.Ready {
			fmt.Fprintf(&b, " (no %s backend in this build)", m.Backend)
		}
		if m.Problem != "" {
			fmt.Fprintf(&b, " — %s", m.Problem)
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

// State is what one machine is doing now, for the command line and for a
// script that wants a single answer.
func (s *vmService) State(name string) (vm.State, error) {
	v := s.Refresh(name)
	if v.Err != nil {
		return v.State, v.Err
	}
	if !v.Ready {
		return v.State, s.noBackend(v.Machine)
	}
	return v.State, nil
}

// begin takes a machine's operation lock, so two long operations - a start and
// a delete - never overlap on one machine. Machines are independent: two of
// them can be worked on at once.
func (s *vmService) begin(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[name] {
		return fmt.Errorf("%s already has an operation in progress", name)
	}
	s.busy[name] = true
	return nil
}

func (s *vmService) end(name string, err error) {
	s.mu.Lock()
	s.busy[name] = false
	o := s.seen[name]
	o.err = err
	s.seen[name] = o
	s.mu.Unlock()
}

// splitHostPort reads "127.0.0.1:3389" apart, for a viewer that takes them one
// by one.
func splitHostPort(address string) (string, int, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

// openBrowser hands a URL to the desktop's opener.
func openBrowser(url string) error {
	bin, err := exec.LookPath("xdg-open")
	if err != nil {
		return errors.New("no browser opener (xdg-open) is installed")
	}
	cmd := exec.Command(bin, url)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// openSpice shows a guest whose display is SPICE on a Unix socket.
func openSpice(socket string) error {
	if socket == "" {
		return errors.New("this machine has no SPICE socket")
	}
	bin, err := exec.LookPath("remote-viewer")
	if err != nil {
		return errors.New("remote-viewer (virt-viewer) is not installed")
	}
	cmd := exec.Command(bin, "spice+unix://"+socket)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// rdpDisplay is the monitor FreeRDP's window appears on: its logical size and
// Hyprland's scale, which the guest's own display scaling follows.
type rdpDisplay struct {
	Width, Height int
	Scale         float64
}

// rdpScale is FreeRDP's desktop scale for a monitor. XWayland here forces
// scale 1, so the monitor scale must become the remote DPI: otherwise Windows
// renders at 100% into a 2x physical screen and its UI looks too small.
func rdpScale(scale float64) int {
	if scale <= 0 {
		return 100
	}
	return min(500, max(100, int(scale*100+0.5)))
}

// rdpArgs builds FreeRDP's arguments for a monitor. It opens a normal window,
// not fullscreen, at 80% of the monitor's logical size so the desktop remains
// usable; the remote resolution matches that window and follows resizes.
func rdpArgs(address string, port int, user, password string, d *rdpDisplay) []string {
	args := []string{
		"/v:" + net.JoinHostPort(address, fmt.Sprint(port)),
		"/u:" + user,
		"/p:" + password,
		"/dynamic-resolution", "/cert:tofu", "/clipboard",
	}
	if d == nil || d.Width <= 0 || d.Height <= 0 {
		return args
	}
	width, height := rdpWindowSize(d.Width, d.Height)
	args = append(args,
		fmt.Sprintf("/size:%dx%d", width, height),
		fmt.Sprintf("/scale-desktop:%d", rdpScale(d.Scale)),
		fmt.Sprintf("/scale-device:%d", min(180, max(100, rdpScale(d.Scale)))),
	)
	return args
}

// rdpWindowSize keeps a margin around the remote desktop on its monitor.
func rdpWindowSize(width, height int) (int, int) {
	w, h := int(float64(width)*0.8), int(float64(height)*0.8)
	return max(800, w), max(600, h)
}

// startRDPClient opens FreeRDP on a loopback endpoint. The SDL3 client is
// preferred: it is the maintained Wayland client and draws the remote cursor.
// wlfreerdp is deprecated and loses the pointer on Hyprland, so it is only a
// last compatibility fallback. X11 runs through XWayland at scale 1 here.
func startRDPClient(address string, port int, user, password string, d *rdpDisplay) error {
	for _, name := range []string{"sdl-freerdp3", "sdl-freerdp", "wlfreerdp3", "wlfreerdp", "xfreerdp3", "xfreerdp"} {
		bin, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, rdpArgs(address, port, user, password, d)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		go cmd.Wait()
		return nil
	}
	return errors.New("FreeRDP is not installed")
}
