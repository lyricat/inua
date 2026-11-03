package dbusexport

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"github.com/lyricat/inua/internal/dbustest"
)

const testIface = "org.inua.ExportTest"

type echo struct{}

func (echo) Echo(s string) (string, *dbus.Error) { return s, nil }

type exporter struct{ conn *dbus.Conn }

// Exercise exports from the goroutines godbus uses to execute incoming calls.
func (e exporter) Cycle(id uint32) (uint32, *dbus.Error) {
	path := dbus.ObjectPath(fmt.Sprintf("/requests/r%d", id))
	if err := Export(e.conn, echo{}, path, testIface); err != nil {
		return 0, dbus.MakeFailedError(err)
	}
	if err := Export(e.conn, nil, path, testIface); err != nil {
		return 0, dbus.MakeFailedError(err)
	}
	return id, nil
}

func TestConcurrentExportsAndCalls(t *testing.T) {
	addr := dbustest.Start(t)
	server, client := dbustest.Conn(t, addr), dbustest.Conn(t, addr)
	if err := Export(server, exporter{server}, "/service", testIface); err != nil {
		t.Fatal(err)
	}
	obj := client.Object(server.Names()[0], "/service")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			<-start
			own := dbus.ObjectPath(fmt.Sprintf("/objects/o%d", worker))
			iface := fmt.Sprintf("%s.Worker%d", testIface, worker)
			for i := range 300 {
				path := own
				if i%2 == 0 {
					path = "/objects/shared"
				}
				var err error
				switch i % 8 {
				case 0:
					err = Export(server, echo{}, path, iface)
				case 1:
					err = ExportWithMap(server, echo{}, map[string]string{"Echo": "echo"}, path, iface)
				case 2:
					err = ExportSubtree(server, echo{}, path, iface)
				case 3:
					err = ExportSubtreeWithMap(server, echo{}, map[string]string{"Echo": "echo"}, path, iface)
				case 4:
					err = ExportMethodTable(server, map[string]any{"Echo": echo{}.Echo}, path, iface)
				case 5:
					err = ExportSubtreeMethodTable(server, map[string]any{"Echo": echo{}.Echo}, path, iface)
				case 6:
					err = ExportAll(server, echo{}, path, iface)
				case 7:
					_, err = Properties(server, path, prop.Map{iface: {"Value": {Value: "ready"}}})
				}
				if err != nil {
					t.Error(err)
					return
				}
				if err := Export(server, nil, path, iface); err != nil {
					t.Error(err)
				}
				if err := Export(server, nil, path, "org.freedesktop.DBus.Properties"); err != nil {
					t.Error(err)
				}
			}
		})
	}
	for caller := range 4 {
		wg.Go(func() {
			<-start
			for i := range 100 {
				id := uint32(caller*100 + i)
				var got uint32
				if err := obj.CallWithContext(ctx, testIface+".Cycle", 0, id).Store(&got); err != nil {
					t.Error(err)
					return
				}
				if got != id {
					t.Errorf("Cycle(%d) = %d", id, got)
				}
			}
		})
	}
	close(start)
	wg.Wait()
	exports.Lock()
	defer exports.Unlock()
	if len(exports.conns) != 0 {
		t.Errorf("retained %d idle connections", len(exports.conns))
	}
}

