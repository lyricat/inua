package vmui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/vm"
)

// VMManager is the UI-facing boundary for the machines: the list, one
// machine's page, and the actions on it. Everything that reads a backend may
// block (a Docker call, a QMP round trip), so the window calls those off the
// GTK thread and paints what comes back.
type VMManager interface {
	// Machines is the list, from what is on disk. It reads no backend.
	Machines() []MachineView
	// Snapshot is one machine as it was last observed.
	Snapshot(name string) MachineView
	// Refresh reads one machine's backend and returns its view.
	Refresh(name string) MachineView
	// RefreshAll reads every machine and re-reads the host.
	RefreshAll() []MachineView
	// Templates are what a new machine can be made from.
	Templates() []vm.Template
	// Create writes a new machine from a template; nothing is installed.
	Create(templateID, name string) (vm.Machine, error)
	// Save writes a machine's definition and has its backend make what it
	// needs; Install does that and starts the guest as well.
	Save(vm.Machine) error
	Install(vm.Machine) error
	Start(name string) error
	Stop(name string) error
	Connect(name string, display *rdpDisplay) error
	Console(name string) error
	// Remove takes the machine's container apart and keeps the disk.
	Remove(name string) error
	// DeleteDisk deletes the guest's own files, keeping the machine.
	DeleteDisk(name string) error
	// Delete takes the machine away entirely.
	Delete(name string) error
	// Fix applies a failing check's remedy, asking for the password through
	// the session's polkit agent when it needs one.
	Fix(vm.Remedy) (done string, relogin bool, err error)
}

// machineRow is one row of the machine list: what a repaint updates.
type machineRow struct {
	name  string
	title gtk.Label
	sub   gtk.Label
}

// templateRow is one row of the new-machine page.
type templateRow struct {
	id    string
	title gtk.Label
	sub   gtk.Label
}

// vmWindow is the manager: the machines on the left, the chosen one's page on
// the right, and the page that makes a new one.
type vmWindow struct {
	win     *gtk.ApplicationWindow
	manager VMManager
	c       *appui.Ctx // the app, for a notification the window raises

	title gtk.Label
	sub   gtk.Label

	stack gtk.Stack

	// the list
	machineList gtk.ListBox
	newButton   gtk.Button
	views       []MachineView
	rows        []machineRow

	// a new machine
	templateList gtk.ListBox
	templateRows []templateRow
	nameEntry    gtk.Entry
	createButton gtk.Button
	createNote   gtk.Label
	createHint   string // the sentence Create shows until something goes wrong
	templateID   string
	templateName string // the template's own suggested name, to know a name was typed
	newOpen      bool

	// the page of a machine whose backend this build does not have
	naNote gtk.Label

	// prereq
	checkRows gtk.Box

	// setup
	setupSummary                    gtk.Label
	profileDesc                     gtk.Label
	accept, advancedToggle, install gtk.Button
	advanced                        gtk.Revealer
	memory, cpus, disk              gtk.SpinButton
	setupDisk                       gtk.Box
	setupRemoveData, setupDelete    gtk.Button
	setupNote                       gtk.Label

	// provisioning
	progress                                 gtk.ProgressBar
	progressText                             gtk.Label
	console, provStart, provStop, provRemove gtk.Button

	// stopped
	readySummary              gtk.Label
	readyMemory               gtk.SpinButton
	readyCPUs                 gtk.SpinButton
	diskSize                  gtk.Label
	readySave                 gtk.Button
	start, remove, removeData gtk.Button

	// starting
	bootProgress     gtk.ProgressBar
	bootProgressText gtk.Label
	bootConsole      gtk.Button
	bootStop         gtk.Button

	// running
	runSummary   gtk.Label
	connect      gtk.Button
	consoleReady gtk.Button
	stop         gtk.Button

	mu            sync.Mutex // the chosen machine, read by the window's watch
	selected      string
	closed        bool
	removeArmed   bool
	dataArmed     bool
	deleteArmed   bool
	setupFilled   bool
	readyFilled   bool
	operationBusy bool
	checkRefs     gtkx.Scope // the rows built for the current check list
	fixButtons    []fixTarget
	note          string // what the last action did, shown until the next one
	watchCancel   context.CancelFunc
	lastPage      vm.Page
	lastChecks    []vm.DoctorItem
	onClick       func(gtk.Button)
	onRowSelect   func(gtk.ListBox, uintptr)
	onTemplate    func(gtk.ListBox, uintptr)
	onNameSubmit  func(gtk.Entry)
}

