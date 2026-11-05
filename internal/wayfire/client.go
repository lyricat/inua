// Package wayfire is the Wayfire backend of the compositor contract.
// Wayfire's control is its plugins' IPC methods on $WAYFIRE_SOCKET (a u32
// length in host order, then a JSON object {"method", "data"}): ipc and
// ipc-rules for the windows and the events, vswitch for the workspace
// grid. A method is there only while its plugin is loaded, so the
// capabilities are probed with list-methods at each connection.
//
// Wayfire's workspaces are a grid of cells per workspace set, a set per
// output; a view's place is relative to the cell its output shows. What
// was observed on Wayfire 0.11 is in docs/compositor-experiments.md.
package wayfire

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

// maxMessage is the IPC's own limit (plugins/ipc).
const maxMessage = 1 << 20

// Client calls methods on one Wayfire.
type Client struct {
	Socket  string
	Timeout time.Duration
}

// SocketFromEnv is the socket of the Wayfire this process runs under.
func SocketFromEnv() (string, error) {
	s := os.Getenv("WAYFIRE_SOCKET")
	if s == "" {
		return "", compositor.ErrNoCompositor
	}
	return s, nil
}

func NewClient(socket string) *Client { return &Client{Socket: socket, Timeout: 2 * time.Second} }

func writeMessage(w io.Writer, method string, data any) error {
	if data == nil {
		data = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{"method": method, "data": data})
	if err != nil {
		return err
	}
	msg := make([]byte, 4, 4+len(body))
	binary.NativeEndian.PutUint32(msg, uint32(len(body)))
	_, err = w.Write(append(msg, body...))
	return err
}

func readMessage(r io.Reader) (json.RawMessage, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	n := binary.NativeEndian.Uint32(hdr)
	if n > maxMessage {
		return nil, fmt.Errorf("wayfire: message of %d bytes", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// replyError is the error a reply carries: an object with "error".
func replyError(method string, body json.RawMessage) error {
	var e struct {
		Error *string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != nil {
		return fmt.Errorf("wayfire: %s: %s", method, *e.Error)
	}
	return nil
}

// Call runs a method on a connection of its own and returns its reply (an
// object, or an array for the list methods), or Wayfire's error.
func (c *Client) Call(ctx context.Context, method string, data any) (json.RawMessage, error) {
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
	if err := writeMessage(conn, method, data); err != nil {
		return nil, err
	}
	body, err := readMessage(conn)
	if err != nil {
		return nil, err
	}
	return body, replyError(method, body)
}

// Methods are the methods the loaded plugins have.
func (c *Client) Methods(ctx context.Context) (map[string]bool, error) {
	body, err := c.Call(ctx, "list-methods", nil)
	if err != nil {
		return nil, err
	}
	var r struct{ Methods []string }
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("wayfire: list-methods: %w", err)
	}
	out := make(map[string]bool, len(r.Methods))
	for _, m := range r.Methods {
		out[m] = true
	}
	return out, nil
}

// watch subscribes to every event and sends each one's name to out until
// the connection fails or ctx ends. The returned error is never nil.
func watch(ctx context.Context, socket string, connected func(), out chan<- string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err := writeMessage(conn, "window-rules/events/watch", nil); err != nil {
		return err
	}
	body, err := readMessage(conn)
	if err != nil {
		return err
	}
	if err := replyError("window-rules/events/watch", body); err != nil {
		return err
	}
	connected()
	for {
		body, err := readMessage(conn)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("wayfire: event connection closed")
			}
			return err
		}
		var ev struct{ Event string }
		if json.Unmarshal(body, &ev) != nil || ev.Event == "" {
			continue
		}
		select {
		case out <- ev.Event:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
