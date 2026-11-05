package hypr

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RoomRight is process pid's window, and how far it may grow to its right
// on its monitor, in layout pixels: to the monitor's edge, less what bars
// reserve there and the outer gap.
func (c *Client) RoomRight(ctx context.Context, pid int) (Rect, int, error) {
	clients, err := c.Request(ctx, "j/clients")
	if err != nil {
		return Rect{}, 0, err
	}
	monitors, err := c.Request(ctx, "j/monitors")
	if err != nil {
		return Rect{}, 0, err
	}
	gaps, _ := c.Request(ctx, "j/getoption general:gaps_out")
	return roomRight(clients, monitors, gapsOut(gaps), pid)
}

// gapsOut reads general:gaps_out's right gap from getoption's reply: an
// int, or "top right bottom left" (CSS-like, as Hyprland takes it).
func gapsOut(reply []byte) int {
	var o struct {
		Int    *int   `json:"int"`
		Custom string `json:"custom"`
	}
	if json.Unmarshal(reply, &o) != nil {
		return 0
	}
	if o.Int != nil {
		return *o.Int
	}
	f := strings.Fields(o.Custom)
	pick := 0 // one value: all sides
	if len(f) >= 2 {
		pick = 1
	}
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(f[pick])
	return n
}

func roomRight(clientsJSON, monitorsJSON []byte, gap, pid int) (Rect, int, error) {
	var clients []struct {
		Pid     int    `json:"pid"`
		Mapped  bool   `json:"mapped"`
		At      [2]int `json:"at"`
		Size    [2]int `json:"size"`
		Monitor int    `json:"monitor"`
	}
	if err := json.Unmarshal(clientsJSON, &clients); err != nil {
		return Rect{}, 0, fmt.Errorf("clients: %w", err)
	}
	var monitors []struct {
		ID        int     `json:"id"`
		X         int     `json:"x"`
		Width     int     `json:"width"`
		Height    int     `json:"height"`
		Scale     float64 `json:"scale"`
		Transform int     `json:"transform"`
		Reserved  [4]int  `json:"reserved"` // left, top, right, bottom
	}
	if err := json.Unmarshal(monitorsJSON, &monitors); err != nil {
		return Rect{}, 0, fmt.Errorf("monitors: %w", err)
	}
	for _, cl := range clients {
		if cl.Pid != pid || !cl.Mapped {
			continue
		}
		r := Rect{X: cl.At[0], Y: cl.At[1], W: cl.Size[0], H: cl.Size[1]}
		for _, m := range monitors {
			if m.ID != cl.Monitor {
				continue
			}
			w := m.Width
			if m.Transform%2 == 1 { // turned a quarter
				w = m.Height
			}
			if m.Scale > 0 {
				w = int(math.Round(float64(w) / m.Scale))
			}
			right := m.X + w - m.Reserved[2] - gap
			return r, max(0, right-(r.X+r.W)), nil
		}
		return r, 0, nil // its monitor unknown: no room
	}
	return Rect{}, 0, fmt.Errorf("no window of process %d", pid)
}
