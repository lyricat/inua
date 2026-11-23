package ui

import (
	"context"
	"strconv"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
)

type dockMove struct{ window, workspace string }

func (s *shellUI) addDockMoveMenu(menu *gtk.Box, it shell.DockItem) []dockMove {
	if !s.caps.Has(compositor.CapMoveWithoutFollow) || len(it.Windows) == 0 {
		return nil
	}
	choices := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	var moves []dockMove
	for _, w := range it.Windows {
		rows := choices
		if len(it.Windows) > 1 {
			rows = gtk.NewBox(gtk.OrientationVerticalValue, 0)
		}
		for _, ws := range s.snap.Workspaces {
			if ws.ID == w.Workspace {
				continue
			}
			label := i18n.Tf("Workspace %s", ws.Name)
			if len(s.snap.Outputs) > 1 && ws.Output != "" {
				label += " · " + ws.Output
			}
			item := gtk.NewButtonWithLabel(label)
			item.AddCssClass("menu-item")
			item.SetName("move:" + strconv.Itoa(len(moves)))
			item.ConnectClicked(&s.onDockMenuItem)
			gtkx.Append(rows, &item.Widget)
			moves = append(moves, dockMove{w.ID, ws.ID})
		}
		if rows != choices {
			if rows.GetFirstChild() != nil {
				title := w.Title
				if title == "" {
					title = i18n.T("Untitled Window")
				}
				addDockMoveGroup(choices, rows, title)
			} else {
				rows.Unref()
			}
		}
	}
	if len(moves) == 0 {
		choices.Unref()
		return nil
	}
	addDockMoveGroup(menu, choices, i18n.T("Move to Workspace"))
	return moves
}

func addDockMoveGroup(parent, rows *gtk.Box, title string) {
	group := gtk.NewExpander(title)
	group.AddCssClass("menu-item")
	group.SetChild(&rows.Widget)
	rows.Unref()
	gtkx.Append(parent, &group.Widget)
}

func (s *shellUI) moveDockWindow(it shell.DockItem, index string) {
	i, err := strconv.Atoi(index)
	if err != nil || i < 0 || i >= len(s.dockState.menuMoves) || !s.caps.Has(compositor.CapMoveWithoutFollow) {
		return
	}
	move := s.dockState.menuMoves[i]
	if _, ok := s.snap.Workspace(move.workspace); !ok {
		return
	}
	for _, w := range it.Windows {
		if w.ID == move.window && w.Workspace != move.workspace {
			s.compositorDo("dock move window", func(ctx context.Context) error {
				return s.comp.MoveWindow(ctx, move.window, move.workspace, false)
			})
			return
		}
	}
}
