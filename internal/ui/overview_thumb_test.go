package ui

import (
	"testing"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/hypr"
)

func TestFillOverviewThumbnailsInitializesMaps(t *testing.T) {
	s := &shellUI{}
	o := &workspaceOverview{images: map[string]*gtk.Picture{}}
	s.fillOverviewThumbnails(o, hypr.Snapshot{}, 0)
	if o.thumbStarted == nil || o.thumbPaths == nil {
		t.Fatalf("thumbnail maps were not initialized: started=%v paths=%v", o.thumbStarted, o.thumbPaths)
	}
}
