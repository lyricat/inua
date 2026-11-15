package lightui

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/apps/weather"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/uikit"
)

// refreshEvery is how often the forecast is read again.
const refreshEvery = 15 * time.Minute

type weatherApp struct {
	c     *appui.Ctx
	data  weather.Data
	http  *http.Client
	loads weather.Loads // the chosen city's forecast
	temps weather.Temps // each city's temperature now, for the index
	// The forecast on the page: a change that leaves the key as it is keeps
	// it there. Asked is when one was last asked for.
	shown         weather.Key
	showing       bool
	asked         time.Time
	cancelLoad    context.CancelFunc
	cancelTemps   context.CancelFunc
	tempsLoads    weather.Loads // the index's reading, by source and units
	messageDo     func()        // what the message's button does
	searchGen     int           // the search whose answer counts
	results       []weather.Place
	pages         gtk.Stack
	place, where  gtk.Label
	nowTemp       gtk.Label
	nowText       gtk.Label
	nowRange      gtk.Label
	updated       gtk.Label
	message       gtk.Label
	hoursTitle    gtk.Label
	daysTitle     gtk.Label
	placesEmpty   gtk.Label
	nowIcon       gtk.Image
	hours, days   gtk.Box
	hoursBlock    gtk.Box
	daysBlock     gtk.Box
	placeResults  gtk.Box
	placeList     gtk.Box
	details       gtk.Grid
	more          gtk.Button
	messageAction gtk.Button
	placeSearch   gtk.SearchEntry
	unitC, unitF  gtk.ToggleButton
	onButton      func(gtk.Button)
	onMore        func(gtk.Button)
	onMenuClosed  func(gtk.Popover)
	onSearch      func(gtk.SearchEntry)
	onSearchEnter func(gtk.SearchEntry)
	onUnit        func(gtk.ToggleButton)
	onTick        glib.SourceFunc
}

func (a *weatherApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.http = &http.Client{Timeout: 15 * time.Second}
	win := &gtk.ApplicationWindow{}
	ui := c.Load("weather.ui")
	ui.Get("weather", win)
	ui.Get("pages", &a.pages)
	ui.Get("place", &a.place)
	ui.Get("place_where", &a.where)
	ui.Get("more", &a.more)
	ui.Get("now_temp", &a.nowTemp)
	ui.Get("now_text", &a.nowText)
	ui.Get("now_range", &a.nowRange)
	ui.Get("now_icon", &a.nowIcon)
	ui.Get("updated", &a.updated)
	ui.Get("message", &a.message)
	ui.Get("message_action", &a.messageAction)
	ui.Get("hours_block", &a.hoursBlock)
	ui.Get("hours_title", &a.hoursTitle)
	ui.Get("hours", &a.hours)
	ui.Get("days_block", &a.daysBlock)
	ui.Get("days_title", &a.daysTitle)
	ui.Get("days", &a.days)
	ui.Get("details", &a.details)
	ui.Get("place_results", &a.placeResults)
	ui.Get("place_list", &a.placeList)
	ui.Get("places_empty", &a.placesEmpty)
	ui.Get("place_search", &a.placeSearch)
	ui.Get("unit_c", &a.unitC)
	ui.Get("unit_f", &a.unitF)
	ui.Close()

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.onMore = func(gtk.Button) { a.openMore() }
	a.onMenuClosed = func(pop gtk.Popover) {
		gtkx.Post(func() { pop.Unparent() })
	}
	a.onSearch = func(e gtk.SearchEntry) { a.search(e.GetText()) }
	a.onSearchEnter = func(gtk.SearchEntry) {
		if len(a.results) > 0 {
			a.button("place-add:0")
		}
	}
	a.onUnit = func(t gtk.ToggleButton) {
		if !t.GetActive() {
			return
		}
		if f := a.unitF.GetActive(); f != a.data.Fahrenheit {
			a.data.Fahrenheit = f
			a.save()
			a.update()
		}
	}
	a.onTick = func(uintptr) bool {
		_, ok := a.data.Current()
		_, known := weather.SourceByID(a.data.SourceID())
		if ok && known && time.Since(a.asked) > refreshEvery {
			a.refresh()
		}
		return true
	}
	a.messageAction.ConnectClicked(&a.onButton)
	a.more.ConnectClicked(&a.onMore)
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
		a.placeSearch.GrabFocus()
	}
	glib.TimeoutAdd(30000, &a.onTick, 0)
	menuWindow(c, win, []appui.MenuGroup{{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "weather-refresh", Label: "Refresh", Activate: func(gio.SimpleAction, uintptr) { a.refresh() }}, {Name: "weather-places", Label: "Add a city", Activate: func(gio.SimpleAction, uintptr) { a.placeSearch.GrabFocus() }}}}}}})
	return win
}

func (a *weatherApp) ThemeChanged() {}

func (a *weatherApp) save() {
	if err := store.Save(weather.StoreName, a.data); err != nil {
		log.Printf("weather: %v", err)
	}
}