// openWindow builds the manager window from the app's layouts: a second start
// of inua-vm brings this one forward instead of making another (appui).
func openWindow(c *appui.Ctx, manager VMManager) *vmWindow {
	v := &vmWindow{manager: manager, c: c, win: &gtk.ApplicationWindow{}}
	builder := c.Load("vm.ui")
	builder.Get("vm", v.win)
	builder.Get("vm_title", &v.title)
	builder.Get("vm_sub", &v.sub)
	builder.Get("vm_stack", &v.stack)
	builder.Get("vm_machines", &v.machineList)
	builder.Get("vm_new", &v.newButton)
	builder.Get("vm_templates", &v.templateList)
	builder.Get("vm_new_name", &v.nameEntry)
	builder.Get("vm_create", &v.createButton)
	builder.Get("vm_create_note", &v.createNote)
	builder.Get("vm_na_note", &v.naNote)
	builder.Get("vm_check_rows", &v.checkRows)
	builder.Get("vm_setup_summary", &v.setupSummary)
	builder.Get("vm_profile_desc", &v.profileDesc)
	builder.Get("vm_accept", &v.accept)
	builder.Get("vm_advanced_toggle", &v.advancedToggle)
	builder.Get("vm_advanced", &v.advanced)
	builder.Get("vm_memory", &v.memory)
	builder.Get("vm_cpus", &v.cpus)
	builder.Get("vm_disk", &v.disk)
	builder.Get("vm_install", &v.install)
	builder.Get("vm_setup_disk", &v.setupDisk)
	builder.Get("vm_setup_remove_data", &v.setupRemoveData)
	builder.Get("vm_setup_delete", &v.setupDelete)
	builder.Get("vm_setup_note", &v.setupNote)
	builder.Get("vm_progress", &v.progress)
	builder.Get("vm_progress_text", &v.progressText)
	builder.Get("vm_console", &v.console)
	builder.Get("vm_prov_start", &v.provStart)
	builder.Get("vm_prov_stop", &v.provStop)
	builder.Get("vm_prov_remove", &v.provRemove)
	builder.Get("vm_ready_summary", &v.readySummary)
	builder.Get("vm_ready_memory", &v.readyMemory)
	builder.Get("vm_ready_cpus", &v.readyCPUs)
	builder.Get("vm_disk_size", &v.diskSize)
	builder.Get("vm_ready_save", &v.readySave)
	builder.Get("vm_start", &v.start)
	builder.Get("vm_remove", &v.remove)
	builder.Get("vm_remove_data", &v.removeData)
	builder.Get("vm_boot_progress", &v.bootProgress)
	builder.Get("vm_boot_progress_text", &v.bootProgressText)
	builder.Get("vm_boot_console", &v.bootConsole)
	builder.Get("vm_boot_stop", &v.bootStop)
	builder.Get("vm_run_summary", &v.runSummary)
	builder.Get("vm_connect", &v.connect)
	builder.Get("vm_console_ready", &v.consoleReady)
	builder.Get("vm_stop", &v.stop)
	builder.Close()

	v.onClick = func(b gtk.Button) { v.clicked(b) }
	for _, b := range []*gtk.Button{&v.accept, &v.advancedToggle, &v.install,
		&v.console, &v.provStart, &v.provStop, &v.provRemove, &v.readySave, &v.start,
		&v.connect, &v.consoleReady, &v.stop, &v.remove, &v.removeData, &v.setupRemoveData,
		&v.setupDelete, &v.bootConsole, &v.bootStop, &v.newButton, &v.createButton} {
		b.ConnectClicked(&v.onClick)
	}
	v.onRowSelect = func(_ gtk.ListBox, ptr uintptr) {
		if ptr == 0 {
			return
		}
		row := gtk.ListBoxRowNewFromInternalPtr(ptr)
		if row == nil || row.GetName() == "" {
			return
		}
		v.choose(row.GetName())
	}
	v.machineList.ConnectRowSelected(&v.onRowSelect)
	v.onTemplate = func(_ gtk.ListBox, ptr uintptr) {
		if ptr == 0 {
			return
		}
		if row := gtk.ListBoxRowNewFromInternalPtr(ptr); row != nil {
			v.chooseTemplate(row.GetName())
		}
	}
	v.templateList.ConnectRowSelected(&v.onTemplate)
	v.onNameSubmit = func(_ gtk.Entry) { v.createMachine() }
	v.nameEntry.ConnectActivate(&v.onNameSubmit)

	v.createHint = v.createNote.GetText()
	v.fillTemplates()
	v.paintAll(manager.Machines())
	v.startWatch()
	v.win.Present()
	return v
}

// startWatch follows the chosen machine while the window is open, and the
// whole list less often, so an install that takes minutes moves the page on
// its own and a machine started elsewhere shows up here.
func (v *vmWindow) startWatch() {
	ctx, cancel := context.WithCancel(context.Background())
	v.watchCancel = cancel
	go func() {
		fast := time.NewTicker(2 * time.Second)
		slow := time.NewTicker(10 * time.Second)
		defer fast.Stop()
		defer slow.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-fast.C:
				if name := v.selectedName(); name != "" {
					view := v.manager.Refresh(name)
					gtkx.Post(func() {
						if v.closed {
							return
						}
						for i, x := range v.views {
							if x.Name == view.Name {
								v.views[i] = view
							}
						}
						v.updateRows()
						v.paint()
					})
				}
			case <-slow.C:
				views := v.manager.RefreshAll()
				gtkx.Post(func() {
					if !v.closed {
						v.paintAll(views)
					}
				})
			}
		}
	}()
}

// selectedName is the machine the panel shows. The watch reads it from its own
// goroutine while GTK callbacks write it, so it has a lock of its own.
func (v *vmWindow) selectedName() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.selected
}

func (v *vmWindow) setSelected(name string) {
	v.mu.Lock()
	v.selected = name
	v.mu.Unlock()
}

// choose shows a machine's page, reading its backend now rather than waiting
// for the next tick: the user just asked for it.
func (v *vmWindow) choose(name string) {
	if v.selectedName() == name && !v.newOpen {
		return
	}
	v.newOpen = false
	v.setSelected(name)
	v.note = ""
	v.rememberArmed(0)
	v.lastPage = ""
	v.setupFilled, v.readyFilled = false, false
	v.paint()
	name = v.selectedName()
	go func() {
		view := v.manager.Refresh(name)
		gtkx.Post(func() {
			if v.closed || v.selectedName() != name {
				return
			}
			for i, x := range v.views {
				if x.Name == view.Name {
					v.views[i] = view
				}
			}
			v.updateRows()
			v.paint()
		})
	}()
}

