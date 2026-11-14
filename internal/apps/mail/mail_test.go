package mail

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// --- a test IMAP server ---

type testServer struct {
	addr   string
	user   *imapmemserver.User
	bodies atomic.Int32 // FETCH commands that asked for whole messages
}

// counting counts a session's FETCH commands for whole messages; the
// memory server's session is IMAP4rev2, which the server asks of it.
type counting struct {
	imapserver.SessionIMAP4rev2
	n *atomic.Int32
}

func (c counting) Fetch(w *imapserver.FetchWriter, set imap.NumSet, options *imap.FetchOptions) error {
	for _, sec := range options.BodySection {
		if sec.Specifier == imap.PartSpecifierNone && len(sec.Part) == 0 && sec.Partial == nil {
			c.n.Add(1)
			break
		}
	}
	return c.SessionIMAP4rev2.Fetch(w, set, options)
}

func newIMAP(t *testing.T, boxes ...string) *testServer {
	t.Helper()
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("me@example.com", "secret")
	for _, b := range append([]string{"INBOX"}, boxes...) {
		if err := user.Create(b, nil); err != nil {
			t.Fatal(err)
		}
	}
	mem.AddUser(user)
	ts := &testServer{user: user}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return counting{mem.NewSession().(imapserver.SessionIMAP4rev2), &ts.bodies}, nil, nil
		},
		InsecureAuth: true,
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		Logger:       nopLogger{},
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	old := DialIMAP
	DialIMAP = func(addr string, opts *imapclient.Options) (*imapclient.Client, error) {
		return imapclient.DialInsecure(addr, opts)
	}
	t.Cleanup(func() { DialIMAP = old })
	ts.addr = ln.Addr().String()
	return ts
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...any) {}

