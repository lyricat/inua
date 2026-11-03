package lightui

import (
	"bytes"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"context"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/stocks"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// quotesEvery is how often prices are read again.
const quotesEvery = time.Minute

type stocksApp struct {
	c                         *appui.Ctx
	data                      stocks.Data
	http                      *http.Client
	quotes                    map[string]stocks.Quote // 1D, for the list
	shown                     stocks.Quote            // the selected symbol over the range
	fetchedList, fetchedShown time.Time

	search                   gtk.SearchEntry
	results, list, ranges    gtk.Box
	detail                   gtk.Stack
	name, symbol, price      gtk.Label
	change, updated, message gtk.Label
	chart                    gtk.Picture
	stats                    gtk.Grid
	rangeButtons             map[string]*gtk.ToggleButton
	matches                  []stocks.Match
	searchGen, shownGen      int
	filling                  bool
	onButton                 func(gtk.Button)
	onRange                  func(gtk.ToggleButton)
	onSearch                 func(gtk.SearchEntry)
	onTick                   glib.SourceFunc
}

func (a *stocksApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.http = &http.Client{Timeout: 15 * time.Second}
	a.quotes = map[string]stocks.Quote{}
	win := &gtk.ApplicationWindow{}
	var remove gtk.Button
	ui := c.Load("stocks.ui")
	ui.Get("stocks", win)
	ui.Get("symbol_search", &a.search)
	ui.Get("results", &a.results)
	ui.Get("list", &a.list)
	ui.Get("ranges", &a.ranges)
	ui.Get("detail", &a.detail)
	ui.Get("name", &a.name)
	ui.Get("symbol", &a.symbol)
	ui.Get("price", &a.price)
	ui.Get("change", &a.change)
	ui.Get("updated", &a.updated)
	ui.Get("message", &a.message)
	ui.Get("chart", &a.chart)
	ui.Get("stats", &a.stats)
	ui.Get("remove", &remove)
	var action gtk.Button
	ui.Get("message_action", &action)
	ui.Close()
	action.SetVisible(false)

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.onRange = func(t gtk.ToggleButton) {
		if !a.filling && t.GetActive() {
			a.data.Range = t.GetName()
			a.save()
			a.loadShown()
		}
	}
	a.onSearch = func(e gtk.SearchEntry) { a.find(e.GetText()) }
	a.onTick = func(uintptr) bool {
		if time.Since(a.fetchedList) > quotesEvery {
			a.loadList()
		}
		if time.Since(a.fetchedShown) > quotesEvery {
			a.loadShown()
		}
		return true
	}
	remove.ConnectClicked(&a.onButton)
	a.search.ConnectSearchChanged(&a.onSearch)

	if err := store.Load(stocks.StoreName, &a.data); err != nil {
		log.Printf("stocks: %v", err)
	}
	if a.data.Symbols == nil {
		a.data.Symbols = slices.Clone(stocks.DefaultSymbols)
	}
	if a.data.Range == "" {
		a.data.Range = "1d"
	}
	if a.data.Selected == "" && len(a.data.Symbols) > 0 {
		a.data.Selected = a.data.Symbols[0]
	}
	a.rangeButtons = map[string]*gtk.ToggleButton{}
	var group *gtk.ToggleButton
	for _, r := range stocks.Ranges {
		t := gtk.NewToggleButtonWithLabel(r.Label)
		t.SetName(r.Name)
		if group != nil {
			t.SetGroup(group)
		} else {
			group = t
		}
		t.ConnectToggled(&a.onRange)
		a.rangeButtons[r.Name] = t
		gtkx.Append(&a.ranges, &t.Widget)
	}
	a.showList()
	a.loadList()
	a.loadShown()
	glib.TimeoutAdd(5000, &a.onTick, 0)
	menuWindow(c, win, []appui.MenuGroup{{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "stocks-refresh", Label: "Refresh", Activate: func(gio.SimpleAction, uintptr) { a.loadList(); a.loadShown() }}, {Name: "stocks-search", Label: "Find a stock", Activate: func(gio.SimpleAction, uintptr) { a.search.GrabFocus() }}}}}}})
	return win
}

// themeChanged draws the chart again in the new colours.
func (a *stocksApp) ThemeChanged() { a.drawChart() }