// paintAll takes a fresh list: the panel and the rows both follow from it.
// It runs on the GTK thread.
func (v *vmWindow) paintAll(views []MachineView) {
	if v.closed || v.win == nil {
		return
	}
	v.views = views
	selected := v.selectedName()
	if !v.has(selected) {
		selected = ""
		if len(views) > 0 && !v.newOpen {
			selected = views[0].Name
		}
		v.lastPage, v.setupFilled, v.readyFilled = "", false, false
	}
	v.setSelected(selected)
	v.rebuildRows()
	v.paint()
}

func (v *vmWindow) has(name string) bool {
	if name == "" {
		return false
	}
	for _, x := range v.views {
		if x.Name == name {
			return true
		}
	}
	return false
}

func (v *vmWindow) viewOf(name string) MachineView {
	for _, x := range v.views {
		if x.Name == name {
			return x
		}
	}
	return MachineView{Name: name}
}

// rebuildRows makes a row per machine, and only when the machines themselves
// changed: a plain repaint updates the labels in place, which is what the
// watch does every two seconds.
func (v *vmWindow) rebuildRows() {
	same := len(v.rows) == len(v.views)
	if same {
		for i, r := range v.rows {
			if r.name != v.views[i].Name {
				same = false
				break
			}
		}
	}
	if same {
		v.updateRows()
		return
	}
	for {
		row := v.machineList.GetRowAtIndex(0)
		if row == nil {
			break
		}
		v.machineList.Remove(&row.Widget)
	}
	v.rows = nil
	for _, x := range v.views {
		row, built := v.buildRow(x)
		v.machineList.Append(&row.Widget)
		v.rows = append(v.rows, built)
	}
	v.selectRow()
}

// updateRows brings the rows' labels up to date.
func (v *vmWindow) updateRows() {
	for i := range v.rows {
		if i >= len(v.views) {
			break
		}
		x := v.views[i]
		v.rows[i].title.SetText(x.Name)
		v.rows[i].sub.SetText(vmListSub(x))
	}
}

