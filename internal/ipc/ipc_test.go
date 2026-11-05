package ipc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/sockdir"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(sockdir.New(t), "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, path, func(ctx context.Context, r Request) Response {
			if r.Target == "peer" {
				return Response{OK: true, Result: strconv.Itoa(Peer(ctx))}
			}
			if r.Target != "launcher" {
				return Response{Error: "no target " + r.Target}
			}
			return Response{OK: true, Result: r.Function + ":" + strings.Join(r.Args, ",")}
		})
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()

	cctx, ccancel := context.WithTimeout(context.Background(), time.Second)
	defer ccancel()
	var resp Response
	var err error
	for range 50 { // wait for the listener
		if resp, err = Call(cctx, path, Request{Op: "call", Target: "launcher", Function: "open", Args: []string{"a", "b"}}); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || resp.Result != "open:a,b" {
		t.Fatalf("resp %+v err %v", resp, err)
	}
	if _, err := Call(cctx, path, Request{Op: "call", Target: "nope"}); err == nil || !strings.Contains(err.Error(), "no target") {
		t.Fatalf("expected handler error, got %v", err)
	}

	// The handler knows who called: on Linux, this process.
	want := "0"
	if runtime.GOOS == "linux" {
		want = strconv.Itoa(os.Getpid())
	}
	if resp, err := Call(cctx, path, Request{Op: "call", Target: "peer"}); err != nil || resp.Result != want {
		t.Errorf("peer %q (%v), want %s", resp.Result, err, want)
	}

	// A second server on the same live socket must refuse to start.
	if err := Serve(context.Background(), path, nil); err == nil || !strings.Contains(err.Error(), "another instance") {
		t.Fatalf("second Serve: %v", err)
	}
}

// A caller that stops waiting (inua-pinentry, its passphrase answered
// elsewhere) ends the request's context: the handler gives it up.
func TestCallerHangsUp(t *testing.T) {
	path := filepath.Join(sockdir.New(t), "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	given := make(chan struct{})
	go ServeListener(ctx, ln, path, func(rctx context.Context, r Request) Response {
		<-rctx.Done()
		close(given)
		return Response{Error: "cancelled"}
	})
	cctx, ccancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		ccancel()
	}()
	if _, err := Call(cctx, path, Request{Op: "call", Target: "password"}); err == nil {
		t.Error("a call called off was answered")
	}
	select {
	case <-given:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler kept waiting for a caller gone")
	}
}
