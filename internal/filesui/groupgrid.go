package filesui

import (
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
)

const maxGridColumns = 64

func (p *pane) groupedGrid() bool {
	return p.mode == inFolder && p.shownView() == files.GridView && p.fv.Order.GroupBy != files.GroupNone
}

func (p *pane) gridColumns() uint32 {
	if !p.groupedGrid() {
		return 0
	}
	adj := p.gridScroll.GetHadjustment()
	width := adj.GetPageSize()
	adj.Unref()
	if width <= 0 {
		width = float64(p.box.GetWidth())
	}
	// Account for grid padding and room around each tile at this zoom.
	cell := max(126, int(gridIcon(p.w.app.settings.Zoom))+24)
	return uint32(max(1, min(maxGridColumns, (int(width)-24)/cell)))
}

func (p *pane) queueGridResize() {
	if p.gridResize || !p.groupedGrid() {
		return
	}
	p.gridResize = true
	// The adjustment notifies during GTK allocation. Reflow after it.
	p.w.app.h.later(0, func() {
		p.gridResize = false
		if !p.destroyed && p.gridCols != p.gridColumns() && p.groupedGrid() {
			p.refill(p.selectedNames(), false)
		}
	})
}

func (p *pane) layoutGrid(names []string) []string {
	cols := p.gridColumns()
	if cols != p.gridCols {
		p.gridCols = cols
		p.grid.SetMinColumns(max(1, cols))
		limit := cols
		if limit == 0 {
			limit = maxGridColumns
		}
		p.grid.SetMaxColumns(limit)
		setClass(&p.grid.Widget, "grouped", cols != 0)
	}
	if cols == 0 {
		p.gridSlots, p.gridFiles = nil, nil
		return names
	}
	layout, slots, positions := gridSectionSlots(names, int(cols), func(pos int) string {
		return p.groupHeading(uint32(pos), p.shown[pos])
	})
	p.gridSlots, p.gridFiles = slots, positions
	return layout
}

// A header occupies one row of GridView slots and the next group never
// shares a row with the previous one. Empty strings and slash-prefixed
// headers cannot be real filenames. Neither maps to a file position.
func gridSectionSlots(names []string, cols int, heading func(int) string) (layout []string, slots []int, positions []uint32) {
	layout = make([]string, 0, len(names)+cols)
	slots = make([]int, 0, cap(layout))
	positions = make([]uint32, len(names))
	add := func(name string, pos int) {
		layout = append(layout, name)
		slots = append(slots, pos)
	}
	for pos, name := range names {
		if title := heading(pos); title != "" {
			for len(slots)%cols != 0 {
				add("", -1)
			}
			add("/"+title, -1)
			for range cols - 1 {
				add("/", -1)
			}
		}
		positions[pos] = uint32(len(slots))
		add(name, pos)
	}
	return layout, slots, positions
}

func (p *pane) shownPosition(pos uint32) (int, bool) {
	if p.gridSlots != nil {
		if int(pos) >= len(p.gridSlots) {
			return 0, false
		}
		shown := p.gridSlots[pos]
		return shown, shown >= 0 && shown < len(p.shown)
	}
	return int(pos), int(pos) < len(p.shown)
}

func (p *pane) modelPosition(shown int) uint32 {
	if p.gridSlots != nil {
		return p.gridFiles[shown]
	}
	return uint32(shown)
}

func (p *pane) bindGridSlot(li *gtk.ListItem, r *row, pos uint32, file bool) {
	li.SetSelectable(file)
	li.SetActivatable(file)
	li.SetFocusable(file)
	r.box.SetVisible(file)
	r.group.SetVisible(false)
	if file || pos >= p.model.GetNItems() {
		return
	}
	if title, header := strings.CutPrefix(p.model.GetString(pos), "/"); header {
		r.group.SetText(title)
		r.group.SetTooltipText(title)
		r.group.SetVisible(true)
	}
}