// buildRow is one machine in the list: its system's mark, its name, and what
// it is and is doing under it.
func (v *vmWindow) buildRow(x MachineView) (gtk.ListBoxRow, machineRow) {
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	mark := gtk.NewImageFromIconName(vmOSIcon(x.Machine.OS))
	mark.AddCssClass("vm-os-mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(box, &mark.Widget)

	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	title := gtk.NewLabel(x.Name)
	title.AddCssClass("vm-row-name")
	title.SetXalign(0)
	sub := gtk.NewLabel(vmListSub(x))
	sub.AddCssClass("vm-row-sub")
	sub.SetXalign(0)
	sub.SetEllipsize(pango.EllipsizeEndValue)
	gtkx.Append(text, &title.Widget)
	gtkx.Append(text, &sub.Widget)
	gtkx.Append(box, &text.Widget)

	row := gtk.NewListBoxRow()
	row.SetName(x.Name)
	row.SetChild(&box.Widget)
	return *row, machineRow{name: x.Name, title: *title, sub: *sub}
}

// selectRow marks the chosen machine in the list.
func (v *vmWindow) selectRow() {
	name := v.selectedName()
	for i, r := range v.rows {
		if r.name == name {
			if row := v.machineList.GetRowAtIndex(int32(i)); row != nil {
				v.machineList.SelectRow(row)
			}
			return
		}
	}
}

// paint picks the page and fills it. It runs on the GTK thread and reads only
// what is already in hand: no backend is touched here.
func (v *vmWindow) paint() {
	if v.closed || v.win == nil {
		return
	}
	// Making a new machine is a page of its own, and it outranks one that is
	// merely chosen: the user just asked for it.
	if v.newOpen {
		v.title.SetText(i18n.T("New machine"))
		v.sub.SetText(i18n.T("Pick what the new machine runs."))
		v.stack.SetVisibleChildName("new")
		return
	}
	name := v.selectedName()
	if name == "" {
		v.title.SetText(i18n.T("Virtual machines"))
		v.sub.SetText(i18n.T("Machines keep running with this window closed."))
		v.stack.SetVisibleChildName("empty")
		return
	}
	view := v.viewOf(name)
	v.title.SetText(view.Name)
	if !view.Ready {
		v.sub.SetText(i18n.T("No backend for this machine"))
		v.naNote.SetText(i18n.Tf("This machine needs the %s backend, which this build does not have.", vmBackendText(view.Machine.Backend)))
		v.stack.SetVisibleChildName("unavailable")
		return
	}
	if v.note != "" {
		v.sub.SetText(v.note)
	} else {
		v.sub.SetText(vmStateText(view))
	}
	if view.Page != v.lastPage {
		if view.Page == vm.PageSetup {
			v.setupFilled = false
		}
		if view.Page == vm.PageStopped {
			v.readyFilled = false
		}
		v.lastPage = view.Page
	}
	v.stack.SetVisibleChildName(string(view.Page))
	switch view.Page {
	case vm.PagePrereq:
		v.showChecks(view)
	case vm.PageSetup:
		v.showSetup(view)
	case vm.PageProvisioning:
		v.showProvisioning(view)
	case vm.PageStopped:
		v.showStopped(view)
	case vm.PageStarting:
		v.showStarting(view)
	case vm.PageRunning:
		v.showRunning(view)
	}
}

// fillTemplates fills the new-machine page, once: the templates do not change
// while the window is open.
func (v *vmWindow) fillTemplates() {
	templates := v.manager.Templates()
	v.templateRows = nil
	for _, t := range templates {
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		mark := gtk.NewImageFromIconName(vmOSIcon(t.OS))
		mark.AddCssClass("vm-os-mark")
		mark.SetValign(gtk.AlignCenterValue)
		gtkx.Append(box, &mark.Widget)

		text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		title := gtk.NewLabel(vmTemplateName(t))
		title.AddCssClass("vm-row-name")
		title.SetXalign(0)
		sub := gtk.NewLabel(vmTemplateText(t))
		sub.AddCssClass("vm-template-sub")
		sub.SetXalign(0)
		sub.SetWrap(true)
		gtkx.Append(text, &title.Widget)
		gtkx.Append(text, &sub.Widget)
		gtkx.Append(box, &text.Widget)

		row := gtk.NewListBoxRow()
		row.SetName(t.ID)
		row.SetChild(&box.Widget)
		v.templateList.Append(&row.Widget)
		v.templateRows = append(v.templateRows, templateRow{id: t.ID, title: *title, sub: *sub})
	}
	if len(templates) > 0 {
		v.chooseTemplate(templates[0].ID)
	}
}

// showNew opens the new-machine page.
func (v *vmWindow) showNew() {
	v.newOpen = true
	v.note = ""
	if v.templateID == "" && len(v.templateRows) > 0 {
		v.chooseTemplate(v.templateRows[0].id)
	}
	v.paint()
}

// chooseTemplate picks the template a new machine starts from, and offers the
// name that template usually takes.
func (v *vmWindow) chooseTemplate(id string) {
	t, ok := vm.TemplateByID(id)
	if !ok {
		return
	}
	v.templateID = id
	for i, r := range v.templateRows {
		if r.id == id {
			if row := v.templateList.GetRowAtIndex(int32(i)); row != nil {
				v.templateList.SelectRow(row)
			}
		}
	}
	v.createNote.RemoveCssClass("error")
	v.createNote.SetText(v.createHint)
	if want := t.Name; v.nameEntry.GetText() == "" || v.nameEntry.GetText() == v.templateName {
		v.nameEntry.SetText(want)
	}
	v.templateName = t.Name
}

// createMachine writes the new machine and shows it: nothing is installed or
// started here, so the new machine's page says what it needs next.
func (v *vmWindow) createMachine() {
	if v.operationBusy {
		return
	}
	id, name := v.templateID, v.nameEntry.GetText()
	if id == "" {
		return
	}
	v.operationBusy = true
	go func() {
		m, err := v.manager.Create(id, name)
		views := v.manager.Machines()
		gtkx.Post(func() {
			v.operationBusy = false
			if err != nil {
				v.createNote.SetText(i18n.Tf("That name cannot be used: %v", err))
				v.createNote.AddCssClass("error")
				return
			}
			v.createNote.RemoveCssClass("error")
			v.createNote.SetText(v.createHint)
			v.newOpen = false
			v.setSelected(m.Name)
			v.lastPage, v.setupFilled, v.readyFilled = "", false, false
			v.paintAll(views)
		})
	}()
}

// showChecks lists what the host is missing, one row each, in the user's
// language: the platform reports a reason, not a sentence.
func (v *vmWindow) showChecks(s MachineView) {
	failed := failedChecks(s.Checks)
	if doctorItemsEqual(v.lastChecks, failed) {
		return
	}
	v.lastChecks = failed
	v.checkRefs.Release()
	v.fixButtons = nil
	gtkx.Clear(&v.checkRows)
	for _, c := range failed {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("row")

		mark := gtk.NewLabel("")
		mark.AddCssClass("mark")
		mark.SetValign(gtk.AlignCenterValue)
		gtkx.Append(row, &mark.Widget)

		text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		name := gtk.NewLabel(vmCheckName(c.Name))
		name.AddCssClass("label")
		name.SetXalign(0)
		reason := gtk.NewLabel(vmCheckReason(c.Name, c.Reason))
		reason.AddCssClass("sub")
		reason.SetXalign(0)
		reason.SetWrap(true)
		gtkx.Append(text, &name.Widget)
		gtkx.Append(text, &reason.Widget)
		gtkx.Append(row, &text.Widget)

		// What the user can do about it: a button when Inua can apply the fix
		// itself, the platform's instructions when it cannot.
		switch {
		case c.Remedy != nil:
			label := i18n.T("Fix")
			if c.Remedy.Group != "" {
				label = i18n.Tf("Add me to %s", c.Remedy.Group)
			}
			btn := gtk.NewButtonWithLabel(label)
			btn.AddCssClass("outlined")
			btn.SetValign(gtk.AlignCenterValue)
			btn.SetName("fix")
			v.fixButtons = append(v.fixButtons, fixTarget{btn: *btn, remedy: *c.Remedy})
			btn.ConnectClicked(&v.onClick)
			gtkx.Append(row, &btn.Widget)
		default:
			if hint := vmPackageHint(c.Name, s.Distribution); hint != "" {
				hintLabel := gtk.NewLabel(hint)
				hintLabel.AddCssClass("sub")
				hintLabel.AddCssClass("vm-hint")
				hintLabel.SetValign(gtk.AlignCenterValue)
				gtkx.Append(row, &hintLabel.Widget)
			}
		}
		gtkx.Append(&v.checkRows, &row.Widget)
	}
}

// failedChecks are the host checks that did not pass.
func failedChecks(checks []vm.DoctorItem) []vm.DoctorItem {
	var out []vm.DoctorItem
	for _, c := range checks {
		if !c.OK {
			out = append(out, c)
		}
	}
	return out
}

// vmPackageHint is how to install a missing tool on this distribution, as a
// line the user can copy. The window never runs a package manager: choosing
// packages and changing a system is the user's call, not a button's.
func vmPackageHint(check, distro string) string {
	if check == "Secret Service" {
		return i18n.T("Start inua-keyring, or install a Secret Service.")
	}
	if check == "Operating system" {
		return i18n.T("This manager needs NixOS, Ubuntu or Debian.")
	}
	plan := vm.PlanPackages(distro)
	pkg := ""
	for _, name := range plan.Packages {
		switch check {
		case "Docker", "Docker Compose v2":
			if strings.Contains(strings.ToLower(name), "docker") {
				pkg = name
			}
		case "FreeRDP":
			if strings.Contains(strings.ToLower(name), "freerdp") {
				pkg = name
			}
		case "Browser opener":
			if strings.Contains(name, "xdg") {
				pkg = name
			}
		case "KVM":
			if strings.Contains(strings.ToLower(name), "qemu") || strings.Contains(name, "kvm") {
				pkg = name
			}
		case "QEMU":
			if strings.Contains(strings.ToLower(name), "qemu") {
				pkg = name
			}
		case "Firmware (UEFI)":
			if strings.Contains(strings.ToLower(name), "ovmf") || strings.Contains(strings.ToLower(name), "edk2") {
				pkg = name
			}
		case "Waydroid":
			if strings.Contains(strings.ToLower(name), "waydroid") {
				pkg = name
			}
		}
		if pkg != "" {
			break
		}
	}
	if pkg == "" {
		return ""
	}
	switch plan.PackageManager {
	case "nix":
		return i18n.Tf("Add %s to your NixOS configuration.", pkg)
	case "apt":
		return "sudo apt install " + pkg
	}
	return i18n.T("Install") + ": " + pkg
}

// vmCheckName is a check's name. The platform names them in English; these are
// the ones the window shows.
func vmCheckName(name string) string {
	switch name {
	case "Operating system":
		return i18n.T("Operating system")
	case "KVM":
		return i18n.T("Hardware virtualization (KVM)")
	case "Docker":
		return i18n.T("Docker")
	case "Docker Compose v2":
		return i18n.T("Docker Compose v2")
	case "FreeRDP":
		return i18n.T("FreeRDP")
	case "Browser opener":
		return i18n.T("Browser opener")
	case "Disk":
		return i18n.T("Disk space")
	case "TUN":
		return i18n.T("Network device (TUN)")
	case "Secret Service":
		return i18n.T("Secret Service")
	case "QEMU":
		return "QEMU"
	case "Firmware (UEFI)":
		return i18n.T("UEFI firmware")
	case "Waydroid":
		return "Waydroid"
	case "Binder":
		return i18n.T("Binder (the kernel's Android support)")
	}
	return name
}

// vmCheckReason words why a check failed. The Docker socket is the one case
// whose reason has to carry a warning: the socket is there and this user may
// not use it, and the fix — joining the docker group — would give everything
// running as this user what amounts to root on this host. Inua explains it,
// shows the command for whoever decides they want it, and points at the two
// ways that do not widen the account's rights; the QEMU backend needs no
// Docker at all.
func vmCheckReason(name string, r vm.DoctorReason) string {
	if name == "Docker" && r == vm.ReasonDenied {
		return i18n.T("Your user may not use Docker's socket. Joining the docker group would let anything running as you control this machine as root, so Inua will not do it for you: run `sudo gpasswd -a $USER docker` yourself if you accept that, or use rootless Docker or Podman instead.")
	}
	switch r {
	case vm.ReasonMissing:
		return i18n.T("Not installed on this system.")
	case vm.ReasonDenied:
		return i18n.T("Installed, but your user may not use it.")
	case vm.ReasonStopped:
		return i18n.T("Installed, but not running.")
	case vm.ReasonTooOld:
		return i18n.T("This Linux distribution is not supported.")
	}
	return i18n.T("Could not be checked.")
}

// fixTarget ties a button to the remedy it applies.
type fixTarget struct {
	btn    gtk.Button
	remedy vm.Remedy
}

// showSetup fills the page shown before a machine's guest exists: the profile
// to give it, and the one action that means "install now".
func (v *vmWindow) showSetup(s MachineView) {
	m := s.Machine
	if !v.setupFilled {
		recommended := s.Recommended
		if m.MemoryMiB == 0 {
			m.MemoryMiB, m.CPUs, m.DiskGiB = recommended.MemoryMiB, recommended.CPUs, recommended.DiskGiB
		}
		v.memory.SetValue(float64(m.MemoryMiB))
		v.cpus.SetValue(float64(m.CPUs))
		v.disk.SetValue(float64(m.DiskGiB))
		v.setupFilled = true
	}
	v.setupSummary.SetText(vmProfileText(v.setupMachine()))
	v.profileDesc.SetText(i18n.T("A balanced profile leaves memory and CPU for your system."))
	if s.Leftovers {
		v.setupNote.SetText(i18n.T("Files from a previous attempt are still here. Install reuses them; delete them to start over."))
	} else {
		v.setupNote.SetText(vmInstallNote(s))
	}
	v.setupDisk.SetVisible(s.Leftovers)
	v.accept.SetSensitive(!s.Busy)
	v.install.SetSensitive(!s.Busy)
	v.advancedToggle.SetSensitive(!s.Busy)
	v.setupRemoveData.SetSensitive(!s.Busy)
	v.setupDelete.SetSensitive(!s.Busy)
}

// showProvisioning fills the page shown while a machine's guest is being
// installed: the image is being pulled, the system is installing, or it is
// stopped in the middle of it.
func (v *vmWindow) showProvisioning(s MachineView) {
	if s.Progress > 0 {
		v.progress.SetFraction(s.Progress)
	} else {
		v.progress.Pulse()
	}
	switch {
	case s.ProgressText != "":
		v.progressText.SetText(s.ProgressText)
	case s.State == vm.StateStarting, s.State == vm.StateRunning:
		v.progressText.SetText(i18n.T("The system is installing; this takes a while."))
	case s.State == vm.StateStopping:
		v.progressText.SetText(i18n.T("Stopping…"))
	default:
		v.progressText.SetText(i18n.T("The installation is paused. Continue to resume."))
	}
	up := s.State == vm.StateRunning || s.State == vm.StateStarting
	v.provStart.SetVisible(!up)
	v.provStop.SetVisible(up)
	v.provStart.SetSensitive(!s.Busy)
	v.provStop.SetSensitive(!s.Busy)
	v.console.SetSensitive(!s.Busy && up)
	v.provRemove.SetSensitive(!s.Busy && !up)
}

// showStopped fills the page shown when the guest is installed and off: start
// it, change its memory or CPU count, remove it, or delete its files.
func (v *vmWindow) showStopped(s MachineView) {
	m := s.Machine
	v.readySummary.SetText(vmProfileText(m))
	if !v.readyFilled {
		v.readyMemory.SetValue(float64(m.MemoryMiB))
		v.readyCPUs.SetValue(float64(m.CPUs))
		v.readyFilled = true
	}
	v.diskSize.SetText(fmt.Sprintf("%d GiB", m.DiskGiB))
	v.start.SetSensitive(!s.Busy)
	v.readySave.SetSensitive(!s.Busy)
	v.remove.SetSensitive(!s.Busy)
	v.removeData.SetSensitive(!s.Busy)
}

// showStarting fills the page shown while an installed guest is coming up.
func (v *vmWindow) showStarting(s MachineView) {
	if s.Progress > 0 {
		v.bootProgress.SetFraction(s.Progress)
	} else {
		v.bootProgress.Pulse()
	}
	if s.ProgressText != "" {
		v.bootProgressText.SetText(s.ProgressText)
	} else if s.State == vm.StateStopping {
		v.bootProgressText.SetText(i18n.T("Stopping…"))
	} else {
		v.bootProgressText.SetText(i18n.T("The system is starting; ready in a minute or two."))
	}
	v.bootConsole.SetSensitive(!s.Busy)
	v.bootStop.SetSensitive(!s.Busy)
}

// showRunning fills the page shown while the guest is up: connect or stop.
func (v *vmWindow) showRunning(s MachineView) {
	v.runSummary.SetText(vmProfileText(s.Machine))
	v.connect.SetSensitive(!s.Busy)
	v.consoleReady.SetSensitive(!s.Busy)
	v.stop.SetSensitive(!s.Busy)
}

// clicked dispatches the window's buttons by their id, one handler for all of
// them (puregotk makes one native trampoline per function).
func (v *vmWindow) clicked(b gtk.Button) {
	name := v.selectedName()
	switch b.GetName() {
	case "new_machine":
		v.showNew()
	case "create_machine":
		v.createMachine()
	case "advanced_toggle":
		v.advanced.SetRevealChild(!v.advanced.GetRevealChild())
	case "accept":
		v.acceptProfile()
	case "ready_save":
		v.run(func() error { return v.manager.Save(v.readyMachine()) })
	case "install":
		m := v.setupMachine()
		v.run(func() error { return v.manager.Install(m) })
	case "start", "prov_start":
		v.run(func() error { return v.manager.Start(name) })
	case "connect":
		display := v.display()
		v.run(func() error { return v.manager.Connect(name, display) })
	case "console", "console_ready", "boot_console":
		v.run(func() error { return v.manager.Console(name) })
	case "prov_stop", "stop", "boot_stop":
		v.run(func() error { return v.manager.Stop(name) })
	case "remove", "prov_remove":
		if !v.removeArmed {
			v.rememberArmed(1)
			label := i18n.T("Confirm remove")
			v.remove.SetLabel(label)
			v.provRemove.SetLabel(label)
			return
		}
		v.rememberArmed(0)
		v.remove.SetLabel(i18n.T("Remove"))
		v.provRemove.SetLabel(i18n.T("Remove"))
		v.run(func() error { return v.manager.Remove(name) })
	case "remove_data":
		if !v.dataArmed {
			v.rememberArmed(2)
			v.removeData.SetLabel(i18n.T("Confirm delete"))
			return
		}
		v.rememberArmed(0)
		v.removeData.SetLabel(i18n.T("Delete disk"))
		v.run(func() error { return v.manager.DeleteDisk(name) })
	case "setup_remove_data":
		if !v.dataArmed {
			v.rememberArmed(2)
			v.setupRemoveData.SetLabel(i18n.T("Confirm delete"))
			return
		}
		v.rememberArmed(0)
		v.setupRemoveData.SetLabel(i18n.T("Delete disk"))
		v.run(func() error { return v.manager.DeleteDisk(name) })
	case "setup_delete":
		if !v.deleteArmed {
			v.rememberArmed(3)
			v.setupDelete.SetLabel(i18n.T("Confirm delete"))
			return
		}
		v.rememberArmed(0)
		v.setupDelete.SetLabel(i18n.T("Delete"))
		v.run(func() error { return v.manager.Delete(name) })
	case "fix":
		for _, t := range v.fixButtons {
			if t.btn.GoPointer() == b.GoPointer() {
				v.applyFix(t.remedy)
				return
			}
		}
	}
}

// rememberArmed keeps one destructive action armed at a time: arming a second
// disarms the first, so "Confirm delete" is never left waiting somewhere the
// user is not looking.
func (v *vmWindow) rememberArmed(which int) {
	if v.removeArmed {
		v.remove.SetLabel(i18n.T("Remove"))
		v.provRemove.SetLabel(i18n.T("Remove"))
	}
	if v.dataArmed {
		v.removeData.SetLabel(i18n.T("Delete disk"))
		v.setupRemoveData.SetLabel(i18n.T("Delete disk"))
	}
	if v.deleteArmed {
		v.setupDelete.SetLabel(i18n.T("Delete"))
	}
	v.removeArmed = which == 1
	v.dataArmed = which == 2
	v.deleteArmed = which == 3
}

// applyFix runs a remedy. The polkit dialog it may open lives on the main
// thread, so the work goes to a goroutine (as the other actions do) and the
// result comes back to the window.
func (v *vmWindow) applyFix(r vm.Remedy) {
	if v.operationBusy {
		return
	}
	v.operationBusy = true
	go func() {
		_, relogin, err := v.manager.Fix(r)
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			switch {
			case err != nil:
				v.note = i18n.Tf("The fix did not work: %v", err)
			case relogin:
				// A process's group list is fixed when it starts, so the
				// change is real but this session cannot see it yet: say so in
				// the window and in a notification.
				v.note = i18n.T("Done. Log out and back in for it to take effect.")
				v.askRelogin()
			default:
				v.note = i18n.T("Done.")
			}
			v.paintAll(views)
		})
	}()
}

