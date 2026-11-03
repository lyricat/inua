// Package wayfire implements the toolkit-free client for Wayfire's IPC socket.
package wayfire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/display"
)

const maxMessageSize = 1 << 20

// Client sends length-prefixed JSON requests to the Wayfire IPC socket.
type Client struct {
	Socket  string
	Timeout time.Duration

	mu                 sync.RWMutex
	methods            map[string]bool
	lastErr            error
	snapshot           compositor.Snapshot
	events             chan compositor.Event
	workspacePositions map[int]workspacePosition
}

type viewRecord struct {
	ID          int          `json:"id"`
	PID         int          `json:"pid"`
	Title       string       `json:"title"`
	AppID       string       `json:"app-id"`
	OutputID    int          `json:"output-id"`
	OutputName  string       `json:"output-name"`
	Activated   bool         `json:"activated"`
	WorkspaceID int64        `json:"wset-index"`
	Geometry    viewGeometry `json:"geometry"`
}

type outputRecord struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Geometry struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"geometry"`
	WorkspaceSetID uint64 `json:"wset-index"`
	Scale          float64  `json:"scale"`
	Refresh        float64  `json:"refresh-rate"`
	Modes          []string `json:"available-modes"`
	Mode           string   `json:"mode"`
	Workspace      struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"grid_width"`
		Height int `json:"grid_height"`
	} `json:"workspace"`
}