func TestExportSemantics(t *testing.T) {
	// Subtrees follow the keyring lifecycle: export before owning the name,
	// serve calls with stable interfaces, then release the name and drain calls
	// before removal. Concurrent subtree mutation and introspection reads inside
	// godbus are outside this regression's scope (godbus/dbus#460).
	addr := dbustest.Start(t)
	server, client := dbustest.Conn(t, addr), dbustest.Conn(t, addr)
	for _, tc := range []struct {
		name    string
		export  func(any, dbus.ObjectPath, string) error
		method  string
		subtree bool
	}{
		{"plain", func(v any, p dbus.ObjectPath, i string) error { return Export(server, v, p, i) }, "Echo", false},
		{"all", func(v any, p dbus.ObjectPath, i string) error { return ExportAll(server, v, p, i) }, "Echo", false},
		{"mapped", func(v any, p dbus.ObjectPath, i string) error {
			return ExportWithMap(server, v, map[string]string{"Echo": "echo"}, p, i)
		}, "echo", false},
		{"subtree", func(v any, p dbus.ObjectPath, i string) error { return ExportSubtree(server, v, p, i) }, "Echo", true},
		{"subtree_mapped", func(v any, p dbus.ObjectPath, i string) error {
			return ExportSubtreeWithMap(server, v, map[string]string{"Echo": "echo"}, p, i)
		}, "echo", true},
		{"table", func(v any, p dbus.ObjectPath, i string) error {
			var methods map[string]any
			if v != nil {
				methods = map[string]any{"echo": echo{}.Echo, "ignored": 1}
			}
			return ExportMethodTable(server, methods, p, i)
		}, "echo", false},
		{"subtree_table", func(v any, p dbus.ObjectPath, i string) error {
			var methods map[string]any
			if v != nil {
				methods = map[string]any{"echo": echo{}.Echo}
			}
			return ExportSubtreeMethodTable(server, methods, p, i)
		}, "echo", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := dbus.ObjectPath("/" + tc.name)
			if err := tc.export(nil, path, testIface); err != nil {
				t.Fatalf("empty removal: %v", err)
			}
			for _, v := range []any{nil, echo{}} {
				if err := tc.export(v, "invalid", testIface); err == nil || err.Error() != `dbus: Invalid path name: "invalid"` {
					t.Fatalf("invalid path: %v", err)
				}
			}
			if err := tc.export(echo{}, path, testIface); err != nil {
				t.Fatal(err)
			}
			if err := Export(server, echo{}, path, testIface+".Other"); err != nil {
				t.Fatal(err)
			}
			callPath := path
			destination := server.Names()[0]
			if tc.subtree {
				callPath += "/child"
				destination = testIface
				if reply, err := server.RequestName(destination, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
					t.Fatalf("own subtree name: %v, %v", reply, err)
				}
				t.Cleanup(func() { server.ReleaseName(destination) })
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			obj := client.Object(destination, callPath)
			var got string
			if err := obj.CallWithContext(ctx, testIface+"."+tc.method, 0, "hello").Store(&got); err != nil || got != "hello" {
				t.Fatalf("method reply = %q, %v", got, err)
			}
			if tc.method == "echo" {
				if err := obj.CallWithContext(ctx, testIface+".Echo", 0, "hello").Err; err == nil {
					t.Error("original mapped name still exported")
				}
			}
			if tc.subtree {
				var calls sync.WaitGroup
				for range 4 {
					calls.Go(func() {
						for range 25 {
							var got string
							if err := obj.CallWithContext(ctx, testIface+"."+tc.method, 0, "hello").Store(&got); err != nil || got != "hello" {
								t.Errorf("subtree reply = %q, %v", got, err)
								return
							}
						}
					})
				}
				calls.Wait()
				if reply, err := server.ReleaseName(destination); err != nil || reply != dbus.ReleaseNameReplyReleased {
					t.Fatalf("release subtree name: %v, %v", reply, err)
				}
			}
			if err := tc.export(nil, path, testIface); err != nil {
				t.Fatal(err)
			}
			if tc.subtree {
				// Start serving again to check removal while the remaining
				// interfaces stay unchanged for the whole call phase.
				if reply, err := server.RequestName(destination, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
					t.Fatalf("reclaim subtree name: %v, %v", reply, err)
				}
			}
			if err := obj.CallWithContext(ctx, testIface+"."+tc.method, 0, "hello").Err; err == nil {
				t.Error("removed interface still callable")
			}
			if err := client.Object(destination, path).CallWithContext(ctx, testIface+".Other.Echo", 0, "kept").Store(&got); err != nil || got != "kept" {
				t.Fatalf("other interface lost: %q, %v", got, err)
			}
			if tc.subtree {
				if reply, err := server.ReleaseName(destination); err != nil || reply != dbus.ReleaseNameReplyReleased {
					t.Fatalf("release subtree name: %v, %v", reply, err)
				}
			}
			if err := Export(server, nil, path, testIface+".Other"); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("properties", func(t *testing.T) {
		props, err := Properties(server, "/properties", prop.Map{testIface: {"Value": {Value: "before", Writable: true}}})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		obj := client.Object(server.Names()[0], "/properties")
		var value dbus.Variant
		if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, testIface, "Value").Store(&value); err != nil || value.Value() != "before" {
			t.Fatalf("Get = %v, %v", value, err)
		}
		if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Set", 0, testIface, "Value", dbus.MakeVariant("after")).Err; err != nil {
			t.Fatal(err)
		}
		if value, err := props.Get(testIface, "Value"); err != nil || value.Value() != "after" {
			t.Fatalf("Set: %v, %v", value, err)
		}
		if _, err := Properties(server, "invalid", nil); err == nil || !strings.Contains(err.Error(), "Invalid path name") {
			t.Fatalf("invalid property path: %v", err)
		}
		if err := Export(server, nil, "/properties", "org.freedesktop.DBus.Properties"); err != nil {
			t.Fatal(err)
		}
		if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, testIface, "Value").Err; err == nil {
			t.Error("removed properties still callable")
		}
	})
}