// run performs a machine's action off the GTK thread and repaints what it left
// behind: the state of every machine may have moved (a start, a removal).
func (v *vmWindow) run(f func() error) {
	name := v.selectedName()
	if v.closed || v.operationBusy || name == "" {
		return
	}
	v.operationBusy = true
	v.paint()
	go func() {
		err := f()
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			if err != nil {
				v.note = i18n.Tf("That did not work: %v", err)
			} else {
				v.note = ""
			}
			v.paintAll(views)
		})
	}()
}

// display is the monitor the manager window is on: the screen on which its
// Connect opens FreeRDP, so the guest follows that screen's scale. Without a
// monitor to read, FreeRDP keeps its own resolution (rdpArgs).
func (v *vmWindow) display() *rdpDisplay {
	if v.win == nil {
		return nil
	}
	surface := v.win.GetSurface()
	if surface == nil {
		return nil
	}
	defer surface.Unref()
	gdkDisplay := surface.GetDisplay()
	if gdkDisplay == nil {
		return nil
	}
	defer gdkDisplay.Unref()
	mon := gdkDisplay.GetMonitorAtSurface(surface)
	if mon == nil {
		return nil
	}
	defer mon.Unref()
	var rect gdk.Rectangle
	mon.GetGeometry(&rect)
	return &rdpDisplay{Width: int(rect.Width), Height: int(rect.Height), Scale: float64(mon.GetScaleFactor())}
}

