package weather

import (
	"slices"
	"time"
)

// SourceID is the chosen source's ID: Open-Meteo when none was chosen. It
// may be one this version does not know (SourceByID says so); it is kept as
// it is until the user chooses another.
func (d Data) SourceID() string {
	if d.Source == "" {
		return OpenMeteo
	}
	return d.Source
}

// Current is the chosen city, Selected kept within the list.
func (d *Data) Current() (Place, bool) {
	if len(d.Places) == 0 {
		d.Selected = 0
		return Place{}, false
	}
	d.Selected = max(0, min(d.Selected, len(d.Places)-1))
	return d.Places[d.Selected], true
}

// Select chooses the city at i; false if there is none there.
func (d *Data) Select(i int) bool {
	if i < 0 || i >= len(d.Places) {
		return false
	}
	d.Selected = i
	return true
}

// Add adds a city and chooses it; one already in the list (the same
// coordinates) is chosen instead of added again.
func (d *Data) Add(p Place) {
	if i := slices.IndexFunc(d.Places, func(q Place) bool { return q.Lat == p.Lat && q.Lon == p.Lon }); i >= 0 {
		d.Selected = i
		return
	}
	d.Places = append(d.Places, p)
	d.Selected = len(d.Places) - 1
}

// Remove removes the city at i. The chosen city stays chosen; when it is
// the one removed, the one before it is (the next, for the first).
func (d *Data) Remove(i int) bool {
	if i < 0 || i >= len(d.Places) {
		return false
	}
	d.Places = slices.Delete(d.Places, i, i+1)
	if d.Selected > i || d.Selected == i && i > 0 {
		d.Selected--
	}
	d.Current()
	return true
}

// Key is what a forecast is of: a source, a place and the units. A
// forecast is shown only for the key of what is chosen now.
type Key struct {
	Source     string
	Lat, Lon   float64
	Fahrenheit bool
}

// KeyOf is the key of a place's forecast as d would read it.
func (d Data) KeyOf(p Place) Key {
	return Key{Source: d.SourceID(), Lat: p.Lat, Lon: p.Lon, Fahrenheit: d.Fahrenheit}
}

// Loads says which answer still counts. Each change of the city, the
// source or the units begins a load, and only an answer to the newest
// counts; Stop, when there is nothing to read (no city, an unknown
// source), makes every answer on its way count for nothing.
type Loads struct {
	gen int
	key Key
	on  bool
}

// Ticket is a request's: what Current checks its answer against.
type Ticket struct {
	gen int
	Key Key
}

// Begin begins reading key, and leaves every earlier ticket stale.
func (l *Loads) Begin(k Key) Ticket {
	l.gen++
	l.key, l.on = k, true
	return Ticket{gen: l.gen, Key: k}
}

// Stop leaves every ticket stale, with nothing being read.
func (l *Loads) Stop() {
	l.gen++
	l.on = false
}

// Current says whether an answer to t is still wanted.
func (l *Loads) Current(t Ticket) bool {
	return l.on && t.gen == l.gen && t.Key == l.key
}

// Temps are the temperatures now at the cities, for the sidebar. They are
// kept by key, so a value read from another source or in other units is
// never shown for this one.
type Temps struct {
	m map[Key]stamped
}

type stamped struct {
	v  float64
	at time.Time
}

// Put keeps the temperature for k, read at at.
func (t *Temps) Put(k Key, v float64, at time.Time) {
	if t.m == nil {
		t.m = map[Key]stamped{}
	}
	t.m[k] = stamped{v, at}
}

// Get is the temperature for k, if it was read.
func (t *Temps) Get(k Key) (float64, bool) {
	s, ok := t.m[k]
	return s.v, ok
}

// Fresh says whether the temperature for k was read within age of now.
func (t *Temps) Fresh(k Key, now time.Time, age time.Duration) bool {
	s, ok := t.m[k]
	return ok && now.Sub(s.at) < age
}
