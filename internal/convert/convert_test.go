package convert

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var rates = &Rates{Base: "EUR", Date: "2026-10-09", Rates: map[string]float64{"USD": 1.1, "JPY": 165, "TWD": 35}}

func TestEvaluate(t *testing.T) {
	for _, tc := range []struct{ q, text, copy string }{
		{"10 km to mi", "6.21371 mi", "6.21371"},
		{"10km in miles", "6.21371 mi", "6.21371"},
		{"5 kg", "11.0231 lb", "11.0231"},
		{"32 f in c", "0 °C", "0"},
		{"100 c to f", "212 °F", "212"},
		{"0 k in c", "-273.15 °C", "-273.15"},
		{"1 gib to mb", "1073.74 MB", "1073.74"},
		{"90 min to hours", "1.5 h", "1.5"},
		{"6 ft", "1.8288 m", "1.8288"},
		{"1,000 m in km", "1 km", "1"},
		{"110 usd to eur", "100.00 EUR", "100.00"},
		{"$11 to jpy", "1,650.00 JPY", "1650.00"},
		{"100 eur in twd", "3,500.00 TWD", "3500.00"},
		{"2^10 + 3", "= 1027", "1027"},
		{"=sqrt(16)*2", "= 8", "8"},
		{"15% * 80", "= 12", "12"},
	} {
		a, ok := Evaluate(tc.q, rates)
		if !ok || a.Text != tc.text || a.Copy != tc.copy {
			t.Errorf("Evaluate(%q) = %+v, %v; want %q copying %q", tc.q, a, ok, tc.text, tc.copy)
		}
	}
}

func TestNotQuestions(t *testing.T) {
	for _, q := range []string{"", "firefox", "42", "10 km to kg", "100 usd", "100 xyz to abc", "2048 game", "7zip"} {
		if a, ok := Evaluate(q, rates); ok {
			t.Errorf("Evaluate(%q) = %+v, want nothing", q, a)
		}
	}
	if _, ok := Evaluate("100 usd to eur", nil); ok {
		t.Error("currencies without rates should answer nothing")
	}
}

// rateServers serves the ECB's rates at /ecb, the Bank of England's at
// /boe and Coinbase's at /cb, each failing when its flag is set (the Bank
// of England's with the ECB's unless boeUp is set).
func rateServers(t *testing.T, ecbDown, cbDown *bool) *httptest.Server {
	return rateServersWithBoE(t, ecbDown, cbDown, new(bool))
}

func rateServersWithBoE(t *testing.T, ecbDown, cbDown, boeUp *bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ecb" && !*ecbDown:
			w.Write([]byte(`{"amount":1.0,"base":"EUR","date":"2026-10-09","rates":{"USD":1.1,"JPY":165.0}}`))
		case r.URL.Path == "/boe" && (!*ecbDown || *boeUp):
			w.Write([]byte(`{"bank":"boe","rate_date":"2026-10-08","rates":[` +
				`{"base":"GBP","quote":"USD","type":"reference","value":1.25},` +
				`{"base":"EUR","quote":"GBP","type":"reference","value":0.8},` +
				`{"base":"GBP","quote":"JPY","type":"buy","value":190.0}]}`))
		case r.URL.Path == "/cb" && !*cbDown:
			w.Write([]byte(`{"data":{"currency":"USD","rates":{"USD":"1","EUR":"0.9090909","BTC":"0.00001","ETH":"0.0004","DOGE":"10","CUP":"24","BAD":"x"}}}`))
		default:
			http.Error(w, "down", http.StatusBadGateway)
		}
	}))
	t.Cleanup(srv.Close)
	oldR, oldF, oldM := RatesURL, FallbackURL, MoreURL
	RatesURL, FallbackURL, MoreURL = srv.URL+"/ecb", srv.URL+"/boe", srv.URL+"/cb"
	t.Cleanup(func() { RatesURL, FallbackURL, MoreURL = oldR, oldF, oldM })
	return srv
}