// put appends a message to a mailbox, as another client delivering it.
func (s *testServer) put(t *testing.T, mailbox, raw string, flags ...imap.Flag) {
	t.Helper()
	c, err := imapclient.DialInsecure(s.addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login("me@example.com", "secret").Wait(); err != nil {
		t.Fatal(err)
	}
	raw = strings.ReplaceAll(raw, "\n", "\r\n")
	cmd := c.Append(mailbox, int64(len(raw)), &imap.AppendOptions{Flags: flags})
	cmd.Write([]byte(raw))
	cmd.Close()
	if _, err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func (s *testServer) session(t *testing.T) *Session {
	t.Helper()
	acc := Account{ID: "me", Address: "me@example.com", Provider: Password, IMAP: s.addr, SMTP: "127.0.0.1:1"}
	sess := NewSession(acc, Settings{}, StoreAt(t.TempDir()), SecretsAt(filepath.Join(t.TempDir(), "s.json")))
	sess.UseAuth(Auth{User: "me@example.com", Password: "secret"})
	t.Cleanup(sess.Close)
	return sess
}

func msg(id, from, subject, body string, extra ...string) string {
	h := "From: " + from + "\nTo: Me <me@example.com>\nSubject: " + subject + "\nDate: Mon, 13 Oct 2026 10:00:00 +0000\nMessage-ID: <" + id + ">\n"
	for _, e := range extra {
		h += e + "\n"
	}
	return h + "Content-Type: text/plain; charset=utf-8\n\n" + body + "\n"
}

const withAttachment = `From: Ana <ana@studio.dev>
To: me@example.com
Subject: =?utf-8?q?Q3_r=C3=A9port?=
Date: Tue, 14 Oct 2026 09:00:00 +0000
Message-ID: <q3@studio.dev>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="b1"

--b1
Content-Type: multipart/alternative; boundary="b2"

--b2
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

Voil=E0 the numbers.

On Mon, Ben wrote:
> old text
--b2
Content-Type: text/html; charset=utf-8

<p>Voilà the <b>numbers</b>.</p>
--b2--

--b1
Content-Type: application/pdf; name="q3.pdf"
Content-Disposition: attachment; filename="q3.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQK
--b1--
`

func TestSyncAndActions(t *testing.T) {
	srv := newIMAP(t, "Sent", "Archive", "Trash")
	srv.put(t, "INBOX", msg("a@x", "Ben <ben@x.org>", "Hello", "First line of the text.\n\n-- \nBen"))
	srv.put(t, "INBOX", withAttachment, imap.FlagSeen)
	ctx := context.Background()
	s := srv.session(t)

	boxes, err := s.Mailboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var roles []Role
	for _, b := range boxes {
		roles = append(roles, b.Role)
	}
	if !slices.Equal(roles, []Role{RoleInbox, RoleSent, RoleArchive, RoleTrash}) {
		t.Fatalf("roles %v", roles)
	}
	if boxes[0].Total != 2 || boxes[0].Unread != 1 {
		t.Fatalf("inbox counts %+v", boxes[0])
	}

	res, err := s.Sync(ctx, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	items := s.Items("INBOX")
	if len(res.New) != 2 || len(items) != 2 {
		t.Fatalf("new %v items %d", res.New, len(items))
	}
	q3, hello := items[0], items[1]
	if q3.Subject != "Q3 réport" || !q3.Attach || q3.Snippet != "Voilà the numbers." || !q3.Has(Seen) {
		t.Fatalf("q3 %+v", *q3.Envelope)
	}
	if hello.Snippet != "First line of the text." || hello.From.Name != "Ben" || hello.Attach {
		t.Fatalf("hello %+v", *hello.Envelope)
	}

	// The cache: a new session reads the index before connecting.
	m, err := s.Message(ctx, "INBOX", q3.UID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Shown()) != 1 || m.Shown()[0].Name != "q3.pdf" || string(m.Shown()[0].Data) != "%PDF-1.4\n" {
		t.Fatalf("attachments %+v", m.Shown())
	}

	// Flags both ways.
	if err := s.SetFlag(ctx, "INBOX", []uint32{hello.UID}, Seen, true); err != nil {
		t.Fatal(err)
	}
	other := srv.session(t)
	other.Sync(ctx, "INBOX")
	if it := other.Items("INBOX"); !it[1].Has(Seen) {
		t.Fatal("seen did not reach the server")
	}
	other.SetFlag(ctx, "INBOX", []uint32{hello.UID}, Flagged, true)
	if res, _ := s.Sync(ctx, "INBOX"); !res.Changed || !s.Items("INBOX")[1].Has(Flagged) {
		t.Fatal("a flag set elsewhere did not come back")
	}

	// New mail, then a move and a delete.
	srv.put(t, "INBOX", msg("c@x", "Cara <cara@x.org>", "Third", "Hi"))
	res, _ = s.Sync(ctx, "INBOX")
	if len(res.New) != 1 || len(s.Items("INBOX")) != 3 {
		t.Fatalf("new mail: %+v", res)
	}
	if err := s.Move(ctx, "INBOX", []uint32{hello.UID}, "Archive"); err != nil {
		t.Fatal(err)
	}
	if len(s.Items("INBOX")) != 2 {
		t.Fatal("moved message still listed")
	}
	if _, err := s.Sync(ctx, "Archive"); err != nil || len(s.Items("Archive")) != 1 {
		t.Fatalf("archive: %v %d", err, len(s.Items("Archive")))
	}
	arch := s.Items("Archive")[0]
	if err := s.Delete(ctx, "Archive", []uint32{arch.UID}); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.Sync(ctx, "Archive"); len(s.Items("Archive")) != 0 || len(res.New) != 0 {
		t.Fatal("deleted message came back")
	}

	// A move the server refuses puts the message back.
	if err := s.Move(ctx, "INBOX", []uint32{q3.UID}, "Nowhere"); err == nil || len(s.Items("INBOX")) != 2 {
		t.Fatalf("refused move: %v, %d listed", err, len(s.Items("INBOX")))
	}

	// Gone elsewhere: the next sync drops it.
	other.Sync(ctx, "INBOX")
	other.Delete(ctx, "INBOX", []uint32{q3.UID})
	if res, _ := s.Sync(ctx, "INBOX"); !slices.Equal(res.Gone, []uint32{q3.UID}) {
		t.Fatalf("gone %v", res.Gone)
	}
	if _, ok := s.Raw("INBOX", q3.UID); ok {
		t.Fatal("a gone message stayed in the cache")
	}

	// Search on the server.
	uids, err := s.Search(ctx, "INBOX", ParseQuery("from:cara", time.Now()))
	if err != nil || len(uids) != 1 {
		t.Fatalf("search %v %v", uids, err)
	}
}

func TestFirstSyncAndOlder(t *testing.T) {
	srv := newIMAP(t)
	for i := range 5 {
		srv.put(t, "INBOX", msg(string(rune('a'+i))+"@x", "x@x.org", "n"+string(rune('0'+i)), "t"))
	}
	ctx := context.Background()
	s := srv.session(t)
	var x Index
	err := s.Do(ctx, func(c *Conn) error {
		res, err := c.Sync("INBOX", &x, 2)
		if err != nil {
			return err
		}
		if len(res.New) != 2 || !x.Older || x.Messages[0].Subject != "n4" {
			t.Fatalf("first sync %+v older %v", res, x.Older)
		}
		n, err := c.Older("INBOX", &x, 2)
		if n != 2 || !x.Older || err != nil {
			t.Fatalf("older %d %v %v", n, x.Older, err)
		}
		n, err = c.Older("INBOX", &x, 2)
		if n != 1 || x.Older || len(x.Messages) != 5 {
			t.Fatalf("oldest %d %v %d", n, x.Older, len(x.Messages))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUIDValidityStartsOver(t *testing.T) {
	srv := newIMAP(t)
	srv.put(t, "INBOX", msg("a@x", "x@x.org", "one", "t"))
	ctx := context.Background()
	s := srv.session(t)
	s.Sync(ctx, "INBOX")
	s.imu.Lock()
	s.indexes["INBOX"].Validity++
	s.indexes["INBOX"].Messages[0].UID = 99 // what the old validity meant
	s.imu.Unlock()
	res, err := s.Sync(ctx, "INBOX")
	if err != nil || !res.Reset {
		t.Fatalf("reset %v %v", res, err)
	}
	if items := s.Items("INBOX"); len(items) != 1 || items[0].UID == 99 {
		t.Fatalf("items after reset %+v", items)
	}
}

func TestParse(t *testing.T) {
	m, err := Parse([]byte(strings.ReplaceAll(withAttachment, "\n", "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Q3 réport" || m.From.Addr != "ana@studio.dev" || m.MessageID != "q3@studio.dev" {
		t.Fatalf("header %+v", m)
	}
	if !strings.HasPrefix(m.Text, "Voilà the numbers.") || m.FromHTML {
		t.Fatalf("text %q", m.Text)
	}
	written, quoted := Split(m.Text)
	if written != "Voilà the numbers." || !strings.HasPrefix(quoted, "On Mon, Ben wrote:") {
		t.Fatalf("split %q / %q", written, quoted)
	}
	// HTML only.
	raw := "From: a@b.c\r\nSubject: s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<style>p{}</style><h1>Hi</h1><p>Read <a href=\"https://evil.example/x\">paypal.com</a> now.</p><ul><li>one</li><li>two</li></ul><blockquote>old</blockquote>"
	m, err = Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := "Hi\n\nRead paypal.com <https://evil.example/x> now.\n\n• one\n• two\n\n> old"
	if m.Text != want || !m.FromHTML {
		t.Fatalf("html text\n%q\nwant\n%q", m.Text, want)
	}
	if len(m.Links) != 1 || m.Links[0].Text != "paypal.com" {
		t.Fatalf("links %+v", m.Links)
	}
}

// --- sending ---

type smtpBackend struct {
	mu    sync.Mutex
	got   []byte
	from  string
	rcpts []string
	user  string
	token string // XOAUTH2's
}

func (b *smtpBackend) NewSession(*smtp.Conn) (smtp.Session, error) { return &smtpSession{b: b}, nil }

type smtpSession struct{ b *smtpBackend }

func (s *smtpSession) AuthMechanisms() []string { return []string{"PLAIN", "XOAUTH2"} }
func (s *smtpSession) Auth(mech string) (sasl.Server, error) {
	if mech == "PLAIN" {
		return sasl.NewPlainServer(func(_, user, pass string) error {
			if user != "me@example.com" || pass != "secret" {
				return errors.New("no")
			}
			return nil
		}), nil
	}
	return xoauth2Server{s.b}, nil
}
func (s *smtpSession) Mail(from string, _ *smtp.MailOptions) error { s.b.from = from; return nil }
func (s *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.b.rcpts = append(s.b.rcpts, to)
	return nil
}
func (s *smtpSession) Data(r io.Reader) error {
	b, _ := io.ReadAll(r)
	s.b.mu.Lock()
	s.b.got = b
	s.b.mu.Unlock()
	return nil
}
func (s *smtpSession) Reset()        {}
func (s *smtpSession) Logout() error { return nil }

type xoauth2Server struct{ b *smtpBackend }

func (x xoauth2Server) Next(resp []byte) ([]byte, bool, error) {
	if string(resp) != "user="+x.b.user+"\x01auth=Bearer "+x.b.token+"\x01\x01" {
		return nil, true, errors.New("bad token")
	}
	return nil, true, nil
}

func newSMTP(t *testing.T) (*smtpBackend, string) {
	be := &smtpBackend{}
	srv := smtp.NewServer(be)
	srv.AllowInsecureAuth = true
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	old := DialSMTP
	DialSMTP = func(addr string) (*smtp.Client, error) { return smtp.Dial(addr) }
	t.Cleanup(func() { DialSMTP = old })
	return be, ln.Addr().String()
}

func TestSendWithAttachmentAndSent(t *testing.T) {
	srv := newIMAP(t, "Sent")
	be, smtpAddr := newSMTP(t)
	ctx := context.Background()
	s := srv.session(t)
	s.Acc.SMTP, s.Acc.Name = smtpAddr, "Me Myself"
	s.Mailboxes(ctx)
	file := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(file, []byte("the notes"), 0o600)
	d := Draft{To: "Ana <ana@studio.dev>, ben@x.org", Bcc: "boss@x.org", Subject: "Numbers", Body: "Here they are.", Attachments: []string{file}}
	b, err := s.Send(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if be.from != "me@example.com" || !slices.Equal(be.rcpts, []string{"ana@studio.dev", "ben@x.org", "boss@x.org"}) {
		t.Fatalf("envelope %s %v", be.from, be.rcpts)
	}
	m, err := Parse(be.got)
	if err != nil {
		t.Fatal(err)
	}
	if m.Text != "Here they are." || len(m.Attachments) != 1 || string(m.Attachments[0].Data) != "the notes" || m.Attachments[0].Name != "notes.txt" {
		t.Fatalf("sent %+v", m)
	}
	if bytes.Contains(be.got, []byte("boss@x.org")) {
		t.Fatal("Bcc in the header")
	}
	if m.From.Name != "Me Myself" || m.MessageID != b.MessageID {
		t.Fatalf("from %+v id %s", m.From, m.MessageID)
	}
	s.Sync(ctx, "Sent")
	if sent := s.Items("Sent"); len(sent) != 1 || !sent[0].Has(Seen) || sent[0].MessageID != b.MessageID {
		t.Fatalf("sent folder %+v", sent)
	}
}

func TestReplyAndForward(t *testing.T) {
	m, _ := Parse([]byte(strings.ReplaceAll(withAttachment, "\n", "\r\n")))
	m.Cc = []Address{{Addr: "me@example.com"}, {Name: "Ben", Addr: "ben@x.org"}}
	acc := Account{ID: "me", Address: "me@example.com", Signature: "Me"}
	mine := func(a string) bool { return a == "me@example.com" }
	d := Reply(m, acc, true, mine)
	if d.To != "Ana <ana@studio.dev>" || d.Cc != "Ben <ben@x.org>" {
		t.Fatalf("reply all to %q cc %q", d.To, d.Cc)
	}
	if d.Subject != "Re: Q3 réport" || d.InReplyTo != "q3@studio.dev" || !slices.Equal(d.References, []string{"q3@studio.dev"}) {
		t.Fatalf("reply %+v", d)
	}
	if !strings.HasPrefix(d.Body, "\n\n-- \nMe\n\n") || !strings.Contains(d.Body, "> Voilà the numbers.\n") {
		t.Fatalf("body %q", d.Body)
	}
	b, err := Build(d, acc, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Parse(b.Raw)
	if got.InReplyTo != "q3@studio.dev" || !slices.Equal(got.References, []string{"q3@studio.dev"}) {
		t.Fatalf("reply headers %q %v", got.InReplyTo, got.References)
	}
	f, err := Forward(m, acc, t.TempDir())
	if err != nil || len(f.Attachments) != 1 || filepath.Base(f.Attachments[0]) != "q3.pdf" || f.Subject != "Fwd: Q3 réport" {
		t.Fatalf("forward %+v %v", f, err)
	}
	if _, err := Build(Draft{Subject: "x"}, acc, time.Now()); !errors.Is(err, ErrNoRecipient) {
		t.Fatalf("no recipient: %v", err)
	}
	if _, err := ParseAddresses("Ana <ana@studio.dev>, nobody"); err == nil {
		t.Fatal("a bare name was taken")
	}
	if l, _ := ParseAddresses(`"Souza, Ana" <ana@studio.dev>; ben@x.org`); len(l) != 2 || l[0].Name != "Souza, Ana" {
		t.Fatalf("addresses %+v", l)
	}
}

// --- OAuth ---

type fakeProvider struct {
	srv       *httptest.Server
	secret    string // the client_secret the last trade sent
	challenge string
	refresh   string
	refreshed int
}

func newProvider(t *testing.T) *fakeProvider {
	p := &fakeProvider{refresh: "r1"}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		p.secret = r.Form.Get("client_secret")
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("client_id") != "cid" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			claims, _ := json.Marshal(map[string]string{"email": "ana@gmail.com", "name": "Ana Souza"})
			idt := "x." + base64.RawURLEncoding.EncodeToString(claims) + ".y"
			json.NewEncoder(w).Encode(map[string]any{"access_token": "a1", "refresh_token": p.refresh, "expires_in": 3600, "id_token": idt})
		case "refresh_token":
			if r.Form.Get("refresh_token") != p.refresh {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "Token has been expired or revoked."})
				return
			}
			p.refreshed++
			p.refresh = "r2" // a provider may give a new refresh token
			json.NewEncoder(w).Encode(map[string]any{"access_token": "a2", "refresh_token": "r2", "expires_in": 3600})
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeProvider) endpoint() Endpoint {
	return Endpoint{AuthURL: p.srv.URL + "/auth", TokenURL: p.srv.URL + "/token", Scopes: []string{"mail", "openid", "email"}, Client: OAuthClient{ClientID: "cid"}}
}

func TestOAuthSignIn(t *testing.T) {
	p := newProvider(t)
	si, err := StartSignIn(p.endpoint(), "ana@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(si.URL)
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("login_hint") != "ana@gmail.com" || q.Get("scope") != "mail openid email" {
		t.Fatalf("auth url %s", si.URL)
	}
	p.challenge = q.Get("code_challenge")
	// The browser, sent back with a wrong state first: refused, still waiting.
	redirect := q.Get("redirect_uri")
	resp, err := http.Get(redirect + "?code=the-code&state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := si.Wait(ctx); err == nil || !strings.Contains(err.Error(), "another request") {
		t.Fatalf("wrong state: %v", err)
	}

	si, _ = StartSignIn(p.endpoint(), "")
	u, _ = url.Parse(si.URL)
	q = u.Query()
	p.challenge = q.Get("code_challenge")
	resp, err = http.Get(q.Get("redirect_uri") + "?code=the-code&state=" + url.QueryEscape(q.Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(page), "Signed in") {
		t.Fatalf("page %s", page)
	}
	tok, err := si.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "a1" || tok.Refresh != "r1" || tok.Email != "ana@gmail.com" || tok.Name != "Ana Souza" {
		t.Fatalf("token %+v", tok)
	}

	// Refresh: a new refresh token is kept; an old one is refused.
	var saved string
	ts := NewTokens(p.endpoint(), "r1", func(r string) error { saved = r; return nil })
	if a, err := ts.Access(ctx); a != "a2" || err != nil || saved != "r2" {
		t.Fatalf("refresh %q %v saved %q", a, err, saved)
	}
	if a, _ := ts.Access(ctx); a != "a2" || p.refreshed != 1 {
		t.Fatal("refreshed again before the token ended")
	}
	old := NewTokens(p.endpoint(), "r1", nil)
	if _, err := old.Access(ctx); !errors.Is(err, ErrSignInAgain) {
		t.Fatalf("revoked: %v", err)
	}
}

func TestXOAUTH2Send(t *testing.T) {
	be, addr := newSMTP(t)
	p := newProvider(t)
	be.user, be.token = "ana@gmail.com", "a2"
	a := Auth{User: "ana@gmail.com", Tokens: NewTokens(p.endpoint(), "r1", nil)}
	b, err := Build(Draft{To: "x@y.org", Subject: "s", Body: "b"}, Account{Address: "ana@gmail.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := Send(context.Background(), addr, a, b); err != nil {
		t.Fatal(err)
	}
	if len(be.got) == 0 {
		t.Fatal("nothing sent")
	}
	be.token = "other"
	if err := Send(context.Background(), addr, a, b); !errors.Is(err, ErrAuth) {
		t.Fatalf("refused token: %v", err)
	}
}

func TestSettingsAndSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.toml")
	var s Settings
	a := s.Add(Account{Address: "Ana.Souza@Gmail.com", Provider: Google})
	b := s.Add(Account{Address: "ana.souza@gmail.co", Provider: Google})
	again := s.Add(Account{Address: "ana.souza@gmail.com", Provider: Google, Name: "Ana"})
	if a.ID != "ana-souza-gmail" || b.ID != "ana-souza-gmail-2" || again.ID != a.ID || len(s.Accounts) != 2 {
		t.Fatalf("ids %q %q %q (%d)", a.ID, b.ID, again.ID, len(s.Accounts))
	}
	s.Accounts = append(s.Accounts, Account{ID: "fm", Address: "me@fastmail.com", Provider: Password, IMAP: "imap.fastmail.com", SMTP: "smtp.fastmail.com:587"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil || len(got.Accounts) != 3 || got.Accounts[0].Name != "Ana" {
		t.Fatalf("load %+v %v", got, err)
	}
	if i, sm := got.Accounts[2].Servers(); i != "imap.fastmail.com:993" || sm != "smtp.fastmail.com:587" {
		t.Fatalf("servers %s %s", i, sm)
	}
	oldID, oldSecret := GoogleClientID, GoogleClientSecret
	defer func() { GoogleClientID, GoogleClientSecret = oldID, oldSecret }()
	GoogleClientID, GoogleClientSecret = "gid", ""
	if _, err := got.Endpoint(Google); !errors.Is(err, ErrNoClient) {
		t.Fatalf("a Google client without its secret: %v", err)
	}
	GoogleClientSecret = "gsecret"
	if ep, err := got.Endpoint(Google); err != nil || ep.Client.ClientID != "gid" {
		t.Fatalf("inua's Google client: %+v %v", ep, err)
	}
	got.OAuth.Google = OAuthClient{ClientID: "mine", ClientSecret: "s"}
	if ep, _ := got.Endpoint(Google); ep.Client.ClientID != "mine" {
		t.Fatalf("the user's own client: %+v", ep)
	}
	got.OAuth.Microsoft.ClientID = "mid"
	if ep, err := got.Endpoint(Microsoft); err != nil || !strings.Contains(ep.AuthURL, "/common/") {
		t.Fatalf("microsoft %+v %v", ep, err)
	}

	sec := SecretsAt(filepath.Join(t.TempDir(), "s.json"))
	sec.Set("a", SecretRefresh, "tok")
	if v, _ := sec.Get("a", SecretRefresh); v != "tok" {
		t.Fatal("secret not kept")
	}
	info, _ := os.Stat(sec.file)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file mode %v", info.Mode())
	}
	sec.Forget("a")
	if v, _ := sec.Get("a", SecretRefresh); v != "" {
		t.Fatal("secret not forgotten")
	}
}

func env(id, from, subject string, at int, refs ...string) Item {
	e := &Envelope{UID: uint32(at), MessageID: id, From: Address{Addr: from}, Subject: subject, Date: time.Date(2026, 10, at, 0, 0, 0, 0, time.UTC)}
	if len(refs) > 0 {
		e.InReplyTo, e.References = refs[len(refs)-1], refs
	}
	return Item{Account: "a", Mailbox: "INBOX", Envelope: e}
}

func TestDetect(t *testing.T) {
	for addr, want := range map[string]Provider{
		"ana@gmail.com": Google, "Ana@GoogleMail.com": Google, "ana@outlook.com": Microsoft, "ana@hotmail.co.uk": Microsoft,
		"ana@live.jp": Microsoft, "ana@fastmail.com": Password, "ana@studio.dev": Password, "nobody": Password,
	} {
		if got := Detect(addr); got != want {
			t.Errorf("%s: %s, want %s", addr, got, want)
		}
	}
}

func TestGroup(t *testing.T) {
	inbox := []Item{
		env("1", "ana@x", "Plan", 1),
		env("2", "ben@x", "Other", 2),
		env("3", "ana@x", "Re: Plan", 4, "1", "s1"),
	}
	sent := []Item{env("s1", "me@x", "Re: Plan", 3, "1"), env("s9", "me@x", "Unrelated", 5)}
	cs := Group(inbox, sent)
	if len(cs) != 2 {
		t.Fatalf("%d conversations", len(cs))
	}
	plan := cs[0]
	if plan.Subject() != "Plan" || len(plan.Messages) != 3 || plan.Messages[1].MessageID != "s1" {
		t.Fatalf("plan %+v", plan.Messages)
	}
	people := plan.People(func(a string) bool { return a == "me@x" })
	if len(people) != 1 || people[0].Addr != "ana@x" {
		t.Fatalf("people %+v", people)
	}
	// The same message in two mailboxes is listed once.
	dup := env("2", "ben@x", "Other", 2)
	dup.Mailbox = "Archive"
	if cs := Group(append(inbox, dup), nil); len(cs[1].Messages) != 1 {
		t.Fatalf("duplicate %+v", cs[1].Messages)
	}
}

func TestQuery(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	q := ParseQuery(`from:ana has:attachment is:unread after:2026-10-01 "q3 report" older_than:1d`, now)
	if !slices.Equal(q.From, []string{"ana"}) || !q.Attach || !q.Unread || q.After.Day() != 1 || !slices.Equal(q.Words, []string{"q3 report"}) || q.Before.Day() != 18 {
		t.Fatalf("query %+v", q)
	}
	e := &Envelope{From: Address{Name: "Ana", Addr: "ana@x"}, Subject: "The Q3 report", Attach: true, Date: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)}
	if !q.Match(e) {
		t.Fatal("no match")
	}
	e.Set(Seen, true)
	if q.Match(e) {
		t.Fatal("matched a read message")
	}
	if !Looks("the invoice hetzner sent last month") || Looks("from:ana") || Looks("invoice") || !Looks("上个月的发票在哪里呢") {
		t.Fatal("Looks")
	}
}

func TestJudge(t *testing.T) {
	m := &Message{
		From:           Address{Name: "PayPal Service", Addr: "service@paypa1-secure.com"},
		ReplyTo:        []Address{{Addr: "x@other.ru"}},
		Authentication: "mx.google.com; spf=fail smtp.mailfrom=paypa1-secure.com; dmarc=fail (p=NONE) header.from=paypa1-secure.com",
		Links:          []Link{{Text: "paypal.com/verify", URL: "http://185.22.4.19/verify"}, {Text: "www.paypal.com", URL: "https://login.evil.example/"}},
	}
	j := Judge(m, nil)
	if j.Verdict != Suspicious || j.Steps[0].Level != LevelBad || j.Steps[2].Level != LevelWarn || j.Steps[3].Level != LevelBad {
		t.Fatalf("phishing %+v", j)
	}
	if !strings.Contains(j.Steps[3].Found, "2 links") {
		t.Fatalf("links %q", j.Steps[3].Found)
	}
	good := &Message{
		From:           Address{Name: "GitHub", Addr: "noreply@github.com"},
		Authentication: "mx.google.com; dkim=pass header.i=@github.com; spf=pass; dmarc=pass",
		Links:          []Link{{Text: "github.com/lyricat/inua", URL: "https://github.com/lyricat/inua/pull/31"}, {Text: "View it", URL: "https://github.com/x"}},
	}
	if j := Judge(good, func(string) bool { return true }); j.Verdict != Fine {
		t.Fatalf("fine %+v", j)
	}
	if j := Judge(&Message{From: Address{Name: "Apple Support", Addr: "help@apple-care.net"}}, nil); j.Verdict != Careful {
		t.Fatalf("brand %+v", j)
	}
	if j := Judge(&Message{From: Address{Name: "ceo@studio.dev", Addr: "a@b.ru"}}, nil); j.Verdict != Suspicious {
		t.Fatalf("name address %+v", j)
	}
	if site("a.b.example.co.uk") != "example.co.uk" || site("mail.github.com") != "github.com" {
		t.Fatal("site")
	}
}

func TestSortRules(t *testing.T) {
	cases := []struct {
		e    Envelope
		mine bool
		want Tag
		ok   bool
	}{
		{Envelope{From: Address{Addr: "ana@x"}}, true, TagWait, true},
		{Envelope{From: Address{Addr: "noreply@shop.com"}, Subject: "Your order #12"}, false, TagReceipt, true},
		{Envelope{From: Address{Addr: "news@blog.com"}, Bulk: true}, false, TagNews, true},
		{Envelope{From: Address{Addr: "notifications@github.com"}, Bulk: true}, false, TagNotice, true},
		{Envelope{From: Address{Addr: "info@account.netflix.com"}, Subject: "Netflix: Your sign-in code", Bulk: true}, false, TagNotice, true},
		{Envelope{From: Address{Addr: "team@hf.co"}, Subject: "[Hugging Face] Security incident update", Bulk: true}, false, TagNotice, true},
		{Envelope{From: Address{Addr: "news@blog.com"}, Subject: "Unicode tricks this week", Bulk: true}, false, TagNews, true},
		{Envelope{From: Address{Addr: "ana@x"}, Subject: "Lunch?"}, false, TagNone, false},                                         // not to the user
		{Envelope{From: Address{Addr: "ana@x"}, To: []Address{{Addr: "Me@x"}}, Snippet: "Free on Friday?"}, false, TagNone, false}, // the assistant decides
		{Envelope{From: Address{Addr: "ana@x"}, To: []Address{{Addr: "me@x"}}, Snippet: "Here it is."}, false, TagNone, false},
	}
	for _, c := range cases {
		if got, ok := Rules(c.e, c.mine, "me@x"); got != c.want || ok != c.ok {
			t.Errorf("%+v: %q %v", c.e, got, ok)
		}
	}
}

// A newsletter's conversation is list mail from a sender that is not a
// service, sorted otherwise by no one, that the user never answered.
func TestNews(t *testing.T) {
	news := func(id string, at int, refs ...string) Item {
		it := env(id, "letters@blog.com", "This week", at, refs...)
		it.Bulk = true
		return it
	}
	mine := func(addr string) bool { return addr == "me@x" }
	letter := &Conversation{Messages: []Item{news("<1>", 1)}}
	if !letter.News(mine) {
		t.Error("a newsletter is not news")
	}
	notice := env("<2>", "notifications@github.com", "CI failed", 2)
	notice.Bulk = true
	person := env("<3>", "ana@x", "Lunch?", 3)
	answered := &Conversation{Messages: []Item{news("<4>", 4), env("<5>", "me@x", "Re: This week", 5, "<4>")}}
	for name, c := range map[string]*Conversation{
		"a service's notification": {Messages: []Item{notice}},
		"a person's mail":          {Messages: []Item{person}},
		"one the user answered":    answered,
		"none":                     {},
	} {
		if c.News(mine) {
			t.Errorf("%s is news", name)
		}
	}
	// The user's sorting comes first; the assistant's where the rules
	// settle nothing.
	sorted := news("<6>", 6)
	sorted.Tag, sorted.TagByUser = TagFYI, true
	tagged := person
	tagged.Envelope = &Envelope{From: person.From, Tag: TagNews}
	if IsNews(*sorted.Envelope) || !IsNews(*tagged.Envelope) {
		t.Error("a sorted message's tag is not followed")
	}
	stale := news("<7>", 7)
	stale.Subject, stale.Tag = "Your sign-in code", TagNews
	if IsNews(*stale.Envelope) {
		t.Error("a tag the rules set before is followed")
	}
}

// A sender's newsletters stack: one conversation, its newest issue's
// subject, the newest stack first.
func TestStack(t *testing.T) {
	a1, b1, a2 := env("<a1>", "Weekly@blog.com", "Issue 1", 1), env("<b1>", "news@shop.com", "Sale", 2), env("<a2>", "weekly@blog.com", "Issue 2", 3)
	cs := Stack(Group([]Item{a1, b1, a2}, nil))
	if len(cs) != 2 || cs[0].ID != "stack:weekly@blog.com" || len(cs[0].Messages) != 2 || cs[1].ID != "stack:news@shop.com" {
		t.Fatalf("stacks %+v", cs)
	}
	if !cs[0].Stacked() || cs[0].Subject() != "Issue 2" || cs[0].Messages[0].Subject != "Issue 1" {
		t.Errorf("a stack: %v %q %q", cs[0].Stacked(), cs[0].Subject(), cs[0].Messages[0].Subject)
	}
}

func TestAnswers(t *testing.T) {
	if got := ParseIdeas("1. Yes, Friday works\n- \"Ask Ben\"\n\n• Not this quarter\nfourth"); !slices.Equal(got, []string{"Yes, Friday works", "Ask Ben", "Not this quarter"}) {
		t.Fatalf("ideas %q", got)
	}
	if got := ParseSearch("```\nfrom:hetzner invoice after:2026-09-01\n```"); got != "from:hetzner invoice after:2026-09-01" {
		t.Fatalf("search %q", got)
	}
	if j, ok := ParseJudgment("**SUSPICIOUS**\nThe link goes elsewhere."); j.Verdict != Suspicious || len(j.Reasons) != 1 || j.Reasons[0] != "The link goes elsewhere." || !ok {
		t.Errorf("an answer in another form: %+v", j)
	}
	j, ok := ParseJudgment("VERDICT: CAREFUL\n- Reply-To is another domain.\n- The link text names a bank.\nDO: Check with the sender another way.")
	if !ok || j.Verdict != Careful || len(j.Reasons) != 2 || j.Advice != "Check with the sender another way." {
		t.Errorf("the asked form: %+v", j)
	}
	if _, ok := ParseJudgment("maybe"); ok {
		t.Error("no verdict read as one")
	}
	said := []Said{{From: Address{Name: "Ana", Addr: "a@x"}, Text: "Numbers by Friday?"}}
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	p := SummaryPrompt(said, "me@x", day, "ja", "")
	if !strings.Contains(p, "Numbers by Friday?") || !strings.Contains(p, `"ja"`) || !strings.Contains(p, "the language the conversation is written in") || !strings.Contains(p, "Monday 19 October 2026") {
		t.Fatalf("summary prompt %s", p)
	}
	// A language chosen: that one, whatever the conversation's.
	p = SummaryPrompt(said, "me@x", day, "ja", "zh-Hans")
	if !strings.Contains(p, `"zh-Hans", whatever the conversation's language`) || strings.Contains(p, "the language the conversation is written in") {
		t.Fatalf("summary prompt in a chosen language %s", p)
	}
}

func TestPolishPrompt(t *testing.T) {
	p := PolishPrompt("Hi Aiko, the 14th works.", Shorter, "", false)
	if !strings.Contains(p, "the text of this e-mail") || !strings.Contains(p, "shorter") || !strings.HasSuffix(p, "Hi Aiko, the 14th works.") {
		t.Fatalf("whole text %s", p)
	}
	p = PolishPrompt("the 14th works", Translate, "ja", true)
	if !strings.Contains(p, "this part of an e-mail") || !strings.Contains(p, `"ja"`) {
		t.Fatalf("a part, translated %s", p)
	}
	p = PolishPrompt("the 14th works", Custom, "mention dinner", false)
	if !strings.Contains(p, "What they ask: mention dinner") {
		t.Fatalf("in the user's words %s", p)
	}
}

func TestReadingSettings(t *testing.T) {
	for n, want := range map[int]int{0: 2, 1: 1, 3: 3, 7: 2, -1: 2} {
		if got := (Reading{PreviewLines: n}).Lines(); got != want {
			t.Errorf("preview_lines %d: %d lines, want %d", n, got, want)
		}
	}
	for n, want := range map[int]float64{0: 1, 100: 1, 130: 1.3, 85: 0.85, 300: 1, 40: 1} {
		if got := (Reading{TextSize: n}).Scale(); got != want {
			t.Errorf("text_size %d: %v, want %v", n, got, want)
		}
	}
}

func TestDraftsOnServer(t *testing.T) {
	srv := newIMAP(t, "Drafts")
	ctx := context.Background()
	s := srv.session(t)
	s.Acc.Name = "Me"
	s.Mailboxes(ctx)
	file := filepath.Join(t.TempDir(), "plan.txt")
	os.WriteFile(file, []byte("the plan"), 0o600)
	d := Draft{ID: "d1", To: "Ana <ana@studio.dev>, ben@", Bcc: "boss@x.org", Subject: "Plan",
		Body: "First go.\n\n-- \nMe", Attachments: []string{file}, InReplyTo: "q3@studio.dev", References: []string{"q3@studio.dev"}}
	uid, err := s.SaveDraft(ctx, d)
	if err != nil || uid == 0 {
		t.Fatalf("first save: %d %v", uid, err)
	}
	d.Server, d.Body = uid, "Second go.\n\n-- \nMe"
	uid2, err := s.SaveDraft(ctx, d)
	if err != nil || uid2 == uid {
		t.Fatalf("second save: %d %v", uid2, err)
	}
	s.Sync(ctx, "Drafts")
	items := s.Items("Drafts")
	if len(items) != 1 || items[0].UID != uid2 || !items[0].Has(DraftFlag) {
		t.Fatalf("drafts kept: %+v", items)
	}
	m, err := s.Message(ctx, "Drafts", uid2)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DraftOf(m, s.Acc, uid2, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if back.To != "Ana <ana@studio.dev>, ben@" || back.Bcc != "boss@x.org" || back.Subject != "Plan" || back.Body != "Second go.\n\n-- \nMe" {
		t.Fatalf("draft back: %+v", back)
	}
	if back.InReplyTo != "q3@studio.dev" || len(back.Attachments) != 1 || back.Server != uid2 {
		t.Fatalf("draft back: %+v", back)
	}
	if err := s.DeleteDraft(ctx, uid2); err != nil {
		t.Fatal(err)
	}
	if s.Sync(ctx, "Drafts"); len(s.Items("Drafts")) != 0 {
		t.Fatal("the draft stayed")
	}
}

const googleInvite = "BEGIN:VCALENDAR\r\nPRODID:-//Google Inc//Google Calendar 70.9054//EN\r\nVERSION:2.0\r\nMETHOD:REQUEST\r\n" +
	"BEGIN:VTIMEZONE\r\nTZID:Asia/Tokyo\r\nEND:VTIMEZONE\r\n" +
	"BEGIN:VEVENT\r\nDTSTART;TZID=Asia/Tokyo:20261021T140000\r\nDTEND;TZID=Asia/Tokyo:20261021T150000\r\n" +
	"DTSTAMP:20261019T010000Z\r\nORGANIZER;CN=Ana Souza:mailto:ana@studio.dev\r\nUID:abc123@google.com\r\n" +
	"ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=\r\n TRUE;CN=me@example.com;X-NUM-GUESTS=0:mailto:me@example.com\r\n" +
	"ATTENDEE;PARTSTAT=ACCEPTED;CN=Ben Ito:mailto:ben@studio.dev\r\n" +
	"SEQUENCE:2\r\nSUMMARY:Q3 review\\, with numbers\r\nLOCATION:Room 4 / meet.google.com/abc-defg-hij\r\n" +
	"DESCRIPTION:Bring the deck.\\nThanks\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestInvite(t *testing.T) {
	inv := ParseInvite([]byte(googleInvite))
	if inv == nil || inv.Method != "REQUEST" || !inv.Asks() || inv.Summary != "Q3 review, with numbers" || inv.UID != "abc123@google.com" {
		t.Fatalf("invite %+v", inv)
	}
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	if !inv.Start.Equal(time.Date(2026, 10, 21, 14, 0, 0, 0, tokyo)) || inv.End.Sub(inv.Start) != time.Hour {
		t.Fatalf("times %v %v", inv.Start, inv.End)
	}
	if inv.Organizer.Addr != "ana@studio.dev" || inv.Organizer.Name != "Ana Souza" || len(inv.Attendees) != 2 {
		t.Fatalf("people %+v %+v", inv.Organizer, inv.Attendees)
	}
	if inv.Status("ME@example.com") != "NEEDS-ACTION" || inv.Status("ben@studio.dev") != Accepted || inv.URL != "https://meet.google.com/abc-defg-hij" {
		t.Fatalf("status %q url %q", inv.Status("me@example.com"), inv.URL)
	}
	outlook := ParseInvite([]byte("BEGIN:VCALENDAR\nMETHOD:CANCEL\nBEGIN:VEVENT\nDTSTART;TZID=Pacific Standard Time:20261021T090000\nSUMMARY:Standup\nEND:VEVENT\nEND:VCALENDAR\n"))
	la, _ := time.LoadLocation("America/Los_Angeles")
	if outlook == nil || outlook.Method != "CANCEL" || outlook.Asks() || !outlook.Start.Equal(time.Date(2026, 10, 21, 9, 0, 0, 0, la)) {
		t.Fatalf("outlook %+v", outlook)
	}
	day := ParseInvite([]byte("BEGIN:VEVENT\nDTSTART;VALUE=DATE:20261101\nSUMMARY:Holiday\nEND:VEVENT\n"))
	if day == nil || !day.AllDay || day.End.Sub(day.Start) != 24*time.Hour {
		t.Fatalf("all day %+v", day)
	}
	if ParseInvite([]byte("BEGIN:VCALENDAR\nEND:VCALENDAR\n")) != nil {
		t.Fatal("an event from nothing")
	}

	// The answer, sent to the organizer.
	srv := newIMAP(t, "Sent")
	be, smtpAddr := newSMTP(t)
	ctx := context.Background()
	s := srv.session(t)
	s.Acc.SMTP, s.Acc.Name = smtpAddr, "Lyric"
	s.Mailboxes(ctx)
	if err := s.Answer(ctx, inv, Accepted); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(be.rcpts, []string{"ana@studio.dev"}) {
		t.Fatalf("sent to %v", be.rcpts)
	}
	got := string(be.got)
	for _, want := range []string{"Subject: Accepted: Q3 review, with numbers", "method=REPLY", "METHOD:REPLY", "UID:abc123@google.com",
		"SEQUENCE:2", "PARTSTAT=ACCEPTED", "mailto:me@example.com", "ORGANIZER;CN=Ana Souza:mailto:ana@studio.dev"} {
		if !strings.Contains(strings.ReplaceAll(got, "\r\n ", ""), want) {
			t.Errorf("the reply lacks %q:\n%s", want, got)
		}
	}
}

func TestSyncKeepsLocal(t *testing.T) {
	srv := newIMAP(t)
	srv.put(t, "INBOX", msg("a@x", "x@x.org", "one", "t"))
	ctx := context.Background()
	s := srv.session(t)
	s.Sync(ctx, "INBOX")
	uid := s.Items("INBOX")[0].UID
	// A sync that began before the tag and the answer were kept.
	s.imu.Lock()
	work := s.index("INBOX").copy()
	s.imu.Unlock()
	s.SetTag("INBOX", uid, TagFYI, true)
	s.SaveAnswer("INBOX", uid, Accepted)
	s.imu.Lock()
	keepLocal(work, s.index("INBOX"))
	s.indexes["INBOX"] = work
	s.imu.Unlock()
	if it := s.Items("INBOX")[0]; it.Tag != TagFYI || !it.TagByUser || s.AnswerOf("INBOX", uid) != Accepted {
		t.Fatalf("lost: %+v %q", *it.Envelope, s.AnswerOf("INBOX", uid))
	}
	if _, err := s.Sync(ctx, "INBOX"); err != nil || s.AnswerOf("INBOX", uid) != Accepted {
		t.Fatalf("a sync lost the answer: %v", err)
	}
}

func TestAttachmentText(t *testing.T) {
	if s, err := (Attachment{Name: "notes.md", Type: "text/markdown", Data: []byte("# Plan\n\nShip on Friday.")}).Text(); err != nil || s != "# Plan\n\nShip on Friday." {
		t.Fatalf("text %q %v", s, err)
	}
	var docx bytes.Buffer
	zw := zip.NewWriter(&docx)
	f, _ := zw.Create("word/document.xml")
	io.WriteString(f, `<w:document><w:body><w:p><w:r><w:t>Contract &amp; terms</w:t></w:r></w:p><w:p><w:r><w:t>Sign by 1 Nov.</w:t></w:r></w:p></w:body></w:document>`)
	zw.Close()
	a := Attachment{Name: "contract.docx", Data: docx.Bytes()}
	if s, err := a.Text(); err != nil || s != "Contract & terms\nSign by 1 Nov." || !a.HasText() {
		t.Fatalf("docx %q %v", s, err)
	}
	if (Attachment{Name: "photo.png", Type: "image/png"}).HasText() {
		t.Fatal("a picture has text")
	}
	if _, err := (Attachment{Name: "x.zip", Type: "application/zip"}).Text(); !errors.Is(err, ErrNoText) {
		t.Fatalf("zip: %v", err)
	}
	pdf := "%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n" +
		"3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj\n" +
		"4 0 obj << /Length 52 >> stream\nBT /F1 12 Tf 72 712 Td (Invoice total 14.28 EUR) Tj ET\nendstream endobj\n" +
		"5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n"
	s, err := (Attachment{Name: "R0021455.pdf", Type: "application/pdf", Data: []byte(pdf)}).Text()
	if err != nil && strings.Contains(err.Error(), "poppler") {
		t.Skip("no poppler:", err)
	}
	if err != nil || !strings.Contains(s, "Invoice total 14.28 EUR") {
		t.Fatalf("pdf %q %v", s, err)
	}
}

func TestHoldAndUndo(t *testing.T) {
	srv := newIMAP(t, "Archive")
	for i := range 3 {
		srv.put(t, "INBOX", msg(fmt.Sprintf("h%d@x", i), "x@x.org", fmt.Sprint("n", i), "t"))
	}
	ctx := context.Background()
	s := srv.session(t)
	s.Mailboxes(ctx)
	s.Sync(ctx, "INBOX")
	held := s.Hold("INBOX", []uint32{3}) // the newest: a sync would fetch it again
	if len(held) != 1 || len(s.Items("INBOX")) != 2 {
		t.Fatalf("held %d, listed %d", len(held), len(s.Items("INBOX")))
	}
	if s.Sync(ctx, "INBOX"); len(s.Items("INBOX")) != 2 {
		t.Fatal("a sync listed a held message again")
	}
	s.Release("INBOX", held)
	if len(s.Items("INBOX")) != 3 {
		t.Fatal("undo did not put it back")
	}
	held = s.Hold("INBOX", []uint32{3})
	if err := s.MoveHeld(ctx, "INBOX", held, "Archive"); err != nil {
		t.Fatal(err)
	}
	s.Sync(ctx, "INBOX")
	s.Sync(ctx, "Archive")
	if len(s.Items("INBOX")) != 2 || len(s.Items("Archive")) != 1 {
		t.Fatalf("after the move: inbox %d, archive %d", len(s.Items("INBOX")), len(s.Items("Archive")))
	}
	held = s.Hold("INBOX", []uint32{2})
	if err := s.MoveHeld(ctx, "INBOX", held, "Nowhere"); err == nil || len(s.Items("INBOX")) != 2 {
		t.Fatalf("a refused move: %v, %d listed", err, len(s.Items("INBOX")))
	}
}

func TestContacts(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	mine := func(a string) bool { return a == "me@x" }
	it := func(from Address, to []Address, days int, bulk bool) Item {
		return Item{Envelope: &Envelope{From: from, To: to, Date: now.AddDate(0, 0, -days), Bulk: bulk}}
	}
	items := []Item{
		it(Address{Addr: "me@x"}, []Address{{Name: "Ana Souza", Addr: "ana@studio.dev"}}, 1, false),
		it(Address{Name: "Ben Ito", Addr: "ben@studio.dev"}, []Address{{Addr: "me@x"}}, 2, false),
		it(Address{Name: "Ben Ito", Addr: "ben@studio.dev"}, []Address{{Addr: "me@x"}}, 3, false),
		it(Address{Name: "Go Blog", Addr: "news@golang.example"}, nil, 1, true),
		it(Address{Name: "GitHub", Addr: "noreply@github.com"}, nil, 1, false),
		it(Address{Name: "Old Friend", Addr: "old@x.org"}, nil, 900, false),
	}
	cs := Contacts(items, mine, now)
	var addrs []string
	for _, c := range cs {
		addrs = append(addrs, c.Addr)
	}
	if !slices.Equal(addrs, []string{"ana@studio.dev", "ben@studio.dev", "old@x.org"}) {
		t.Fatalf("contacts %v", addrs)
	}
	if got := Suggest(cs, "sou", 5); len(got) != 1 || got[0].Name != "Ana Souza" {
		t.Fatalf("by name %+v", got)
	}
	if got := Suggest(cs, "BEN@", 5); len(got) != 1 {
		t.Fatalf("by address %+v", got)
	}
	if Suggest(cs, "", 5) != nil || len(Suggest(cs, "o", 1)) != 1 {
		t.Fatal("empty or limit")
	}
}

func TestClientSecretInKeyring(t *testing.T) {
	dir := t.TempDir()
	keys := SecretsAt(filepath.Join(dir, "s.json"))
	keys.Set(SecretKey, ClientSecretKind(Google), "GOCSPX-kept")
	var s Settings
	s.Keyring = keys
	s.OAuth.Google = OAuthClient{ClientID: "mine.apps.googleusercontent.com", SecretInKeyring: true}
	path := filepath.Join(dir, "mail.toml")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "GOCSPX") || !strings.Contains(string(b), "secret_in_keyring = true") {
		t.Fatalf("mail.toml:\n%s", b)
	}
	ep, err := s.Endpoint(Google)
	if err != nil || ep.Client.ClientSecret != "" {
		t.Fatalf("endpoint %+v %v", ep.Client, err)
	}
	// The trade reads it from the keyring.
	p := newProvider(t)
	ep.TokenURL = p.srv.URL + "/token"
	ep.Client.ClientID = "cid"
	if _, err := ep.Refresh(context.Background(), "r1"); err != nil || p.secret != "GOCSPX-kept" {
		t.Fatalf("refresh sent secret %q: %v", p.secret, err)
	}
	// Gone from the keyring: said, not sent empty.
	keys.Set(SecretKey, ClientSecretKind(Google), "")
	if _, err := ep.Refresh(context.Background(), "r1"); err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Fatalf("missing secret: %v", err)
	}
	// One written in mail.toml by hand still wins.
	s.OAuth.Google.ClientSecret = "by-hand"
	if ep, _ := s.Endpoint(Google); ep.Client.ClientSecret != "by-hand" || ep.secret != nil {
		t.Fatalf("by hand %+v", ep.Client)
	}
}

func TestShownHost(t *testing.T) {
	for text, want := range map[string]string{
		"yarn.lock":                  "",
		"readme.md":                  "",
		"setup.py":                   "",
		"Click here":                 "",
		"paypal.com":                 "paypal.com",
		"www.readme.md":              "www.readme.md",
		"https://notes.zip/x":        "notes.zip",
		"bitbank.cc":                 "bitbank.cc",
		"github.com/fox-one/x":       "github.com",
		"accounts.google.co.uk/home": "accounts.google.co.uk",
	} {
		if got := shownHost(text); got != want {
			t.Errorf("shownHost(%q) = %q, want %q", text, got, want)
		}
	}
	if site("a.example.co.uk") != "example.co.uk" || site("mail.github.com") != "github.com" {
		t.Error("site")
	}
}

// A Google account whose sign-in gave no photo (some Workspace accounts):
// the profile, read with the account's token, gives it.
func TestPictureFromProfile(t *testing.T) {
	p := newProvider(t)
	pic := "https://lh3.googleusercontent.com/a/photo=s96-c"
	profile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer a2" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"sub": "1", "picture": pic})
	}))
	t.Cleanup(profile.Close)
	acc := Account{ID: "ws", Address: "me@example.org", Provider: Google}
	s := NewSession(acc, Settings{}, StoreAt(t.TempDir()), SecretsAt(filepath.Join(t.TempDir(), "s.json")))
	t.Cleanup(s.Close)
	s.ep = p.endpoint()
	s.ep.UserInfo = profile.URL
	s.UseAuth(Auth{User: acc.Address, Tokens: NewTokens(s.ep, "r1", nil)})
	got, err := s.Picture(context.Background())
	if err != nil || got != pic {
		t.Fatalf("picture %q, %v", got, err)
	}
	// None given, or not an https address: no photo.
	pic = "http://example.org/p.png"
	if got, err := s.Picture(context.Background()); err != nil || got != "" {
		t.Fatalf("plain http picture %q, %v", got, err)
	}
	// A provider with no profile to read: nothing asked.
	s.ep.UserInfo = ""
	if got, err := s.Picture(context.Background()); err != nil || got != "" {
		t.Fatalf("no profile %q, %v", got, err)
	}
}

// A conversation's messages are read in one command, on a connection of
// their own: reading never waits for a command the session is busy with.
func TestMessagesTogether(t *testing.T) {
	srv := newIMAP(t)
	for i := range 3 {
		srv.put(t, "INBOX", msg(fmt.Sprintf("m%d@x", i), "Ben <ben@x.org>", fmt.Sprintf("Part %d", i), "Text."))
	}
	ctx := context.Background()
	s := srv.session(t)
	if _, err := s.Sync(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	var uids []uint32
	for _, it := range s.Items("INBOX") {
		uids = append(uids, it.UID)
	}
	// The connection that syncs is busy until the messages are read.
	busy, done := make(chan struct{}), make(chan struct{})
	go s.Do(ctx, func(*Conn) error { close(busy); <-done; return nil })
	<-busy
	before := srv.bodies.Load()
	read := make(chan []Read, 1)
	go func() { read <- s.Messages(ctx, "INBOX", append(uids, 999)) }()
	var reads []Read
	select {
	case reads = <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("reading waited for the busy connection")
	}
	close(done)
	if n := srv.bodies.Load() - before; n != 1 {
		t.Fatalf("%d FETCH commands for 3 messages", n)
	}
	for _, r := range reads[:3] {
		if r.Err != nil || r.Message == nil || r.Message.Text == "" {
			t.Fatalf("read %+v", r)
		}
	}
	if !errors.Is(reads[3].Err, ErrNotFound) {
		t.Fatalf("a message not there: %v", reads[3].Err)
	}
	// Kept: read again, nothing is asked.
	before = srv.bodies.Load()
	s.Messages(ctx, "INBOX", uids)
	if srv.bodies.Load() != before {
		t.Fatal("kept messages fetched again")
	}
}

// Prefetch keeps the newest messages, not a large one, without marking
// any read, and asks nothing once they are kept.
func TestPrefetch(t *testing.T) {
	srv := newIMAP(t)
	for i := range PrefetchCount + 3 {
		srv.put(t, "INBOX", msg(fmt.Sprintf("p%d@x", i), "Ben <ben@x.org>", fmt.Sprintf("Note %d", i), "Text."))
	}
	srv.put(t, "INBOX", msg("big@x", "Ben <ben@x.org>", "Big", strings.Repeat("x", PrefetchMax+1)))
	ctx := context.Background()
	s := srv.session(t)
	if _, err := s.Sync(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	items := s.Items("INBOX")
	slices.SortFunc(items, func(a, b Item) int { return int(int64(b.UID) - int64(a.UID)) })
	before := srv.bodies.Load()
	n, err := s.Prefetch(ctx, "INBOX")
	if err != nil || n != PrefetchCount-1 { // the newest but the big one
		t.Fatalf("prefetched %d, %v", n, err)
	}
	if srv.bodies.Load()-before != 1 {
		t.Fatal("prefetch took more than one command")
	}
	for i, it := range items {
		kept := s.store.HasRaw(s.Acc.ID, "INBOX", it.UID)
		if want := i > 0 && i < PrefetchCount; kept != want {
			t.Fatalf("message %d (%s): kept %v", i, it.Subject, kept)
		}
	}
	if n, _ := s.Prefetch(ctx, "INBOX"); n != 0 {
		t.Fatalf("prefetched again: %d", n)
	}
	// None marked read, here or on the server.
	other := srv.session(t)
	other.Sync(ctx, "INBOX")
	for _, it := range other.Items("INBOX") {
		if it.Has(Seen) {
			t.Fatalf("%s marked read", it.Subject)
		}
	}
}

// Messages indexed before Verified was kept have the provider's
// Authentication-Results read at a sync: a sender's other messages show
// its logo too, not the one opened alone.
func TestSyncReadsAuth(t *testing.T) {
	s := newIMAP(t)
	s.put(t, "INBOX", msg("a@github.com", "GitHub <noreply@github.com>", "Passed", "hi",
		"Authentication-Results: mx.example.com; dmarc=pass (p=REJECT) header.from=github.com"))
	s.put(t, "INBOX", msg("b@github.com", "GitHub <noreply@github.com>", "Failed", "hi",
		"Authentication-Results: mx.example.com; dmarc=fail (p=REJECT) header.from=github.com"))
	sess := s.session(t)
	ctx := context.Background()
	if _, err := sess.Sync(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	// As an index from before: neither read.
	sess.imu.Lock()
	x := sess.index("INBOX")
	for i := range x.Messages {
		x.Messages[i].Verified, x.Messages[i].AuthRead = false, false
	}
	sess.imu.Unlock()
	if _, err := sess.Sync(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	sess.imu.Lock()
	defer sess.imu.Unlock()
	got := map[string]bool{}
	for _, e := range sess.index("INBOX").Messages {
		if !e.AuthRead {
			t.Errorf("%s: not read", e.Subject)
		}
		got[e.Subject] = e.Verified
	}
	if !got["Passed"] || got["Failed"] {
		t.Errorf("verified: %v", got)
	}
}

// A message's details: who, when, who mailed and signed it, its list;
// what says nothing is left out.
func TestDetails(t *testing.T) {
	raw := msg("n1@blog.com", "Blog <letters@blog.com>", "This week", "Hello",
		"Sender: bounce@mailer.net",
		"Reply-To: Blog <letters@blog.com>",
		"Cc: Ben <ben@x.org>",
		"Return-Path: <bounce-77@mailer.net>",
		"List-Id: Blog weekly <weekly.blog.com>",
		"Authentication-Results: mx.example.com; spf=pass smtp.mailfrom=mailer.net; dkim=pass header.i=@blog.com; dmarc=pass header.from=blog.com",
		"DKIM-Signature: v=1; a=rsa-sha256; d=mailer.net; s=k1; b=x")
	m, err := Parse([]byte(strings.ReplaceAll(raw, "\n", "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range Details(m) {
		v := d.Text
		for _, a := range d.People {
			v += a.Addr + " "
		}
		got[d.Name] = strings.TrimSpace(v)
	}
	for name, want := range map[string]string{
		"From": "letters@blog.com", "Sent by": "bounce@mailer.net", "To": "me@example.com", "Cc": "ben@x.org",
		"Mailed by": "mailer.net", "Signed by": "blog.com", "Checks": "SPF pass · DKIM pass · DMARC pass",
		"Mailing list": "Blog weekly <weekly.blog.com>", "Message-ID": "<n1@blog.com>", "Subject": "This week",
	} {
		if got[name] != want {
			t.Errorf("%s: %q, want %q", name, got[name], want)
		}
	}
	for _, name := range []string{"Reply to", "Bcc"} {
		if _, ok := got[name]; ok {
			t.Errorf("%s shown: %q", name, got[name])
		}
	}
	if !strings.Contains(got["Date"], "2026") {
		t.Errorf("date %q", got["Date"])
	}
}

// An HTML message whose start is all its style sheet still has a
// snippet: its part is read further; one indexed without it before gets
// it at the next sync.
func TestSnippetPastStyle(t *testing.T) {
	s := newIMAP(t)
	style := "<html><head><style>" + strings.Repeat(".a{color:red}\n", 400) + "</style></head><body><p>New sign-in to your account</p></body></html>"
	raw := "From: Spotify <no-reply@spotify.com>\nTo: me@example.com\nSubject: New sign-in\nDate: Mon, 13 Oct 2026 10:00:00 +0000\nMessage-ID: <sp1@spotify.com>\n" +
		"Content-Type: text/html; charset=utf-8\n\n" + style + "\n"
	s.put(t, "INBOX", raw)
	sess := s.session(t)
	ctx := context.Background()
	if _, err := sess.Sync(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	snippet := func() (string, bool) {
		sess.imu.Lock()
		defer sess.imu.Unlock()
		e := sess.index("INBOX").Messages[0]
		return e.Snippet, e.SnippetRead
	}
	if sn, ok := snippet(); sn != "New sign-in to your account" || !ok {
		t.Fatalf("snippet %q, read %v", sn, ok)
	}
	// As an index from before: none, not read.
	sess.imu.Lock()
	x := sess.index("INBOX")
	x.Messages[0].Snippet, x.Messages[0].SnippetRead = "", false
	sess.imu.Unlock()
	if _, err := sess.Sync(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if sn, ok := snippet(); sn != "New sign-in to your account" || !ok {
		t.Errorf("read again: %q, read %v", sn, ok)
	}
}

func TestDeletePermanently(t *testing.T) {
	srv := newIMAP(t, "Trash")
	srv.put(t, "INBOX", msg("a@x", "Ben <ben@x.org>", "Hello", "Hi"))
	srv.put(t, "INBOX", msg("b@x", "Ben <ben@x.org>", "Again", "Hi"))
	srv.put(t, "Trash", msg("c@x", "Ben <ben@x.org>", "Old", "Hi"))
	ctx := context.Background()
	s := srv.session(t)
	if _, err := s.Mailboxes(ctx); err != nil {
		t.Fatal(err)
	}
	s.Sync(ctx, "INBOX")
	s.Sync(ctx, "Trash")
	count := func(box string) int {
		other := srv.session(t)
		other.Mailboxes(ctx)
		if _, err := other.Sync(ctx, box); err != nil {
			t.Fatal(err)
		}
		return len(other.Items(box))
	}

	// From the inbox, by way of the Trash: gone from both.
	hello := s.Items("INBOX")[1]
	if err := s.DeletePermanently(ctx, "INBOX", []uint32{hello.UID}); err != nil {
		t.Fatal(err)
	}
	if len(s.Items("INBOX")) != 1 || count("INBOX") != 1 || count("Trash") != 1 {
		t.Fatalf("after deleting: inbox %d on the server %d, trash %d", len(s.Items("INBOX")), count("INBOX"), count("Trash"))
	}

	// Emptying takes what came after the last sync too.
	srv.put(t, "Trash", msg("d@x", "Ben <ben@x.org>", "New", "Hi"))
	if err := s.Empty(ctx, "Trash"); err != nil {
		t.Fatal(err)
	}
	if len(s.Items("Trash")) != 0 || count("Trash") != 0 {
		t.Fatalf("after emptying: %d listed, %d on the server", len(s.Items("Trash")), count("Trash"))
	}
	if count("INBOX") != 1 {
		t.Fatal("emptying the Trash touched the inbox")
	}
}
