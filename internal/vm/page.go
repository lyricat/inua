package vm

// Page is the manager window's page: one screen for each stage, so the
// window never shows controls that belong to a different stage.
type Page string

const (
	PagePrereq       Page = "prereq"       // the host is missing something
	PageSetup        Page = "setup"        // environment is ready; nothing installed
	PageProvisioning Page = "provisioning" // Windows is installing for the first time
	PageStopped      Page = "stopped"      // Windows is installed and the guest is off
	PageStarting     Page = "starting"     // Windows is installed and the guest is coming up
	PageRunning      Page = "running"      // Windows is up
)

// PageFor picks the page from what is true of the host and the guest.
//
// The window walks this order: fix the host, install Windows, then either
// start it, wait for it, or use it. Each page is exclusive of the others.
func PageFor(available, hasProject, installed bool, state State, busy bool) Page {
	if !available {
		return PagePrereq
	}
	if !installed {
		if hasProject || busy {
			return PageProvisioning
		}
		return PageSetup
	}
	switch state {
	case StateRunning:
		return PageRunning
	case StateStarting, StateStopping:
		return PageStarting
	}
	if busy {
		return PageStarting
	}
	return PageStopped
}