func TestBankOfEnglandFallback(t *testing.T) {
	var ecbDown, cbDown, boeUp bool
	srv := rateServersWithBoE(t, &ecbDown, &cbDown, &boeUp)
	// With the ECB's rates the Bank of England's are not asked for.
	r, err := FetchRates(context.Background(), srv.Client())
	if err != nil || r.Base != "EUR" {
		t.Fatalf("%+v %v", r, err)
	}
	// Without them, the Bank of England's, brought to the pound: both
	// orientations, reference rates only.
	ecbDown, boeUp = true, true
	r, err = FetchRates(context.Background(), srv.Client())
	if err != nil || r.Base != "GBP" || r.Date != "2026-10-08" || len(r.Rates) != 2 {
		t.Fatalf("without the ECB: %+v %v", r, err)
	}
	for q, want := range map[string]string{
		"10 eur to usd": "10.00 USD", // 10 EUR = 8 GBP = 10 USD
		"1 gbp to eur":  "1.25 EUR",
		"1 btc in eur":  "", // Coinbase's dollar, which the Bank of England's table has, then the pound
	} {
		a, ok := Evaluate(q, r)
		if q == "1 btc in eur" {
			if !ok || !strings.HasSuffix(a.Text, " EUR") {
				t.Errorf("Evaluate(%q) = %+v", q, a)
			}
			continue
		}
		if got := map[bool]string{true: a.Text}[ok]; got != want {
			t.Errorf("Evaluate(%q) = %q, want %q", q, got, want)
		}
	}
	if a, _ := Evaluate("1 usd to gbp", r); !strings.Contains(a.Detail, "rates of 2026-10-08") {
		t.Errorf("fiat detail %q", a.Detail)
	}
}

func TestCrypto(t *testing.T) {
	var ecbDown, cbDown bool
	srv := rateServers(t, &ecbDown, &cbDown)
	r, err := FetchRates(context.Background(), srv.Client())
	if err != nil || r.More["BTC"] != 0.00001 || r.MoreGot.IsZero() || r.Rates["JPY"] != 165 {
		t.Fatalf("%+v %v", r, err)
	}
	for q, want := range map[string]string{
		"1 eth to usd":      "2,500.00 USD",
		"1 btc in eur":      "90,909.09 EUR", // through the ECB's dollar: 100,000 USD at 1.1 a euro
		"100 usd to btc":    "0.001000 BTC",
		"2 bitcoin to jpy":  "30,000,000.00 JPY",
		"1000 doge to usdt": "",           // no USDT price: no answer
		"100 jpy to usd":    "0.6667 USD", // the ECB's rates, small amounts in significant digits
	} {
		a, ok := Evaluate(q, r)
		if got := map[bool]string{true: a.Text}[ok]; got != want {
			t.Errorf("Evaluate(%q) = %q, want %q", q, got, want)
		}
	}
	if a, _ := Evaluate("1 eth to usd", r); !strings.Contains(a.Detail, "Coinbase") {
		t.Errorf("crypto detail %q", a.Detail)
	}
	if a, _ := Evaluate("1 usd to jpy", r); !strings.Contains(a.Detail, "rates of 2026-10-09") {
		t.Errorf("fiat detail %q", a.Detail)
	}
	// A unit stays a unit, though Coinbase knows CUP.
	if a, ok := Evaluate("1 cup to ml", r); !ok || !strings.HasSuffix(a.Text, "ml") {
		t.Errorf("1 cup to ml = %+v", a)
	}

	// One service down: the other answers.
	ecbDown = true
	r, err = FetchRates(context.Background(), srv.Client())
	if err != nil || r.Base != "USD" {
		t.Fatalf("without the ECB: %+v %v", r, err)
	}
	if a, ok := Evaluate("1 eur to usd", r); !ok || a.Text != "1.10 USD" {
		t.Errorf("without the ECB, 1 eur to usd = %+v", a)
	}
	ecbDown, cbDown = false, true
	if r, err = FetchRates(context.Background(), srv.Client()); err != nil || r.More != nil {
		t.Fatalf("without Coinbase: %+v %v", r, err)
	}
	ecbDown = true
	if _, err = FetchRates(context.Background(), srv.Client()); err == nil {
		t.Error("both down, no error")
	}
}

func TestRates(t *testing.T) {
	var ecbDown, cbDown bool
	srv := rateServers(t, &ecbDown, &cbDown)
	r, err := FetchRates(context.Background(), srv.Client())
	if err != nil || r.Rates["JPY"] != 165 || r.Got.IsZero() {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Stale(time.Now()) || !r.Stale(time.Now().Add(MoreMaxAge+time.Minute)) {
		t.Error("Coinbase's prices go stale after MoreMaxAge")
	}
	path := filepath.Join(t.TempDir(), "rates.json")
	if err := SaveRates(path, r); err != nil {
		t.Fatal(err)
	}
	if got := LoadRates(path); got == nil || got.Rates["USD"] != 1.1 {
		t.Fatalf("read back %+v", got)
	}
	if LoadRates(filepath.Join(t.TempDir(), "none")) != nil {
		t.Error("missing cache should be nil")
	}
}
