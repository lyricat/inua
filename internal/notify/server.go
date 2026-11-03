package notify

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/lyricat/inua/internal/dbusexport"
)

const (
	Name  = "org.freedesktop.Notifications"
	Path  = dbus.ObjectPath("/org/freedesktop/Notifications")
	iface = Name
)

const serverXML = `<node>
 <interface name="org.freedesktop.Notifications">
  <method name="GetCapabilities"><arg type="as" direction="out"/></method>
  <method name="Notify">
   <arg name="app_name" type="s" direction="in"/>
   <arg name="replaces_id" type="u" direction="in"/>
   <arg name="app_icon" type="s" direction="in"/>
   <arg name="summary" type="s" direction="in"/>
   <arg name="body" type="s" direction="in"/>
   <arg name="actions" type="as" direction="in"/>
   <arg name="hints" type="a{sv}" direction="in"/>
   <arg name="expire_timeout" type="i" direction="in"/>
   <arg name="id" type="u" direction="out"/>
  </method>
  <method name="CloseNotification"><arg name="id" type="u" direction="in"/></method>
  <method name="GetServerInformation">
   <arg name="name" type="s" direction="out"/>
   <arg name="vendor" type="s" direction="out"/>
   <arg name="version" type="s" direction="out"/>
   <arg name="spec_version" type="s" direction="out"/>
  </method>
  <signal name="NotificationClosed"><arg name="id" type="u"/><arg name="reason" type="u"/></signal>
  <signal name="ActionInvoked"><arg name="id" type="u"/><arg name="action_key" type="s"/></signal>
  <signal name="ActivationToken"><arg name="id" type="u"/><arg name="activation_token" type="s"/></signal>
 </interface>` + introspect.IntrospectDeclarationString + `</node>`

// Handler receives what apps send. Both are called from D-Bus goroutines.
type Handler interface {
	// Notify is a new notification, or one replacing the notification with
	// the same ID.
	Notify(n Notification)
	// Close is an app withdrawing a notification (CloseNotification).
	Close(id uint32)
}

// Server is the notification server; its methods send the signals that
// tell apps what the user did.
type Server struct {
	conn *dbus.Conn
	h    Handler

	mu     sync.Mutex
	nextID uint32
	owner  bool // holds the name (not waiting in the queue)
}

// NewServer prepares a server on conn; Serve runs it.
func NewServer(conn *dbus.Conn, h Handler) *Server {
	return &Server{conn: conn, h: h, nextID: 1}
}

// SetNextID makes IDs start after ones already handed out (the center
// remembers notifications across restarts).
func (s *Server) SetNextID(id uint32) {
	s.mu.Lock()
	s.nextID = max(s.nextID, id)
	s.mu.Unlock()
}

// NewID hands out an ID for a notification of the shell's own.
func (s *Server) NewID() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	return id
}

// Owner reports whether the server holds the name. While another daemon
// (mako, dunst) has it, the server waits in the queue.
func (s *Server) Owner() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.owner
}

