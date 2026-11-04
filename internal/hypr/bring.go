package hypr

import (
	"context"
	"encoding/json"
	"fmt"
)

// BringHere moves the window of process pid to the active workspace, if
// it is elsewhere, and focuses it: for an app started again while its one
// window is open on another workspace.
func (c *Client) BringHere(ctx context.Context, pid int) error {
	active, err := c.Request(ctx, "j/activeworkspace")
	if err != nil {
		return err
	}
	clients, err := c.Request(ctx, "j/clients")
	if err != nil {
		return err
	}
	cmds, err := bringHere(active, clients, pid)
	if err != nil {
		return err
	}
	for _, cmd := range cmds {
		if err := c.Dispatch(ctx, cmd); err != nil {
			return err
		}
	}
	return nil
}

// Focus focuses the window sel matches ("class:…", "address:…") where it
// is, its workspace shown: in the classic form, else in Hyprland 0.55's
// Lua. It fails when no window matches.
func (c *Client) Focus(ctx context.Context, sel string) error {
	err := c.Dispatch(ctx, "focuswindow "+sel)
	if err == nil {
		return nil
	}
	if lerr := c.LuaDispatch(ctx, fmt.Sprintf(`hl.dsp.focus({window=%q})`, sel)); lerr != nil {
		return err
	}
	return nil
}

// bringHere is the dispatches BringHere sends, from Hyprland's replies.
func bringHere(activeJSON, clientsJSON []byte, pid int) ([]string, error) {
	var active struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(activeJSON, &active); err != nil {
		return nil, fmt.Errorf("activeworkspace: %w", err)
	}
	var clients []struct {
		Address   string `json:"address"`
		Pid       int    `json:"pid"`
		Mapped    bool   `json:"mapped"`
		Workspace struct {
			ID int `json:"id"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(clientsJSON, &clients); err != nil {
		return nil, fmt.Errorf("clients: %w", err)
	}
	for _, cl := range clients {
		if cl.Pid != pid || !cl.Mapped {
			continue
		}
		addr := "address:" + NormalizeAddress(cl.Address)
		if cl.Workspace.ID == active.ID {
			return []string{"focuswindow " + addr}, nil
		}
		return []string{
			fmt.Sprintf("movetoworkspacesilent %d,%s", active.ID, addr),
			"focuswindow " + addr,
		}, nil
	}
	return nil, fmt.Errorf("no window of process %d", pid)
}

// Rect is a window's place: its top-left corner and size, in layout pixels.
// WindowOf is where process pid's window is.
func (c *Client) WindowOf(ctx context.Context, pid int) (Rect, error) {
	clients, err := c.Request(ctx, "j/clients")
	if err != nil {
		return Rect{}, err
	}
	return windowOf(clients, pid)
}

// windowOf reads process pid's mapped window's place from Hyprland's
// client list.
func windowOf(clientsJSON []byte, pid int) (Rect, error) {
	var clients []struct {
		Pid    int    `json:"pid"`
		Mapped bool   `json:"mapped"`
		At     [2]int `json:"at"`
		Size   [2]int `json:"size"`
	}
	if err := json.Unmarshal(clientsJSON, &clients); err != nil {
		return Rect{}, fmt.Errorf("clients: %w", err)
	}
	for _, cl := range clients {
		if cl.Pid == pid && cl.Mapped {
			return Rect{X: cl.At[0], Y: cl.At[1], W: cl.Size[0], H: cl.Size[1]}, nil
		}
	}
	return Rect{}, fmt.Errorf("no window of process %d", pid)
}

// PlaceWindow moves and resizes process pid's floating window to r. A
// window keeps the size and place it grew to, even once what grew it is
// gone.
func (c *Client) PlaceWindow(ctx context.Context, pid int, r Rect) error {
	if err := c.Dispatch(ctx, fmt.Sprintf("resizewindowpixel exact %d %d,pid:%d", r.W, r.H, pid)); err != nil {
		return err
	}
	return c.Dispatch(ctx, fmt.Sprintf("movewindowpixel exact %d %d,pid:%d", r.X, r.Y, pid))
}