func (a *stocksApp) save() {
	if err := store.Save(stocks.StoreName, a.data); err != nil {
		log.Printf("stocks: %v", err)
	}
}

func (a *stocksApp) button(name string) {
	switch {
	case name == "remove":
		i := slices.Index(a.data.Symbols, a.data.Selected)
		if i < 0 {
			return
		}
		a.data.Symbols = slices.Delete(a.data.Symbols, i, i+1)
		a.data.Selected = ""
		if len(a.data.Symbols) > 0 {
			a.data.Selected = a.data.Symbols[min(i, len(a.data.Symbols)-1)]
		}
		a.save()
		a.showList()
		a.loadShown()
	case strings.HasPrefix(name, "show:"):
		a.data.Selected = strings.TrimPrefix(name, "show:")
		a.save()
		a.showList()
		a.loadShown()
	case strings.HasPrefix(name, "add:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(name, "add:"))
		if i < 0 || i >= len(a.matches) {
			return
		}
		sym := a.matches[i].Symbol
		if !slices.Contains(a.data.Symbols, sym) {
			a.data.Symbols = append(a.data.Symbols, sym)
		}
		a.data.Selected = sym
		a.save()
		a.search.SetText("")
		a.showList()
		a.loadList()
		a.loadShown()
	}
}

// loadList reads the day's quote of every symbol on the list.
func (a *stocksApp) loadList() {
	a.fetchedList = time.Now()
	syms := slices.Clone(a.data.Symbols)
	go func() {
		var mu sync.Mutex
		got := map[string]stocks.Quote{}
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for _, s := range syms {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				q, err := stocks.Get(ctx, a.http, s, "1d")
				cancel()
				if err != nil {
					log.Printf("stocks: %v", err)
					return
				}
				mu.Lock()
				got[s] = q
				mu.Unlock()
			}()
		}
		wg.Wait()
		gtkx.Post(func() {
			for s, q := range got {
				a.quotes[s] = q
			}
			a.showList()
		})
	}()
}

func (a *stocksApp) showList() {
	gtkx.Clear(&a.list)
	for _, sym := range a.data.Symbols {
		q, ok := a.quotes[sym]
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		name := sym
		if ok {
			name = q.Name
		}
		t := text(displaySymbol(sym), name)
		t.SetHexpand(true)
		gtkx.Append(row, &t.Widget)
		right := gtk.NewBox(gtk.OrientationVerticalValue, 3)
		right.SetValign(gtk.AlignCenterValue)
		p := gtk.NewLabel("—")
		p.AddCssClass("index-value")
		p.SetXalign(1)
		chip := gtk.NewLabel("")
		chip.AddCssClass("chip")
		chip.SetHalign(gtk.AlignEndValue)
		if ok {
			p.SetText(stocks.Price(q.Price))
			_, pct := q.Change()
			chip.SetText(stocks.Signed(pct, true))
			chip.AddCssClass(trend(pct))
		}
		gtkx.Append(right, &p.Widget)
		gtkx.Append(right, &chip.Widget)
		gtkx.Append(row, &right.Widget)
		b := indexItem("show:"+sym, &row.Widget, sym == a.data.Selected, &a.onButton)
		row.Unref()
		gtkx.Append(&a.list, &b.Widget)
	}
}

// displaySymbol drops the caret of index symbols: "^GSPC" → "GSPC".
func displaySymbol(s string) string { return strings.TrimPrefix(s, "^") }

func trend(v float64) string {
	if v < 0 {
		return "down"
	}
	return "up"
}

// loadShown reads the selected symbol over the chosen range.
func (a *stocksApp) loadShown() {
	a.fetchedShown = time.Now()
	sym, rng := a.data.Selected, a.data.Range
	a.filling = true
	if t := a.rangeButtons[rng]; t != nil {
		t.SetActive(true)
	}
	a.filling = false
	if sym == "" {
		a.message.SetText(i18n.T("Search for a symbol to add it."))
		a.detail.SetVisibleChildName("message")
		return
	}
	a.shownGen++
	gen := a.shownGen
	if a.shown.Symbol != sym {
		a.message.SetText(i18n.Tf("Loading %s…", displaySymbol(sym)))
		a.detail.SetVisibleChildName("message")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		q, err := stocks.Get(ctx, a.http, sym, rng)
		cancel()
		gtkx.Post(func() {
			if gen != a.shownGen {
				return
			}
			if err != nil {
				log.Printf("stocks: %v", err)
				a.message.SetText(i18n.Tf("%s could not be read.\n%s", displaySymbol(sym), shortErr(err)))
				a.detail.SetVisibleChildName("message")
				return
			}
			a.shown = q
			a.showQuote()
		})
	}()
}

