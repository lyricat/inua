package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/sockdir"
)

const statusJSON = `{
  "BackendState": "Running",
  "Self": {"ID": "nSELF", "HostName": "laptop", "DNSName": "laptop.example.ts.net.", "OS": "linux",
           "TailscaleIPs": ["100.64.0.1", "fd7a:115c:a1e0::1"], "Online": true},
  "CurrentTailnet": {"Name": "example", "MagicDNSSuffix": "example.ts.net", "MagicDNSEnabled": true},
  "Peer": {
    "k1": {"ID": "nPHONE", "HostName": "localhost", "DNSName": "phone.example.ts.net.", "OS": "iOS",
           "TailscaleIPs": ["100.64.0.2"], "Online": false, "LastSeen": "2026-10-04T10:00:00Z"},
    "k2": {"ID": "nPI", "HostName": "pi", "DNSName": "pi.example.ts.net.", "OS": "linux",
           "TailscaleIPs": ["fd7a:115c:a1e0::3", "100.64.0.3"], "Online": true, "CurAddr": "192.168.1.9:41641",
           "ExitNodeOption": true, "Tags": ["tag:home"], "LastSeen": "0001-01-01T00:00:00Z",
           "KeyExpiry": "2026-10-20T00:00:00Z"},
    "k3": {"ID": "nBOX", "HostName": "box", "DNSName": "", "OS": "linux",
           "TailscaleIPs": ["100.64.0.4"], "Online": true, "Relay": "tok", "ExitNodeOption": true, "ExitNode": true}
  }
}`

func TestStatus(t *testing.T) {
	var st Status
	if err := json.Unmarshal([]byte(statusJSON), &st); err != nil {
		t.Fatal(err)
	}
	if s, on := st.State(); s != "Connected" || !on {
		t.Errorf("state %q %v", s, on)
	}
	var names []string
	for _, p := range st.Peers("") {
		names = append(names, p.Name())
	}
	// Online first, by name; a peer without MagicDNS goes by host name.
	if want := []string{"box", "pi", "phone"}; !slices.Equal(names, want) {
		t.Errorf("peers %v, want %v", names, want)
	}
	for q, want := range map[string]int{"PI": 1, "100.64.0.2": 1, "ios": 1, "tag:home": 1, "linux": 2, "nothing": 0} {
		if n := len(st.Peers(q)); n != want {
			t.Errorf("Peers(%q) = %d, want %d", q, n, want)
		}
	}
	pi := st.PeerByID("nPI")
	if pi.IPv4() != "100.64.0.3" || pi.MagicDNS() != "pi.example.ts.net" || pi.Connection() != "direct, 192.168.1.9:41641" {
		t.Errorf("pi %q %q %q", pi.IPv4(), pi.MagicDNS(), pi.Connection())
	}
	if c := st.PeerByID("nBOX").Connection(); c != "relayed through tok" {
		t.Errorf("box %q", c)
	}
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	if s := st.PeerByID("nPHONE").Seen(now); s != "7 d ago" {
		t.Errorf("seen %q", s)
	}
	if s := pi.Seen(now); s != "online" {
		t.Errorf("seen online %q", s)
	}
	if e := pi.Expiry(now); e != "in 8 d" {
		t.Errorf("expiry %q", e)
	}
	var exits []string
	for _, p := range st.ExitNodes() {
		exits = append(exits, p.ID)
	}
	if !slices.Equal(exits, []string{"nBOX", "nPI"}) {
		t.Errorf("exit nodes %v", exits)
	}
	if st.Online() != 2 {
		t.Errorf("online %d", st.Online())
	}
	if s, on := (Status{}).State(); s != "Not running" || on {
		t.Errorf("empty state %q", s)
	}
}

func TestOfferExit(t *testing.T) {
	p := Prefs{AdvertiseRoutes: []string{"192.168.1.0/24"}}
	if p.OffersExit() {
		t.Error("offers")
	}
	on := p.OfferExit(true)
	if !on.AdvertiseRoutesSet || !slices.Equal(on.AdvertiseRoutes, []string{"192.168.1.0/24", "0.0.0.0/0", "::/0"}) {
		t.Errorf("on %+v", on)
	}
	p.AdvertiseRoutes = on.AdvertiseRoutes
	if !p.OffersExit() {
		t.Error("does not offer")
	}
	if off := p.OfferExit(false); !slices.Equal(off.AdvertiseRoutes, []string{"192.168.1.0/24"}) {
		t.Errorf("off %+v", off)
	}
	// Offering nothing still sends an empty list, which the daemon sets.
	b, _ := json.Marshal(Prefs{}.OfferExit(false))
	if !strings.Contains(string(b), `"AdvertiseRoutes":[]`) || !strings.Contains(string(b), `"AdvertiseRoutesSet":true`) {
		t.Errorf("empty routes %s", b)
	}
}

// fakeDaemon serves a local API on a socket in a temporary directory.
func fakeDaemon(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	sock := filepath.Join(sockdir.New(t), "tailscaled.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return NewClient(sock)
}

func TestClient(t *testing.T) {
	var got []string
	var patched Edit
	var sent bytes.Buffer
	c := fakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		if r.Method != http.MethodGet && r.Header.Get("Sec-Tailscale") != "localapi" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		switch {
		case r.URL.Path == "/localapi/v0/status":
			io.WriteString(w, statusJSON)
		case r.URL.Path == "/localapi/v0/prefs" && r.Method == http.MethodPatch:
			json.NewDecoder(r.Body).Decode(&patched)
			json.NewEncoder(w).Encode(Prefs{WantRunning: patched.WantRunning, ShieldsUp: true})
		case r.URL.Path == "/localapi/v0/ping":
			json.NewEncoder(w).Encode(PingResult{NodeName: "pi", LatencySeconds: 0.012, Endpoint: "192.168.1.9:41641"})
		case strings.HasPrefix(r.URL.Path, "/localapi/v0/file-put/"):
			io.Copy(&sent, r.Body)
		case r.URL.Path == "/localapi/v0/logout":
			http.Error(w, "access denied", http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil || st.Self.Name() != "laptop" || len(st.Peer) != 3 {
		t.Fatalf("status %v %+v", err, st.Self)
	}
	p, err := c.EditPrefs(ctx, Edit{WantRunning: true, WantRunningSet: true})
	if err != nil || !p.WantRunning || !patched.WantRunningSet || patched.ShieldsUpSet {
		t.Errorf("edit %v %+v %+v", err, p, patched)
	}
	r, err := c.Ping(ctx, "100.64.0.3")
	if err != nil || r.Endpoint == "" {
		t.Errorf("ping %v %+v", err, r)
	}
	if err := c.SendFile(ctx, "nPI", "a b.png", strings.NewReader("pixels"), 6); err != nil || sent.String() != "pixels" {
		t.Errorf("send %v %q", err, sent.String())
	}
	if err := c.Logout(ctx); !errors.Is(err, ErrPermission) {
		t.Errorf("logout refused: %v", err)
	}
	want := []string{
		"GET /localapi/v0/status",
		"PATCH /localapi/v0/prefs",
		"POST /localapi/v0/ping?ip=100.64.0.3&type=disco",
		"PUT /localapi/v0/file-put/nPI/a%20b.png",
		"POST /localapi/v0/logout",
	}
	if !slices.Equal(got, want) {
		t.Errorf("requests\n%v\nwant\n%v", got, want)
	}

	if _, err := NewClient(filepath.Join(sockdir.New(t), "none.sock")).Status(ctx); !errors.Is(err, ErrNotRunning) {
		t.Errorf("no daemon: %v", err)
	}
}
