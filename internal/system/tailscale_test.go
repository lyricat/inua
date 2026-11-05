package system

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/lyricat/inua/internal/sockdir"
)

func TestTailscaleRead(t *testing.T) {
	socket := filepath.Join(sockdir.New(t), "tailscaled.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"BackendState": "Running",
  "Self": {"HostName": "laptop", "DNSName": "laptop.example.ts.net.", "TailscaleIPs": ["fd7a:115c:a1e0::1", "100.64.0.1"], "Online": true},
  "Peer": {"k1": {"HostName": "pi", "DNSName": "pi.example.ts.net.", "TailscaleIPs": ["100.64.0.3"], "Online": true, "ExitNode": true}}}`)
	})}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })

	st := NewTailscale(socket).Read(context.Background())
	want := TailscaleState{Installed: true, Running: true, Backend: "Running", Online: 1, ExitNode: "pi",
		IP: "100.64.0.1", DNS: "laptop.example.ts.net"}
	if st != want {
		t.Errorf("state\n got %+v\nwant %+v", st, want)
	}
}
