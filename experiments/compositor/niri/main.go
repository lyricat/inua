// Command niri is a disposable probe of niri's IPC for #61: it lists windows
// and workspaces, follows the event stream, and runs the few actions the
// vertical slice needs. It prints everything it sends and receives.
package main

import (
	"bufio"
	"context"
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

// request sends one JSON request on a fresh connection and returns the
// reply, which niri sends as {"Ok": ...} or {"Err": "..."}.
func request(sock string, req any) (json.RawMessage, error) {
	c, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	b, _ := json.Marshal(req)
	logf("-> %s", b)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	logf("<- %s", strings.TrimSpace(string(line)))
	var reply struct {
		Ok  json.RawMessage `json:"Ok"`
		Err *string         `json:"Err"`
	}
	if err := json.Unmarshal(line, &reply); err != nil {
		return nil, fmt.Errorf("reply is not Ok/Err: %w", err)
	}
	if reply.Err != nil {
		return nil, errors.New(*reply.Err)
	}
	return reply.Ok, nil
}

func action(sock string, name string, fields map[string]any) error {
	if fields == nil {
		fields = map[string]any{}
	}
	_, err := request(sock, map[string]any{"Action": map[string]any{name: fields}})
	return err
}

// follow keeps the event stream open and prints each event. The first
// events are the full state (WorkspacesChanged, WindowsChanged, ...).
func follow(ctx context.Context, sock string) error {
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return err
	}
	defer c.Close()
	go func() { <-ctx.Done(); c.Close() }()
	if _, err := c.Write([]byte("\"EventStream\"\n")); err != nil {
		return err
	}
	r := bufio.NewReader(c)
	n := 0
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return err
		}
		n++
		logf("ev %d %s", n, strings.TrimSpace(string(line)))
	}
}

func main() {
	focusWindow := flag.Uint64("focus-window", 0, "focus the window with this id")
	focusWorkspace := flag.Int("focus-workspace", 0, "focus the workspace with this index")
	focusWorkspaceName := flag.String("focus-workspace-name", "", "focus the workspace with this name")
	moveWindow := flag.Uint64("move-window", 0, "move this window ...")
	to := flag.Int("to", 0, "... to the workspace with this index, without focusing it")
	reconnect := flag.Bool("reconnect", false, "reconnect with backoff when the socket closes")
	noFollow := flag.Bool("no-follow", false, "do not follow events")
	flag.Parse()

	sock := os.Getenv("NIRI_SOCKET")
	if sock == "" {
		logf("NIRI_SOCKET is not set: not inside a niri session")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for _, req := range []string{"Version", "FocusedOutput", "Outputs", "Workspaces", "Windows", "FocusedWindow"} {
		if _, err := request(sock, req); err != nil {
			logf("%s: %v", req, err)
		}
	}

	switch {
	case *focusWindow != 0:
		logf("FocusWindow: %v", action(sock, "FocusWindow", map[string]any{"id": *focusWindow}))
	case *focusWorkspaceName != "":
		logf("FocusWorkspace: %v", action(sock, "FocusWorkspace", map[string]any{"reference": map[string]any{"Name": *focusWorkspaceName}}))
	case *focusWorkspace != 0:
		logf("FocusWorkspace: %v", action(sock, "FocusWorkspace", map[string]any{"reference": map[string]any{"Index": *focusWorkspace}}))
	case *moveWindow != 0:
		logf("MoveWindowToWorkspace: %v", action(sock, "MoveWindowToWorkspace", map[string]any{
			"window_id": *moveWindow, "reference": map[string]any{"Index": *to}, "focus": false}))
	}
	// Probe: a request niri does not know must come back as Err, not close
	// the socket, and a bad id must be an Err too.
	if _, err := request(sock, "ReturnError"); err != nil {
		logf("ReturnError as expected: %v", err)
	}
	if err := action(sock, "FocusWindow", map[string]any{"id": uint64(1 << 62)}); err != nil {
		logf("FocusWindow with an unknown id: %v", err)
	}
	if *noFollow {
		return
	}

	delay := 250 * time.Millisecond
	for {
		err := follow(ctx, sock)
		if ctx.Err() != nil {
			return
		}
		logf("event stream ended: %v", err)
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
	}
}

// parseIndex is kept for -to given as "3" or "idx:3".
func parseIndex(s string) (int, error) {
	return strconv.Atoi(strings.TrimPrefix(s, "idx:"))
}