// Serve provides org.freedesktop.Notifications until ctx ends. If another
// process owns the name, it waits in the bus queue and takes over when that
// process leaves; owner is called whenever that changes.
func (s *Server) Serve(ctx context.Context, owner func(bool)) error {
	if err := dbusexport.Export(s.conn, methods{s}, Path, iface); err != nil {
		return err
	}
	defer dbusexport.Export(s.conn, nil, Path, iface)
	if err := dbusexport.Export(s.conn, introspect.Introspectable(serverXML), Path, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	defer dbusexport.Export(s.conn, nil, Path, "org.freedesktop.DBus.Introspectable")

	sigs := make(chan *dbus.Signal, 16)
	s.conn.Signal(sigs)
	defer s.conn.RemoveSignal(sigs)
	match := []dbus.MatchOption{dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchArg(0, Name)}
	if err := s.conn.AddMatchSignal(match...); err != nil {
		return err
	}
	defer s.conn.RemoveMatchSignal(match...)

	reply, err := s.conn.RequestName(Name, 0) // queue behind another daemon
	if err != nil {
		return err
	}
	defer s.conn.ReleaseName(Name)
	s.setOwner(reply == dbus.RequestNameReplyPrimaryOwner, owner)

	for {
		select {
		case <-ctx.Done():
			s.setOwner(false, owner)
			return nil
		case sig, ok := <-sigs:
			if !ok {
				return nil
			}
			// The bus also says NameAcquired for the connection's own
			// unique name when it joins; only Name's count.
			if len(sig.Body) == 0 || sig.Body[0] != Name {
				continue
			}
			switch sig.Name {
			case "org.freedesktop.DBus.NameAcquired":
				s.setOwner(true, owner)
			case "org.freedesktop.DBus.NameLost":
				s.setOwner(false, owner)
			}
		}
	}
}

func (s *Server) setOwner(on bool, notify func(bool)) {
	s.mu.Lock()
	changed := s.owner != on
	s.owner = on
	s.mu.Unlock()
	if changed && notify != nil {
		notify(on)
	}
}

// Closed tells the app its notification is gone, and why.
func (s *Server) Closed(id, reason uint32) {
	s.conn.Emit(Path, iface+".NotificationClosed", id, reason)
}

// Invoke tells the app the user chose an action ("default" for the
// notification itself). token, if set, lets the app raise its window.
func (s *Server) Invoke(id uint32, key, token string) {
	if token != "" {
		s.conn.Emit(Path, iface+".ActivationToken", id, token)
	}
	s.conn.Emit(Path, iface+".ActionInvoked", id, key)
}

// methods is the exported D-Bus object; Server's own methods stay off the
// bus.
type methods struct{ s *Server }

func (m methods) GetCapabilities() ([]string, *dbus.Error) {
	return []string{"actions", "body", "body-markup", "body-hyperlinks", "icon-static", "persistence"}, nil
}

func (m methods) GetServerInformation() (string, string, string, string, *dbus.Error) {
	return "inua", "inua", "1", "1.2", nil
}

func (m methods) CloseNotification(id uint32) *dbus.Error {
	m.s.h.Close(id)
	return nil
}

func (m methods) Notify(sender dbus.Sender, app string, replaces uint32, icon, summary, body string,
	actions []string, hints map[string]dbus.Variant, _ int32) (uint32, *dbus.Error) {
	s := m.s
	s.mu.Lock()
	id := replaces
	if id == 0 {
		id = s.nextID
		s.nextID++
	}
	s.mu.Unlock()

	n := Notification{
		ID:      id,
		App:     app,
		Icon:    icon,
		Summary: summary,
		Body:    Markup(body),
		Urgency: Normal,
		Time:    time.Now(),
		Sender:  string(sender),
	}
	for i := 0; i+1 < len(actions); i += 2 {
		if actions[i] == "default" {
			n.Default = true
			continue
		}
		n.Actions = append(n.Actions, Action{Key: actions[i], Label: actions[i+1]})
	}
	readHints(&n, hints)
	s.h.Notify(n)
	return id, nil
}

func readHints(n *Notification, hints map[string]dbus.Variant) {
	str := func(key string) string {
		v, _ := hints[key].Value().(string)
		return v
	}
	n.Entry = strings.TrimSuffix(str("desktop-entry"), ".desktop")
	n.ImgPath = filePath(str("image-path"))
	if n.ImgPath == "" {
		n.ImgPath = filePath(str("image_path"))
	}
	if strings.HasPrefix(n.Icon, "file://") {
		n.Icon = filePath(n.Icon)
	}
	switch u := hints["urgency"].Value().(type) {
	case byte:
		n.Urgency = Urgency(min(u, 2))
	case int32:
		n.Urgency = Urgency(min(max(u, 0), 2))
	case uint32:
		n.Urgency = Urgency(min(u, 2))
	}
	n.Transient, _ = hints["transient"].Value().(bool)
	n.Resident, _ = hints["resident"].Value().(bool)
	for _, key := range []string{"image-data", "image_data", "icon_data"} {
		if img := readImage(hints[key]); img != nil {
			n.Image = img
			break
		}
	}
}

// readImage reads an (iiibiiay) image: width, height, rowstride, has
// alpha, bits per sample, channels, data. Only 8-bit RGB and RGBA are
// kept.
func readImage(v dbus.Variant) *Image {
	// A struct arrives as its fields (and godbus then reports the variant
	// as "av"), so the field types are the check.
	f, ok := v.Value().([]any)
	if !ok || len(f) != 7 {
		return nil
	}
	w, _ := f[0].(int32)
	h, _ := f[1].(int32)
	stride, _ := f[2].(int32)
	alpha, _ := f[3].(bool)
	bits, _ := f[4].(int32)
	channels, _ := f[5].(int32)
	data, _ := f[6].([]byte)
	want := int32(3)
	if alpha {
		want = 4
	}
	if bits != 8 || channels != want {
		return nil
	}
	img := &Image{Width: int(w), Height: int(h), Stride: int(stride), Alpha: alpha, Data: data}
	if img.Pixels() == nil { // sizes that do not add up, worked out without overflow
		return nil
	}
	return img
}

// filePath turns a file:// URI into a path; a path stays, anything else is
// dropped.
func filePath(s string) string {
	switch {
	case strings.HasPrefix(s, "/"):
		return s
	case strings.HasPrefix(s, "file://"):
		if u, err := url.Parse(s); err == nil {
			return u.Path
		}
	}
	return ""
}
