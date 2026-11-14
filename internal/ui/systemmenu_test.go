package ui

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/i18n"
)

// systemLeaves are the commands of a system menu's lines, by label.
func systemLeaves(lines []systemLine, out map[string]string) {
	for _, l := range lines {
		if len(l.lines) > 0 {
			systemLeaves(l.lines, out)
		} else if !l.separator {
			out[l.label] = l.command
		}
	}
}

func systemSection(t *testing.T, menu []systemLine, label string) []systemLine {
	t.Helper()
	i := slices.IndexFunc(menu, func(l systemLine) bool { return l.label == label })
	if i < 0 {
		t.Fatalf("no %q section in %+v", label, menu)
	}
	return menu[i].lines
}

// settingsUIPages reads the settings window's pages, name and title, from
// its layout.
func settingsUIPages(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "ui", "settings.ui"))
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Name  string `xml:"name,attr"`
		Value string `xml:",chardata"`
	}
	pages := map[string]string{}
	d := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "object" || !slices.Contains(se.Attr, xml.Attr{Name: xml.Name{Local: "class"}, Value: "GtkStackPage"}) {
			continue
		}
		// A page's name and title come before its child.
		var name, title string
		for name == "" || title == "" {
			tok, err := d.Token()
			if err != nil {
				t.Fatal(err)
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "property" {
				var p property
				if err := d.DecodeElement(&p, &se); err != nil {
					t.Fatal(err)
				}
				switch p.Name {
				case "name":
					name = p.Value
				case "title":
					title = p.Value
				}
			}
		}
		pages[name] = title
	}
	if len(pages) == 0 {
		t.Fatal("no pages in settings.ui")
	}
	return pages
}

func TestSystemMenuSettingsPages(t *testing.T) {
	ui := settingsUIPages(t)
	for name, title := range ui {
		if !slices.Contains(settingsPages, name) {
			t.Errorf("settings.ui page %q is not in settingsGroups", name)
		}
		if got := settingsPageTitles[name]; got != title {
			t.Errorf("page %q: title %q, settings.ui has %q", name, got, title)
		}
	}
	settings := systemSection(t, systemSection(t, systemMenu(false), "System"), "Settings")
	var got []string
	for _, l := range settings {
		if l.separator {
			continue
		}
		page, ok := strings.CutPrefix(l.command, "settings:")
		if !ok {
			t.Errorf("%q runs %q, not a settings page", l.label, l.command)
			continue
		}
		if _, ok := ui[page]; !ok {
			t.Errorf("%q opens page %q, which settings.ui has not", l.label, page)
		}
		if l.label != ui[page] {
			t.Errorf("page %q is labelled %q, its title is %q", page, l.label, ui[page])
		}
		got = append(got, page)
	}
	if !slices.Equal(got, settingsPages) {
		t.Errorf("settings submenu: %v, want every page in the index's order %v", got, settingsPages)
	}
}

func TestSystemMenuSections(t *testing.T) {
	menu := systemMenu(false)
	var labels []string
	for _, l := range menu {
		labels = append(labels, l.label)
	}
	if want := []string{"System", "Session", "Power", "Help"}; !slices.Equal(labels, want) {
		t.Fatalf("sections %v, want %v", labels, want)
	}
	leaves := map[string]string{}
	systemLeaves(menu, leaves)
	for label, command := range map[string]string{
		"Launcher":           "launcher",
		"Screenshot":         "screenshot",
		"Control Center":     "control",
		"Lock Screen":        "lock",
		"Log Out":            "logout",
		"Suspend":            "suspend",
		"Restart":            "reboot",
		"Shut Down":          "poweroff",
		"About inua":         "settings:about",
		"Keyboard Shortcuts": "settings:shortcuts",
	} {
		if leaves[label] != command {
			t.Errorf("%q runs %q, want %q", label, leaves[label], command)
		}
	}
	for _, page := range []string{"about", "shortcuts"} {
		if !slices.Contains(settingsPages, page) {
			t.Errorf("Help opens page %q, which is not a settings page", page)
		}
	}
	if got := systemMenuEntries(false); len(got) != len(menu) || !got[0].Submenu || !got[0].Enabled {
		t.Errorf("bar buttons %+v: want one enabled submenu per section", got)
	}
}

func TestSystemMenuHibernate(t *testing.T) {
	has := func(can bool) bool {
		leaves := map[string]string{}
		systemLeaves(systemSection(t, systemMenu(can), "Power"), leaves)
		return leaves["Hibernate"] == "hibernate"
	}
	if has(false) {
		t.Error("Hibernate offered where the machine cannot")
	}
	if !has(true) {
		t.Error("Hibernate missing where the machine can")
	}
}

// Every label of the system menu is in the catalogs, so make i18n found
// it, and the menu's own (the pages' are the index's) is translated.
func TestSystemMenuTranslated(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "po", "inua.pot"))
	if err != nil {
		t.Fatal(err)
	}
	pot, err := i18n.ParsePO(string(data))
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, e := range pot {
		known[e.ID] = true
	}
	leaves := map[string]string{}
	var labels []string
	var walk func([]systemLine)
	walk = func(lines []systemLine) {
		for _, l := range lines {
			if !l.separator {
				labels = append(labels, l.label)
			}
			walk(l.lines)
		}
	}
	walk(systemMenu(true))
	systemLeaves(systemMenu(true), leaves)
	for _, l := range labels {
		if !known[l] {
			t.Errorf("%q is not in po/inua.pot", l)
		}
	}
	locales, err := filepath.Glob(filepath.Join("..", "i18n", "locales", "*.po"))
	if err != nil || len(locales) == 0 {
		t.Fatalf("no catalogs: %v", err)
	}
	for _, path := range locales {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := i18n.ParsePO(string(data))
		if err != nil {
			t.Fatal(err)
		}
		translated := map[string]bool{}
		for _, e := range entries {
			translated[e.ID] = len(e.Str) > 0 && e.Str[0] != ""
		}
		for _, l := range labels {
			if page, ok := strings.CutPrefix(leaves[l], "settings:"); ok && settingsPageTitles[page] == l {
				continue // a page's title, translated as the index has it
			}
			if !translated[l] {
				t.Errorf("%s: %q is not translated", filepath.Base(path), l)
			}
		}
	}
}