type workspaceSetRecord struct {
	Index     uint64 `json:"index"`
	Name      string `json:"name"`
	Output    string `json:"output-name"`
	OutputID  uint64 `json:"output-id"`
	Workspace struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"grid_width"`
		Height int `json:"grid_height"`
	} `json:"workspace"`
}

type viewGeometry struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type WayfireState struct {
	Views         []viewRecord
	Outputs       []outputRecord
	WorkspaceSets []workspaceSetRecord
}

type workspacePosition struct{ X, Y int }

var _ compositor.Client = (*Client)(nil)

// FromEnv resolves the socket exported to Wayfire clients by its ipc plugin.
func FromEnv() (*Client, error) {
	socket := os.Getenv("WAYFIRE_SOCKET")
	if socket == "" {
		socket = os.Getenv("_WAYFIRE_SOCKET")
	}
	if socket == "" {
		return nil, errors.New("WAYFIRE_SOCKET is not set; not running under Wayfire with the IPC plugin")
	}
	return &Client{Socket: socket, Timeout: 2 * time.Second}, nil
}

func (c *Client) dial(ctx context.Context) (net.Conn, context.CancelFunc, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("connect Wayfire IPC: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	return conn, cancel, nil
}

func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return errors.New("Wayfire IPC request is empty")
	}
	if len(payload) > maxMessageSize {
		return fmt.Errorf("Wayfire IPC request exceeds %d bytes", maxMessageSize)
	}
	frame := []byte{byte(len(payload)), byte(len(payload) >> 8), byte(len(payload) >> 16), byte(len(payload) >> 24)}
	if err := writeAll(w, append(frame, payload...)); err != nil {
		return fmt.Errorf("write Wayfire IPC request: %w", err)
	}
	return nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func readFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("read Wayfire IPC response header: %w", err)
	}
	length := uint32(header[0]) | uint32(header[1])<<8 | uint32(header[2])<<16 | uint32(header[3])<<24
	if length > maxMessageSize {
		return nil, fmt.Errorf("Wayfire IPC response length %d is invalid", length)
	}
	response := make([]byte, int(length))
	if _, err := io.ReadFull(r, response); err != nil {
		return nil, fmt.Errorf("read Wayfire IPC response body: %w", err)
	}
	return response, nil
}

func decodeResponse(method string, response []byte) (json.RawMessage, error) {
	if !json.Valid(response) {
		return nil, fmt.Errorf("decode Wayfire IPC response: malformed JSON %q", response)
	}
	if response[0] == '{' {
		var envelope struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(response, &envelope); err != nil {
			return nil, fmt.Errorf("decode Wayfire IPC response: %w", err)
		}
		if envelope.Error != "" {
			return nil, fmt.Errorf("Wayfire IPC %s: %s", method, envelope.Error)
		}
	}
	return json.RawMessage(response), nil
}

// Request sends one method call and returns its single framed response.
func (c *Client) Request(ctx context.Context, method string, data any) (json.RawMessage, error) {
	if strings.TrimSpace(method) == "" {
		return nil, errors.New("Wayfire IPC method is empty")
	}
	payload, err := json.Marshal(struct {
		Method string `json:"method"`
		Data   any    `json:"data"`
	}{method, data})
	if err != nil {
		return nil, fmt.Errorf("encode Wayfire IPC request: %w", err)
	}
	conn, cancel, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer conn.Close()
	if err := writeFrame(conn, payload); err != nil {
		return nil, err
	}
	response, err := readFrame(conn)
	if err != nil {
		return nil, err
	}
	return decodeResponse(method, response)
}

func (c *Client) setLastError(err error) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
}

// Capabilities reports implemented Wayfire IPC operations. Method availability
// is populated only from the running compositor's list-methods response.
func (c *Client) Capabilities() compositor.Capabilities {
	c.mu.RLock()
	defer c.mu.RUnlock()
	caps := compositor.Capabilities{Reasons: map[string]string{}}
	if c.lastErr != nil {
		caps.Reasons["Connection"] = c.lastErr.Error()
	}
	caps.StateQuery = c.methods["window-rules/list-views"] && c.methods["window-rules/list-outputs"] && c.methods["window-rules/list-wsets"]
	caps.StateEvents = c.methods["window-rules/events/watch"]
	caps.FocusWindow = c.methods["window-rules/focus-view"]
	caps.MoveWindow = c.methods["vswitch/set-workspace"]
	caps.CloseWindow = c.methods["window-rules/close-view"]
	caps.SwitchWorkspace = c.methods["vswitch/set-workspace"]
	caps.Reasons["ResizeWindow"] = "Wayfire resize IPC method is not mapped"
	caps.Reasons["CreateWorkspace"] = "Wayfire uses a configured fixed workspace grid"
	caps.Reasons["ShortcutListing"] = "Wayfire IPC does not provide a verified shortcut listing"
	caps.Reasons["WindowCapture"] = "Wayfire overview output capture is not connected"
	caps.Reasons["SessionLock"] = "Inua does not connect its lock surface to Wayfire"
	if c.methods == nil {
		caps.Reasons["IPCMethodList"] = "Wayfire IPC methods have not been enumerated from the running compositor"
	}
	if !caps.StateQuery {
		caps.Reasons["StateQuery"] = "Wayfire list-views/list-outputs/list-wsets methods are unavailable"
	}
	if !caps.StateEvents {
		caps.Reasons["StateEvents"] = "Wayfire window-rules/events/watch is unavailable"
	}
	if !caps.FocusWindow {
		caps.Reasons["FocusWindow"] = "Wayfire window-rules/focus-view is unavailable"
	}
	if !caps.MoveWindow {
		caps.Reasons["MoveWindow"] = "Wayfire vswitch/set-workspace is unavailable"
	}
	if !caps.CloseWindow {
		caps.Reasons["CloseWindow"] = "Wayfire window-rules/close-view is unavailable"
	}
	if !caps.SwitchWorkspace {
		caps.Reasons["SwitchWorkspace"] = "Wayfire vswitch/set-workspace is unavailable"
	}
	return caps
}

func (c *Client) SetSnapshot(snapshot compositor.Snapshot) {
	c.mu.Lock()
	c.snapshot = snapshot
	c.mu.Unlock()
}

func (c *Client) Snapshot(context.Context) (compositor.Snapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.snapshot.Monitors == nil {
		return compositor.Snapshot{}, errors.New("Wayfire state is unavailable: no runtime protocol state has been mapped")
	}
	return c.snapshot, nil
}
func (c *Client) Events(ctx context.Context) (<-chan compositor.Event, error) {
	if !c.hasMethod("window-rules/events/watch") {
		return nil, errors.New("Wayfire state events are unavailable")
	}
	c.mu.Lock()
	if c.events == nil {
		c.events = make(chan compositor.Event, 32)
	}
	events := c.events
	c.mu.Unlock()
	go func() {
		_ = c.Watch(ctx, []string{"view-mapped", "view-unmapped", "view-set-output", "view-geometry-changed", "view-wset-changed", "view-focused", "view-title-changed", "view-app-id-changed", "output-gain-focus", "output-wset-changed", "wset-workspace-changed"}, func(raw json.RawMessage) {
			c.RecordWorkspaceEvent(raw)
			var data map[string]any
			if json.Unmarshal(raw, &data) != nil {
				return
			}
			select {
			case events <- compositor.Event{Kind: fmt.Sprint(data["event"]), Data: data}:
			default:
			}
		})
	}()
	return events, nil
}

func (c *Client) hasMethod(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.methods[name]
}

func (c *Client) FocusWindow(ctx context.Context, address string) error {
	id, err := parseViewID(address)
	if err != nil {
		return err
	}
	_, err = c.Request(ctx, "window-rules/focus-view", map[string]int{"id": id})
	return err
}

func (c *Client) MoveWindowToWorkspace(ctx context.Context, address string, workspace int) error {
	return c.windowToWorkspace(ctx, "vswitch/set-workspace", address, workspace)
}

// SendWindowToWorkspace moves the window without following it.
func (c *Client) SendWindowToWorkspace(ctx context.Context, address string, workspace int) error {
	return c.windowToWorkspace(ctx, "vswitch/send-view", address, workspace)
}

func (c *Client) windowToWorkspace(ctx context.Context, method, address string, workspace int) error {
	id, err := parseViewID(address)
	if err != nil {
		return err
	}
	raw, err := c.Request(ctx, "window-rules/get-focused-output", map[string]any{})
	if err != nil {
		return err
	}
	var focused struct {
		Info outputRecord `json:"info"`
	}
	if err := json.Unmarshal(raw, &focused); err != nil {
		return fmt.Errorf("decode focused output: %w", err)
	}
	width := focused.Info.Workspace.Width
	if width < 1 {
		width = 1
	}
	_, err = c.Request(ctx, method, map[string]any{
		"x": (workspace - 1) % width, "y": (workspace - 1) / width,
		"output-id": focused.Info.ID, "view-id": id,
	})
	return err
}

// Windows lists Wayfire's views with their IPC IDs, which focus and close
// take. The foreign-toplevel protocol uses different handle numbers.
func (c *Client) Windows(ctx context.Context) (compositor.Snapshot, error) {
	raw, err := c.Request(ctx, "window-rules/list-views", map[string]any{})
	if err != nil {
		return compositor.Snapshot{}, err
	}
	var views []viewRecord
	if err := json.Unmarshal(raw, &views); err != nil {
		return compositor.Snapshot{}, fmt.Errorf("decode window-rules/list-views: %w", err)
	}
	snapshot := compositor.Snapshot{}
	for _, view := range views {
		if view.WorkspaceID < 0 || view.AppID == "" {
			continue
		}
		address := fmt.Sprintf("wf:%d", view.ID)
		snapshot.Toplevels = append(snapshot.Toplevels, compositor.Toplevel{
			Address: address, PID: view.PID, Title: view.Title, Class: view.AppID,
			Monitor: view.OutputName, Activated: view.Activated,
			GeometryX: view.Geometry.X, GeometryY: view.Geometry.Y,
			GeometryWidth: view.Geometry.Width, GeometryHeight: view.Geometry.Height,
		})
		if view.Activated {
			snapshot.ActiveToplevel = address
		}
	}
	focusedID := -1
	if raw, err := c.Request(ctx, "window-rules/get-focused-output", map[string]any{}); err == nil {
		var focused struct {
			Info outputRecord `json:"info"`
		}
		if json.Unmarshal(raw, &focused) == nil {
			focusedID = focused.Info.ID
		}
	}
	raw, err = c.Request(ctx, "window-rules/list-outputs", map[string]any{})
	if err != nil {
		return snapshot, err
	}
	var outputs []outputRecord
	if err := json.Unmarshal(raw, &outputs); err != nil {
		return snapshot, fmt.Errorf("decode window-rules/list-outputs: %w", err)
	}
	for _, output := range outputs {
		workspaceID := output.Workspace.Y*output.Workspace.Width + output.Workspace.X + 1
		monitor := compositor.Monitor{
			ID: output.ID, Name: output.Name,
			X: output.Geometry.X, Y: output.Geometry.Y,
			Width: output.Geometry.Width, Height: output.Geometry.Height,
			Scale: c.outputScale(ctx, output.Name), Focused: output.ID == focusedID,
			ActiveWorkspaceID: workspaceID,
		}
		snapshot.Monitors = append(snapshot.Monitors, monitor)
		if monitor.Focused {
			snapshot.FocusedMonitor = monitor.Name
		}
	}
	return snapshot, nil
}

// outputScale reads an output's configured scale; Wayfire's list-outputs
// reports pixels, not logical size.
func (c *Client) outputScale(ctx context.Context, name string) float64 {
	raw, err := c.Request(ctx, "wayfire/get-config-option", map[string]any{"option": "output:" + name + "/scale"})
	if err != nil {
		return 1
	}
	var reply struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &reply) != nil {
		return 1
	}
	scale, err := strconv.ParseFloat(reply.Value, 64)
	if err != nil || scale <= 0 {
		return 1
	}
	return scale
}

// SettingsOutputs returns connected outputs in the display settings model.
func (c *Client) SettingsOutputs(ctx context.Context) ([]display.Output, error) {
	raw, err := c.Request(ctx, "window-rules/list-outputs", map[string]any{})
	if err != nil {
		return nil, err
	}
	var outputs []outputRecord
	if err := json.Unmarshal(raw, &outputs); err != nil {
		return nil, fmt.Errorf("decode Wayfire outputs: %w", err)
	}
	result := make([]display.Output, 0, len(outputs))
	for _, output := range outputs {
		width, height := output.Geometry.Width, output.Geometry.Height
		mode := output.Mode
		if mode == "" && width > 0 && height > 0 {
			mode = fmt.Sprintf("%dx%d", width, height)
			if output.Refresh > 0 {
				mode += fmt.Sprintf("@%.2f", output.Refresh)
			}
		}
		modes := output.Modes
		if len(modes) == 0 && mode != "" {
			modes = []string{mode}
		}
		result = append(result, display.Output{
			Name: output.Name, Width: width, Height: height, Refresh: output.Refresh,
			X: output.Geometry.X, Y: output.Geometry.Y, Scale: c.outputScale(ctx, output.Name), Modes: modes,
		})
	}
	return result, nil
}

// FocusedAppID names the application of the focused view.
func (c *Client) FocusedAppID(ctx context.Context) (string, error) {
	raw, err := c.Request(ctx, "window-rules/list-views", map[string]any{})
	if err != nil {
		return "", err
	}
	var views []viewRecord
	if err := json.Unmarshal(raw, &views); err != nil {
		return "", fmt.Errorf("decode window-rules/list-views: %w", err)
	}
	for _, view := range views {
		if view.Activated {
			return view.AppID, nil
		}
	}
	return "", errors.New("no Wayfire window has the focus")
}

func (c *Client) CloseWindow(ctx context.Context, address string) error {
	id, err := parseViewID(address)
	if err != nil {
		return err
	}
	_, err = c.Request(ctx, "window-rules/close-view", map[string]int{"id": id})
	return err
}

func (c *Client) SwitchWorkspace(ctx context.Context, workspace int) error {
	snapshot, err := c.Snapshot(ctx)
	if err != nil {
		return err
	}
	for _, ws := range snapshot.Workspaces {
		if ws.ID != workspace {
			continue
		}
		var output *compositor.Monitor
		for i := range snapshot.Monitors {
			if snapshot.Monitors[i].Name == ws.Monitor {
				output = &snapshot.Monitors[i]
				break
			}
		}
		if output == nil {
			return fmt.Errorf("Wayfire output %q is unavailable", ws.Monitor)
		}
		width := gridWidthFromSnapshot(snapshot, output.Name)
		_, err = c.Request(ctx, "vswitch/set-workspace", map[string]any{"x": (workspace - 1) % width, "y": (workspace - 1) / width, "output-id": output.ID})
		return err
	}
	return fmt.Errorf("Wayfire workspace %d is unavailable", workspace)
}

func (c *Client) Methods(ctx context.Context) ([]string, error) {
	raw, err := c.Request(ctx, "list-methods", map[string]any{})
	if err != nil {
		c.setLastError(err)
		return nil, err
	}
	var reply struct {
		Methods []string `json:"methods"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		c.setLastError(err)
		return nil, fmt.Errorf("decode Wayfire methods: %w", err)
	}
	if reply.Methods == nil {
		err := errors.New("Wayfire list-methods response has no methods array")
		c.setLastError(err)
		return nil, err
	}
	methods := make(map[string]bool, len(reply.Methods))
	for _, method := range reply.Methods {
		methods[method] = true
	}
	c.mu.Lock()
	c.methods = methods
	c.lastErr = nil
	c.mu.Unlock()
	return reply.Methods, nil
}