// askRelogin says, in a notification, that the change needs a fresh login.
// Logging out is the shell's to do — Hyprland 0.55 runs a Lua configuration and
// rejects the classic dispatcher, and the shell already knows which form its
// compositor takes — so this points at the control center's power row rather
// than ending the session from here.
func (v *vmWindow) askRelogin() {
	if v.c == nil || v.c.App == nil {
		return
	}
	n := gio.NewNotification(i18n.T("Log out to finish"))
	n.SetBody(i18n.T("The change takes effect the next time you log in."))
	v.c.App.SendNotification("vm-relogin", n)
	n.Unref()
}

// setupMachine is the chosen machine with the setup page's editors applied.
func (v *vmWindow) setupMachine() vm.Machine {
	m := v.viewOf(v.selectedName()).Machine
	m.MemoryMiB = int(v.memory.GetValueAsInt())
	m.CPUs = int(v.cpus.GetValueAsInt())
	m.DiskGiB = int(v.disk.GetValueAsInt())
	return m
}

// readyMachine is the chosen machine with the stopped page's editors applied.
// The disk keeps its size: it is the one field that cannot change under an
// installed guest.
func (v *vmWindow) readyMachine() vm.Machine {
	m := v.viewOf(v.selectedName()).Machine
	m.MemoryMiB = int(v.readyMemory.GetValueAsInt())
	m.CPUs = int(v.readyCPUs.GetValueAsInt())
	return m
}

