// Package mpris makes a player an MPRIS player on the session bus
// (org.mpris.MediaPlayer2 and its Player), so the control center, media
// keys and the desk functions see and drive it (docs/feat/players.md).
package mpris

import (
	"strconv"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/lyricat/inua/internal/dbusexport"
)

const (
	path        = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	rootIface   = "org.mpris.MediaPlayer2"
	playerIface = "org.mpris.MediaPlayer2.Player"
	propsIface  = "org.freedesktop.DBus.Properties"
)

// Handler is what the player does when asked over the bus. Its functions
// run on the bus's goroutines; nil ones are not offered.
type Handler struct {
	Raise                                        func()
	PlayPause, Play, Pause, Stop, Next, Previous func()
	// Seek moves by an offset; SetPosition goes to a place.
	Seek        func(offset time.Duration)
	SetPosition func(pos time.Duration)
	SetVolume   func(v float64)
	// Position is where it plays now.
	Position func() time.Duration
}

// State is what the player shows.
type State struct {
	Status                    string // "Playing", "Paused" or "Stopped"
	Title, Album, ArtURL, URL string
	Artists                   []string
	Length                    time.Duration
	Volume                    float64
	CanNext, CanPrev, CanSeek bool
	Track                     int // a number for the track, for its id
}

// Server is a player on the bus.
type Server struct {
	conn     *dbus.Conn
	name     string
	identity string
	desktop  string
	h        Handler

	mu sync.Mutex
	st State
}

// Serve puts a player on conn as org.mpris.MediaPlayer2.NAME ("inua.music").
func Serve(conn *dbus.Conn, name, identity, desktopEntry string, h Handler) (*Server, error) {
	s := &Server{conn: conn, name: "org.mpris.MediaPlayer2." + name, identity: identity, desktop: desktopEntry, h: h,
		st: State{Status: "Stopped", Volume: 1}}
	for iface, v := range map[string]any{rootIface: root{s}, playerIface: player{s}, propsIface: props{s}} {
		if err := dbusexport.ExportWithMap(conn, v, map[string]string{"SeekBy": "Seek"}, path, iface); err != nil {
			return nil, err
		}
	}
	if err := dbusexport.Export(conn, introspect.Introspectable(introspection), path, "org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, err
	}
	if _, err := conn.RequestName(s.name, dbus.NameFlagDoNotQueue); err != nil {
		return nil, err
	}
	return s, nil
}

// Close leaves the bus.
func (s *Server) Close() {
	s.conn.ReleaseName(s.name)
	for _, iface := range []string{rootIface, playerIface, propsIface, "org.freedesktop.DBus.Introspectable"} {
		dbusexport.Export(s.conn, nil, path, iface)
	}
}

// Update shows a new state, telling the bus what changed.
func (s *Server) Update(st State) {
	s.mu.Lock()
	old := s.playerProps()
	s.st = st
	now := s.playerProps()
	s.mu.Unlock()
	changed := map[string]dbus.Variant{}
	for k, v := range now {
		if k == "Position" {
			continue // asked for, not told
		}
		if o, ok := old[k]; !ok || o.String() != v.String() {
			changed[k] = v
		}
	}
	if len(changed) > 0 {
		s.conn.Emit(path, propsIface+".PropertiesChanged", playerIface, changed, []string{})
	}
}

// Seeked tells the bus the position jumped.
func (s *Server) Seeked(pos time.Duration) {
	s.conn.Emit(path, playerIface+".Seeked", int64(pos/time.Microsecond))
}

func (s *Server) trackID() dbus.ObjectPath {
	return dbus.ObjectPath("/io/github/lyricat/inua/track/" + strconv.Itoa(s.st.Track))
}

// playerProps are the Player's properties; s.mu held.
func (s *Server) playerProps() map[string]dbus.Variant {
	st := s.st
	meta := map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(s.trackID())}
	if st.Title != "" {
		meta["xesam:title"] = dbus.MakeVariant(st.Title)
	}
	if len(st.Artists) > 0 {
		meta["xesam:artist"] = dbus.MakeVariant(st.Artists)
	}
	if st.Album != "" {
		meta["xesam:album"] = dbus.MakeVariant(st.Album)
	}
	if st.Length > 0 {
		meta["mpris:length"] = dbus.MakeVariant(int64(st.Length / time.Microsecond))
	}
	if st.ArtURL != "" {
		meta["mpris:artUrl"] = dbus.MakeVariant(st.ArtURL)
	}
	if st.URL != "" {
		meta["xesam:url"] = dbus.MakeVariant(st.URL)
	}
	var pos int64
	if s.h.Position != nil {
		pos = int64(s.h.Position() / time.Microsecond)
	}
	loaded := st.Title != "" || st.URL != ""
	return map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant(st.Status),
		"LoopStatus":     dbus.MakeVariant("None"),
		"Rate":           dbus.MakeVariant(1.0),
		"Shuffle":        dbus.MakeVariant(false),
		"Metadata":       dbus.MakeVariant(meta),
		"Volume":         dbus.MakeVariant(st.Volume),
		"Position":       dbus.MakeVariant(pos),
		"MinimumRate":    dbus.MakeVariant(1.0),
		"MaximumRate":    dbus.MakeVariant(1.0),
		"CanGoNext":      dbus.MakeVariant(st.CanNext && s.h.Next != nil),
		"CanGoPrevious":  dbus.MakeVariant(st.CanPrev && s.h.Previous != nil),
		"CanPlay":        dbus.MakeVariant(loaded && s.h.Play != nil),
		"CanPause":       dbus.MakeVariant(loaded && s.h.Pause != nil),
		"CanSeek":        dbus.MakeVariant(st.CanSeek && s.h.SetPosition != nil),
		"CanControl":     dbus.MakeVariant(true),
	}
}

