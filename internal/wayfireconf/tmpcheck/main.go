package main

import (
	"fmt"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/wayfireconf"
)

func main() {
	for _, content := range []string{"", "\n"} {
		func() {
			defer func() { fmt.Printf("content %q panic: %v\n", content, recover()) }()
			got, err := wayfireconf.Render(content, config.Default())
			fmt.Printf("content %q err=%v\n%s\n", content, err, got)
		}()
	}
}