func (v *vmWindow) acceptProfile() {
	s := v.viewOf(v.selectedName())
	v.memory.SetValue(float64(s.Recommended.MemoryMiB))
	v.cpus.SetValue(float64(s.Recommended.CPUs))
	v.disk.SetValue(float64(s.Recommended.DiskGiB))
	v.advanced.SetRevealChild(true)
	v.setupFilled = true
	v.setupSummary.SetText(vmProfileText(v.setupMachine()))
}

// vmStateText is the headline under the machine's name: what it is doing.
func vmStateText(s MachineView) string {
	if s.Err != nil {
		return i18n.Tf("Something went wrong: %s", s.Err.Error())
	}
	switch s.Page {
	case vm.PagePrereq:
		return i18n.T("This system is missing something the guest needs")
	case vm.PageSetup:
		if s.Leftovers {
			return i18n.T("Files from a previous attempt are still here")
		}
		return i18n.T("Not installed yet")
	case vm.PageProvisioning:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		if s.State == vm.StateStarting || s.State == vm.StateRunning {
			return i18n.T("Installing")
		}
		return i18n.T("Installation paused")
	case vm.PageStarting:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		return i18n.T("Starting")
	case vm.PageRunning:
		return i18n.T("Running")
	case vm.PageStopped:
		return i18n.T("Stopped")
	}
	return i18n.T("Virtual machines")
}

