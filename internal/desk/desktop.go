package desk

import (
	"cmp"
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/clipboard"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	_ "github.com/lyricat/inua/internal/hypr" // the compositors Detect knows
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/notify"
)

// Local connects the tools to this session: the shell's control
// socket ($INUA_INSTANCE, else default), Hyprland, the installed apps
// (terminal runs those that want one), the clipboard and its history, the
// notification history, Tailscale.
func Local(terminal []string) Desktop {
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	d := Desktop{
		Call: func(ctx context.Context, target, fn string, args ...string) (string, error) {
			resp, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: target, Function: fn, Args: args})
			return resp.Result, err
		},
		Launch:    func(e *desktop.Entry) error { return desktop.LaunchWith(e, terminal) },
		Tailscale: tailscale.NewClient(""),
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		ClipRead: func(ctx context.Context) (string, error) {
			out, err := exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", "text").Output()
			return string(out), err
		},
		ClipWrite: func(ctx context.Context, text string) error {
			cmd := exec.CommandContext(ctx, "wl-copy")
			cmd.Stdin = strings.NewReader(text)
			return cmd.Run()
		},
		ClipImage: func(ctx context.Context, path, mime string) error {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			cmd := exec.CommandContext(ctx, "wl-copy", "--type", cmp.Or(mime, "image/png"))
			cmd.Stdin = f
			return cmd.Run()
		},
		ClipHistory: clipboard.HistoryPath(),
		NoteHistory: notify.HistoryPath(),
		NoteHistoryOn: func() bool {
			c, _, err := config.Load(config.DefaultPath())
			return err == nil && c.Notify.History
		},
	}
	var once sync.Once
	var apps []*desktop.Entry
	d.Apps = func() []*desktop.Entry {
		once.Do(func() { apps = desktop.Scan(desktop.DataDirs(), os.Getenv("LANG")) })
		return apps
	}
	if b, err := compositor.Detect(); err == nil {
		d.Compositor = b
	}
	return d
}