func (a *stocksApp) showQuote() {
	q := a.shown
	a.name.SetText(q.Name)
	a.symbol.SetText(strings.Join(nonEmpty(displaySymbol(q.Symbol), q.Exchange, q.Currency), " · "))
	a.price.SetText(stocks.Price(q.Price))
	abs, pct := q.Change()
	a.change.SetText(stocks.Signed(abs, false) + "  " + stocks.Signed(pct, true))
	a.change.RemoveCssClass("up")
	a.change.RemoveCssClass("down")
	a.change.AddCssClass(trend(pct))
	a.drawChart()

	clearGrid(&a.stats)
	base := i18n.T("Previous close")
	if a.data.Range != "1d" {
		base = i18n.T("Range start")
	}
	for i, c := range [][2]string{
		{i18n.T("High"), stocks.Price(q.High)}, {i18n.T("Low"), stocks.Price(q.Low)}, {base, stocks.Price(q.Base)},
	} {
		box := cell(c[0], c[1])
		a.stats.Attach(&box.Widget, int32(i), 0, 1, 1)
		box.Unref()
	}
	a.updated.SetText(i18n.Tf("As of %s", i18n.Time(q.Time.Local(), "Jan 2, 15:04")))
	a.detail.SetVisibleChildName("quote")
}

func nonEmpty(ss ...string) []string {
	return slices.DeleteFunc(ss, func(s string) bool { return s == "" })
}

// drawChart draws the shown quote's chart at the picture's size, in the
// theme's colour for its trend.
func (a *stocksApp) drawChart() {
	q := a.shown
	if len(q.Points) < 2 {
		noPaintable(&a.chart)
		return
	}
	scale := max(1, int(a.chart.GetScaleFactor()))
	w, h := int(a.chart.GetWidth()), int(a.chart.GetHeight())
	if w < 50 || h < 50 {
		w, h = 500, 220
	}
	_, pct := q.Change()
	token := "ok"
	if pct < 0 {
		token = "danger"
	}
	base := q.Base
	if a.data.Range != "1d" {
		base = 0 // only the day's chart marks where it began
	}
	img := stocks.Chart(q.Points, base, w*scale, h*scale, hexColor(a.c.Colors[token]))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return
	}
	data := buf.Bytes()
	tex, err := gdk.NewTextureFromBytes(glib.NewBytes(data, uint(len(data))))
	if err != nil {
		log.Printf("stocks: chart: %v", err)
		return
	}
	a.chart.SetPaintable(tex)
	tex.Unref()
}

// hexColor reads "#rrggbb"; anything else is a mid grey.
func hexColor(s string) color.NRGBA {
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if err != nil || len(s) != 7 {
		return color.NRGBA{128, 128, 128, 255}
	}
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

// find searches symbols as the query is typed.
func (a *stocksApp) find(q string) {
	q = strings.TrimSpace(q)
	a.results.SetVisible(q != "")
	a.list.SetVisible(q == "")
	a.searchGen++
	gen := a.searchGen
	if q == "" {
		gtkx.Clear(&a.results)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ms, err := stocks.Search(ctx, a.http, q)
		cancel()
		gtkx.Post(func() {
			if gen != a.searchGen {
				return
			}
			gtkx.Clear(&a.results)
			a.matches = ms
			if err != nil || len(ms) == 0 {
				msg := i18n.T("Nothing by that name")
				if err != nil {
					msg = i18n.Tf("Could not search: %s", shortErr(err))
				}
				l := gtk.NewLabel(msg)
				l.AddCssClass("index-note")
				l.SetXalign(0)
				l.SetWrap(true)
				gtkx.Append(&a.results, &l.Widget)
				return
			}
			for i, m := range ms {
				t := text(displaySymbol(m.Symbol)+"  "+m.Name, strings.Join(nonEmpty(m.Kind, m.Exchange), " · "))
				gtkx.Append(&a.results, &indexItem("add:"+strconv.Itoa(i), &t.Widget, false, &a.onButton).Widget)
			}
		})
	}()
}
