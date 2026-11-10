package ui

import (
	"log"
	"strings"

	"github.com/lyricat/inua/internal/dbusmenu"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// publishSettingsMenu gives the settings window a menu the bar can show:
// the pages under View, and Close under File.
func (s *shellUI) publishSettingsMenu() {
	st := &s.settings
	var pages []dbusmenu.Node
	for _, name := range settingsPages {
		child := st.pages.GetChildByName(name)
		if child == nil {
			continue
		}
		pages = append(pages, dbusmenu.Node{Label: st.pages.GetPage(child).GetTitle(), Action: "page:" + name, Enabled: true})
	}
	nodes := []dbusmenu.Node{
		{Label: i18n.T("File"), Enabled: true, Children: []dbusmenu.Node{
			{Label: i18n.T("Close"), Action: "close", Enabled: true},
		}},
		{Label: i18n.T("View"), Enabled: true, Children: pages},
	}
	menu, err := dbusmenu.Publish(nodes, func(action string) {
		gtkx.Post(func() {
			switch {
			case action == "close":
				if st.win != nil {
					st.win.Close()
				}
			case strings.HasPrefix(action, "page:"):
				s.showSettingsPage(strings.TrimPrefix(action, "page:"))
			}
		})
	})
	if err != nil {
		log.Printf("settings menu: %v", err)
		return
	}
	st.menu = menu
}
