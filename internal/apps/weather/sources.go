package weather

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Source is where forecasts come from. Places are found with Open-Meteo's
// geocoding whatever the source: a source is asked by coordinates.
type Source struct {
	ID   string // kept in Data.Source
	Name string // shown: the menu, the foot, the messages
	Get  func(ctx context.Context, c *http.Client, p Place, fahrenheit bool) (Forecast, error)
}

// The sources' IDs.
const (
	OpenMeteo = "open-meteo"
	Wttr      = "wttr"
)

// Sources are the sources to choose from, the default first.
var Sources = []Source{
	{ID: OpenMeteo, Name: "Open-Meteo", Get: Get},
	{ID: Wttr, Name: "wttr.in", Get: GetWttr},
}

// SourceByID finds a source; "" is the default, Open-Meteo. An ID this
// version does not know is not found: the caller says so rather than
// reading from another source.
func SourceByID(id string) (Source, bool) {
	if id == "" {
		id = OpenMeteo
	}
	for _, s := range Sources {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// WttrURL is wttr.in's address; tests point it elsewhere.
var WttrURL = "https://wttr.in"

// GetWttr reads the forecast for a place from wttr.in's JSON (format=j1),
// which has three days in three-hour steps: the next 24 hours from the
// step under way, and three days. A day's chance of rain is the highest
// chance of rain or snow among its steps. Times are the place's own: the
// answer gives them without a zone, read in the place's time zone.
func GetWttr(ctx context.Context, c *http.Client, p Place, fahrenheit bool) (Forecast, error) {
	u := WttrURL + "/" + strconv.FormatFloat(p.Lat, 'f', 4, 64) + "," + strconv.FormatFloat(p.Lon, 'f', 4, 64) + "?format=j1"
	type value struct {
		Value string `json:"value"`
	}
	var r struct {
		Current []struct {
			Obs      string `json:"localObsDateTime"`
			TempC    string `json:"temp_C"`
			TempF    string `json:"temp_F"`
			FeelsC   string `json:"FeelsLikeC"`
			FeelsF   string `json:"FeelsLikeF"`
			Humidity string `json:"humidity"`
			Code     string `json:"weatherCode"`
			WindKmph string `json:"windspeedKmph"`
			WindMph  string `json:"windspeedMiles"`
		} `json:"current_condition"`
		Weather []struct {
			Date      string `json:"date"`
			MaxC      string `json:"maxtempC"`
			MaxF      string `json:"maxtempF"`
			MinC      string `json:"mintempC"`
			MinF      string `json:"mintempF"`
			Astronomy []struct {
				Sunrise string `json:"sunrise"`
				Sunset  string `json:"sunset"`
			} `json:"astronomy"`
			Hourly []struct {
				Time  string `json:"time"` // "0", "300", … "2100"
				TempC string `json:"tempC"`
				TempF string `json:"tempF"`
				Code  string `json:"weatherCode"`
				Rain  string `json:"chanceofrain"`
				Snow  string `json:"chanceofsnow"`
			} `json:"hourly"`
		} `json:"weather"`
	}
	var f Forecast
	if err := getJSON(ctx, c, u, &r); err != nil {
		return f, err
	}
	bad := func(format string, a ...any) (Forecast, error) {
		return Forecast{}, unavailable("wttr.in", fmt.Errorf(format, a...))
	}
	if len(r.Current) == 0 || len(r.Weather) == 0 {
		return bad("no current weather or days in the answer")
	}
	loc := time.UTC
	if p.Timezone != "" {
		if l, err := time.LoadLocation(p.Timezone); err == nil {
			loc = l
		}
	}
	var numErr error
	num := func(s string) float64 {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil && numErr == nil {
			numErr = fmt.Errorf("not a number: %q", s)
		}
		return v
	}
	pick := func(c, fahr string) float64 {
		if fahrenheit {
			return num(fahr)
		}
		return num(c)
	}

	cur := r.Current[0]
	now, err := time.ParseInLocation("2006-01-02 03:04 PM", cur.Obs, loc)
	if err != nil {
		now = time.Now().In(loc)
	}
	n := &f.Now
	n.Temp, n.Feels = pick(cur.TempC, cur.TempF), pick(cur.FeelsC, cur.FeelsF)
	n.Humidity = int(num(cur.Humidity))
	n.Wind = pick(cur.WindKmph, cur.WindMph)
	n.Code = wwoCode(int(num(cur.Code)))

	type span struct{ rise, set time.Time }
	days := map[string]span{}
	for _, w := range r.Weather {
		date, err := time.ParseInLocation("2006-01-02", w.Date, loc)
		if err != nil {
			return bad("not a date: %q", w.Date)
		}
		d := Day{Date: date, Max: pick(w.MaxC, w.MaxF), Min: pick(w.MinC, w.MinF)}
		if len(w.Astronomy) > 0 {
			// "No sunrise" in a polar day or night: left zero.
			d.Sunrise = clock(date, w.Astronomy[0].Sunrise)
			d.Sunset = clock(date, w.Astronomy[0].Sunset)
		}
		days[w.Date] = span{d.Sunrise, d.Sunset}
		// The day's sky is that of its middle; its rain the highest chance.
		mid := -1
		for _, h := range w.Hourly {
			at := int(num(h.Time))
			t := date.Add(time.Duration(at/100)*time.Hour + time.Duration(at%100)*time.Minute)
			d.Rain = max(d.Rain, int(num(h.Rain)), int(num(h.Snow)))
			code := wwoCode(int(num(h.Code)))
			if mid < 0 || at <= 1200 {
				mid, d.Code = at, code
			}
			// The steps from the one under way now, for 24 hours.
			if !t.Add(3*time.Hour).After(now) || len(f.Hours) == 8 {
				continue
			}
			f.Hours = append(f.Hours, Hour{Time: t, Temp: pick(h.TempC, h.TempF), Code: code, Day: daytime(t.Add(90*time.Minute), d.Sunrise, d.Sunset)})
		}
		f.Days = append(f.Days, d)
	}
	if numErr != nil {
		return bad("%v", numErr)
	}
	if s, ok := days[now.Format("2006-01-02")]; ok {
		n.Day = daytime(now, s.rise, s.set)
	} else {
		n.Day = daytime(now, time.Time{}, time.Time{})
	}
	f.Unit, f.Wind = "°C", "km/h"
	if fahrenheit {
		f.Unit, f.Wind = "°F", "mph"
	}
	return f, nil
}

// clock is a time of day, "06:15 AM", on date; zero if it is not one.
func clock(date time.Time, s string) time.Time {
	t, err := time.Parse("03:04 PM", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return time.Date(date.Year(), date.Month(), date.Day(), t.Hour(), t.Minute(), 0, 0, date.Location())
}

// daytime says whether t is between sunrise and sunset; without them (a
// polar day or night, or none given), whether it is between 6 and 18.
func daytime(t, rise, set time.Time) bool {
	if rise.IsZero() || set.IsZero() {
		return t.Hour() >= 6 && t.Hour() < 18
	}
	return !t.Before(rise) && t.Before(set)
}

// wwoCode turns a World Weather Online condition code, wttr.in's, into the
// WMO code Describe names; -1 (Unknown) for one it does not know.
func wwoCode(c int) int {
	switch c {
	case 113:
		return 0 // clear, sunny
	case 116:
		return 2 // partly cloudy
	case 119, 122:
		return 3 // cloudy, overcast
	case 143, 248:
		return 45 // mist, fog
	case 260:
		return 48 // freezing fog
	case 263, 266:
		return 51 // light drizzle
	case 185, 281:
		return 56 // freezing drizzle
	case 284:
		return 57
	case 176, 293, 296, 353:
		return 61 // light rain, showers
	case 299, 302, 356:
		return 63
	case 305, 308, 359:
		return 65 // heavy rain
	case 182, 311, 317, 362:
		return 66 // freezing rain, sleet
	case 314, 320, 365:
		return 67
	case 179, 227, 323, 326, 368:
		return 71 // light snow
	case 329, 332:
		return 73
	case 230, 335, 338, 371:
		return 75 // heavy snow, blizzard
	case 350, 374, 377:
		return 77 // ice pellets
	case 200, 386, 389, 392, 395:
		return 95 // thunder
	}
	return -1
}

// HourSpan is how many hours the forecast's hours cover, and the step
// between them: 24 and 1 for Open-Meteo, 24 and 3 for wttr.in.
func (f Forecast) HourSpan() (hours, step int) {
	if len(f.Hours) == 0 {
		return 0, 0
	}
	step = 1
	if len(f.Hours) > 1 {
		step = max(1, int(f.Hours[1].Time.Sub(f.Hours[0].Time).Round(time.Hour)/time.Hour))
	}
	last := f.Hours[len(f.Hours)-1].Time.Sub(f.Hours[0].Time).Round(time.Hour)
	return int(last/time.Hour) + step, step
}