func (a *weatherApp) button(name string) {
	switch {
	case name == "message":
		if a.messageDo != nil {
			a.messageDo()
		}
	case strings.HasPrefix(name, "place-add:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(name, "place-add:"))
		if i >= 0 && i < len(a.results) {
			a.data.Add(a.results[i])
			a.save()
			a.placeSearch.SetText("")
			a.update()
		}
	case strings.HasPrefix(name, "place-show:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(name, "place-show:"))
		if a.data.Select(i) {
			a.save()
			a.update()
		}
	case strings.HasPrefix(name, "place-remove:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(name, "place-remove:"))
		a.remove(i)
	}
}

func (a *weatherApp) remove(i int) {
	if a.data.Remove(i) {
		a.save()
		a.update()
	}
}

// setSource chooses where the forecast comes from, at once and for good.
func (a *weatherApp) setSource(id string) {
	if a.data.Source == id || a.data.Source == "" && id == weather.OpenMeteo {
		return
	}
	a.data.Source = id
	a.save()
	a.update()
}

// openMore opens More: the sources, the chosen one marked, then Refresh and
// Remove for the chosen city. It is made as it opens, so it shows what is
// chosen now.
func (a *weatherApp) openMore() {
	cur := a.data.SourceID()
	var items []uikit.Item
	for _, s := range weather.Sources {
		mark := "off"
		if s.ID == cur {
			mark = "radio"
		}
		items = append(items, uikit.Item{Label: i18n.Tf("Forecast from %s", s.Name), Mark: mark, Run: func() { a.setSource(s.ID) }})
	}
	items = append(items, uikit.Item{Separator: true})
	if p, ok := a.data.Current(); ok {
		items = append(items,
			uikit.Item{Label: i18n.T("Refresh"), Run: a.refresh},
			uikit.Item{Label: i18n.Tf("Remove %s", p.Name), Run: func() { a.remove(a.data.Selected) }})
	} else {
		items = append(items, uikit.Item{Label: i18n.T("Add a city"), Run: func() { a.placeSearch.GrabFocus() }})
	}
	pop := uikit.NewMenu(items, uikit.Below)
	pop.SetParent(&a.more.Widget)
	pop.ConnectClosed(&a.onMenuClosed)
	pop.Unref() // the button holds it
	pop.Popup()
}

// update shows what is chosen after a change: the index, and the chosen
// city's weather, read again unless it is on the page already.
func (a *weatherApp) update() {
	a.showPlaces()
	if p, ok := a.data.Current(); ok && a.showing && a.data.KeyOf(p) == a.shown {
		return
	}
	a.refresh()
}

// showPlaces shows the cities in the index, the chosen one marked, each
// with its temperature now from the chosen source in the chosen units.
func (a *weatherApp) showPlaces() {
	cur, ok := a.data.Current()
	a.place.SetText(i18n.T("Weather"))
	a.where.SetText("")
	if ok {
		a.place.SetText(cur.Name)
		a.where.SetText(cur.Where())
	}
	a.placesEmpty.SetVisible(len(a.data.Places) == 0)
	gtkx.Clear(&a.placeList)
	for i, p := range a.data.Places {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
		body := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		t := text(p.Name, p.Where())
		t.SetHexpand(true)
		gtkx.Append(body, &t.Widget)
		v := gtk.NewLabel("")
		v.AddCssClass("index-value")
		if temp, ok := a.temps.Get(a.data.KeyOf(p)); ok {
			v.SetText(weather.Temp(temp))
		}
		gtkx.Append(body, &v.Widget)
		b := indexItem("place-show:"+strconv.Itoa(i), &body.Widget, i == a.data.Selected, &a.onButton)
		body.Unref()
		b.SetHexpand(true)
		b.SetTooltipText(p.Name)
		gtkx.Append(row, &b.Widget)
		x := plainIcon("place-remove:"+strconv.Itoa(i), "window-close-symbolic", i18n.Tf("Remove %s", p.Name), &a.onButton)
		x.AddCssClass("tight")
		gtkx.Append(row, &x.Widget)
		gtkx.Append(&a.placeList, &row.Widget)
	}
}

// refresh reads the chosen city's forecast from the chosen source. Until it
// comes, a forecast of anything else (another city, source or units) gives
// way to a line saying what is being read; an answer for anything but what
// is chosen when it comes is dropped.
func (a *weatherApp) refresh() {
	if a.cancelLoad != nil {
		a.cancelLoad()
		a.cancelLoad = nil
	}
	a.stopTemps() // begun again with the forecast
	p, ok := a.data.Current()
	src, known := weather.SourceByID(a.data.SourceID())
	switch {
	case !ok:
		a.loads.Stop()
		a.showMessage(i18n.T("Add a city to see its weather."), i18n.T("Add a city"), func() { a.placeSearch.GrabFocus() })
		return
	case !known:
		// Not read from another source in its place: the user chooses.
		a.loads.Stop()
		a.showMessage(i18n.Tf("Weather has no source called “%s”.\nChoose one in More, or use Open-Meteo.", a.data.Source),
			i18n.T("Use Open-Meteo"), func() { a.setSource(weather.OpenMeteo) })
		return
	}
	key := a.data.KeyOf(p)
	ticket := a.loads.Begin(key)
	a.asked = time.Now()
	if !a.showing || a.shown != key {
		a.showMessage(i18n.Tf("Reading the weather in %s from %s…", p.Name, src.Name), "", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	a.cancelLoad = cancel
	go func() {
		f, err := src.Get(ctx, a.http, p, key.Fahrenheit)
		cancel()
		gtkx.Post(func() {
			if !a.loads.Current(ticket) {
				return // something else is chosen now
			}
			if err != nil {
				log.Printf("weather: %s: %v", src.Name, err)
				msg := i18n.Tf("%s could not be reached. Check the connection.\n%s", src.Name, shortErr(err))
				if weather.Unavailable(err) {
					msg = i18n.Tf("%s did not give a forecast just now.\n%s\nTry again, or choose another source in More.", src.Name, shortErr(err))
				}
				a.showMessage(msg, i18n.T("Try again"), a.refresh)
				return
			}
			a.temps.Put(key, f.Now.Temp, time.Now())
			a.shown, a.showing = key, true
			a.showForecast(f, src)
			a.showPlaces()
			a.readTemps(src)
		})
	}()
}

// stopTemps drops the index's reading on its way.
func (a *weatherApp) stopTemps() {
	if a.cancelTemps != nil {
		a.cancelTemps()
		a.cancelTemps = nil
	}
	a.tempsLoads.Stop()
}

// readTemps reads the temperature now at the cities the index has none
// for, or an old one, from src, the chosen source.
func (a *weatherApp) readTemps(src weather.Source) {
	ticket := a.tempsLoads.Begin(weather.Key{Source: src.ID, Fahrenheit: a.data.Fahrenheit})
	now := time.Now()
	var todo []weather.Place
	for _, p := range a.data.Places {
		if !a.temps.Fresh(a.data.KeyOf(p), now, refreshEvery) {
			todo = append(todo, p)
		}
	}
	if len(todo) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelTemps = cancel
	go func() {
		for _, p := range todo {
			if ctx.Err() != nil {
				return
			}
			c, done := context.WithTimeout(ctx, 15*time.Second)
			f, err := src.Get(c, a.http, p, ticket.Key.Fahrenheit)
			done()
			if err != nil {
				continue
			}
			gtkx.Post(func() {
				if !a.tempsLoads.Current(ticket) {
					return // another source or units now
				}
				k := ticket.Key
				k.Lat, k.Lon = p.Lat, p.Lon
				a.temps.Put(k, f.Now.Temp, time.Now())
				a.showPlaces()
			})
		}
	}()
}

// showMessage puts a line in the forecast's place, and a button doing do
// under it when action is not "".
func (a *weatherApp) showMessage(text, action string, do func()) {
	a.showing = false
	a.message.SetText(text)
	a.messageAction.SetLabel(action)
	a.messageAction.SetVisible(action != "")
	a.messageDo = do
	a.pages.SetVisibleChildName("message")
}

func (a *weatherApp) showForecast(f weather.Forecast, src weather.Source) {
	desc, icon := weather.Describe(f.Now.Code, f.Now.Day)
	a.nowTemp.SetText(weather.Temp(f.Now.Temp))
	a.nowText.SetText(desc)
	a.nowIcon.SetFromIconName(icon)
	a.nowRange.SetText("")
	if len(f.Days) > 0 {
		a.nowRange.SetText(i18n.Tf("H %s   L %s", weather.Temp(f.Days[0].Max), weather.Temp(f.Days[0].Min)))
	}

	// What the source has, said as it is: wttr.in's hours are in steps of
	// three and its days three.
	span, step := f.HourSpan()
	a.hoursBlock.SetVisible(span > 0)
	if step > 1 {
		a.hoursTitle.SetText(i18n.Tf("NEXT %d HOURS · EVERY %d", span, step))
	} else {
		a.hoursTitle.SetText(i18n.Nf("NEXT %d HOUR", "NEXT %d HOURS", span, span))
	}
	a.daysBlock.SetVisible(len(f.Days) > 0)
	a.daysTitle.SetText(i18n.Nf("%d DAY", "%d DAYS", len(f.Days), len(f.Days)))

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
			[2]string{i18n.T("SUNRISE"), clockOrDash(f.Days[0].Sunrise)},
			[2]string{i18n.T("SUNSET"), clockOrDash(f.Days[0].Sunset)})
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
	a.updated.SetText(i18n.Tf("UPDATED %s · %s", time.Now().Format("15:04"), strings.ToUpper(src.Name)))
	a.pages.SetVisibleChildName("forecast")
}

// clockOrDash is a time of day, or a dash for none (no sunrise in a polar
// night).
func clockOrDash(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("15:04")
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
	a.placesEmpty.SetVisible(q == "" && len(a.data.Places) == 0)
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
				l.AddCssClass("index-note")
				l.SetWrap(true)
				l.SetXalign(0)
				gtkx.Append(&a.placeResults, &l.Widget)
				return
			}
			for i, p := range places {
				t := text(p.Name, p.Where())
				gtkx.Append(&a.placeResults, &indexItem("place-add:"+strconv.Itoa(i), &t.Widget, false, &a.onButton).Widget)
				t.Unref()
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
