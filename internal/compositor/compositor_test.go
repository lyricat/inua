package compositor

import (
	"context"
	"errors"
	"testing"
)

func numbered() Snapshot {
	return Snapshot{
		Outputs:    []Output{{Name: "A", ActiveWorkspace: "1"}, {Name: "B", ActiveWorkspace: "4"}},
		Workspaces: []Workspace{{ID: "1", Order: 1, Output: "A", Active: true}, {ID: "2", Order: 2, Output: "A"}, {ID: "4", Order: 4, Output: "B", Active: true}, {ID: "12", Order: 12, Output: "B"}},
		Windows:    []Window{{ID: "0x1", Workspace: "1"}, {ID: "0x2", Workspace: "2"}, {ID: "0x4", Workspace: "4"}},
	}
}

func TestNextNumbered(t *testing.T) {
	snap := numbered()
	if ws, ok := NextNumbered(snap, "B", nil, 1); !ok || ws.ID != "12" {
		t.Fatalf("empty workspace on B should be recycled: %+v %v", ws, ok)
	}
	taken := func(id string) bool { return id == "12" }
	if ws, ok := NextNumbered(snap, "B", taken, 1); !ok || ws.ID != "3" || ws.Output != "B" {
		t.Fatalf("with 12 reserved: %+v %v", ws, ok)
	}
	if ws, _ := NextNumbered(Snapshot{}, "", nil, 11); ws.ID != "11" {
		t.Fatalf("fresh from 11: %+v", ws)
	}
	snap.Workspaces = append(snap.Workspaces, Workspace{ID: "3", Order: 3, Output: "A"})
	if ws, _ := NextNumbered(snap, "A", nil, 1); ws.ID != "3" {
		t.Fatalf("empty on A: %+v", ws)
	}
	if ws, _ := NextNumbered(snap, "B", taken, 1); ws.ID != "3" {
		t.Fatalf("lowest empty anywhere: %+v", ws)
	}
	snap.Workspaces = append(snap.Workspaces, Workspace{ID: "special:x", Order: -99})
	if ws, _ := NextNumbered(snap, "B", taken, 1); ws.ID != "3" {
		t.Fatalf("non-numeric ids ignored: %+v", ws)
	}
}

type stub struct{ Backend }

func TestDetect(t *testing.T) {
	regMu.Lock()
	saved := backends
	backends = nil
	regMu.Unlock()
	t.Cleanup(func() {
		regMu.Lock()
		backends = saved
		regMu.Unlock()
	})
	if _, err := Detect(); !errors.Is(err, ErrNoCompositor) {
		t.Fatalf("nothing registered: %v", err)
	}
	Register("absent", func() (Backend, error) { return nil, ErrNoCompositor })
	Register("broken", func() (Backend, error) { return nil, errors.New("socket gone") })
	Register("here", func() (Backend, error) { return stub{}, nil })
	if _, err := Detect(); err == nil || err.Error() != "socket gone" {
		t.Fatalf("a present compositor that cannot be reached must be reported: %v", err)
	}
}

func TestSnapshotLookups(t *testing.T) {
	snap := numbered()
	if snap.ActiveWorkspace("B") != "4" || snap.ActiveWorkspace("Z") != "" {
		t.Fatal("ActiveWorkspace")
	}
	if _, ok := snap.Window("0x9"); ok {
		t.Fatal("unknown window found")
	}
	if ws, ok := snap.Workspace("12"); !ok || ws.Output != "B" {
		t.Fatal("Workspace")
	}
	var cs Capabilities
	if cs.Has(CapGeometry) || cs[CapPreviews] != Unsupported {
		t.Fatal("nil capabilities must read as unsupported")
	}
	_ = context.Background()
}