// vmListSub is a machine's line in the list: what it is, and what it is doing.
func vmListSub(s MachineView) string {
	if !s.Ready {
		return i18n.Tf("%s · no backend in this build", vmOSText(s.Machine.OS))
	}
	return vmOSText(s.Machine.OS) + " · " + vmShortState(s)
}

// vmShortState is one or two words for the list, where the headline's sentence
// would not fit.
func vmShortState(s MachineView) string {
	if s.Busy {
		return i18n.T("Working")
	}
	switch s.Page {
	case vm.PagePrereq:
		return i18n.T("Needs the host")
	case vm.PageSetup:
		return i18n.T("Not installed")
	case vm.PageProvisioning:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		return i18n.T("Installing")
	case vm.PageStarting:
		return i18n.T("Starting")
	case vm.PageRunning:
		return i18n.T("Running")
	case vm.PageStopped:
		return i18n.T("Stopped")
	}
	return i18n.T("Unknown")
}

// vmOSText names a system the way the window says it. The systems' own names
// are proper nouns and stay as they are: the catalog already translates the
// word "windows" as the thing a program draws in, which is not this.
func vmOSText(os vm.OS) string {
	switch os {
	case vm.OSWindows:
		return "Windows"
	case vm.OSLinux:
		return "Linux"
	case vm.OSFreeBSD:
		return "FreeBSD"
	case vm.OSAndroid:
		return "Android"
	case vm.OSCustom:
		return i18n.T("Custom system")
	}
	return string(os)
}

// vmBackendText names a backend. QEMU, Dockur and Waydroid are their own names
// too, and only the phrase around them is translated.
func vmBackendText(b vm.BackendKind) string { return string(b) }

// vmOSIcon is a system's mark, from the icons the shell installs.
func vmOSIcon(os vm.OS) string {
	switch os {
	case vm.OSWindows:
		return "vm-os-windows-symbolic"
	case vm.OSLinux:
		return "vm-os-linux-symbolic"
	case vm.OSFreeBSD:
		return "vm-os-freebsd-symbolic"
	case vm.OSAndroid:
		return "vm-os-android-symbolic"
	}
	return "vm-os-custom-symbolic"
}

// vmInstallNote says what installing this machine costs, which is not the same
// for a container that downloads Windows and a system booted off an ISO.
func vmInstallNote(s MachineView) string {
	switch s.Machine.Backend {
	case vm.BackendDockur:
		return i18n.T("Downloads about 8 GB and installs Windows; takes 30+ minutes. Windows needs a licence.")
	default:
		return i18n.T("The guest installs from its own media, as it would on a real machine.")
	}
}

// vmTemplateName is a template's title: the system it installs. The two
// Windows templates differ by how they install it, which their own line says.
func vmTemplateName(t vm.Template) string {
	switch t.ID {
	case "windows-dockur", "windows-qemu":
		return "Windows 11"
	case "ubuntu":
		return "Ubuntu"
	case "fedora":
		return "Fedora"
	case "arch":
		return "Arch Linux"
	case "freebsd":
		return "FreeBSD"
	case "android":
		return "Android"
	case "custom":
		return i18n.T("Custom system")
	}
	return vmOSText(t.OS)
}

// vmTemplateText says what a template gives, in one line.
func vmTemplateText(t vm.Template) string {
	switch t.ID {
	case "windows-dockur":
		return i18n.T("Windows 11, installed for you in a container.")
	case "windows-qemu":
		return i18n.T("Windows 11 from your own ISO, with the virtio drivers.")
	case "ubuntu":
		return i18n.T("Ubuntu, booted with QEMU from its ISO.")
	case "fedora":
		return i18n.T("Fedora, booted with QEMU from its ISO.")
	case "arch":
		return i18n.T("Arch Linux, booted with QEMU from its ISO.")
	case "freebsd":
		return i18n.T("FreeBSD, booted with QEMU from its ISO.")
	case "android":
		return i18n.T("Android in Waydroid, with the host's display and GPU.")
	case "custom":
		return i18n.T("Your own system: an ISO, a disk or a kernel, on any architecture.")
	}
	return string(t.Backend)
}

// vmProfileText is a machine's resources, as the section meta reads them.
func vmProfileText(m vm.Machine) string {
	return fmt.Sprintf("%s · %d CPU · %d GiB", humanMiB(m.MemoryMiB), m.CPUs, m.DiskGiB)
}

// humanMiB shows a memory size the way the other windows do.
func humanMiB(mib int) string {
	if mib >= 1024 {
		return fmt.Sprintf("%d GiB", mib/1024)
	}
	return fmt.Sprintf("%d MiB", mib)
}

func doctorItemsEqual(a, b []vm.DoctorItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].OK != b[i].OK || a[i].Reason != b[i].Reason {
			return false
		}
		ar, br := a[i].Remedy, b[i].Remedy
		if (ar == nil) != (br == nil) {
			return false
		}
		if ar != nil && ar.Group != br.Group {
			return false
		}
	}
	return true
}
