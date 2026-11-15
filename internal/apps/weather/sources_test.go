package weather

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the places' zones, wherever the tests run

	"github.com/lyricat/inua/internal/apps/store"
)

// wttrJSON is wttr.in's format=j1, cut down to what is read: observed at
// 10:23 in Taipei, two days of three-hour steps.
const wttrJSON = `{
"current_condition":[{"localObsDateTime":"2026-10-10 10:23 AM","temp_C":"27","temp_F":"81","FeelsLikeC":"30","FeelsLikeF":"86",
 "humidity":"71","weatherCode":"116","windspeedKmph":"15","windspeedMiles":"9"}],
"weather":[
 {"date":"2026-10-10","maxtempC":"28","maxtempF":"82","mintempC":"22","mintempF":"72",
  "astronomy":[{"sunrise":"05:49 AM","sunset":"05:32 PM"}],
  "hourly":[
   {"time":"0","tempC":"23","tempF":"73","weatherCode":"113","chanceofrain":"0","chanceofsnow":"0"},
   {"time":"300","tempC":"22","tempF":"72","weatherCode":"113","chanceofrain":"0","chanceofsnow":"0"},
   {"time":"600","tempC":"23","tempF":"73","weatherCode":"116","chanceofrain":"10","chanceofsnow":"0"},
   {"time":"900","tempC":"26","tempF":"79","weatherCode":"116","chanceofrain":"20","chanceofsnow":"0"},
   {"time":"1200","tempC":"28","tempF":"82","weatherCode":"176","chanceofrain":"64","chanceofsnow":"0"},
   {"time":"1500","tempC":"27","tempF":"81","weatherCode":"353","chanceofrain":"80","chanceofsnow":"0"},
   {"time":"1800","tempC":"25","tempF":"77","weatherCode":"119","chanceofrain":"30","chanceofsnow":"0"},
   {"time":"2100","tempC":"24","tempF":"75","weatherCode":"113","chanceofrain":"0","chanceofsnow":"0"}]},
 {"date":"2026-10-11","maxtempC":"29","maxtempF":"84","mintempC":"21","mintempF":"70",
  "astronomy":[{"sunrise":"05:50 AM","sunset":"05:31 PM"}],
  "hourly":[
   {"time":"0","tempC":"23","tempF":"73","weatherCode":"113","chanceofrain":"0","chanceofsnow":"0"},
   {"time":"300","tempC":"22","tempF":"72","weatherCode":"113","chanceofrain":"0","chanceofsnow":"0"},
   {"time":"600","tempC":"23","tempF":"73","weatherCode":"113","chanceofrain":"0","chanceofsnow":"0"},
   {"time":"900","tempC":"26","tempF":"79","weatherCode":"113","chanceofrain":"0","chanceofsnow":"0"},
   {"time":"1200","tempC":"29","tempF":"84","weatherCode":"389","chanceofrain":"5","chanceofsnow":"0"},
   {"time":"1500","tempC":"28","tempF":"82","weatherCode":"113","chanceofrain":"0","chanceofsnow":"0"}]}]}`

