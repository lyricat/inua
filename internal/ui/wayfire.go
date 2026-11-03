package ui

import (
	"context"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/wayfire"
)

// startWayfire reads native Wayland toplevel events for the Dock and window
// launcher. Wayfire starts the shell from its autostart plugin so its IPC
// socket is available for window actions.
func (s *shellUI) startWayfire() {
	client, err := wayfire.FromEnv()
	if err != nil {
		log.Printf("wayfire: %v", err)
	} else {
		s.wayfire = client
	}

	go func() {
		err := wayfire.NewWayland().Run(context.Background(), func(snapshot compositor.Snapshot) {
			gtkx.Post(func() { s.applyWayfireSnapshot(snapshot) })
		})
		if err != nil {
			log.Printf("wayfire: %v", err)
		}
	}()
}

func (s *shellUI) applyWayfireSnapshot(snapshot compositor.Snapshot) {
	s.windowSnap = wayfireSnapshot(snapshot)
	s.launcher.st.SetConnected(true)
	s.launcher.st.SetSnapshot(s.windowSnap)
	s.renderDocks()
	s.syncLauncher()
}

func wayfireSnapshot(snapshot compositor.Snapshot) hypr.Snapshot {
	out := hypr.Snapshot{
		ActiveToplevel: snapshot.ActiveToplevel,
		FocusedMonitor: snapshot.FocusedMonitor,
	}
	for _, monitor := range snapshot.Monitors {
		out.Monitors = append(out.Monitors, hypr.Monitor{
			ID: monitor.ID, Name: monitor.Name, Description: monitor.Description,
			X: monitor.X, Y: monitor.Y, Width: monitor.Width, Height: monitor.Height,
			Scale: monitor.Scale, Focused: monitor.Focused, ActiveWorkspaceID: monitor.ActiveWorkspaceID,
		})
	}
	for _, workspace := range snapshot.Workspaces {
		out.Workspaces = append(out.Workspaces, hypr.Workspace{
			ID: workspace.ID, Name: workspace.Name, Monitor: workspace.Monitor,
			Active: workspace.Active, Focused: workspace.Focused,
			HasFullscreen: workspace.HasFullscreen, Urgent: workspace.Urgent,
		})
	}
	toplevels := slices.Clone(snapshot.Toplevels)
	active := func(t compositor.Toplevel) bool {
		return t.Activated || t.Address == snapshot.ActiveToplevel
	}
	slices.SortStableFunc(toplevels, func(a, b compositor.Toplevel) int {
		if active(a) != active(b) {
			if active(a) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Address, b.Address)
	})
	for i, toplevel := range toplevels {
		history := i + 1
		if active(toplevel) {
			history = 0
			if out.ActiveToplevel == "" {
				out.ActiveToplevel = toplevel.Address
			}
		}
		out.Toplevels = append(out.Toplevels, hypr.Toplevel{
			Address: toplevel.Address, PID: toplevel.PID, Title: toplevel.Title,
			Class: toplevel.Class, WorkspaceID: toplevel.WorkspaceID, Monitor: toplevel.Monitor,
			Activated: active(toplevel),
			Raw:       map[string]any{"focusHistoryID": float64(history)},
		})
	}
	return out
}

func (s *shellUI) focusWayfireWindow(address string) {
	client := s.wayfire
	if client == nil {
		log.Printf("wayfire: cannot focus %s without the IPC socket", address)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := client.FocusWindow(ctx, address); err != nil {
			log.Printf("wayfire: focus %s: %v", address, err)
		}
	}()
}

func (s *shellUI) closeWayfireWindow(address string) {
	client := s.wayfire
	if client == nil {
		if strings.HasPrefix(address, "wf:") {
			log.Printf("wayfire: cannot close %s without the IPC socket", address)
			return
		}
		s.dispatch("closewindow address:" + address)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := client.CloseWindow(ctx, address); err != nil {
			log.Printf("wayfire: close %s: %v", address, err)
		}
	}()
}
