// Package weather is the Weather app's logic: finding places (Open-Meteo's
// geocoding) and reading forecasts from one of Sources: Open-Meteo
// (https://open-meteo.com) or wttr.in (https://wttr.in), both free and
// without a key.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/lyricat/inua/internal/i18n"
)

// StoreName is the app's file in the store.
const StoreName = "weather"

// Data is what the Weather app keeps.
type Data struct {
	Places     []Place `json:"places"`
	Selected   int     `json:"selected"`
	Fahrenheit bool    `json:"fahrenheit"`
	// Source is the forecast's source, a Sources ID; "" (a file from before
	// there was a choice) is Open-Meteo.
	Source string `json:"source,omitempty"`
}

// Place is a place to show the weather of.
type Place struct {
	Name     string  `json:"name"`
	Region   string  `json:"region"` // state or province
	Country  string  `json:"country"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Timezone string  `json:"timezone"`
}

// Where is the place's region and country: "Taiwan", "California · United
// States".
func (p Place) Where() string {
	switch {
	case p.Region == "" || p.Region == p.Country || p.Region == p.Name:
		return p.Country
	case p.Country == "":
		return p.Region
	}
	return p.Region + " · " + p.Country
}

// The services; tests point them elsewhere.
var (
	GeocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
	ForecastURL = "https://api.open-meteo.com/v1/forecast"
)

// Search finds places by name.
func Search(ctx context.Context, c *http.Client, name string) ([]Place, error) {
	q := url.Values{"name": {name}, "count": {"8"}, "language": {"en"}, "format": {"json"}}
	var r struct {
		Results []struct {
			Name      string  `json:"name"`
			Admin1    string  `json:"admin1"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
		} `json:"results"`
	}
	if err := getJSON(ctx, c, GeocodeURL+"?"+q.Encode(), &r); err != nil {
		return nil, err
	}
	places := make([]Place, 0, len(r.Results))
	for _, p := range r.Results {
		places = append(places, Place{Name: p.Name, Region: p.Admin1, Country: p.Country, Lat: p.Latitude, Lon: p.Longitude, Timezone: p.Timezone})
	}
	return places, nil
}

// Forecast is the weather at a place: now, the next hours and days.
type Forecast struct {
	Now struct {
		Temp, Feels float64
		Humidity    int
		Wind        float64 // km/h, or mph with Fahrenheit
		Code        int     // WMO weather code
		Day         bool
	}
	Hours []Hour
	Days  []Day
	Unit  string // "°C" or "°F"
	Wind  string // "km/h" or "mph"
}

type Hour struct {
	Time time.Time
	Temp float64
	Code int
	Day  bool
}

type Day struct {
	Date     time.Time
	Code     int
	Min, Max float64
	Rain     int // chance of precipitation, %
	Sunrise  time.Time
	Sunset   time.Time
}

