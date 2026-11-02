package keyring

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/portal"
	"github.com/lyricat/inua/internal/proc"
)

const (
	// Name is the Secret Service's bus name.
	Name = "org.freedesktop.secrets"
	// OwnName tells inua-keyring apart from other Secret Services.
	OwnName = "io.github.lyricat.Inua.Keyring"

	basePath     = dbus.ObjectPath("/org/freedesktop/secrets")
	ifService    = "org.freedesktop.Secret.Service"
	ifCollection = "org.freedesktop.Secret.Collection"
	ifItem       = "org.freedesktop.Secret.Item"
	ifSession    = "org.freedesktop.Secret.Session"
	ifProps      = "org.freedesktop.DBus.Properties"
	ifIntrospect = "org.freedesktop.DBus.Introspectable"
)

// Secret is the specification's (oayays): a secret as a session sends it.
type Secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

var (
	errNoObject  = dbus.NewError("org.freedesktop.Secret.Error.NoSuchObject", []any{"No such object"})
	errNoSession = dbus.NewError("org.freedesktop.Secret.Error.NoSession", []any{"No such session"})
)

func failed(err error) *dbus.Error {
	if errors.Is(err, ErrNoCollection) || errors.Is(err, ErrNoItem) {
		return errNoObject
	}
	return dbus.MakeFailedError(err)
}

// Service serves a Store as the Secret Service.
type Service struct {
	conn  *dbus.Conn
	store *Store

	// foreign leaves out OwnName, so tests can stand in for gnome-keyring.
	foreign bool

	// OnRead hears of each secret handed out; first is the app's first
	// read since the service started. It runs on the request's goroutine.
	OnRead func(a Access, first bool)

	// Trust decides which apps read which secret
	// (docs/feat/keyring-trust.md); nil: every app reads any.
	Trust *Trust
	// Ask asks the user whether an app may read a secret; nil, or an
	// error: Untrusted answers.
	Ask func(ctx context.Context, q Question) (Answer, error)
	// Untrusted answers when nobody can be asked: AllowOnce or Deny
	// (keyring.untrusted, as it is when asked); nil: AllowOnce.
	Untrusted func() Answer

	mu       sync.Mutex
	sessions map[dbus.ObjectPath]*session
	nextSess int
	apps     map[string]app  // the clients, by unique name
	readers  map[string]bool // the apps that read a secret, by name
	// decisions are the answers kept for a process: allowed once or
	// denied, by unique name, then item.
	decisions map[string]map[string]Answer
	pending   map[string]*pending // questions on their way, by client and item
	later     []laterTrust        // apps to trust once the key can be read
}

// app is a client of the service: its program and PID, and the detail
// the record of reads keeps.
type app struct {
	name                string
	pid                 int
	exe, command, chain string
	id                  Identity // who it is, for the trust list
	idErr               error    // why it is not known
}

type session struct {
	owner string // the client's unique name
	c     crypter
}

// NewService prepares the service on conn; Serve runs it.
func NewService(conn *dbus.Conn, store *Store) *Service {
	s := &Service{conn: conn, store: store, sessions: map[dbus.ObjectPath]*session{}, apps: map[string]app{}, readers: map[string]bool{},
		decisions: map[string]map[string]Answer{}, pending: map[string]*pending{}}
	store.OnLock = func(unlocked bool) {
		if unlocked && s.Trust != nil {
			go s.trustLater()
		}
	}
	return s
}