func serve(t *testing.T, body string, status int) (*httptest.Server, *string) {
	t.Helper()
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.String()
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

var taipei = Place{Name: "Taipei", Lat: 25.05, Lon: 121.53, Timezone: "Asia/Taipei"}

func TestGetWttr(t *testing.T) {
	srv, asked := serve(t, wttrJSON, 200)
	WttrURL = srv.URL
	f, err := GetWttr(context.Background(), srv.Client(), taipei, false)
	if err != nil {
		t.Fatal(err)
	}
	if *asked != "/25.0500,121.5300?format=j1" {
		t.Errorf("asked %s", *asked)
	}
	if f.Now.Temp != 27 || f.Now.Feels != 30 || f.Now.Humidity != 71 || f.Now.Wind != 15 || f.Now.Code != 2 || !f.Now.Day {
		t.Errorf("now %+v", f.Now)
	}
	if f.Unit != "°C" || f.Wind != "km/h" {
		t.Errorf("units %s %s", f.Unit, f.Wind)
	}
	// From the step under way (09:00) for 24 hours: eight steps.
	if len(f.Hours) != 8 || f.Hours[0].Time.Hour() != 9 || f.Hours[7].Time.Day() != 11 || f.Hours[7].Time.Hour() != 6 {
		t.Fatalf("hours %+v", f.Hours)
	}
	if _, off := f.Hours[0].Time.Zone(); off != 8*3600 {
		t.Errorf("hours are not in the place's time: offset %d", off)
	}
	if f.Hours[2].Code != 61 || !f.Hours[2].Day || f.Hours[4].Day {
		t.Errorf("15:00 is rain by day, 21:00 night: %+v", f.Hours)
	}
	if h, step := f.HourSpan(); h != 24 || step != 3 {
		t.Errorf("span %d every %d", h, step)
	}
	if len(f.Days) != 2 || f.Days[0].Max != 28 || f.Days[0].Min != 22 || f.Days[0].Rain != 80 || f.Days[0].Code != 61 || f.Days[1].Code != 95 {
		t.Errorf("days %+v", f.Days)
	}
	if s := f.Days[0].Sunset; s.Hour() != 17 || s.Minute() != 32 || s.Day() != 10 {
		t.Errorf("sunset %v", s)
	}

	f, err = GetWttr(context.Background(), srv.Client(), taipei, true)
	if err != nil {
		t.Fatal(err)
	}
	if f.Now.Temp != 81 || f.Now.Feels != 86 || f.Now.Wind != 9 || f.Unit != "°F" || f.Wind != "mph" || f.Days[1].Max != 84 || f.Hours[0].Temp != 79 {
		t.Errorf("fahrenheit: now %+v days %+v", f.Now, f.Days)
	}
}

func TestGetWttrNight(t *testing.T) {
	body := strings.Replace(wttrJSON, "2026-10-10 10:23 AM", "2026-10-10 10:23 PM", 1)
	srv, _ := serve(t, body, 200)
	WttrURL = srv.URL
	f, err := GetWttr(context.Background(), srv.Client(), taipei, false)
	if err != nil {
		t.Fatal(err)
	}
	if f.Now.Day {
		t.Error("22:23 is night")
	}
	// From 21:00: the rest of the answer, six steps of the next day.
	if len(f.Hours) != 7 || f.Hours[0].Time.Hour() != 21 {
		t.Errorf("hours %+v", f.Hours)
	}
}

func TestGetWttrPolar(t *testing.T) {
	body := strings.ReplaceAll(wttrJSON, `"sunrise":"05:49 AM","sunset":"05:32 PM"`, `"sunrise":"No sunrise","sunset":"No sunset"`)
	srv, _ := serve(t, body, 200)
	WttrURL = srv.URL
	f, err := GetWttr(context.Background(), srv.Client(), taipei, false)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Days[0].Sunrise.IsZero() || !f.Days[0].Sunset.IsZero() {
		t.Errorf("no sunrise should be none, not midnight: %+v", f.Days[0])
	}
}

func TestSourcesUnavailable(t *testing.T) {
	cases := map[string]struct {
		body   string
		status int
	}{
		"busy":       {"Sorry, we are running out of queries", 503},
		"text":       {"Sorry, we are running out of queries", 200},
		"empty":      {"{}", 200},
		"no days":    {`{"current_condition":[{"temp_C":"1"}],"weather":[]}`, 200},
		"bad number": {strings.Replace(wttrJSON, `"humidity":"71"`, `"humidity":"lots"`, 1), 200},
		"bad date":   {strings.Replace(wttrJSON, `"date":"2026-10-11"`, `"date":"tomorrow"`, 1), 200},
	}
	for name, c := range cases {
		srv, _ := serve(t, c.body, c.status)
		WttrURL, ForecastURL = srv.URL, srv.URL
		for _, s := range Sources {
			if name != "busy" && name != "text" && name != "empty" && s.ID != Wttr {
				continue // the cases of wttr.in's answer
			}
			_, err := s.Get(context.Background(), srv.Client(), taipei, false)
			if err == nil || !Unavailable(err) {
				t.Errorf("%s from %s: %v, want the source unavailable", name, s.Name, err)
			}
		}
	}
}

func TestSourcesNetwork(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // nothing answers there
	WttrURL, ForecastURL = srv.URL, srv.URL
	for _, s := range Sources {
		_, err := s.Get(context.Background(), http.DefaultClient, taipei, false)
		if err == nil || Unavailable(err) {
			t.Errorf("%s: %v, want the network's error", s.Name, err)
		}
	}
}

func TestSourceByID(t *testing.T) {
	if s, ok := SourceByID(""); !ok || s.ID != OpenMeteo {
		t.Errorf("no choice should be Open-Meteo, got %+v", s)
	}
	if s, ok := SourceByID(Wttr); !ok || s.Name != "wttr.in" {
		t.Errorf("wttr: %+v", s)
	}
	if _, ok := SourceByID("met-no"); ok {
		t.Error("an unknown source should not be found as another")
	}
	if (Data{Source: "met-no"}).SourceID() != "met-no" {
		t.Error("an unknown source should be kept as it is")
	}
}

// Switching sources reads from the chosen one only: each answers with a
// different temperature.
func TestSourceSwitching(t *testing.T) {
	om, omAsked := serve(t, forecastJSON, 200)
	wt, wtAsked := serve(t, wttrJSON, 200)
	ForecastURL, WttrURL = om.URL, wt.URL
	d := Data{Places: []Place{taipei}}
	for _, c := range []struct {
		source string
		temp   float64
		asked  *string
	}{{"", 27, omAsked}, {Wttr, 27, wtAsked}, {OpenMeteo, 27, omAsked}} {
		*omAsked, *wtAsked = "", ""
		d.Source = c.source
		s, ok := SourceByID(d.SourceID())
		if !ok {
			t.Fatal(c.source)
		}
		p, _ := d.Current()
		if _, err := s.Get(context.Background(), http.DefaultClient, p, false); err != nil {
			t.Fatal(err)
		}
		if *c.asked == "" || *omAsked != "" && *wtAsked != "" {
			t.Errorf("source %q: asked Open-Meteo %q, wttr.in %q", c.source, *omAsked, *wtAsked)
		}
	}
}

func TestSelection(t *testing.T) {
	a, b, c := Place{Name: "A", Lat: 1}, Place{Name: "B", Lat: 2}, Place{Name: "C", Lat: 3}
	var d Data
	if _, ok := d.Current(); ok {
		t.Fatal("no city should be none chosen")
	}
	d.Add(a)
	d.Add(b)
	d.Add(c)
	if p, _ := d.Current(); p != c {
		t.Errorf("an added city should be chosen, got %+v", p)
	}
	d.Add(Place{Name: "A again", Lat: 1})
	if len(d.Places) != 3 || d.Selected != 0 {
		t.Errorf("a city added again should be chosen, not added: %+v", d)
	}
	if d.Select(3) || d.Select(-1) || d.Selected != 0 {
		t.Error("no city there")
	}
	d.Select(2)
	d.Remove(0) // before the chosen one: C stays chosen
	if p, _ := d.Current(); p != c {
		t.Errorf("after removing A: %+v", p)
	}
	d.Remove(1) // the chosen one: the one before it
	if p, _ := d.Current(); p != b {
		t.Errorf("after removing C: %+v", p)
	}
	d.Remove(0)
	if _, ok := d.Current(); ok || d.Selected != 0 || len(d.Places) != 0 {
		t.Errorf("after removing all: %+v", d)
	}

	d = Data{Places: []Place{a, b}, Selected: 0}
	d.Remove(0) // the first and chosen: the next
	if p, _ := d.Current(); p != b {
		t.Errorf("after removing the first: %+v", p)
	}
	d = Data{Places: []Place{a, b}, Selected: 7} // a file edited by hand
	if p, ok := d.Current(); !ok || p != b || d.Selected != 1 {
		t.Errorf("out of range: %+v", d)
	}
}

func TestPreferences(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	// A file from before the choice of source reads as Open-Meteo.
	path := store.Path(StoreName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"places":[{"name":"Taipei","lat":25.05,"lon":121.53,"timezone":"Asia/Taipei"},{"name":"Oslo","lat":59.91,"lon":10.75}],"selected":1,"fahrenheit":true}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	var d Data
	if err := store.Load(StoreName, &d); err != nil {
		t.Fatal(err)
	}
	if d.SourceID() != OpenMeteo || d.Source != "" || !d.Fahrenheit {
		t.Errorf("old file: %+v", d)
	}
	if p, _ := d.Current(); p.Name != "Oslo" {
		t.Errorf("the chosen city should come back: %+v", p)
	}

	d.Source = Wttr
	d.Select(0)
	if err := store.Save(StoreName, d); err != nil {
		t.Fatal(err)
	}
	var back Data
	if err := store.Load(StoreName, &back); err != nil {
		t.Fatal(err)
	}
	if back.SourceID() != Wttr || back.Selected != 0 || len(back.Places) != 2 || back.Places[0].Timezone != "Asia/Taipei" {
		t.Errorf("saved and read again: %+v", back)
	}
	raw, _ := os.ReadFile(path)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m["source"] != Wttr {
		t.Errorf("file %s", raw)
	}
}

// An answer counts only for what is chosen when it comes: another city,
// source or units, or no city at all, leaves it stale.
func TestLoads(t *testing.T) {
	var l Loads
	d := Data{Places: []Place{taipei, {Name: "Oslo", Lat: 59.91, Lon: 10.75}}}
	p, _ := d.Current()
	first := l.Begin(d.KeyOf(p))
	if !l.Current(first) {
		t.Fatal("the newest load counts")
	}
	d.Select(1)
	p, _ = d.Current()
	second := l.Begin(d.KeyOf(p))
	if l.Current(first) || !l.Current(second) {
		t.Error("another city: the first answer is stale")
	}
	d.Source = Wttr
	third := l.Begin(d.KeyOf(p))
	if l.Current(second) || third.Key.Source != Wttr {
		t.Error("another source: the earlier answer is stale")
	}
	d.Fahrenheit = true
	fourth := l.Begin(d.KeyOf(p))
	if l.Current(third) || !fourth.Key.Fahrenheit {
		t.Error("other units: the earlier answer is stale")
	}
	// The same key read again (a refresh) still leaves the earlier stale.
	fifth := l.Begin(d.KeyOf(p))
	if l.Current(fourth) || !l.Current(fifth) {
		t.Error("a refresh: the earlier answer is stale")
	}
	d.Remove(1)
	d.Remove(0)
	l.Stop()
	if l.Current(fifth) {
		t.Error("no city: every answer is stale")
	}
}

func TestTemps(t *testing.T) {
	var ts Temps
	now := time.Now()
	d := Data{}
	om := d.KeyOf(taipei)
	d.Source = Wttr
	wt := d.KeyOf(taipei)
	d.Fahrenheit = true
	wtF := d.KeyOf(taipei)
	ts.Put(om, 27, now)
	if _, ok := ts.Get(wt); ok {
		t.Error("Open-Meteo's temperature shown for wttr.in")
	}
	if _, ok := ts.Get(wtF); ok {
		t.Error("Celsius shown for Fahrenheit")
	}
	if v, ok := ts.Get(om); !ok || v != 27 {
		t.Error(v, ok)
	}
	if !ts.Fresh(om, now.Add(time.Minute), 15*time.Minute) || ts.Fresh(om, now.Add(time.Hour), 15*time.Minute) || ts.Fresh(wt, now, time.Hour) {
		t.Error("freshness")
	}
}

func TestUnavailableError(t *testing.T) {
	err := unavailable("wttr.in", errors.New("503 Service Unavailable"))
	if !Unavailable(err) || err.Error() != "wttr.in: 503 Service Unavailable" {
		t.Error(err)
	}
	if Unavailable(context.DeadlineExceeded) {
		t.Error("a timeout is the network's")
	}
}
