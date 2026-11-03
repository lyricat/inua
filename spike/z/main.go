package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/lyricat/inua/internal/appmenu"
)

// Reads a process's menu the way the bar does, printing each source.
func main() {
	pid, _ := strconv.Atoi(os.Args[1])
	r := appmenu.New(nil)
	_ = r
	a := appmenu.NewA11y()
	e, err := a.Menu(pid)
	fmt.Println("a11y:", len(e), err)
	for _, x := range e {
		fmt.Printf("  %q -> %d\n", x.Label, len(x.Children))
	}
}