// Serve provides the Secret Service until ctx ends. It fails when another
// process holds the name: inua-keyring does not queue behind gnome-keyring.
func (s *Service) Serve(ctx context.Context) error {
	for iface, v := range map[string]any{
		ifService: serviceObj{s}, ifCollection: collectionObj{s}, ifItem: itemObj{s},
		ifSession: sessionObj{s}, ifProps: propsObj{s}, ifIntrospect: introspectObj{s},
	} {
		if err := dbusexport.ExportSubtree(s.conn, v, basePath, iface); err != nil {
			return err
		}
		defer dbusexport.ExportSubtree(s.conn, nil, basePath, iface)
	}
	if !s.foreign {
		if err := dbusexport.Export(s.conn, ownObj{s}, ownPath, ownIface); err != nil {
			return err
		}
		defer dbusexport.Export(s.conn, nil, ownPath, ownIface)
		node := introspect.Node{Name: string(ownPath)}
		if err := dbusexport.Export(s.conn, introspect.NewIntrospectable(&node), ownPath, ifIntrospect); err == nil {
			defer dbusexport.Export(s.conn, nil, ownPath, ifIntrospect)
		}
	}
	// Sessions end with the clients that opened them.
	sigs := make(chan *dbus.Signal, 16)
	s.conn.Signal(sigs)
	defer s.conn.RemoveSignal(sigs)
	match := []dbus.MatchOption{dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged")}
	if err := s.conn.AddMatchSignal(match...); err != nil {
		return err
	}
	defer s.conn.RemoveMatchSignal(match...)

	names := []string{Name, OwnName}
	if s.foreign {
		names = names[:1]
	}
	for _, name := range names {
		reply, err := s.conn.RequestName(name, dbus.NameFlagDoNotQueue)
		if err != nil {
			return err
		}
		if reply != dbus.RequestNameReplyPrimaryOwner {
			return fmt.Errorf("%s is already served by another process (%s)", name, owner(s.conn, name))
		}
		defer s.conn.ReleaseName(name)
	}
	// The portal's Secret, for sandboxed apps (docs/portals.md).
	if !s.foreign {
		if unexport, err := portal.Export(s.conn, s.PortalSecret()); err != nil {
			log.Printf("portal: %v", err)
		} else {
			defer unexport()
			if err := portal.Own(s.conn, portal.KeyringName); err != nil {
				log.Printf("portal: %v", err)
			} else {
				defer s.conn.ReleaseName(portal.KeyringName)
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-sigs:
			if !ok {
				return nil
			}
			if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" && len(sig.Body) == 3 && sig.Body[2] == "" {
				gone, _ := sig.Body[0].(string)
				s.mu.Lock()
				for p, sess := range s.sessions {
					if sess.owner == gone {
						delete(s.sessions, p)
					}
				}
				delete(s.apps, gone)
				delete(s.decisions, gone)
				s.mu.Unlock()
			}
		}
	}
}

// Serving names the process serving the Secret Service, "zed (pid 4123)",
// and whether it is inua-keyring; "" when no one serves it.
func Serving(conn *dbus.Conn) (who string, ours bool) {
	var unique string
	if conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, Name).Store(&unique) != nil {
		return "", false
	}
	var own string
	ours = conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, OwnName).Store(&own) == nil && own == unique
	var pid uint32
	if conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, unique).Store(&pid) != nil {
		return unique, ours
	}
	return proc.Describe(int(pid)), ours
}

// owner names who holds a bus name, for messages.
func owner(conn *dbus.Conn, name string) string {
	var unique string
	if conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&unique) != nil {
		return "unknown"
	}
	var pid uint32
	if conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, unique).Store(&pid) == nil {
		return fmt.Sprintf("%s, pid %d", unique, pid)
	}
	return unique
}

// --- paths ---

func collPath(id string) dbus.ObjectPath       { return basePath + "/collection/" + dbus.ObjectPath(id) }
func itemPath(coll, id string) dbus.ObjectPath { return collPath(coll) + "/" + dbus.ObjectPath(id) }

type kind int

const (
	kindNone kind = iota
	kindService
	kindCollection
	kindItem
	kindSession
)

// parse says what a path is: the service, a collection (by ID or alias),
// an item, a session.
func (s *Service) parse(p dbus.ObjectPath) (k kind, coll, item string) {
	if p == basePath {
		return kindService, "", ""
	}
	rest, ok := strings.CutPrefix(string(p), string(basePath)+"/")
	if !ok {
		return kindNone, "", ""
	}
	parts := strings.Split(rest, "/")
	switch {
	case parts[0] == "session" && len(parts) == 2:
		return kindSession, "", parts[1]
	case parts[0] == "collection" || parts[0] == "aliases":
		if len(parts) < 2 || len(parts) > 3 {
			return kindNone, "", ""
		}
		coll = parts[1]
		if parts[0] == "aliases" {
			if coll = s.store.Alias(parts[1]); coll == "" {
				return kindNone, "", ""
			}
		}
		if _, ok := s.store.Collection(coll); !ok {
			return kindNone, "", ""
		}
		if len(parts) == 3 {
			return kindItem, coll, parts[2]
		}
		return kindCollection, coll, ""
	}
	return kindNone, "", ""
}

func msgPath(m dbus.Message) dbus.ObjectPath {
	p, _ := m.Headers[dbus.FieldPath].Value().(dbus.ObjectPath)
	return p
}

func sender(m dbus.Message) string {
	v, _ := m.Headers[dbus.FieldSender].Value().(string)
	return v
}

