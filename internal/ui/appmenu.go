package ui

import (
	"context"
	"log"
	"strconv"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/system"
)

type appMenuTarget struct {
	registration appmenu.Registration
	id           int32
}

type appMenuState struct {
	registrar  *appmenu.Registrar
	targets    map[string]appMenuTarget
	nextTarget uint64
}

func (s *shellUI) startAppMenu() {
	conn, err := system.SessionBus()
	if err != nil {
		log.Printf("appmenu: %v", err)
		return
	}
	s.appmenu.registrar = appmenu.New(conn)
	s.appmenu.targets = make(map[string]appMenuTarget)
	s.appmenu.registrar.SetOnChange(func() { gtkx.Post(s.syncAppMenu) })
	go func() {
		if err := s.appmenu.registrar.Run(context.Background()); err != nil {
			log.Printf("appmenu registrar: %v", err)
		}
	}()
}

func focusedPID(snap hypr.Snapshot) int {
	for _, win := range snap.Toplevels {
		if win.Address == snap.ActiveToplevel {
			return win.PID
		}
	}
	return 0
}

func (s *shellUI) syncAppMenu() {
	if s.appmenu.registrar == nil {
		return
	}
	reg, ok := s.appmenu.registrar.ForPID(focusedPID(s.snap))
	for _, b := range s.bars {
		if b.connector() != s.snap.FocusedMonitor || !ok {
			b.appMenuGen++
			gtkx.Clear(&b.appMenu)
			b.appMenuReg = appmenu.Registration{}
			b.appMenu.SetVisible(false)
			continue
		}
		if b.appMenuReg == reg {
			continue
		}
		b.appMenuGen++
		gen := b.appMenuGen
		gtkx.Clear(&b.appMenu)
		s.appmenu.targets = make(map[string]appMenuTarget)
		b.appMenuReg = reg
		b.appMenu.SetVisible(false)
		go s.loadAppMenu(b, gen, reg)
	}
}

func (s *shellUI) refreshAppMenu(reg appmenu.Registration) {
	for _, b := range s.bars {
		if b.appMenuReg != reg || b.connector() != s.snap.FocusedMonitor {
			continue
		}
		b.appMenuGen++
		gen := b.appMenuGen
		gtkx.Clear(&b.appMenu)
		b.appMenu.SetVisible(false)
		go s.loadAppMenu(b, gen, reg)
	}
}

func (s *shellUI) loadAppMenu(b *bar, gen uint64, reg appmenu.Registration) {
	entries, err := s.appmenu.registrar.Menu(reg)
	gtkx.Post(func() {
		pid := focusedPID(s.snap)
		if b.appMenuGen != gen || b.appMenuReg != reg || pid == 0 || uint32(pid) != reg.PID || b.connector() != s.snap.FocusedMonitor {
			return
		}
		gtkx.Clear(&b.appMenu)
		b.appMenu.SetVisible(false)
		if err != nil {
			log.Printf("appmenu %s: %v", reg.Service, err)
			return
		}
		s.appmenu.targets = make(map[string]appMenuTarget)
		for _, entry := range entries {
			if !entry.Separator {
				s.appendAppMenu(b, entry, reg)
			}
		}
		b.appMenu.SetVisible(b.appMenu.GetFirstChild() != nil)
	})
}

func (s *shellUI) appendAppMenu(b *bar, entry appmenu.Entry, reg appmenu.Registration) {
	if len(entry.Children) > 0 {
		button := gtk.NewMenuButton()
		button.SetLabel(entry.Label)
		button.SetHasFrame(false)
		button.SetFocusable(false)
		group := gio.NewSimpleActionGroup()
		menu := s.buildAppMenu(entry.Children, group, reg)
		button.SetMenuModel(&menu.MenuModel)
		button.InsertActionGroup("appmenu", group)
		gtkx.Append(&b.appMenu, &button.Widget)
		menu.Unref()
		group.Unref()
		return
	}
	button := gtk.NewButton()
	button.SetLabel(entry.Label)
	button.SetHasFrame(false)
	button.SetFocusable(false)
	button.SetSensitive(entry.Enabled)
	token := s.addAppMenuTarget(reg, entry.ID)
	button.SetName(token)
	button.ConnectClicked(&s.onAppMenuClicked)
	gtkx.Append(&b.appMenu, &button.Widget)
}

func (s *shellUI) addAppMenuTarget(reg appmenu.Registration, id int32) string {
	s.appmenu.nextTarget++
	token := strconv.FormatUint(s.appmenu.nextTarget, 10)
	s.appmenu.targets[token] = appMenuTarget{registration: reg, id: id}
	return token
}

func (s *shellUI) activateAppMenu(reg appmenu.Registration, id int32) {
	current, ok := s.appmenu.registrar.ForPID(focusedPID(s.snap))
	if !ok || current != reg {
		return
	}
	go func() {
		if err := s.appmenu.registrar.Activate(reg, id); err != nil {
			log.Printf("appmenu activate: %v", err)
		}
		gtkx.Post(func() { s.refreshAppMenu(reg) })
	}()
}

func (s *shellUI) bindAppMenuHandlers() {
	s.onAppMenuClicked = func(button gtk.Button) {
		target, ok := s.appmenu.targets[button.GetName()]
		if !ok || focusedPID(s.snap) == 0 || uint32(focusedPID(s.snap)) != target.registration.PID {
			return
		}
		s.activateAppMenu(target.registration, target.id)
	}
}

func (s *shellUI) buildAppMenu(entries []appmenu.Entry, group *gio.SimpleActionGroup, reg appmenu.Registration) *gio.Menu {
	menu := gio.NewMenu()
	for _, entry := range entries {
		if entry.Separator {
			continue
		}
		label := entry.Label
		if entry.Toggle != "" && entry.Checked {
			label = "✓  " + label
		}
		if len(entry.Children) > 0 {
			sub := s.buildAppMenu(entry.Children, group, reg)
			menu.AppendSubmenu(label, &sub.MenuModel)
			sub.Unref()
			continue
		}
		name := "i" + strconv.Itoa(int(entry.ID))
		if entry.Enabled {
			token := s.addAppMenuTarget(reg, entry.ID)
			action := gio.NewSimpleAction(name, nil)
			onActivate := func(gio.SimpleAction, uintptr) {
				target, ok := s.appmenu.targets[token]
				if !ok {
					return
				}
				s.activateAppMenu(target.registration, target.id)
			}
			action.ConnectActivate(&onActivate)
			group.AddAction(action)
			action.Unref()
			menu.Append(label, "appmenu."+name)
		} else {
			item := gio.NewMenuItem(label, "")
			value := glib.NewVariantBoolean(false)
			item.SetAttributeValue("enabled", value)
			value.Unref()
			menu.AppendItem(item)
			item.Unref()
		}
	}
	return menu
}
