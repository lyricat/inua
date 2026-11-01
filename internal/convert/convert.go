// Package convert answers the launcher's quick questions: arithmetic
// ("2^10 + 3"), units ("10 km to mi", "32 f in c", "5 kg") and currencies
// ("100 usd to jpy"), with exchange rates from frankfurter.app (the
// European Central Bank's reference rates, free and without a key) or,
// when it cannot be reached, the Bank of England's from allratestoday.com
// (also free and without a key).
package convert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/apps/calc"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/rundir"
)

// Answer is a result to show and what Enter copies.
type Answer struct {
	Text   string // "6.21371 mi"
	Detail string // "10 km in miles"
	Copy   string // "6.21371"
}

// Rates are exchange rates for one unit of Base.
type Rates struct {
	Base  string             `json:"base"`
	Date  string             `json:"date"`
	Rates map[string]float64 `json:"rates"`
	Got   time.Time          `json:"got"` // when they were fetched
	// More are Coinbase's rates, each per US dollar: cryptocurrencies, and
	// currencies the ECB does not list. They move fast, so they are
	// fetched again sooner (MoreMaxAge).
	More    map[string]float64 `json:"more,omitempty"`
	MoreGot time.Time          `json:"more_got,omitempty"`
}

// Stale reports whether rates should be fetched again: none, the ECB's
// older than RatesMaxAge, or Coinbase's older than MoreMaxAge.
func (r *Rates) Stale(now time.Time) bool {
	return r == nil || now.Sub(r.Got) > RatesMaxAge || now.Sub(r.MoreGot) > MoreMaxAge
}

// perBase is how many of a currency one of the base buys, and whether
// the price is Coinbase's.
func (r *Rates) perBase(c string) (v float64, coinbase, ok bool) {
	if c == r.Base {
		return 1, false, true
	}
	if v, ok := r.Rates[c]; ok && v > 0 {
		return v, false, true
	}
	if v, ok := r.More[c]; ok && v > 0 {
		base := 1.0 // the base per dollar: the ECB's when it has the dollar,
		// so a crypto price agrees with the ECB's other rates
		if r.Base != "USD" {
			if usd := r.Rates["USD"]; usd > 0 {
				base = 1 / usd
			} else {
				base = r.More[r.Base]
			}
		}
		if base > 0 {
			return v / base, true, true
		}
	}
	return 0, false, false
}

type unit struct {
	kind   string  // what it measures; units convert only within a kind
	factor float64 // in the kind's base unit
	offset float64 // temperatures: base = value*factor + offset
	name   string  // shown in details
	symbol string  // shown with numbers
}

// units by every name they go by.
var units = map[string]unit{}

func add(kind string, factor float64, name, symbol string, aliases ...string) {
	u := unit{kind: kind, factor: factor, name: name, symbol: symbol}
	for _, a := range append(aliases, symbol) {
		units[a] = u
	}
}