// item resolves an item's path.
func (s *Service) item(p dbus.ObjectPath) (Item, bool) {
	k, coll, id := s.parse(p)
	if k != kindItem {
		return Item{}, false
	}
	it, err := s.store.Item(coll, id)
	return it, err == nil
}

func (s *Service) session(p dbus.ObjectPath, who string) (*session, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[p]
	if sess == nil || sess.owner != who {
		return nil, errNoSession
	}
	return sess, nil
}

// sealed reads an item's secret for the client who, sealed for its
// session; gpg-agent's pinentry and OnRead hear which app it is for.
func (s *Service) sealed(it Item, sess *session, sp dbus.ObjectPath, who string) (Secret, *dbus.Error) {
	a := s.client(who)
	label := cmp.Or(it.Label, it.ID)
	asker := a.name
	if a.pid > 0 {
		asker = proc.Describe(a.pid)
	}
	read := Access{Time: time.Now(), App: a.name, PID: a.pid, Secret: label,
		Pass: s.store.Where(it.Collection, it.ID), Collection: it.Collection,
		Exe: a.exe, Command: a.command, Chain: a.chain, Bus: who}
	dec, unchecked, derr := s.permit(it, a, who, label)
	if derr != nil {
		s.denied(read, dec)
		return Secret{}, derr
	}
	secret, ct, err := s.store.Secret(it.Collection, it.ID, asker+"\t"+label)
	if err != nil {
		return Secret{}, failed(err)
	}
	defer clear(secret)
	if unchecked {
		// Trusted by a list whose seal is checked now that it unlocked.
		if dec, derr = s.recheck(it, a, who, label); derr != nil {
			s.denied(read, dec)
			return Secret{}, derr
		}
	}
	if s.Trust != nil {
		s.trustLater()
	}
	params, value, err := sess.c.seal(secret)
	if err != nil {
		return Secret{}, failed(err)
	}
	read.Decision = dec
	if s.OnRead != nil {
		s.mu.Lock()
		first := !s.readers[a.name]
		s.readers[a.name] = true
		s.mu.Unlock()
		s.OnRead(read, first)
	}
	return Secret{Session: sp, Parameters: params, Value: value, ContentType: ct}, nil
}

// denied reports a read the user did not allow.
func (s *Service) denied(read Access, dec string) {
	read.Decision = cmp.Or(dec, DecDenied)
	if s.OnRead != nil {
		s.OnRead(read, false)
	}
}

// client names the process behind a unique bus name, asking the bus once.
func (s *Service) client(unique string) app {
	s.mu.Lock()
	a, ok := s.apps[unique]
	s.mu.Unlock()
	if ok {
		return a
	}
	var pid uint32
	if s.conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, unique).Store(&pid) == nil {
		p := int(pid)
		a = app{name: proc.Name(p), pid: p, exe: proc.Exe(p), command: proc.Command(p), chain: proc.Chain(proc.Parent(p), 8)}
		if s.Trust != nil {
			a.id, a.idErr = s.identify(unique, p)
		}
	} else {
		a.idErr = errors.New("the bus did not say which process it is")
	}
	a.name = cmp.Or(a.name, unique)
	s.mu.Lock()
	s.apps[unique] = a
	s.mu.Unlock()
	return a
}

func (s *Service) opened(sec Secret, who string) ([]byte, *dbus.Error) {
	sess, derr := s.session(sec.Session, who)
	if derr != nil {
		return nil, derr
	}
	secret, err := sess.c.open(sec.Parameters, sec.Value)
	if err != nil {
		return nil, dbus.MakeFailedError(err)
	}
	return secret, nil
}

func (s *Service) emit(p dbus.ObjectPath, signal string, arg dbus.ObjectPath) {
	s.conn.Emit(p, signal, arg)
}

func (s *Service) changed(p dbus.ObjectPath, iface string, props map[string]dbus.Variant) {
	s.conn.Emit(p, ifProps+".PropertiesChanged", iface, props, []string{})
}

// --- org.freedesktop.Secret.Service ---

type serviceObj struct{ s *Service }

func (o serviceObj) at(m dbus.Message) *dbus.Error {
	if msgPath(m) != basePath {
		return errNoObject
	}
	return nil
}