// Get reads the forecast for a place from Open-Meteo: 24 hours and 7 days.
func Get(ctx context.Context, c *http.Client, p Place, fahrenheit bool) (Forecast, error) {
	q := url.Values{
		"latitude":       {strconv.FormatFloat(p.Lat, 'f', 4, 64)},
		"longitude":      {strconv.FormatFloat(p.Lon, 'f', 4, 64)},
		"current":        {"temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,is_day,wind_speed_10m"},
		"hourly":         {"temperature_2m,weather_code,is_day"},
		"daily":          {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max,sunrise,sunset"},
		"timezone":       {"auto"},
		"forecast_days":  {"7"},
		"forecast_hours": {"24"},
	}
	if fahrenheit {
		q.Set("temperature_unit", "fahrenheit")
		q.Set("wind_speed_unit", "mph")
	}
	var r struct {
		Offset  int `json:"utc_offset_seconds"`
		Current *struct {
			Temp     float64 `json:"temperature_2m"`
			Feels    float64 `json:"apparent_temperature"`
			Humidity int     `json:"relative_humidity_2m"`
			Code     int     `json:"weather_code"`
			IsDay    int     `json:"is_day"`
			Wind     float64 `json:"wind_speed_10m"`
		} `json:"current"`
		Hourly struct {
			Time  []string  `json:"time"`
			Temp  []float64 `json:"temperature_2m"`
			Code  []int     `json:"weather_code"`
			IsDay []int     `json:"is_day"`
		} `json:"hourly"`
		Daily struct {
			Time    []string  `json:"time"`
			Code    []int     `json:"weather_code"`
			Max     []float64 `json:"temperature_2m_max"`
			Min     []float64 `json:"temperature_2m_min"`
			Rain    []*int    `json:"precipitation_probability_max"`
			Sunrise []string  `json:"sunrise"`
			Sunset  []string  `json:"sunset"`
		} `json:"daily"`
	}
	var f Forecast
	if err := getJSON(ctx, c, ForecastURL+"?"+q.Encode(), &r); err != nil {
		return f, err
	}
	if r.Current == nil {
		return f, unavailable("open-meteo", errors.New("no current weather in the answer"))
	}
	loc := time.FixedZone("", r.Offset) // the place's own time
	parse := func(layout, s string) time.Time {
		t, _ := time.ParseInLocation(layout, s, loc)
		return t
	}
	n := &f.Now
	n.Temp, n.Feels, n.Humidity, n.Code, n.Day, n.Wind = r.Current.Temp, r.Current.Feels, r.Current.Humidity, r.Current.Code, r.Current.IsDay == 1, r.Current.Wind
	h := r.Hourly
	for i := range min(len(h.Time), len(h.Temp), len(h.Code), len(h.IsDay)) {
		f.Hours = append(f.Hours, Hour{Time: parse("2006-01-02T15:04", h.Time[i]), Temp: h.Temp[i], Code: h.Code[i], Day: h.IsDay[i] == 1})
	}
	d := r.Daily
	for i := range min(len(d.Time), len(d.Code), len(d.Max), len(d.Min), len(d.Rain), len(d.Sunrise), len(d.Sunset)) {
		day := Day{Date: parse("2006-01-02", d.Time[i]), Code: d.Code[i], Max: d.Max[i], Min: d.Min[i],
			Sunrise: parse("2006-01-02T15:04", d.Sunrise[i]), Sunset: parse("2006-01-02T15:04", d.Sunset[i])}
		if d.Rain[i] != nil {
			day.Rain = *d.Rain[i]
		}
		f.Days = append(f.Days, day)
	}
	f.Unit, f.Wind = "°C", "km/h"
	if fahrenheit {
		f.Unit, f.Wind = "°F", "mph"
	}
	return f, nil
}

// Describe names a WMO weather code and the icon to draw for it.
func Describe(code int, day bool) (text, icon string) {
	sky := func(dayIcon, nightIcon string) string {
		if day {
			return dayIcon
		}
		return nightIcon
	}
	switch code {
	case 0:
		return i18n.T("Clear sky"), sky("weather-clear-symbolic", "weather-clear-night-symbolic")
	case 1:
		return i18n.T("Mostly clear"), sky("weather-clear-symbolic", "weather-clear-night-symbolic")
	case 2:
		return i18n.T("Partly cloudy"), sky("weather-few-clouds-symbolic", "weather-few-clouds-night-symbolic")
	case 3:
		return i18n.T("Cloudy"), "weather-overcast-symbolic"
	case 45, 48:
		return i18n.T("Fog"), "weather-fog-symbolic"
	case 51, 53, 55, 56, 57:
		return i18n.T("Drizzle"), "weather-showers-scattered-symbolic"
	case 61, 63, 66, 80, 81:
		return i18n.T("Rain"), "weather-showers-symbolic"
	case 65, 67, 82:
		return i18n.T("Heavy rain"), "weather-showers-symbolic"
	case 71, 73, 77, 85:
		return i18n.T("Snow"), "weather-snow-symbolic"
	case 75, 86:
		return i18n.T("Heavy snow"), "weather-snow-symbolic"
	case 95, 96, 99:
		return i18n.T("Thunderstorm"), "weather-storm-symbolic"
	}
	return i18n.T("Unknown"), "weather-severe-alert-symbolic"
}

// Temp shows a temperature rounded to a whole degree: "27°", "−3°".
func Temp(v float64) string {
	n := int(v + 0.5)
	if v < 0 {
		n = int(v - 0.5)
	}
	if n < 0 {
		return fmt.Sprintf("−%d°", -n)
	}
	return fmt.Sprintf("%d°", n)
}

func getJSON(ctx context.Context, c *http.Client, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err // the network's: no answer
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return unavailable(req.URL.Host, errors.New(resp.Status))
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return unavailable(req.URL.Host, fmt.Errorf("not a forecast: %w", err))
	}
	return nil
}

// UnavailableError is a source that answered, but not with what was asked:
// an error status (busy, over its limits, down) or an answer that is not a
// whole forecast. Any other error from Get is the network's.
type UnavailableError struct {
	Host string
	Err  error
}

func (e *UnavailableError) Error() string { return e.Host + ": " + e.Err.Error() }
func (e *UnavailableError) Unwrap() error { return e.Err }

func unavailable(host string, err error) error { return &UnavailableError{Host: host, Err: err} }

// Unavailable says whether err is the source's (UnavailableError) rather
// than the network's.
func Unavailable(err error) bool {
	var u *UnavailableError
	return errors.As(err, &u)
}
