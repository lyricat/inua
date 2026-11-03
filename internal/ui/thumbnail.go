package ui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/hypr"
)

// windowThumbnail captures the focused output with grim and crops a window
// from it. Wayfire has no per-window capture protocol.
func (s *shellUI) windowThumbnail(ctx context.Context, snap hypr.Snapshot, address string) (string, error) {
	var window hypr.Toplevel
	found := false
	for _, candidate := range snap.Toplevels {
		if candidate.Address == address {
			window, found = candidate, true
			break
		}
	}
	if !found {
		return "", errors.New("window is gone")
	}
	var monitor hypr.Monitor
	found = false
	for _, candidate := range snap.Monitors {
		if candidate.Name == window.Monitor {
			monitor, found = candidate, true
			break
		}
	}
	if !found || monitor.Width <= 0 {
		return "", errors.New("window monitor is unknown")
	}
	raw, err := exec.CommandContext(ctx, "grim", "-o", monitor.Name, "-").Output()
	if err != nil {
		return "", err
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	x, y, w, h := 0.0, 0.0, 0.0, 0.0
	if geometry, ok := window.Raw["geometry"].([]any); ok && len(geometry) >= 4 {
		x, _ = geometry[0].(float64)
		y, _ = geometry[1].(float64)
		w, _ = geometry[2].(float64)
		h, _ = geometry[3].(float64)
	} else {
		x, y = overviewPair(window.Raw["at"], 0, 0)
		w, h = overviewPair(window.Raw["size"], 0, 0)
	}
	// at is global; grim captures this output alone.
	x -= float64(monitor.X)
	y -= float64(monitor.Y)
	if w <= 0 || h <= 0 {
		return "", errors.New("window has no geometry")
	}
	factor := float64(img.Bounds().Dx()) / float64(monitor.Width)
	if monitor.Scale > 1 && x+w <= float64(monitor.Width)/monitor.Scale+2 {
		factor = monitor.Scale
	}
	rect := image.Rect(
		int(x*factor), int(y*factor),
		int((x+w)*factor), int((y+h)*factor),
	).Intersect(img.Bounds())
	if rect.Empty() {
		return "", errors.New("window is outside the output")
	}
	thumb := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(thumb, thumb.Bounds(), img, rect.Min, draw.Src)
	dir := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "inua", "thumbs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := strings.NewReplacer(":", "-", "/", "-").Replace(address) + ".png"
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if err := png.Encode(file, thumb); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func setPictureFile(picture *gtk.Picture, path string) {
	texture, err := gdk.NewTextureFromFilename(path)
	if err != nil {
		return
	}
	picture.SetPaintable(texture)
	texture.Unref()
}
