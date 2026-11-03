package style

import "fmt"

// Glass defines the colour tokens of the floating surfaces' background
// for style.transparency (0 opaque to 80 percent): @glass, and
// @glass_empty for an empty workspace's bar islands, the glass taken twice.
// style.css uses them; they go after the theme, which defines @app.
func Glass(transparency int) string {
	a := 1 - float64(transparency)/100
	return fmt.Sprintf("@define-color glass alpha(@app, %.2f);\n@define-color glass_empty alpha(@app, %.2f);\n", a, a*a)
}

// Corners rounds the floating surfaces by style.radius in pixels, the
// dock half as much again. It goes after style.css, whose rules it
// overrides with the same selectors, so the user's stylesheet still wins.
func Corners(radius int) string {
	return fmt.Sprintf(".bar .island, .launcher, .widget-page, .agenda .card, .control .card, .note, .osd-card, .center .card, .clipboard-panel .clipboard-card, popover.menu > contents, popover.glass-menu.gtk-frame > contents, popover.term-menu > contents { border-radius: %dpx; }\n.dock { border-radius: %dpx; }\n", radius, radius*3/2)
}
