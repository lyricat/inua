package appmenu

import "testing"

// A menu whose sections must never be pressed keeps only the sections that
// already hold their items: one filled by pressing it open goes, with a
// top-level item, and what is left is still a menu the bar can show only
// when some section had items.
func TestPrefilledDropsSectionsFilledByPressing(t *testing.T) {
	press := Action{Kind: ActionA11y, Bus: ":1.4", Path: "/org/a11y/atspi/accessible/7"}
	lazy := []Entry{
		{Label: "File", Enabled: true, Action: press},
		{Label: "Edit", Enabled: true, Action: press},
	}
	if !FillableMenu(lazy) {
		t.Fatal("a row of lazy sections is a menu the bar fills")
	}
	if got := Prefilled(lazy); FillableMenu(got) {
		t.Fatalf("lazy sections without pressing: %+v, want no menu", got)
	}

	mixed := []Entry{
		{Label: "File", Enabled: true, Action: press},
		{Separator: true},
		{Label: "View", Enabled: true, Children: []Entry{{Label: "Zoom In", Enabled: true, Action: press}}},
		{Label: "Help", Enabled: true, Action: press},
	}
	got := Prefilled(mixed)
	if len(got) != 1 || got[0].Label != "View" || len(got[0].Children) != 1 {
		t.Fatalf("Prefilled = %+v, want View with its item", got)
	}
	if !FillableMenu(got) {
		t.Fatal("a section with its items is a menu the bar can show")
	}
}