func (s *Server) rootProps() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"CanQuit":             dbus.MakeVariant(false),
		"CanRaise":            dbus.MakeVariant(s.h.Raise != nil),
		"HasTrackList":        dbus.MakeVariant(false),
		"Identity":            dbus.MakeVariant(s.identity),
		"DesktopEntry":        dbus.MakeVariant(s.desktop),
		"SupportedUriSchemes": dbus.MakeVariant([]string{"file"}),
		"SupportedMimeTypes":  dbus.MakeVariant([]string{}),
	}
}

func call(f func()) *dbus.Error {
	if f != nil {
		f()
	}
	return nil
}

type root struct{ s *Server }

func (r root) Raise() *dbus.Error { return call(r.s.h.Raise) }
func (r root) Quit() *dbus.Error  { return nil }

type player struct{ s *Server }

func (p player) PlayPause() *dbus.Error { return call(p.s.h.PlayPause) }
func (p player) Play() *dbus.Error      { return call(p.s.h.Play) }
func (p player) Pause() *dbus.Error     { return call(p.s.h.Pause) }
func (p player) Stop() *dbus.Error      { return call(p.s.h.Stop) }
func (p player) Next() *dbus.Error      { return call(p.s.h.Next) }
func (p player) Previous() *dbus.Error  { return call(p.s.h.Previous) }
func (p player) SeekBy(us int64) *dbus.Error { // D-Bus: Seek
	if p.s.h.Seek != nil {
		p.s.h.Seek(time.Duration(us) * time.Microsecond)
	}
	return nil
}
func (p player) SetPosition(track dbus.ObjectPath, us int64) *dbus.Error {
	p.s.mu.Lock()
	cur := p.s.trackID()
	p.s.mu.Unlock()
	if track == cur && p.s.h.SetPosition != nil {
		p.s.h.SetPosition(time.Duration(us) * time.Microsecond)
	}
	return nil
}
func (p player) OpenUri(string) *dbus.Error { return nil }

type props struct{ s *Server }

func (p props) all(iface string) (map[string]dbus.Variant, *dbus.Error) {
	switch iface {
	case rootIface:
		return p.s.rootProps(), nil
	case playerIface:
		p.s.mu.Lock()
		defer p.s.mu.Unlock()
		return p.s.playerProps(), nil
	}
	return nil, dbus.MakeFailedError(errUnknown(iface))
}

func (p props) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	all, err := p.all(iface)
	if err != nil {
		return dbus.Variant{}, err
	}
	v, ok := all[name]
	if !ok {
		return dbus.Variant{}, dbus.MakeFailedError(errUnknown(name))
	}
	return v, nil
}

func (p props) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) { return p.all(iface) }

func (p props) Set(iface, name string, v dbus.Variant) *dbus.Error {
	if iface == playerIface && name == "Volume" && p.s.h.SetVolume != nil {
		if f, ok := v.Value().(float64); ok {
			p.s.h.SetVolume(min(max(f, 0), 1))
			return nil
		}
	}
	return dbus.MakeFailedError(errUnknown(name))
}

type errUnknown string

func (e errUnknown) Error() string { return "no property or interface " + string(e) }

const introspection = `<node>
 <interface name="org.mpris.MediaPlayer2">
  <method name="Raise"/><method name="Quit"/>
  <property name="CanQuit" type="b" access="read"/><property name="CanRaise" type="b" access="read"/>
  <property name="HasTrackList" type="b" access="read"/><property name="Identity" type="s" access="read"/>
  <property name="DesktopEntry" type="s" access="read"/>
  <property name="SupportedUriSchemes" type="as" access="read"/><property name="SupportedMimeTypes" type="as" access="read"/>
 </interface>
 <interface name="org.mpris.MediaPlayer2.Player">
  <method name="Next"/><method name="Previous"/><method name="Pause"/><method name="PlayPause"/>
  <method name="Stop"/><method name="Play"/>
  <method name="Seek"><arg name="Offset" type="x" direction="in"/></method>
  <method name="SetPosition"><arg name="TrackId" type="o" direction="in"/><arg name="Position" type="x" direction="in"/></method>
  <method name="OpenUri"><arg name="Uri" type="s" direction="in"/></method>
  <signal name="Seeked"><arg name="Position" type="x"/></signal>
  <property name="PlaybackStatus" type="s" access="read"/><property name="LoopStatus" type="s" access="read"/>
  <property name="Rate" type="d" access="read"/><property name="Shuffle" type="b" access="read"/>
  <property name="Metadata" type="a{sv}" access="read"/><property name="Volume" type="d" access="readwrite"/>
  <property name="Position" type="x" access="read"/><property name="MinimumRate" type="d" access="read"/>
  <property name="MaximumRate" type="d" access="read"/><property name="CanGoNext" type="b" access="read"/>
  <property name="CanGoPrevious" type="b" access="read"/><property name="CanPlay" type="b" access="read"/>
  <property name="CanPause" type="b" access="read"/><property name="CanSeek" type="b" access="read"/>
  <property name="CanControl" type="b" access="read"/>
 </interface>
 <interface name="org.freedesktop.DBus.Properties">
  <method name="Get"><arg type="s" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="out"/></method>
  <method name="GetAll"><arg type="s" direction="in"/><arg type="a{sv}" direction="out"/></method>
  <method name="Set"><arg type="s" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="in"/></method>
  <signal name="PropertiesChanged"><arg type="s"/><arg type="a{sv}"/><arg type="as"/></signal>
 </interface>
</node>`
