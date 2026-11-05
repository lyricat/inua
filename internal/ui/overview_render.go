package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/compositor"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/lyricat/inua/internal/shell"
)

// These are borrowed widgets owned by the card tree. Rebuild only when its
// membership or layout changes; focus, titles and window geometry update in place.
type overviewCardWidgets struct {
	canvas                                 *gtk.Overlay
	board                                  *gtk.Fixed
	order                                  string
	monX, monY, monW, monH, thumbW, thumbH float64
}

type overviewWindowWidgets struct {
	button *gtk.Button
	label  *gtk.Label // the title on the preview
	rect   [4]float64
	title  string
}

func overviewLayoutKey(cards []shell.OverviewCard, monitors []compositor.Output, perMonitor bool, monitor string, scale int, searching bool, top, bottom, start, end int) string {
	var key strings.Builder
	fmt.Fprintf(&key, "%t/%q/%d/%t/%d/%d/%d/%d;", perMonitor, monitor, scale, searching, top, bottom, start, end)
	for _, mon := range monitors {
		fmt.Fprintf(&key, "%q/%d/%d/%d/%d/%g;", mon.Name, mon.X, mon.Y, mon.Width, mon.Height, mon.Scale)
	}
	for i, card := range cards {
		if perMonitor && card.Workspace.Output != monitor {
			continue
		}
		fmt.Fprintf(&key, "%d/%s/%q/%q/%t;", i, card.Workspace.ID, card.Workspace.Name, card.Workspace.Output, card.New)
		windows := make([]string, 0, len(card.Windows))
		for _, w := range card.Windows {
			windows = append(windows, fmt.Sprintf("%q/%q;", w.ID, w.AppID))
		}
		slices.Sort(windows)
		for _, window := range windows {
			key.WriteString(window)
		}
	}
	return key.String()
}

func overviewWindowOrder(windows []compositor.Window) string {
	var order strings.Builder
	for _, window := range windows {
		fmt.Fprintf(&order, "%q;", window.ID)
	}
	return order.String()
}

func overviewClass(w *gtk.Widget, class string, enabled bool) {
	if enabled == w.HasCssClass(class) {
		return
	}
	if enabled {
		w.AddCssClass(class)
	} else {
		w.RemoveCssClass(class)
	}
}

func (s *shellUI) updateOverviewWidgets(o *workspaceOverview) {
	for i, card := range o.st.Cards {
		view, ok := o.cardWidgets[i]
		if !ok {
			continue
		}
		order := overviewWindowOrder(card.Windows)
		if order != view.order {
			// Reorder the same widgets using Fixed's API, retaining their
			// controllers and preview textures while focus/title ordering changes.
			for j := len(card.Windows) - 1; j >= 0; j-- {
				window := o.windowWidgets[card.Windows[j].ID]
				window.button.Ref()
				view.board.Remove(&window.button.Widget)
				view.board.Put(&window.button.Widget, window.rect[0], window.rect[1])
				window.button.Unref()
			}
			view.order = order
		}
		overviewClass(&view.canvas.Widget, "selected", i == o.st.Selected)
		overviewClass(&view.canvas.Widget, "active", card.Workspace.Active)
		for _, w := range card.Windows {
			window := o.windowWidgets[w.ID]
			if window == nil {
				continue
			}
			x, y, pw, ph := overviewWindowLayout(w, view.monX, view.monY, view.monW, view.monH, view.thumbW, view.thumbH)
			s.noteOverviewPreviewSize(w.ID, pw, ph, int(o.root.GetScaleFactor()))
			rect := [4]float64{x, y, pw, ph}
			if window.rect != rect {
				view.board.Move(&window.button.Widget, x, y)
				window.button.SetSizeRequest(int32(pw), int32(ph))
				window.rect = rect
			}
			if window.title != w.Title {
				window.button.SetTooltipText(w.Title)
				window.label.SetText(w.Title)
				window.title = w.Title
			}
			overviewClass(&window.button.Widget, "active", w.Focused)
		}
	}
}