func (o serviceObj) OpenSession(m dbus.Message, algorithm string, input dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	if err := o.at(m); err != nil {
		return dbus.MakeVariant(""), "/", err
	}
	var c crypter
	out := dbus.MakeVariant("")
	switch algorithm {
	case AlgPlain:
		c = plain{}
	case AlgDH:
		peer, ok := input.Value().([]byte)
		if !ok {
			return out, "/", dbus.MakeFailedError(errors.New("want the client's public key"))
		}
		key, err := newDHKey()
		if err != nil {
			return out, "/", dbus.MakeFailedError(err)
		}
		aesKey, err := key.aesKey(peer)
		if err != nil {
			return out, "/", dbus.MakeFailedError(err)
		}
		c = aesSession{aesKey}
		out = dbus.MakeVariant(key.pub.Bytes())
	default:
		return out, "/", dbus.NewError("org.freedesktop.DBus.Error.NotSupported", []any{"Algorithm not supported: " + algorithm})
	}
	o.s.mu.Lock()
	o.s.nextSess++
	p := basePath + dbus.ObjectPath(fmt.Sprintf("/session/s%d", o.s.nextSess))
	o.s.sessions[p] = &session{owner: sender(m), c: c}
	o.s.mu.Unlock()
	return out, p, nil
}

func (o serviceObj) CreateCollection(m dbus.Message, props map[string]dbus.Variant, alias string) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	if err := o.at(m); err != nil {
		return "/", "/", err
	}
	label, _ := props[ifCollection+".Label"].Value().(string)
	before := o.s.store.Alias(alias)
	c, err := o.s.store.CreateCollection(label, alias)
	if err != nil {
		return "/", "/", failed(err)
	}
	if alias == "" || before == "" {
		o.s.emit(basePath, ifService+".CollectionCreated", collPath(c.ID))
		o.s.changed(basePath, ifService, map[string]dbus.Variant{"Collections": dbus.MakeVariant(o.s.collections())})
	}
	return collPath(c.ID), "/", nil
}

func (s *Service) collections() []dbus.ObjectPath {
	var out []dbus.ObjectPath
	for _, c := range s.store.Collections() {
		out = append(out, collPath(c.ID))
	}
	return out
}

func (o serviceObj) SearchItems(m dbus.Message, attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	if err := o.at(m); err != nil {
		return nil, nil, err
	}
	unlocked := []dbus.ObjectPath{}
	for _, it := range o.s.store.Search("", attrs) {
		unlocked = append(unlocked, itemPath(it.Collection, it.ID))
	}
	return unlocked, []dbus.ObjectPath{}, nil
}

// Unlock and Lock: the lock is gpg's, so there is nothing to unlock and
// nothing locks.
func (o serviceObj) Unlock(m dbus.Message, objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	if err := o.at(m); err != nil {
		return nil, "/", err
	}
	out := []dbus.ObjectPath{}
	for _, p := range objects {
		if k, _, _ := o.s.parse(p); k == kindCollection || k == kindItem {
			out = append(out, p)
		}
	}
	return out, "/", nil
}

func (o serviceObj) Lock(m dbus.Message, objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	if err := o.at(m); err != nil {
		return nil, "/", err
	}
	return []dbus.ObjectPath{}, "/", nil
}

func (o serviceObj) GetSecrets(m dbus.Message, items []dbus.ObjectPath, sp dbus.ObjectPath) (map[dbus.ObjectPath]Secret, *dbus.Error) {
	if err := o.at(m); err != nil {
		return nil, err
	}
	sess, derr := o.s.session(sp, sender(m))
	if derr != nil {
		return nil, derr
	}
	out := map[dbus.ObjectPath]Secret{}
	for _, p := range items {
		it, ok := o.s.item(p)
		if !ok {
			continue // the specification: items that do not exist are left out
		}
		sec, derr := o.s.sealed(it, sess, sp, sender(m))
		if derr == errDenied && len(items) > 1 {
			continue // the others may be allowed
		}
		if derr != nil {
			return nil, derr
		}
		out[p] = sec
	}
	return out, nil
}

func (o serviceObj) ReadAlias(m dbus.Message, name string) (dbus.ObjectPath, *dbus.Error) {
	if err := o.at(m); err != nil {
		return "/", err
	}
	if id := o.s.store.Alias(name); id != "" {
		return collPath(id), nil
	}
	return "/", nil
}

func (o serviceObj) SetAlias(m dbus.Message, name string, coll dbus.ObjectPath) *dbus.Error {
	if err := o.at(m); err != nil {
		return err
	}
	id := ""
	if coll != "/" {
		k, c, _ := o.s.parse(coll)
		if k != kindCollection {
			return errNoObject
		}
		id = c
	}
	if err := o.s.store.SetAlias(name, id); err != nil {
		return failed(err)
	}
	return nil
}