func init() {
	add("length", 0.001, i18n.M("millimetres"), "mm", "millimeter", "millimeters", "millimetre")
	add("length", 0.01, i18n.M("centimetres"), "cm", "centimeter", "centimeters", "centimetre")
	add("length", 1, i18n.M("metres"), "m", "meter", "meters", "metre", "metres")
	add("length", 1000, i18n.M("kilometres"), "km", "kilometer", "kilometers", "kilometre", "kilometres")
	add("length", 0.0254, i18n.M("inches"), "in", "inch", "inches", `"`)
	add("length", 0.3048, i18n.M("feet"), "ft", "foot", "feet", "'")
	add("length", 0.9144, i18n.M("yards"), "yd", "yard", "yards")
	add("length", 1609.344, i18n.M("miles"), "mi", "mile", "miles")
	add("length", 1852, i18n.M("nautical miles"), "nmi", "nautical mile")

	add("mass", 1e-6, i18n.M("milligrams"), "mg", "milligram", "milligrams")
	add("mass", 0.001, i18n.M("grams"), "g", "gram", "grams")
	add("mass", 1, i18n.M("kilograms"), "kg", "kilo", "kilos", "kilogram", "kilograms")
	add("mass", 1000, i18n.M("tonnes"), "t", "tonne", "tonnes")
	add("mass", 0.028349523125, i18n.M("ounces"), "oz", "ounce", "ounces")
	add("mass", 0.45359237, i18n.M("pounds"), "lb", "lbs", "pound", "pounds")
	add("mass", 6.35029318, i18n.M("stone"), "st", "stone", "stones")

	add("volume", 0.001, i18n.M("millilitres"), "ml", "milliliter", "milliliters", "millilitre")
	add("volume", 1, i18n.M("litres"), "l", "liter", "liters", "litre", "litres")
	add("volume", 1000, i18n.M("cubic metres"), "m3", "m³")
	add("volume", 3.785411784, i18n.M("US gallons"), "gal", "gallon", "gallons")
	add("volume", 0.946352946, i18n.M("US quarts"), "qt", "quart", "quarts")
	add("volume", 0.473176473, i18n.M("US pints"), "pt", "pint", "pints")
	add("volume", 0.2365882365, i18n.M("US cups"), "cup", "cups")
	add("volume", 0.0295735295625, i18n.M("US fluid ounces"), "fl oz", "floz")
	add("volume", 0.01478676478125, i18n.M("tablespoons"), "tbsp", "tablespoon", "tablespoons")
	add("volume", 0.00492892159375, i18n.M("teaspoons"), "tsp", "teaspoon", "teaspoons")

	add("area", 1, i18n.M("square metres"), "m2", "m²", "sqm")
	add("area", 1e6, i18n.M("square kilometres"), "km2", "km²")
	add("area", 1e4, i18n.M("hectares"), "ha", "hectare", "hectares")
	add("area", 4046.8564224, i18n.M("acres"), "ac", "acre", "acres")
	add("area", 0.09290304, i18n.M("square feet"), "ft2", "ft²", "sqft")
	add("area", 2589988.110336, i18n.M("square miles"), "mi2", "mi²")

	add("speed", 1, i18n.M("metres per second"), "m/s", "mps")
	add("speed", 1/3.6, i18n.M("kilometres per hour"), "km/h", "kmh", "kph")
	add("speed", 0.44704, i18n.M("miles per hour"), "mph")
	add("speed", 1852.0/3600, i18n.M("knots"), "kn", "knot", "knots")
	add("speed", 0.3048, i18n.M("feet per second"), "ft/s", "fps")

	add("data", 1, i18n.M("bytes"), "B", "b", "byte", "bytes")
	add("data", 1e3, i18n.M("kilobytes"), "KB", "kb", "kilobyte", "kilobytes")
	add("data", 1e6, i18n.M("megabytes"), "MB", "mb", "megabyte", "megabytes")
	add("data", 1e9, i18n.M("gigabytes"), "GB", "gb", "gigabyte", "gigabytes")
	add("data", 1e12, i18n.M("terabytes"), "TB", "tb", "terabyte", "terabytes")
	add("data", 1024, i18n.M("kibibytes"), "KiB", "kib")
	add("data", 1<<20, i18n.M("mebibytes"), "MiB", "mib")
	add("data", 1<<30, i18n.M("gibibytes"), "GiB", "gib")
	add("data", 1<<40, i18n.M("tebibytes"), "TiB", "tib")
	add("data", 0.125, i18n.M("bits"), "bit", "bits")
	add("data", 125, i18n.M("kilobits"), "kbit", "kbps")
	add("data", 125e3, i18n.M("megabits"), "Mbit", "mbit", "mbps")
	add("data", 125e6, i18n.M("gigabits"), "Gbit", "gbit", "gbps")

	add("time", 0.001, i18n.M("milliseconds"), "ms", "millisecond", "milliseconds")
	add("time", 1, i18n.M("seconds"), "s", "sec", "secs", "second", "seconds")
	add("time", 60, i18n.M("minutes"), "min", "mins", "minute", "minutes")
	add("time", 3600, i18n.M("hours"), "h", "hr", "hrs", "hour", "hours")
	add("time", 86400, i18n.M("days"), "d", "day", "days")
	add("time", 604800, i18n.M("weeks"), "wk", "week", "weeks")
	add("time", 31557600, i18n.M("years"), "yr", "year", "years") // Julian years

	for _, t := range []struct {
		factor, offset float64
		name, symbol   string
		aliases        []string
	}{
		{1, 0, i18n.M("degrees Celsius"), "°C", []string{"c", "°c", "celsius", "degc"}},
		{5.0 / 9, -32 * 5.0 / 9, i18n.M("degrees Fahrenheit"), "°F", []string{"f", "°f", "fahrenheit", "degf"}},
		{1, -273.15, i18n.M("kelvin"), "K", []string{"k", "kelvin"}},
	} {
		u := unit{kind: "temperature", factor: t.factor, offset: t.offset, name: t.name, symbol: t.symbol}
		for _, a := range t.aliases {
			units[a] = u
		}
	}
}