func (c *Client) RecordWorkspaceEvent(raw json.RawMessage) {
	var event struct {
		Event string            `json:"event"`
		View  viewRecord        `json:"view"`
		To    workspacePosition `json:"to"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return
	}
	c.mu.Lock()
	if c.workspacePositions == nil {
		c.workspacePositions = make(map[int]workspacePosition)
	}
	if event.Event == "view-unmapped" {
		delete(c.workspacePositions, event.View.ID)
	} else if event.Event == "view-workspace-changed" {
		c.workspacePositions[event.View.ID] = event.To
	}
	c.mu.Unlock()
}
func parseViewID(address string) (int, error) {
	if !strings.HasPrefix(address, "wf:") {
		return 0, fmt.Errorf("invalid Wayfire view id %q", address)
	}
	return strconv.Atoi(strings.TrimPrefix(address, "wf:"))
}

func gridWidthFromSnapshot(snapshot compositor.Snapshot, output string) int {
	width := 1
	for _, ws := range snapshot.Workspaces {
		if ws.Monitor == output && ws.ID > width {
			width = ws.ID
		}
	}
	return width
}

func (c *Client) RefreshState(ctx context.Context, metadata compositor.Snapshot) (compositor.Snapshot, error) {
	var state WayfireState
	for _, step := range []struct {
		method string
		target any
	}{
		{"window-rules/list-views", &state.Views}, {"window-rules/list-outputs", &state.Outputs}, {"window-rules/list-wsets", &state.WorkspaceSets},
	} {
		raw, err := c.Request(ctx, step.method, map[string]any{})
		if err != nil {
			return compositor.Snapshot{}, err
		}
		if err := json.Unmarshal(raw, step.target); err != nil {
			return compositor.Snapshot{}, fmt.Errorf("decode %s: %w", step.method, err)
		}
	}
	c.mu.RLock()
	positions := make(map[int]workspacePosition, len(c.workspacePositions))
	for id, position := range c.workspacePositions {
		positions[id] = position
	}
	c.mu.RUnlock()
	snapshot := MapRuntimeState(metadata, state, positions)
	c.SetSnapshot(snapshot)
	return snapshot, nil
}

func MapRuntimeState(metadata compositor.Snapshot, state WayfireState, positions map[int]workspacePosition) compositor.Snapshot {
	result := compositor.Snapshot{Monitors: append([]compositor.Monitor(nil), metadata.Monitors...)}
	grids := make(map[uint64]workspaceSetRecord, len(state.WorkspaceSets))
	for _, ws := range state.WorkspaceSets {
		grids[ws.OutputID] = ws
	}
	for _, output := range state.Outputs {
		for i := range result.Monitors {
			if result.Monitors[i].Name == output.Name {
				result.Monitors[i].ID, result.Monitors[i].X, result.Monitors[i].Y = output.ID, output.Geometry.X, output.Geometry.Y
				result.Monitors[i].Width, result.Monitors[i].Height, result.Monitors[i].Scale = output.Geometry.Width, output.Geometry.Height, 1
				ws := grids[output.WorkspaceSetID]
				result.Monitors[i].ActiveWorkspaceID = output.Workspace.Y*ws.Workspace.Width + output.Workspace.X + 1
				if result.Monitors[i].Focused {
					result.FocusedMonitor = output.Name
				}
			}
		}
	}
	for _, ws := range state.WorkspaceSets {
		for y := range ws.Workspace.Height {
			for x := range ws.Workspace.Width {
				id := y*ws.Workspace.Width + x + 1
				active := x == ws.Workspace.X && y == ws.Workspace.Y
				result.Workspaces = append(result.Workspaces, compositor.Workspace{ID: id, Name: strconv.Itoa(id), Monitor: ws.Output, Active: active, Focused: active && result.FocusedMonitor == ws.Output})
			}
		}
	}
	for _, view := range state.Views {
		grid, ok := grids[uint64(view.WorkspaceID)]
		if view.WorkspaceID < 0 || !ok {
			continue
		}

		position, ok := positions[view.ID]
		if !ok {
			continue
		}
		workspaceID := position.Y*grid.Workspace.Width + position.X + 1
		result.Toplevels = append(result.Toplevels, compositor.Toplevel{
			Address: fmt.Sprintf("wf:%d", view.ID), PID: view.PID, Title: view.Title, Class: view.AppID,
			WorkspaceID: workspaceID, Monitor: view.OutputName, Activated: view.Activated,
			WorkspacePositionX: position.X, WorkspacePositionY: position.Y, WorkspaceGridWidth: grid.Workspace.Width,
			GeometryX: view.Geometry.X, GeometryY: view.Geometry.Y, GeometryWidth: view.Geometry.Width, GeometryHeight: view.Geometry.Height,
		})
		if view.Activated {
			result.ActiveToplevel = fmt.Sprintf("wf:%d", view.ID)
		}
	}
	return result
}
func MapState(outputs []OutputState, views []ViewState, activeWorkspace, gridWidth, gridHeight int) compositor.Snapshot {
	result := compositor.Snapshot{}
	for i, output := range outputs {
		id := output.ID
		if id == 0 {
			id = i
		}
		result.Monitors = append(result.Monitors, compositor.Monitor{ID: id, Name: output.Name, Description: output.Description, X: output.X, Y: output.Y, Width: output.Width, Height: output.Height, Scale: output.Scale, Focused: output.Focused})
		if output.Focused {
			result.FocusedMonitor = output.Name
		}
	}
	for _, view := range views {
		result.Toplevels = append(result.Toplevels, compositor.Toplevel{Address: view.Handle, Title: view.Title, Class: view.AppID, WorkspaceID: view.WorkspaceID, Monitor: view.Output, Activated: view.Activated})
		if view.Activated {
			result.ActiveToplevel = view.Handle
		}
	}
	if gridWidth < 1 || gridHeight < 1 {
		return result
	}
	for y := range gridHeight {
		for x := range gridWidth {
			id := y*gridWidth + x
			result.Workspaces = append(result.Workspaces, compositor.Workspace{ID: id, Name: fmt.Sprintf("%d", id+1), Active: id == activeWorkspace, Focused: id == activeWorkspace})
		}
	}
	return result
}

type OutputState struct {
	ID                  int
	Name, Description   string
	X, Y, Width, Height int
	Scale               float64
	Focused             bool
}
type ViewState struct {
	Handle, Title, AppID, Output string
	WorkspaceID                  int
	Activated                    bool
}

// Watch keeps one socket open for Wayfire's window-rules event subscription.
func (c *Client) Watch(ctx context.Context, events []string, receive func(json.RawMessage)) error {
	payload, err := json.Marshal(struct {
		Method string `json:"method"`
		Data   any    `json:"data"`
	}{"window-rules/events/watch", map[string]any{"events": events}})
	if err != nil {
		return fmt.Errorf("encode Wayfire event subscription: %w", err)
	}
	conn, cancel, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	defer conn.Close()
	if err := writeFrame(conn, payload); err != nil {
		return err
	}
	for {
		frame, err := readFrame(conn)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if _, err := decodeResponse("window-rules/events/watch", frame); err != nil {
			return err
		}
		if receive != nil {
			receive(json.RawMessage(frame))
		}
	}
}