// --- org.freedesktop.Secret.Collection ---

type collectionObj struct{ s *Service }

func (o collectionObj) at(m dbus.Message) (string, *dbus.Error) {
	k, coll, _ := o.s.parse(msgPath(m))
	if k != kindCollection {
		return "", errNoObject
	}
	return coll, nil
}

func (o collectionObj) Delete(m dbus.Message) (dbus.ObjectPath, *dbus.Error) {
	coll, derr := o.at(m)
	if derr != nil {
		return "/", derr
	}
	if err := o.s.store.DeleteCollection(coll); err != nil {
		return "/", failed(err)
	}
	if coll != Session {
		o.s.emit(basePath, ifService+".CollectionDeleted", collPath(coll))
		o.s.changed(basePath, ifService, map[string]dbus.Variant{"Collections": dbus.MakeVariant(o.s.collections())})
	}
	return "/", nil
}

func (o collectionObj) SearchItems(m dbus.Message, attrs map[string]string) ([]dbus.ObjectPath, *dbus.Error) {
	coll, derr := o.at(m)
	if derr != nil {
		return nil, derr
	}
	out := []dbus.ObjectPath{}
	for _, it := range o.s.store.Search(coll, attrs) {
		out = append(out, itemPath(coll, it.ID))
	}
	return out, nil
}

func (o collectionObj) CreateItem(m dbus.Message, props map[string]dbus.Variant, secret Secret, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	coll, derr := o.at(m)
	if derr != nil {
		return "/", "/", derr
	}
	value, derr := o.s.opened(secret, sender(m))
	if derr != nil {
		return "/", "/", derr
	}
	label, _ := props[ifItem+".Label"].Value().(string)
	attrs := map[string]string{}
	if v, ok := props[ifItem+".Attributes"]; ok {
		if err := v.Store(&attrs); err != nil {
			return "/", "/", dbus.MakeFailedError(fmt.Errorf("attributes: %w", err))
		}
	}
	// libsecret names the schema here too; keep it where search finds it.
	if t, _ := props[ifItem+".Type"].Value().(string); t != "" && attrs["xdg:schema"] == "" {
		attrs["xdg:schema"] = t
	}
	it, created, err := o.s.store.CreateItem(coll, label, attrs, value, secret.ContentType, replace)
	if err != nil {
		return "/", "/", failed(err)
	}
	o.s.trustCreator(it, sender(m))
	p := itemPath(coll, it.ID)
	cp := collPath(coll)
	if created {
		o.s.emit(cp, ifCollection+".ItemCreated", p)
		o.s.changed(cp, ifCollection, map[string]dbus.Variant{"Items": dbus.MakeVariant(o.s.items(coll))})
	} else {
		o.s.emit(cp, ifCollection+".ItemChanged", p)
	}
	return p, "/", nil
}

func (s *Service) items(coll string) []dbus.ObjectPath {
	out := []dbus.ObjectPath{}
	items, _ := s.store.Items(coll)
	for _, it := range items {
		out = append(out, itemPath(coll, it.ID))
	}
	return out
}

// --- org.freedesktop.Secret.Item ---

type itemObj struct{ s *Service }

func (o itemObj) at(m dbus.Message) (Item, *dbus.Error) {
	it, ok := o.s.item(msgPath(m))
	if !ok {
		return Item{}, errNoObject
	}
	return it, nil
}

func (o itemObj) Delete(m dbus.Message) (dbus.ObjectPath, *dbus.Error) {
	it, derr := o.at(m)
	if derr != nil {
		return "/", derr
	}
	if err := o.s.store.DeleteItem(it.Collection, it.ID); err != nil {
		return "/", failed(err)
	}
	if o.s.Trust != nil {
		o.s.Trust.Forget(ItemKey(it.Collection, it.ID))
	}
	cp := collPath(it.Collection)
	o.s.emit(cp, ifCollection+".ItemDeleted", itemPath(it.Collection, it.ID))
	o.s.changed(cp, ifCollection, map[string]dbus.Variant{"Items": dbus.MakeVariant(o.s.items(it.Collection))})
	return "/", nil
}

func (o itemObj) GetSecret(m dbus.Message, sp dbus.ObjectPath) (Secret, *dbus.Error) {
	it, derr := o.at(m)
	if derr != nil {
		return Secret{}, derr
	}
	sess, derr := o.s.session(sp, sender(m))
	if derr != nil {
		return Secret{}, derr
	}
	return o.s.sealed(it, sess, sp, sender(m))
}

