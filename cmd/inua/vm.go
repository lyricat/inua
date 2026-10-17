package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/lyricat/inua/internal/vm"
	"github.com/lyricat/inua/internal/vmui"
)

func cmdVM(args []string) error {
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "doctor":
		if len(args) != 1 {
			usage()
		}
		return vmReport(false)
	case "list":
		if len(args) != 1 {
			usage()
		}
		names, err := vmui.List()
		if err != nil {
			return err
		}
		for _, name := range names {
			fmt.Println(name)
		}
	case "status":
		fs := flag.NewFlagSet("vm status", flag.ContinueOnError)
		jsonOutput := fs.Bool("json", false, "output JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			usage()
		}
		st := vmui.Status()
		if *jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}
		fmt.Print(vmui.StatusText(st))
	default:
		usage()
	}
	return nil
}

// vmReport prints what `inua-vm --doctor` prints: the checks, the
// recommendation and the machines live in internal/vm, shared with the manager
// app (cmd/inua-vm). Both are read-only: nothing here contacts Docker.
func vmReport(asJSON bool) error {
	report, err := vm.Diagnose("", vm.DefaultStore())
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	fmt.Print(report.Text())
	return nil
}
