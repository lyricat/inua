package gtkx

import "codeberg.org/puregotk/puregotk/v4/gtk"

// DetachPopoverLater keeps pop alive and detaches it outside GTK's signal
// emission. The anchor may already have detached it in the meantime.
func DetachPopoverLater(pop *gtk.Popover) {
	pop.Ref()
	Post(func() {
		defer pop.Unref()
		if pop.GetParent() != nil {
			pop.Unparent()
		}
	})
}
