package appmenu

// Fillable reports whether an entry's items are made when it opens: the
// application answered with a named section and an action to open it, and
// nothing under it yet.
func Fillable(e Entry) bool {
	return len(e.Children) == 0 && !e.Separator && e.Label != "" &&
		e.Action.Kind == ActionA11y && e.Action.Bus != "" && e.Action.Path != ""
}

// FillableMenu reports whether a menu is one the bar can show: sections
// with their items, or named sections the application builds when they
// open (a browser's), which the bar fills one at a time as they are asked
// for. A tree of unnamed items is a toolkit skeleton and stays out.
func FillableMenu(entries []Entry) bool {
	for _, e := range entries {
		if e.Label == "" || e.Separator {
			continue
		}
		if len(e.Children) > 0 || Fillable(e) {
			return true
		}
	}
	return false
}
