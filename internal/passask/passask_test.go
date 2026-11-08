package passask

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/sockdir"
)

// A question the web pages do not show (a security key's PIN) fails as
// soon as the dialog does, not waiting on pages never asked.
func TestAskNotForPagesFailsWithTheDialog(t *testing.T) {
	dir := sockdir.New(t)
	t.Setenv("XDG_RUNTIME_DIR", dir) // no web terminal host here
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan Reply, 1)
	go func() {
		done <- Ask(ctx, filepath.Join(dir, "none.sock"), pinentry.Request{Mode: pinentry.GetPin, SecurityKey: "Test Key"})
	}()
	select {
	case r := <-done:
		if r.Result != keyring.Failed {
			t.Fatalf("reply %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("still waiting after the dialog failed")
	}
}
