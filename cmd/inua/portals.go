package main

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/portalconf"
)

// cmdPortals makes inua's backends the ones xdg-desktop-portal asks, an
// interface at a time (docs/portals.md).
func cmdPortals(args []string) error {
	if len(args) == 0 {
		usage()
	}
	d := portalconf.Default()
	switch args[0] {
	case "status":
		if len(args) == 1 {
			return portalsStatus(d)
		}
	case "enable", "disable":
		ifaces := args[1:]
		if len(ifaces) == 0 {
			ifaces = portalconf.Interfaces()
		}
		on, err := d.Set(args[0] == "enable", ifaces...)
		if err != nil {
			return err
		}
		if on {
			fmt.Println("Wrote", d.Own)
		} else {
			fmt.Println("No interface left on; removed", d.Own)
		}
		msg, err := d.Apply(d.Wanted().Dir != "") // not for the share picker alone
		if err != nil {
			return err
		}
		fmt.Println(msg)
		if slices.ContainsFunc(ifaces, func(i string) bool { return strings.EqualFold(i, portalconf.SharePicker) }) {
			if err := portalconf.RestartXdph(); err != nil {
				return err
			}
			fmt.Println("Restarted xdg-desktop-portal-hyprland for its share picker.")
		}
		return nil
	case "dir":
		// For inua-session: the directory, written again, when one
		// interface is on.
		if len(args) == 1 {
			on, err := d.Refresh()
			if err != nil {
				return err
			}
			if !on {
				os.Exit(1)
			}
			fmt.Println(d.Own)
			return nil
		}
	}
	usage()
	return nil
}

func portalsStatus(d portalconf.Dirs) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	enabled := d.Enabled()
	now, want := portalconf.ReadFrontend(), d.Wanted()
	for _, b := range portalconf.Backends {
		running := "not running"
		if portalconf.HasOwner(conn, b.Bus) {
			running = "running"
		}
		fmt.Printf("%s (%s, %s)\n", b.Name, b.Bus, running)
		for _, i := range b.Interfaces {
			chosen := d.Serving(want, i)
			state := "off (inua portals enable " + i + ")"
			switch {
			case slices.Contains(enabled, i):
				state = "on"
			case chosen != "":
				state = "off, " + portalconf.Label(chosen) + "'s"
			}
			if got := d.Serving(now, i); got != chosen {
				state += "; apps get " + cmp.Or(portalconf.Label(got), "none") + " until the portal service restarts"
			}
			fmt.Printf("  %-14s %s\n", i, state)
		}
	}
	switch {
	case now.Dir == want.Dir:
	case want.Dir == "":
		fmt.Println("The portal service still reads", now.Dir+"; restart it (Settings, Default Apps) for the system's")
	default:
		fmt.Println("The portal service does not read", want.Dir, "yet; Settings, Default Apps, Apply now restarts it with it, as an inua session does")
	}
	return nil
}
