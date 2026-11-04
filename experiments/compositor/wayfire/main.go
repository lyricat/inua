// Command wayfire is a disposable probe of Wayfire's IPC for #61: it lists
// views, outputs and workspace sets, watches events, and runs the few
// methods the vertical slice needs. It needs the ipc and ipc-rules plugins
// (and vswitch for workspaces) enabled in wayfire.ini. It prints everything
// it sends and receives.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

var start = time.Now()

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%9.3f %s\n", time.Since(start).Seconds(), fmt.Sprintf(format, args...))
}

// conn frames messages as a 4-byte length in the host's byte order followed
// by the JSON body (plugins/ipc/ipc.cpp). Wayfire and this program run on
// the same machine, so native order is the protocol's order.
type conn struct{ c net.Conn }

func dial(ctx context.Context, path string) (*conn, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	return &conn{c: c}, nil
}

func (c *conn) send(method string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{"method": method, "data": data})
	logf("-> %s", body)
	hdr := make([]byte, 4)
	binary.NativeEndian.PutUint32(hdr, uint32(len(body)))
	_, err := c.c.Write(append(hdr, body...))
	return err
}

func (c *conn) read() (map[string]any, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c.c, hdr); err != nil {
		return nil, err
	}
	n := binary.NativeEndian.Uint32(hdr)
	if n > 1<<20 {
		return nil, fmt.Errorf("message of %d bytes", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c.c, body); err != nil {
		return nil, err
	}
	logf("<- %s", body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if e, ok := m["error"]; ok {
		return m, fmt.Errorf("wayfire: %v", e)
	}
	return m, nil
}

func (c *conn) call(method string, data map[string]any) (map[string]any, error) {
	if err := c.send(method, data); err != nil {
		return nil, err
	}
	return c.read()
}

func xy(s string) (map[string]any, error) {
	a, b, ok := strings.Cut(s, ",")
	if !ok {
		return nil, errors.New("want X,Y")
	}
	x, err1 := strconv.Atoi(a)
	y, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil {
		return nil, errors.New("want X,Y")
	}
	return map[string]any{"x": x, "y": y}, nil
}

func main() {
	focusView := flag.Int("focus-view", 0, "focus the view with this id")
	setWorkspace := flag.String("set-workspace", "", "switch the output to the grid cell X,Y")
	output := flag.Int("output", -1, "output id for -set-workspace (default: the focused output)")
	sendView := flag.Int("send-view", 0, "send this view ...")
	to := flag.String("to", "", "... to the grid cell X,Y")
	reconnect := flag.Bool("reconnect", false, "reconnect with backoff when the socket closes")
	noFollow := flag.Bool("no-follow", false, "do not watch events")
	flag.Parse()

	path := os.Getenv("WAYFIRE_SOCKET")
	if path == "" {
		logf("WAYFIRE_SOCKET is not set: not inside a Wayfire session, or the ipc plugin is off")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c, err := dial(ctx, path)
	if err != nil {
		logf("dial: %v", err)
		os.Exit(1)
	}
	// Capability probe first: a method is there only when its plugin is.
	methods := map[string]bool{}
	if m, err := c.call("list-methods", nil); err == nil {
		if list, ok := m["methods"].([]any); ok {
			for _, x := range list {
				methods[fmt.Sprint(x)] = true
			}
		}
	}
	for _, need := range []string{"window-rules/list-views", "window-rules/list-outputs", "window-rules/list-wsets",
		"window-rules/focus-view", "window-rules/events/watch", "vswitch/set-workspace", "vswitch/send-view",
		"wayfire/create-headless-output", "command/register-binding"} {
		logf("method %s: %v", need, methods[need])
	}
	for _, m := range []string{"wayfire/configuration", "window-rules/list-outputs", "window-rules/list-wsets",
		"window-rules/list-views", "window-rules/get-focused-view", "window-rules/get-focused-output"} {
		if _, err := c.call(m, nil); err != nil {
			logf("%s: %v", m, err)
		}
	}

	switch {
	case *focusView != 0:
		_, err := c.call("window-rules/focus-view", map[string]any{"id": *focusView})
		logf("focus-view: %v", err)
	case *setWorkspace != "":
		cell, err := xy(*setWorkspace)
		if err != nil {
			logf("-set-workspace: %v", err)
			os.Exit(2)
		}
		if *output >= 0 {
			cell["output-id"] = *output
		} else if fo, err := c.call("window-rules/get-focused-output", nil); err == nil {
			if info, ok := fo["info"].(map[string]any); ok {
				cell["output-id"] = info["id"]
			} else {
				cell["output-id"] = fo["id"]
			}
		}
		_, err = c.call("vswitch/set-workspace", cell)
		logf("set-workspace: %v", err)
	case *sendView != 0:
		cell, err := xy(*to)
		if err != nil {
			logf("-to: %v", err)
			os.Exit(2)
		}
		cell["view-id"] = *sendView
		_, err = c.call("vswitch/send-view", cell)
		logf("send-view: %v", err)
	}
	// Probes: an unknown method and an unknown view id must be errors on the
	// same connection, not a disconnect.
	if _, err := c.call("inua/no-such-method", nil); err != nil {
		logf("unknown method: %v", err)
	}
	if _, err := c.call("window-rules/focus-view", map[string]any{"id": 1 << 30}); err != nil {
		logf("focus-view with an unknown id: %v", err)
	}
	if *noFollow {
		c.c.Close()
		return
	}

	delay := 250 * time.Millisecond
	for {
		err := watch(ctx, c)
		c.c.Close()
		if ctx.Err() != nil {
			return
		}
		logf("event connection ended: %v", err)
		if !*reconnect {
			return
		}
		logf("reconnecting in %v", delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		if delay < 5*time.Second {
			delay *= 2
		}
		if c, err = dial(ctx, os.Getenv("WAYFIRE_SOCKET")); err != nil {
			logf("dial: %v", err)
			continue
		}
	}
}

// watch subscribes to every auto-registered event (no "events" list means
// all of them except view-pre-map) and prints them with a sequence number.
func watch(ctx context.Context, c *conn) error {
	go func() { <-ctx.Done(); c.c.Close() }()
	if _, err := c.call("window-rules/events/watch", nil); err != nil {
		return err
	}
	n := 0
	for {
		m, err := c.read()
		if err != nil {
			return err
		}
		n++
		logf("ev %d %v", n, m["event"])
	}
}
