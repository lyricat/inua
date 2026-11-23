package filesui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/i18n"
)

type entryGroup struct {
	id    string
	label string
	rank  int
}

type groupedEntry struct {
	entry files.Entry
	group entryGroup
}

// sortGroupedEntries keeps the chosen order inside each section, then puts
// the sections in their natural order. Group IDs and labels follow the
// sorted entries so the recycled rows can mark each section's first item.
func sortGroupedEntries(es []files.Entry, order files.Order, now time.Time) (ids, labels []string) {
	files.Sort(es, order)
	if order.GroupBy == files.GroupNone {
		return nil, nil
	}
	grouped := make([]groupedEntry, len(es))
	byType := map[string]entryGroup{}
	for i := range es {
		grouped[i] = groupedEntry{entry: es[i], group: entryGroupFor(&es[i], order.GroupBy, now, byType)}
	}
	slices.SortStableFunc(grouped, func(a, b groupedEntry) int {
		if a.entry.Dir != b.entry.Dir {
			if a.entry.Dir != order.FilesFirst {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(a.group.rank, b.group.rank); c != 0 {
			return c
		}
		return strings.Compare(a.group.id, b.group.id)
	})
	ids, labels = make([]string, len(grouped)), make([]string, len(grouped))
	for i := range grouped {
		es[i] = grouped[i].entry
		ids[i], labels[i] = grouped[i].group.id, grouped[i].group.label
	}
	return ids, labels
}

func (p *pane) groupHeading(pos uint32, index int) string {
	if p.mode != inFolder || p.fv.Order.GroupBy == files.GroupNone || index >= len(p.groupIDs) || index >= len(p.groupNames) {
		return ""
	}
	id := p.groupIDs[index]
	if pos > 0 {
		previous := p.shown[pos-1]
		if previous < len(p.groupIDs) && p.groupIDs[previous] == id {
			return ""
		}
	}
	if count := p.groupCount[id]; count > 0 {
		return fmt.Sprintf("%s · %d", p.groupNames[index], count)
	}
	return p.groupNames[index]
}

func entryGroupFor(e *files.Entry, by files.GroupBy, now time.Time, typeCache map[string]entryGroup) entryGroup {
	if e.Dir {
		return entryGroup{id: "folder", label: i18n.T("Folders"), rank: 0}
	}
	switch by {
	case files.GroupType:
		key := e.Ext()
		if key == "" && e.Mode&0o111 != 0 {
			return entryGroup{id: "type:program", label: i18n.T("Program"), rank: 1}
		}
		cacheKey := key
		if cacheKey == "" {
			cacheKey = "\x00" + strings.ToLower(e.Name)
		}
		if g, ok := typeCache[cacheKey]; ok {
			return g
		}
		var uncertain bool
		mime := gio.ContentTypeGuess(e.Name, nil, 0, &uncertain)
		label := gio.ContentTypeGetDescription(mime)
		if label == "" || label == "Unknown" {
			label = i18n.T("Unknown file type")
		}
		g := entryGroup{id: "type:" + strings.ToLower(label), label: label, rank: 1}
		typeCache[cacheKey] = g
		return g
	case files.GroupModified:
		rank, label := modifiedGroup(e.ModTime, now)
		return entryGroup{id: cmp.Or(strings.ToLower(label), "unknown"), label: label, rank: rank}
	case files.GroupSize:
		rank, label := sizeGroup(e.Size)
		return entryGroup{id: label, label: label, rank: rank}
	}
	return entryGroup{}
}

func modifiedGroup(mod, now time.Time) (int, string) {
	if mod.IsZero() {
		return 10, i18n.T("Unknown date")
	}
	loc := now.Location()
	date := func(t time.Time) time.Time {
		t = t.In(loc)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
	today, day := date(now), date(mod)
	if day.After(today) {
		return 1, i18n.T("Later")
	}
	if day.Equal(today) {
		return 2, i18n.T("Today")
	}
	if day.Equal(today.AddDate(0, 0, -1)) {
		return 3, i18n.T("Yesterday")
	}
	weekStart := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	if !day.Before(weekStart) {
		return 4, i18n.T("Earlier this week")
	}
	lastWeek := weekStart.AddDate(0, 0, -7)
	if !day.Before(lastWeek) {
		return 5, i18n.T("Last week")
	}
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
	if !day.Before(monthStart) {
		return 6, i18n.T("Earlier this month")
	}
	lastMonth := monthStart.AddDate(0, -1, 0)
	if !day.Before(lastMonth) {
		return 7, i18n.T("Last month")
	}
	yearStart := time.Date(today.Year(), 1, 1, 0, 0, 0, 0, loc)
	if !day.Before(yearStart) {
		return 8, i18n.T("Earlier this year")
	}
	return 9, i18n.T("Older")
}

func sizeGroup(size int64) (int, string) {
	switch {
	case size == 0:
		return 1, i18n.T("Empty")
	case size < 1<<20:
		return 2, i18n.T("Under 1 MB")
	case size < 100<<20:
		return 3, i18n.T("1–100 MB")
	case size < 1<<30:
		return 4, i18n.T("100 MB–1 GB")
	default:
		return 5, i18n.T("1 GB or more")
	}
}
