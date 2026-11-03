package lightui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"
	"log"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/calendar"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

type calendarApp struct {
	c        *appui.Ctx
	data     calendar.Data
	shown    time.Time // the month shown, its first day
	selected time.Time // the day chosen
	today    time.Time

	month, dayTitle, daySub gtk.Label
	grid                    gtk.Grid
	startMon, startSun      gtk.ToggleButton
	filling                 bool

	onButton   func(gtk.Button)
	onWeek     func(gtk.ToggleButton)
	onMidnight glib.SourceFunc
}

func (a *calendarApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	win := &gtk.ApplicationWindow{}
	buttons := map[string]*gtk.Button{}
	ui := c.Load("calendar.ui")
	ui.Get("calendar", win)
	ui.Get("month", &a.month)
	ui.Get("grid", &a.grid)
	ui.Get("day_title", &a.dayTitle)
	ui.Get("day_sub", &a.daySub)
	ui.Get("start_mon", &a.startMon)
	ui.Get("start_sun", &a.startSun)
	for _, id := range []string{"today", "prev", "next"} {
		b := &gtk.Button{}
		ui.Get(id, b)
		buttons[id] = b
	}
	ui.Close()

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.onWeek = func(t gtk.ToggleButton) {
		if a.filling || !t.GetActive() {
			return
		}
		a.data.MondayFirst = a.startMon.GetActive()
		a.save()
		a.showMonth()
	}
	a.onMidnight = func(uintptr) bool {
		if now := dayOf(time.Now()); !now.Equal(a.today) {
			a.today = now
			a.showMonth()
		}
		return true
	}
	for _, b := range buttons {
		b.ConnectClicked(&a.onButton)
	}
	a.startMon.ConnectToggled(&a.onWeek)
	a.startSun.ConnectToggled(&a.onWeek)

	if err := store.Load(calendar.StoreName, &a.data); err != nil {
		log.Printf("calendar: %v", err)
	}
	a.filling = true
	if a.data.MondayFirst {
		a.startMon.SetActive(true)
	} else {
		a.startSun.SetActive(true)
	}
	a.filling = false
	a.today = dayOf(time.Now())
	a.selected = a.today
	a.shown = time.Date(a.today.Year(), a.today.Month(), 1, 0, 0, 0, 0, time.Local)
	a.showMonth()
	glib.TimeoutAdd(30000, &a.onMidnight, 0)
	menuWindow(c, win, []appui.MenuGroup{{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "calendar-today", Label: "Today", Activate: func(gio.SimpleAction, uintptr) {
		a.selected = dayOf(time.Now())
		a.shown = time.Date(a.selected.Year(), a.selected.Month(), 1, 0, 0, 0, 0, time.Local)
		a.showMonth()
	}}}}}}})
	return win
}

func (a *calendarApp) ThemeChanged() {}

func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func (a *calendarApp) save() {
	if err := store.Save(calendar.StoreName, a.data); err != nil {
		log.Printf("calendar: %v", err)
	}
}

func (a *calendarApp) button(name string) {
	switch {
	case name == "today":
		a.selected = a.today
		a.shown = time.Date(a.today.Year(), a.today.Month(), 1, 0, 0, 0, 0, time.Local)
	case name == "prev":
		a.shown = a.shown.AddDate(0, -1, 0)
	case name == "next":
		a.shown = a.shown.AddDate(0, 1, 0)
	case strings.HasPrefix(name, "day:"):
		t, err := time.ParseInLocation("2006-01-02", strings.TrimPrefix(name, "day:"), time.Local)
		if err != nil {
			return
		}
		a.selected = t
		if t.Month() != a.shown.Month() { // a day of the month before or after
			a.shown = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.Local)
		}
	}
	a.showMonth()
}

// showMonth draws the month's grid, then the chosen day.
func (a *calendarApp) showMonth() {
	a.month.SetText(i18n.Time(a.shown, "January 2006"))
	clearGrid(&a.grid)
	head := gtk.NewLabel(i18n.T("WK"))
	head.AddCssClass("grid-head")
	head.AddCssClass("week")
	a.grid.Attach(&head.Widget, 0, 0, 1, 1)
	head.Unref()
	names := []string{i18n.T("Su"), i18n.T("Mo"), i18n.T("Tu"), i18n.T("We"), i18n.T("Th"), i18n.T("Fr"), i18n.T("Sa")}
	if a.data.MondayFirst {
		names = append(names[1:], names[0])
	}
	for i, n := range names {
		l := gtk.NewLabel(n)
		l.AddCssClass("grid-head")
		l.SetHexpand(true)
		a.grid.Attach(&l.Widget, int32(i+1), 0, 1, 1)
		l.Unref()
	}
	for r, w := range calendar.Month(a.shown.Year(), a.shown.Month(), a.data.MondayFirst, a.today) {
		wk := gtk.NewLabel(strconv.Itoa(w.Number))
		wk.AddCssClass("week")
		a.grid.Attach(&wk.Widget, 0, int32(r+1), 1, 1)
		wk.Unref()
		for i, d := range w.Days {
			b := gtk.NewButton()
			b.AddCssClass("day")
			if !d.InMonth {
				b.AddCssClass("outside")
			}
			if d.Today {
				b.AddCssClass("today")
			}
			if d.Date.Equal(a.selected) {
				b.AddCssClass("chosen")
			}
			b.SetName("day:" + d.Date.Format("2006-01-02"))
			b.SetVexpand(true) // the day rows share the height; the head keeps its own
			box := gtk.NewBox(gtk.OrientationVerticalValue, 3)
			box.SetValign(gtk.AlignCenterValue)
			n := gtk.NewLabel(strconv.Itoa(d.Date.Day()))
			n.AddCssClass("day-number")
			gtkx.Append(box, &n.Widget)
			b.SetChild(&box.Widget)
			box.Unref()
			b.ConnectClicked(&a.onButton)
			a.grid.Attach(&b.Widget, int32(i+1), int32(r+1), 1, 1)
			b.Unref()
		}
	}
	a.showDay()
}

// showDay says where the chosen day is from today.
func (a *calendarApp) showDay() {
	title, sub := calendar.DayInfo(a.selected, a.today)
	a.dayTitle.SetText(title)
	a.daySub.SetText(sub)
}
