package lightui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"
	"context"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/apps/weather"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// refreshEvery is how often the forecast is read again.
const refreshEvery = 15 * time.Minute

type weatherApp struct {
	c    *appui.Ctx
	data weather.Data
	http *http.Client
	temp map[int]float64 // each place's temperature now, for the list

	pages                      gtk.Stack
	place, nowTemp, nowText    gtk.Label
	nowRange, updated, message gtk.Label
	nowIcon                    gtk.Image
	dots, hours, days          gtk.Box
	placeResults, placeList    gtk.Box
	details                    gtk.Grid
	prev, next, messageAction  gtk.Button
	placesToggle               gtk.ToggleButton
	placesReveal               gtk.Revealer
	placeSearch                gtk.SearchEntry
	unitC, unitF               gtk.ToggleButton
	fetched                    time.Time
	loading, searchGen         int // the requests whose answers count
	results                    []weather.Place
	onButton                   func(gtk.Button)
	onSearch                   func(gtk.SearchEntry)
	onSearchEnter              func(gtk.SearchEntry)
	onUnit, onPlaces           func(gtk.ToggleButton)
	onTick                     glib.SourceFunc
}

func (a *weatherApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.http = &http.Client{Timeout: 15 * time.Second}
	a.temp = map[int]float64{}
	win := &gtk.ApplicationWindow{}
	ui := c.Load("weather.ui")
	ui.Get("weather", win)
	ui.Get("pages", &a.pages)
	ui.Get("place", &a.place)
	ui.Get("dots", &a.dots)
	ui.Get("prev", &a.prev)
	ui.Get("next", &a.next)
	ui.Get("places_toggle", &a.placesToggle)
	ui.Get("places_reveal", &a.placesReveal)
	ui.Get("now_temp", &a.nowTemp)
	ui.Get("now_text", &a.nowText)
	ui.Get("now_range", &a.nowRange)
	ui.Get("now_icon", &a.nowIcon)
	ui.Get("updated", &a.updated)
	ui.Get("message", &a.message)
	ui.Get("message_action", &a.messageAction)
	ui.Get("hours", &a.hours)
	ui.Get("days", &a.days)
	ui.Get("details", &a.details)
	ui.Get("place_results", &a.placeResults)
	ui.Get("place_list", &a.placeList)
	ui.Get("place_search", &a.placeSearch)
	ui.Get("unit_c", &a.unitC)
	ui.Get("unit_f", &a.unitF)
	ui.Close()

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.onSearch = func(e gtk.SearchEntry) { a.search(e.GetText()) }
	a.onSearchEnter = func(gtk.SearchEntry) {
		if len(a.results) > 0 {
			a.button("place-add:0")
		}
	}
	a.onPlaces = func(t gtk.ToggleButton) {
		a.placesReveal.SetRevealChild(t.GetActive())
		if t.GetActive() {
			a.placeSearch.GrabFocus()
		}
	}
	a.onUnit = func(t gtk.ToggleButton) {
		if !t.GetActive() {
			return
		}
		if f := a.unitF.GetActive(); f != a.data.Fahrenheit {
			a.data.Fahrenheit = f
			a.save()
			a.temp = map[int]float64{}
			a.refresh()
		}
	}
	a.onTick = func(uintptr) bool {
		if len(a.data.Places) > 0 && time.Since(a.fetched) > refreshEvery {
			a.refresh()
		}
		return true
	}
	for _, b := range []*gtk.Button{&a.prev, &a.next, &a.messageAction} {
		b.ConnectClicked(&a.onButton)
	}
	a.placesToggle.ConnectToggled(&a.onPlaces)
	a.placeSearch.ConnectSearchChanged(&a.onSearch)
	a.placeSearch.ConnectActivate(&a.onSearchEnter)
	a.unitC.ConnectToggled(&a.onUnit)
	a.unitF.ConnectToggled(&a.onUnit)

	if err := store.Load(weather.StoreName, &a.data); err != nil {
		log.Printf("weather: %v", err)
	}
	a.unitF.SetActive(a.data.Fahrenheit)
	a.showPlaces()
	a.refresh()
	if len(a.data.Places) == 0 {
		a.placesToggle.SetActive(true)
	}
	glib.TimeoutAdd(30000, &a.onTick, 0)
	menuWindow(c, win, []appui.MenuGroup{{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "weather-refresh", Label: "Refresh", Activate: func(gio.SimpleAction, uintptr) { a.refresh() }}, {Name: "weather-places", Label: "Places", Activate: func(gio.SimpleAction, uintptr) { a.placesToggle.SetActive(true); a.placeSearch.GrabFocus() }}}}}}})
	return win
}

