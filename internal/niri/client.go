// Package niri is the niri backend of the compositor contract: niri's
// JSON IPC on $NIRI_SOCKET (one request per connection, one line each
// way; the event stream on a connection kept open, starting with the full
// state), turned into snapshots and the base actions.
package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

// Client sends requests to one niri.
type Client struct {
	Socket  string
	Timeout time.Duration
}

// SocketFromEnv is the socket of the niri this process runs under.
func SocketFromEnv() (string, error) {
	s := os.Getenv("NIRI_SOCKET")
	if s == "" {
		return "", compositor.ErrNoCompositor
	}
	return s, nil
}

func NewClient(socket string) *Client { return &Client{Socket: socket, Timeout: 2 * time.Second} }

// reply is niri's Result<Response, String>.
type reply struct {
	Ok  json.RawMessage `json:"Ok"`
	Err *string         `json:"Err"`
}

// Request sends one request (a string for a unit variant, else an object)
// and returns the Ok value, or niri's error text as an error.
func (c *Client) Request(ctx context.Context, req any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var r reply
	if err := json.Unmarshal(line, &r); err != nil {
		return nil, fmt.Errorf("niri: reply %q: %w", strings.TrimSpace(string(line)), err)
	}
	if r.Err != nil {
		return nil, fmt.Errorf("niri: %s", *r.Err)
	}
	return r.Ok, nil
}

// Action runs an action, such as Action(ctx, "FocusWindow", map[string]any{"id": 3}).
func (c *Client) Action(ctx context.Context, name string, fields map[string]any) error {
	if fields == nil {
		fields = map[string]any{}
	}
	_, err := c.Request(ctx, map[string]any{"Action": map[string]any{name: fields}})
	return err
}

// query decodes the Ok value of a unit request's reply, which niri wraps
// in the Response variant of the same name ({"Windows": [...]}).
func query[T any](ctx context.Context, c *Client, name string) (T, error) {
	var out T
	raw, err := c.Request(ctx, name)
	if err != nil {
		return out, err
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return out, fmt.Errorf("niri: %s: %w", name, err)
	}
	inner, ok := wrapped[name]
	if !ok {
		return out, fmt.Errorf("niri: %s: reply has no %s", name, name)
	}
	if err := json.Unmarshal(inner, &out); err != nil {
		return out, fmt.Errorf("niri: %s: %w", name, err)
	}
	return out, nil
}

// readEvents opens the event stream and sends each event, as niri's
// one-object-per-line map ({"WindowsChanged": {...}}), to out until the
// connection fails or ctx ends. The returned error is never nil.
func readEvents(ctx context.Context, socket string, connected func(), out chan<- map[string]json.RawMessage) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if _, err := conn.Write([]byte("\"EventStream\"\n")); err != nil {
		return err
	}
	connected()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	first := true
	for sc.Scan() {
		line := sc.Bytes()
		if first { // the reply to the request itself: {"Ok":"Handled"}
			first = false
			var r reply
			if json.Unmarshal(line, &r) == nil && r.Err != nil {
				return fmt.Errorf("niri: event stream: %s", *r.Err)
			}
			if json.Unmarshal(line, &r) == nil && r.Ok != nil {
				continue
			}
		}
		var ev map[string]json.RawMessage
		if err := json.Unmarshal(line, &ev); err != nil {
			return fmt.Errorf("niri: event %q: %w", strings.TrimSpace(string(line)), err)
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("niri: event stream closed")
}