// defaultTarget is what a unit converts to when the question names none.
var defaultTarget = map[string]string{
	"km": "mi", "mi": "km", "m": "ft", "ft": "m", "cm": "in", "in": "cm", "mm": "in", "yd": "m",
	"kg": "lb", "lb": "kg", "g": "oz", "oz": "g", "st": "kg",
	"l": "gal", "gal": "l", "ml": "fl oz", "fl oz": "ml", "cup": "ml",
	"km/h": "mph", "mph": "km/h", "kn": "km/h",
	"°C": "°F", "°F": "°C", "K": "°C",
	"ha": "acre", "acre": "ha", "m2": "ft2", "ft2": "m2",
}

// currency symbols and names that mean one currency.
var currencyAlias = map[string]string{
	"$": "USD", "us$": "USD", "dollar": "USD", "dollars": "USD",
	"€": "EUR", "euro": "EUR", "euros": "EUR",
	"£": "GBP", "¥": "JPY", "yen": "JPY", "円": "JPY", "元": "CNY", "yuan": "CNY",
	"nt$": "TWD", "₩": "KRW", "won": "KRW", "₹": "INR", "rupee": "INR", "rupees": "INR",
	"₿": "BTC", "bitcoin": "BTC", "bitcoins": "BTC", "ether": "ETH", "ethereum": "ETH",
	"solana": "SOL", "dogecoin": "DOGE", "tether": "USDT",
}

var (
	// "10 km to mi", "10km in miles", "$100 = jpy", "100 usd"
	questionRe = regexp.MustCompile(`^([^\d\s.,-]{0,3})\s*(-?[\d][\d,]*(?:\.\d+)?|-?\.\d+)(?:e(-?\d+))?\s*([^\d=]*?)(?:(?:\s*\b(?:to|in|as|into)\b|\s*(?:=|->|→))\s*(.+?))?\s*$`)
	digitRe    = regexp.MustCompile(`\d`)
	opRe       = regexp.MustCompile(`[-+*/×÷^%()]|sqrt|√|sin|cos|tan|log|ln|π|pi`)
)

// Evaluate answers a question, if it is one. rates may be nil.
func Evaluate(q string, rates *Rates) (Answer, bool) {
	q = strings.TrimSpace(q)
	if q == "" || !digitRe.MatchString(q) {
		return Answer{}, false
	}
	if a, ok := conversion(q, rates); ok {
		return a, true
	}
	return arithmetic(q)
}

func arithmetic(q string) (Answer, bool) {
	expr := strings.TrimPrefix(q, "=")
	if !opRe.MatchString(strings.ToLower(expr)) {
		return Answer{}, false // a bare number answers nothing
	}
	v, err := calc.Eval(expr, true)
	if err != nil {
		return Answer{}, false
	}
	s := calc.Format(v)
	return Answer{Text: "= " + s, Detail: strings.TrimSpace(expr), Copy: s}, true
}

