package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/rundir"
)

// The portal's Screenshot (docs/portals.md): the shell's own tool, the
// choice going to the app that asked instead of the screenshots folder.

// portalShot is a screenshot or a colour an app waits for, while the tool
// is open for it.
type portalShot struct {
	path  chan string      // the screenshot's file, "" when cancelled
	color chan color.NRGBA // the colour picked
	share chan string      // what to share (portal_share.go)
	gone  chan struct{}    // closed when the tool closes unanswered
}

// portalShots is the shell's ScreenshotHandler.
type portalShots struct{ s *shellUI }

// portalShotFile is a new file for a screenshot an app gets.
func portalShotFile() (string, error) {
	dir, err := rundir.Sub("portal")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("screenshot-%d.png", time.Now().UnixNano())), nil
}

func (h portalShots) Screenshot(appID string, interactive bool, done <-chan struct{}) (string, bool) {
	s := h.s
	if !interactive {
		// The focused screen, as the tool's Screen would take it.
		type where struct {
			mon  string
			wait time.Duration
		}
		at, err := gtkx.Call(time.Second, func() where { return where{s.snap.FocusedOutput, s.hideForShot()} })
		defer gtkx.Post(s.shotShown)
		time.Sleep(at.wait) // the password dialog's details hidden
		focused := at.mon
		path, ferr := portalShotFile()
		if err != nil || ferr != nil || focused == "" {
			return "", false
		}
		if out, err := exec.Command("grim", "-o", focused, path).CombinedOutput(); err != nil {
			log.Printf("portal screenshot: grim: %v %s", err, strings.TrimSpace(string(out)))
			return "", false
		}
		return path, true
	}
	p := &portalShot{path: make(chan string, 1), gone: make(chan struct{})}
	if !h.open(p, "area") {
		return "", false
	}
	select {
	case path := <-p.path:
		return path, path != ""
	case <-p.gone:
		return "", false
	case <-done:
		gtkx.Post(func() { s.closeShot("") })
		return "", false
	}
}

func (h portalShots) PickColor(appID string, done <-chan struct{}) (color.NRGBA, bool) {
	p := &portalShot{color: make(chan color.NRGBA, 1), gone: make(chan struct{})}
	if !h.open(p, "color") {
		return color.NRGBA{}, false
	}
	select {
	case c := <-p.color:
		return c, true
	case <-p.gone:
		return color.NRGBA{}, false
	case <-done:
		gtkx.Post(func() { h.s.closeShot("") })
		return color.NRGBA{}, false
	}
}

// open opens the tool for p, unless it is open already (for the user, or
// another app).
func (h portalShots) open(p *portalShot, mode string) bool {
	s := h.s
	ok, _ := gtkx.Call(time.Second, func() bool {
		sh := &s.shot
		if sh.open || sh.busy || sh.portal != nil {
			return false
		}
		sh.portal = p
		sh.record = false
		s.openShot(mode)
		return true
	})
	return ok
}

// portalTake answers the app waiting for the tool with the choice cut
// from file, a link to its capture that it then removes, and reports
// whether one was waiting.
func (s *shellUI) portalTake(file string, px image.Rectangle) bool {
	p := s.shot.portal
	if p == nil || p.path == nil {
		return false
	}
	s.shot.portal = nil
	go func() {
		defer os.Remove(file) // a link to the capture, for cutting
		path, err := portalShotFile()
		if err == nil {
			var img image.Image
			if img, err = readPNG(file); err == nil {
				if sub, ok := img.(interface {
					SubImage(image.Rectangle) image.Image
				}); ok {
					err = writePNG(path, sub.SubImage(px))
				} else {
					err = fmt.Errorf("cannot crop a %T", img)
				}
			}
		}
		if err != nil {
			log.Printf("portal screenshot: %v", err)
			path = ""
		}
		p.path <- path
	}()
	return true
}

// portalColor answers the app waiting for a colour, and reports whether
// one was waiting.
func (s *shellUI) portalColor(c color.NRGBA) bool {
	p := s.shot.portal
	if p == nil || p.color == nil {
		return false
	}
	s.shot.portal = nil
	p.color <- c
	return true
}

// portalShotClosed is the tool closing: an app still waiting is told no.
func (s *shellUI) portalShotClosed() {
	if p := s.shot.portal; p != nil {
		s.shot.portal = nil
		close(p.gone)
	}
}
