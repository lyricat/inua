package ui

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/lyricat/inua/internal/ipc"
)

// windowRequest acts on the focused window for compositor keybindings.
func (s *shellUI) windowRequest(fn string, args []string) ipc.Response {
	address := s.snap.ActiveToplevel
	if address == "" {
		return ipc.Response{Error: "no focused window"}
	}
	switch fn {
	case "close":
		if s.wayfire != nil {
			s.closeWayfireWindow(address)
		} else {
			s.dispatch("closewindow address:" + address)
		}
		return ipc.Response{OK: true}
	case "move", "send":
		if len(args) != 1 {
			return ipc.Response{Error: "window " + fn + " needs a workspace"}
		}
		workspace, err := strconv.Atoi(args[0])
		if err != nil || workspace < 1 {
			return ipc.Response{Error: "invalid workspace"}
		}
		if s.wayfire == nil {
			return ipc.Response{Error: "moving windows needs Wayfire"}
		}
		client := s.wayfire
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if fn == "move" {
				err = client.MoveWindowToWorkspace(ctx, address, workspace)
			} else {
				err = client.SendWindowToWorkspace(ctx, address, workspace)
			}
			if err != nil {
				log.Printf("window %s %d: %v", fn, workspace, err)
			}
		}()
		return ipc.Response{OK: true}
	default:
		return ipc.Response{Error: "unknown window action " + fn}
	}
}
