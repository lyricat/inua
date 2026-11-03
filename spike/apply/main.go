package main

import (
	"fmt"
	"os"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/hyprconf"
)

// Prints what applies to a Lua config, for the live config file.
func main() {
	c, _, err := config.Load(os.Args[1])
	if err != nil {
		panic(err)
	}
	fmt.Print(hyprconf.RenderLua(c, hyprconf.Devices{}))
}