func (a *weatherApp) ThemeChanged() {}

func (a *weatherApp) save() {
	if err := store.Save(weather.StoreName, a.data); err != nil {
		log.Printf("weather: %v", err)
	}
}

func (a *weatherApp) selected() (weather.Place, bool) {
	if len(a.data.Places) == 0 {
		return weather.Place{}, false
	}
	a.data.Selected = max(0, min(a.data.Selected, len(a.data.Places)-1))
	return a.data.Places[a.data.Selected], true
}

func (a *weatherApp) button(name string) {
	switch {
	case name == "prev" || name == "next":
		if n := len(a.data.Places); n > 1 {
			step := 1
			if name == "prev" {
				step = n - 1
			}
			a.data.Selected = (a.data.Selected + step) % n
			a.save()
			a.showPlaces()
			a.refresh()
		}
	case name == "message":
		if len(a.data.Places) == 0 {
			a.placesToggle.SetActive(true)
		} else {
			a.refresh()
		}
	case strings.HasPrefix(name, "place-add:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(name, "place-add:"))
		if i >= 0 && i < len(a.results) {
			a.data.Places = append(a.data.Places, a.results[i])
			a.data.Selected = len(a.data.Places) - 1
			a.save()
			a.placeSearch.SetText("")
			a.placesToggle.SetActive(false)
			a.showPlaces()
			a.refresh()
		}
	case strings.HasPrefix(name, "place-show:"):
		a.data.Selected, _ = strconv.Atoi(strings.TrimPrefix(name, "place-show:"))
		a.save()
		a.placesToggle.SetActive(false)
		a.showPlaces()
		a.refresh()
	case strings.HasPrefix(name, "place-remove:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(name, "place-remove:"))
		if i < 0 || i >= len(a.data.Places) {
			return
		}
		a.data.Places = slices.Delete(a.data.Places, i, i+1)
		if a.data.Selected >= i {
			a.data.Selected = max(0, a.data.Selected-1)
		}
		a.temp = map[int]float64{}
		a.save()
		a.showPlaces()
		a.refresh()
	}
}

// showPlaces shows the header (the place, a square per place) and the list
// in the places panel.
func (a *weatherApp) showPlaces() {
	n := len(a.data.Places)
	a.prev.SetVisible(n > 1)
	a.next.SetVisible(n > 1)
	gtkx.Clear(&a.dots)
	a.dots.SetVisible(n > 1)
	for i := range n {
		d := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		d.AddCssClass("dot")
		if i == a.data.Selected {
			d.AddCssClass("on")
		}
		gtkx.Append(&a.dots, &d.Widget)
	}
	if p, ok := a.selected(); ok {
		a.place.SetText(p.Name)
	} else {
		a.place.SetText(i18n.T("Weather"))
	}

	gtkx.Clear(&a.placeList)
	for i, p := range a.data.Places {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("row")
		if i == a.data.Selected {
			row.AddCssClass("current")
		}
		body := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		t := text(p.Name, p.Where())
		t.SetHexpand(true)
		gtkx.Append(body, &t.Widget)
		if v, ok := a.temp[i]; ok {
			l := gtk.NewLabel(weather.Temp(v))
			l.AddCssClass("row-temp")
			gtkx.Append(body, &l.Widget)
		}
		b := rowButton("place-show:"+strconv.Itoa(i), &body.Widget, &a.onButton)
		body.Unref()
		b.SetHexpand(true)
		gtkx.Append(row, &b.Widget)
		gtkx.Append(row, &plainIcon("place-remove:"+strconv.Itoa(i), "window-close-symbolic", i18n.Tf("Remove %s", p.Name), &a.onButton).Widget)
		gtkx.Append(&a.placeList, &row.Widget)
	}
}

// refresh reads the chosen place's forecast.
func (a *weatherApp) refresh() {
	p, ok := a.selected()
	if !ok {
		a.showMessage(i18n.T("Add a city to see its weather."), i18n.T("Add a city"))
		return
	}
	a.loading++
	gen, fahrenheit, idx := a.loading, a.data.Fahrenheit, a.data.Selected
	if a.pages.GetVisibleChildName() != "forecast" {
		a.showMessage(i18n.T("Reading the weather…"), "")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		f, err := weather.Get(ctx, a.http, p, fahrenheit)
		cancel()
		gtkx.Post(func() {
			if gen != a.loading {
				return // a newer request is on its way
			}
			if err != nil {
				log.Printf("weather: %v", err)
				a.showMessage(i18n.Tf("The weather could not be read.\n%s", shortErr(err)), i18n.T("Try again"))
				return
			}
			a.fetched = time.Now()
			a.temp[idx] = f.Now.Temp
			a.showPlaces()
			a.showForecast(f)
			a.readTemps()
		})
	}()
}

// readTemps reads the temperature now at the other places, for the list.
func (a *weatherApp) readTemps() {
	places, fahrenheit, gen, shown := slices.Clone(a.data.Places), a.data.Fahrenheit, a.loading, a.data.Selected
	go func() {
		for i, p := range places {
			if i == shown {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			f, err := weather.Get(ctx, a.http, p, fahrenheit)
			cancel()
			if err != nil {
				continue
			}
			gtkx.Post(func() {
				if gen == a.loading { // the list and units are as they were
					a.temp[i] = f.Now.Temp
					a.showPlaces()
				}
			})
		}
	}()
}

func (a *weatherApp) showMessage(text, action string) {
	a.message.SetText(text)
	a.messageAction.SetLabel(action)
	a.messageAction.SetVisible(action != "")
	a.pages.SetVisibleChildName("message")
}

func (a *weatherApp) showForecast(f weather.Forecast) {
	desc, icon := weather.Describe(f.Now.Code, f.Now.Day)
	a.nowTemp.SetText(weather.Temp(f.Now.Temp))
	a.nowText.SetText(desc)
	a.nowIcon.SetFromIconName(icon)
	if len(f.Days) > 0 {
		a.nowRange.SetText(i18n.Tf("H %s   L %s", weather.Temp(f.Days[0].Max), weather.Temp(f.Days[0].Min)))
	}

	gtkx.Clear(&a.hours)
	for i, h := range f.Hours {
		col := gtk.NewBox(gtk.OrientationVerticalValue, 8)
		col.AddCssClass("hour")
		label := h.Time.Format("15")
		if i == 0 {
			label = i18n.T("NOW")
			col.AddCssClass("now")
		}
		t := gtk.NewLabel(label)
		t.AddCssClass("hour-time")
		gtkx.Append(col, &t.Widget)
		_, ic := weather.Describe(h.Code, h.Day)
		img := gtk.NewImageFromIconName(ic)
		img.SetPixelSize(18)
		gtkx.Append(col, &img.Widget)
		v := gtk.NewLabel(weather.Temp(h.Temp))
		v.AddCssClass("hour-temp")
		gtkx.Append(col, &v.Widget)
		gtkx.Append(&a.hours, &col.Widget)
	}

	gtkx.Clear(&a.days)
	lo, hi := 1e9, -1e9
	for _, d := range f.Days {
		lo, hi = min(lo, d.Min), max(hi, d.Max)
	}
	const barW = 110
	for i, d := range f.Days {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("row")
		row.AddCssClass("day")
		name := i18n.Time(d.Date, "Mon")
		if i == 0 {
			name = i18n.T("Today")
		}
		n := gtk.NewLabel(name)
		n.AddCssClass("title")
		n.SetXalign(0)
		n.SetSizeRequest(58, -1)
		gtkx.Append(row, &n.Widget)
		_, ic := weather.Describe(d.Code, true)
		img := gtk.NewImageFromIconName(ic)
		img.SetPixelSize(18)
		gtkx.Append(row, &img.Widget)
		rain := gtk.NewLabel("")
		rain.AddCssClass("rain")
		if d.Rain >= 20 {
			rain.SetText(fmt.Sprintf("%d%%", d.Rain))
		}
		rain.SetHexpand(true)
		rain.SetXalign(0)
		gtkx.Append(row, &rain.Widget)
		mn := gtk.NewLabel(weather.Temp(d.Min))
		mn.AddCssClass("low")
		mn.SetSizeRequest(34, -1)
		mn.SetXalign(1)
		gtkx.Append(row, &mn.Widget)
		gtkx.Append(row, &rangeBar(d.Min, d.Max, lo, hi, barW).Widget)
		mx := gtk.NewLabel(weather.Temp(d.Max))
		mx.AddCssClass("high")
		mx.SetSizeRequest(34, -1)
		mx.SetXalign(1)
		gtkx.Append(row, &mx.Widget)
		gtkx.Append(&a.days, &row.Widget)
	}

	clearGrid(&a.details)
	cells := [][2]string{
		{i18n.T("FEELS LIKE"), weather.Temp(f.Now.Feels)},
		{i18n.T("HUMIDITY"), fmt.Sprintf("%d%%", f.Now.Humidity)},
		{i18n.T("WIND"), fmt.Sprintf("%.0f %s", f.Now.Wind, f.Wind)},
		{i18n.T("RAIN"), "—"},
	}
	if len(f.Days) > 0 {
		cells[3][1] = fmt.Sprintf("%d%%", f.Days[0].Rain)
		cells = append(cells,
			[2]string{i18n.T("SUNRISE"), f.Days[0].Sunrise.Format("15:04")},
			[2]string{i18n.T("SUNSET"), f.Days[0].Sunset.Format("15:04")})
	}
	for i, c := range cells {
		box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
		box.AddCssClass("detail")
		if i%2 == 1 {
			box.AddCssClass("right")
		}
		if i >= 2 {
			box.AddCssClass("lower")
		}
		k := gtk.NewLabel(c[0])
		k.AddCssClass("kicker")
		k.SetXalign(0)
		gtkx.Append(box, &k.Widget)
		v := gtk.NewLabel(c[1])
		v.AddCssClass("detail-value")
		v.SetXalign(0)
		gtkx.Append(box, &v.Widget)
		a.details.Attach(&box.Widget, int32(i%2), int32(i/2), 1, 1)
		box.Unref()
	}
	a.updated.SetText(i18n.Tf("UPDATED %s · OPEN-METEO", time.Now().Format("15:04")))
	a.pages.SetVisibleChildName("forecast")
}

// rangeBar draws a day's span of temperatures within the week's.
func rangeBar(dmin, dmax, lo, hi float64, w int) *gtk.Box {
	track := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	track.AddCssClass("range")
	track.SetSizeRequest(int32(w), -1)
	track.SetValign(gtk.AlignCenterValue)
	span := max(hi-lo, 1)
	start := int(float64(w) * (dmin - lo) / span)
	width := max(6, int(float64(w)*(dmax-dmin)/span))
	fill := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	fill.AddCssClass("range-fill")
	fill.SetMarginStart(int32(min(start, w-width)))
	fill.SetSizeRequest(int32(width), -1)
	fill.SetHalign(gtk.AlignStartValue)
	gtkx.Append(track, &fill.Widget)
	return track
}

// search finds places as the name is typed.
func (a *weatherApp) search(q string) {
	q = strings.TrimSpace(q)
	a.placeResults.SetVisible(q != "")
	a.placeList.SetVisible(q == "")
	a.searchGen++
	gen := a.searchGen
	a.results = nil
	if len([]rune(q)) < 2 {
		gtkx.Clear(&a.placeResults)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		places, err := weather.Search(ctx, a.http, q)
		cancel()
		gtkx.Post(func() {
			if gen != a.searchGen {
				return
			}
			gtkx.Clear(&a.placeResults)
			a.results = places
			if err != nil || len(places) == 0 {
				msg := i18n.T("No place by that name")
				if err != nil {
					msg = i18n.Tf("Could not search: %s", shortErr(err))
				}
				l := gtk.NewLabel(msg)
				l.AddCssClass("empty")
				l.SetWrap(true)
				gtkx.Append(&a.placeResults, &l.Widget)
				return
			}
			for i, p := range places {
				gtkx.Append(&a.placeResults, &rowButton("place-add:"+strconv.Itoa(i), &text(p.Name, p.Where()).Widget, &a.onButton).Widget)
			}
		})
	}()
}

// shortErr is an error's last part, which says what went wrong.
func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i < len(s)-2 {
		return s[i+2:]
	}
	return s
}
