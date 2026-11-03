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

// startWayfire follows Wayfire's IPC view list for the Dock and window
// launcher. Its view IDs are what focus and close take.
func (s *shellUI) startWayfire() {
	client, err := wayfire.FromEnv()
	if err != nil {
		log.Printf("wayfire: %v", err)
		return
	}
	s.wayfire = client
	go followWayfire(client, func(snapshot compositor.Snapshot) {
		gtkx.Post(func() { s.applyWayfireSnapshot(snapshot) })
	})
}

func followWayfire(client *wayfire.Client, update func(compositor.Snapshot)) {
	ctx := context.Background()
	if _, err := client.Methods(ctx); err != nil {
		log.Printf("wayfire: %v", err)
	}
	refresh := func() {
		snapshot, err := client.Windows(ctx)
		if err != nil {
			log.Printf("wayfire: windows: %v", err)
			return
		}
		update(snapshot)
	}
	refresh()
	events, err := client.Events(ctx)
	if err != nil {
		log.Printf("wayfire: events: %v", err)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case _, ok := <-events:
			if !ok {
				events = nil
			}
			refresh()
		case <-ticker.C:
			refresh()
		}
	}
}

func (s *shellUI) applyWayfireSnapshot(snapshot compositor.Snapshot) {
	s.windowSnap = wayfireSnapshot(snapshot)
	s.snap = s.windowSnap
	for _, b := range s.bars {
		b.setHypr(s, s.snap)
	}
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
	origins := map[string][2]float64{}
	for _, monitor := range snapshot.Monitors {
		origins[monitor.Name] = [2]float64{float64(monitor.X), float64(monitor.Y)}
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
		origin := origins[toplevel.Monitor]
		out.Toplevels = append(out.Toplevels, hypr.Toplevel{
			Address: toplevel.Address, PID: toplevel.PID, Title: toplevel.Title,
			Class: toplevel.Class, WorkspaceID: toplevel.WorkspaceID, Monitor: toplevel.Monitor,
			Activated: active(toplevel),
			Raw: map[string]any{
				"focusHistoryID": float64(history),
				"at":             []any{origin[0] + float64(toplevel.GeometryX), origin[1] + float64(toplevel.GeometryY)},
				"size":           []any{float64(toplevel.GeometryWidth), float64(toplevel.GeometryHeight)},
			},
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