func conversion(q string, rates *Rates) (Answer, bool) {
	m := questionRe.FindStringSubmatch(strings.ToLower(q))
	if m == nil {
		return Answer{}, false
	}
	prefix, num, exp, from, to := strings.TrimSpace(m[1]), m[2], m[3], strings.TrimSpace(m[4]), strings.TrimSpace(m[5])
	v, err := strconv.ParseFloat(strings.ReplaceAll(num, ",", ""), 64)
	if err != nil {
		return Answer{}, false
	}
	if exp != "" {
		e, _ := strconv.Atoi(exp)
		v *= math.Pow(10, float64(e))
	}
	if prefix != "" { // "$100"
		if from != "" {
			return Answer{}, false
		}
		from = prefix
	}
	if from == "" {
		return Answer{}, false
	}
	if a, ok := currency(v, from, to, rates); ok {
		return a, true
	}
	fu, ok := units[from]
	if !ok {
		fu, ok = units[strings.TrimSuffix(from, ".")]
	}
	if !ok {
		return Answer{}, false
	}
	if to == "" {
		to = defaultTarget[fu.symbol]
		if to == "" {
			return Answer{}, false
		}
	}
	tu, ok := units[to]
	if !ok || tu.kind != fu.kind {
		return Answer{}, false
	}
	base := v*fu.factor + fu.offset
	out := (base - tu.offset) / tu.factor
	s := number(out)
	return Answer{
		Text:   s + " " + tu.symbol,
		Detail: i18n.Tf("%s %s in %s", number(v), fu.symbol, i18n.T(tu.name)),
		Copy:   s,
	}, true
}

func currency(v float64, from, to string, rates *Rates) (Answer, bool) {
	if rates == nil {
		return Answer{}, false
	}
	code := func(s string) string {
		if c, ok := currencyAlias[s]; ok {
			return c
		}
		if _, ok := units[s]; ok {
			return "" // "cup", "gal": units first, though Cuba's peso is CUP
		}
		c := strings.ToUpper(s)
		if len(s) == 3 || (len(s) >= 2 && len(s) <= 6 && rates.More[c] > 0) { // "doge", "usdt"
			return c
		}
		return ""
	}
	fc, tc := code(from), code(to)
	if fc == "" || tc == "" || to == "" {
		return Answer{}, false
	}
	fr, cb1, ok1 := rates.perBase(fc)
	tr, cb2, ok2 := rates.perBase(tc)
	if !ok1 || !ok2 {
		return Answer{}, false
	}
	s := amount(v / fr * tr)
	source := i18n.Tf("rates of %s", rates.Date)
	if cb1 || cb2 {
		source = i18n.Tf("prices from Coinbase · %s", rates.MoreGot.Local().Format("15:04"))
	}
	return Answer{
		Text:   s + " " + tc,
		Detail: i18n.Tf("%s %s in %s · %s", amount(v), fc, tc, source),
		Copy:   strings.ReplaceAll(s, ",", ""),
	}, true
}

// amount shows money: two decimals, or four significant digits below one
// (0.001205 BTC).
func amount(v float64) string {
	a := math.Abs(v)
	if a == 0 || a >= 1 {
		return money(v)
	}
	digits := 4 - int(math.Floor(math.Log10(a))) - 1
	return strconv.FormatFloat(v, 'f', min(digits, 12), 64)
}

// number shows a converted value to six significant digits.
func number(v float64) string {
	if v == 0 {
		return "0"
	}
	if a := math.Abs(v); a >= 1e12 || a < 1e-6 {
		return calc.Format(v)
	}
	s := strconv.FormatFloat(v, 'g', 6, 64)
	if strings.ContainsRune(s, 'e') {
		s = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return s
}

// money shows an amount with two decimals and thousands separators.
func money(v float64) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', 2, 64)
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String() + "." + frac
	if v < 0 {
		out = "-" + out
	}
	return out
}

// The exchange rate services; tests point them elsewhere. Frankfurter has
// the European Central Bank's rates for about thirty currencies, once a
// working day; when it is down, allratestoday.com serves the Bank of
// England's daily rates for about twenty, published independently of the
// ECB; Coinbase's public rates add the cryptocurrencies.
var (
	RatesURL    = "https://api.frankfurter.app/latest"
	FallbackURL = "https://allratestoday.com/api/open/central-bank/boe"
	MoreURL     = "https://api.coinbase.com/v2/exchange-rates?currency=USD"

	// ServiceTimeout bounds each service's answer, so one that hangs
	// leaves time to ask the others.
	ServiceTimeout = 5 * time.Second
)

// How long cached rates are used before fetching again: the ECB's change
// daily, Coinbase's by the minute.
const (
	RatesMaxAge = 12 * time.Hour
	MoreMaxAge  = 10 * time.Minute
)

