// Command inua-vm is inua's virtual machine manager (docs/inua-vm.md): it runs
// machines with QEMU or, for Windows, with Docker (Dockur) and KVM, and reaches
// them over RDP, SPICE or their own console.
//
//	inua-vm                    open the manager window
//	inua-vm --list             the machines, one name per line
//	inua-vm --doctor           what this host provides for a machine, and which machines there are
//	inua-vm --status [--json]  what each machine is doing now, for scripts
//	inua-vm --start NAME       start a machine
//	inua-vm --stop NAME        stop it, keeping its disk
//
// The host checks, the machines and the backends are internal/vm; the window is
// internal/vmui, an app of its own in the shell's look, so the shell carries no
// VM manager. Docker socket access is privileged host access: the manager says
// so, and never offers to add anyone to the docker group.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/vm"
	"github.com/lyricat/inua/internal/vmui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	doctor := flag.Bool("doctor", false, "check this host for what a machine needs, and list the machines")
	list := flag.Bool("list", false, "list the machines, one name per line")
	status := flag.Bool("status", false, "what each machine is doing now")
	asJSON := flag.Bool("json", false, "with --doctor or --status: write JSON")
	start := flag.String("start", "", "start this machine")
	stop := flag.String("stop", "", "stop this machine, keeping its disk")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-vm [-ui-dir DIR] [--list | --doctor [--json] | --status [--json] | --start NAME | --stop NAME]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}

	switch {
	case *list:
		names, err := vmui.List()
		if err != nil {
			fail(err)
		}
		for _, name := range names {
			fmt.Println(name)
		}
	case *doctor:
		report, err := vm.Diagnose("", vm.DefaultStore())
		if err != nil {
			fail(err)
		}
		if *asJSON {
			writeJSON(report)
			return
		}
		fmt.Print(report.Text())
	case *status:
		st := vmui.Status()
		if *asJSON {
			writeJSON(st)
			return
		}
		fmt.Print(vmui.StatusText(st))
	case *start != "":
		if err := vmui.Start(*start); err != nil {
			fail(err)
		}
	case *stop != "":
		if err := vmui.Stop(*stop); err != nil {
			fail(err)
		}
	default:
		vmui.Register()
		os.Exit(appui.Run("vm", nil, *uiDir))
	}
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "inua-vm:", err)
	os.Exit(1)
}
