package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/lyricat/inua/internal/appmenu"
)

func main() {
	pid, _ := strconv.Atoi(os.Args[1])
	a := appmenu.NewA11y()
	e, err := a.Menu(pid)
	fmt.Println("err", err, "n", len(e))
	for _, x := range e {
		fmt.Printf("  %q -> %d\n", x.Label, len(x.Children))
	}
}