func (o itemObj) SetSecret(m dbus.Message, secret Secret) *dbus.Error {
	it, derr := o.at(m)
	if derr != nil {
		return derr
	}
	value, derr := o.s.opened(secret, sender(m))
	if derr != nil {
		return derr
	}
	if err := o.s.store.SetSecret(it.Collection, it.ID, value, secret.ContentType); err != nil {
		return failed(err)
	}
	o.s.emit(collPath(it.Collection), ifCollection+".ItemChanged", itemPath(it.Collection, it.ID))
	return nil
}

// --- org.freedesktop.Secret.Session ---

type sessionObj struct{ s *Service }

func (o sessionObj) Close(m dbus.Message) *dbus.Error {
	p := msgPath(m)
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	if sess := o.s.sessions[p]; sess == nil || sess.owner != sender(m) {
		return errNoSession
	}
	delete(o.s.sessions, p)
	return nil
}

// --- org.freedesktop.DBus.Properties ---

type propsObj struct{ s *Service }

func (o propsObj) all(p dbus.ObjectPath, iface string) (map[string]dbus.Variant, *dbus.Error) {
	k, coll, id := o.s.parse(p)
	switch {
	case k == kindService && iface == ifService:
		return map[string]dbus.Variant{"Collections": dbus.MakeVariant(o.s.collections())}, nil
	case k == kindCollection && iface == ifCollection:
		c, _ := o.s.store.Collection(coll)
		return map[string]dbus.Variant{
			"Items": dbus.MakeVariant(o.s.items(coll)), "Label": dbus.MakeVariant(c.Label),
			"Locked": dbus.MakeVariant(false), "Created": dbus.MakeVariant(c.Created), "Modified": dbus.MakeVariant(c.Modified),
		}, nil
	case k == kindItem && iface == ifItem:
		it, err := o.s.store.Item(coll, id)
		if err != nil {
			return nil, errNoObject
		}
		return map[string]dbus.Variant{
			"Locked": dbus.MakeVariant(false), "Attributes": dbus.MakeVariant(maps.Clone(it.Attributes)),
			"Label": dbus.MakeVariant(it.Label), "Created": dbus.MakeVariant(it.Created), "Modified": dbus.MakeVariant(it.Modified),
		}, nil
	case k == kindSession && iface == ifSession:
		return map[string]dbus.Variant{}, nil
	case k == kindNone:
		return nil, errNoObject
	}
	return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []any{"No interface " + iface + " here"})
}

func (o propsObj) GetAll(m dbus.Message, iface string) (map[string]dbus.Variant, *dbus.Error) {
	return o.all(msgPath(m), iface)
}

func (o propsObj) Get(m dbus.Message, iface, name string) (dbus.Variant, *dbus.Error) {
	all, derr := o.all(msgPath(m), iface)
	if derr != nil {
		return dbus.MakeVariant(""), derr
	}
	v, ok := all[name]
	if !ok {
		return dbus.MakeVariant(""), dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{"No property " + name})
	}
	return v, nil
}

func (o propsObj) Set(m dbus.Message, iface, name string, value dbus.Variant) *dbus.Error {
	p := msgPath(m)
	k, coll, id := o.s.parse(p)
	var err error
	switch {
	case k == kindCollection && iface == ifCollection && name == "Label":
		label, _ := value.Value().(string)
		err = o.s.store.SetCollectionLabel(coll, label)
	case k == kindItem && iface == ifItem && name == "Label":
		label, _ := value.Value().(string)
		err = o.s.store.SetLabel(coll, id, label)
	case k == kindItem && iface == ifItem && name == "Attributes":
		attrs := map[string]string{}
		if err := value.Store(&attrs); err != nil {
			return dbus.MakeFailedError(err)
		}
		err = o.s.store.SetAttributes(coll, id, attrs)
	case k == kindNone:
		return errNoObject
	default:
		return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{name + " cannot be set"})
	}
	if err != nil {
		return failed(err)
	}
	o.s.changed(p, iface, map[string]dbus.Variant{name: value})
	if k == kindItem {
		o.s.emit(collPath(coll), ifCollection+".ItemChanged", itemPath(coll, id))
	} else {
		o.s.emit(basePath, ifService+".CollectionChanged", collPath(coll))
	}
	return nil
}