// FetchRates reads the latest rates from both services. One of them is
// enough: without the ECB's, the Bank of England's or, failing those too,
// Coinbase's dollar rates stand in.
func FetchRates(ctx context.Context, c *http.Client) (*Rates, error) {
	if c.Timeout == 0 || c.Timeout > ServiceTimeout {
		limited := *c
		limited.Timeout = ServiceTimeout // per request
		c = &limited
	}
	r, err := fetchECB(ctx, c)
	if err != nil {
		if r2, err2 := fetchBoE(ctx, c); err2 == nil {
			r, err = r2, nil
		} else {
			err = errors.Join(err, err2)
		}
	}
	more, merr := fetchCoinbase(ctx, c)
	switch {
	case err != nil && merr != nil:
		return nil, errors.Join(err, merr)
	case err != nil:
		r = &Rates{Base: "USD", Date: time.Now().Format("2006-01-02"), Rates: map[string]float64{}, Got: time.Now()}
	}
	if merr == nil {
		r.More, r.MoreGot = more, time.Now()
	}
	return r, nil
}

// skipped fails a service named in $INUA_RATES_SKIP ("ecb", "boe",
// "coinbase", comma-separated), to try the fallbacks without taking the
// network away.
func skipped(service string) error {
	for _, s := range strings.Split(os.Getenv("INUA_RATES_SKIP"), ",") {
		if strings.TrimSpace(s) == service {
			return fmt.Errorf("%s: skipped by INUA_RATES_SKIP", service)
		}
	}
	return nil
}

// fetchCoinbase reads Coinbase's rates per US dollar.
func fetchCoinbase(ctx context.Context, c *http.Client) (map[string]float64, error) {
	if err := skipped("coinbase"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, MoreURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	var body struct {
		Data struct {
			Currency string            `json:"currency"`
			Rates    map[string]string `json:"rates"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Data.Currency != "USD" || len(body.Data.Rates) == 0 {
		return nil, fmt.Errorf("%s: no rates", req.URL.Host)
	}
	out := make(map[string]float64, len(body.Data.Rates))
	for k, v := range body.Data.Rates {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			out[k] = f
		}
	}
	return out, nil
}

// fetchECB reads the European Central Bank's rates (from Frankfurter).
func fetchECB(ctx context.Context, c *http.Client) (*Rates, error) {
	if err := skipped("ecb"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, RatesURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	var r Rates
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Base == "" || len(r.Rates) == 0 {
		return nil, fmt.Errorf("%s: no rates", req.URL.Host)
	}
	r.Got = time.Now()
	return &r, nil
}

// fetchBoE reads the Bank of England's rates from allratestoday.com, whose
// table lists each pair as "1 base = value quote", and brings them to one
// pound sterling.
func fetchBoE(ctx context.Context, c *http.Client) (*Rates, error) {
	if err := skipped("boe"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, FallbackURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	var body struct {
		Date  string `json:"rate_date"`
		Rates []struct {
			Base, Quote, Type string
			Value             float64
		} `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	const home = "GBP"
	r := &Rates{Base: home, Date: body.Date, Rates: map[string]float64{}, Got: time.Now()}
	for _, q := range body.Rates {
		if q.Type != "reference" || q.Value <= 0 {
			continue
		}
		switch {
		case q.Base == home && q.Quote != home:
			r.Rates[q.Quote] = q.Value
		case q.Quote == home && q.Base != home:
			r.Rates[q.Base] = 1 / q.Value
		}
	}
	if body.Date == "" || len(r.Rates) == 0 {
		return nil, fmt.Errorf("%s: no rates", req.URL.Host)
	}
	return r, nil
}

// CachePath is where rates are kept between runs.
func CachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil { // no home: the runtime directory, checked
		if dir, err = rundir.Dir(); err != nil {
			return ""
		}
		return filepath.Join(dir, "rates.json")
	}
	return filepath.Join(dir, "inua", "rates.json")
}

// LoadRates reads cached rates; nil if there are none.
func LoadRates(path string) *Rates {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var r Rates
	if json.Unmarshal(data, &r) != nil || len(r.Rates)+len(r.More) == 0 {
		return nil
	}
	return &r
}

// SaveRates caches rates.
func SaveRates(path string, r *Rates) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
