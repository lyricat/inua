package desk

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lyricat/inua/internal/apps/sysmon"
	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/apps/weather"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/i18n"
)

// Risk says whether a tool asks before it runs.
type Risk int

const (
	Read   Risk = iota // only looks
	Change             // changes something easily undone; asks when AskBeforeChanges
	Always             // changes something hard to undo, or reaches past inua; always asks
)

// Tool is one of inua's tools.
type Tool struct {
	Name, Description string
	Schema            string // JSON schema of the arguments
	Risk              Risk
	// Use is the permission a workshop app names in its manifest to call
	// it (docs/workshop.md); "" needs none.
	Use string
	// Describe says what a call is about to do, for the window: "Turning
	// on Focus". Nil uses the tool's name.
	Describe func(args map[string]any) string
	Run      func(ctx context.Context, args map[string]any) (any, error)
}

// Call runs the tool and gives its result as text: a string as it is,
// else JSON.
func (t Tool) Call(ctx context.Context, args map[string]any) (string, error) {
	v, err := t.Run(ctx, args)
	if err != nil {
		return "", err
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

// DeclinedPrefix starts the error of a call the user declined, so the
// window says it was declined rather than that it failed.
const DeclinedPrefix = "declined: "

// Desktop is what inua's tools reach: the shell's control socket, Hyprland,
// the installed apps, the clipboard, Tailscale and the web. Tests fill it
// with fakes.
type Desktop struct {
	// Call sends a request to the shell's control socket (`inua call`).
	Call func(ctx context.Context, target, function string, args ...string) (string, error)
	// Compositor is the session's compositor (compositor.Detect), for the
	// window and workspace tools; nil without one.
	Compositor compositor.Backend
	Apps       func() []*desktop.Entry
	Launch     func(*desktop.Entry) error
	ClipRead   func(ctx context.Context) (string, error)
	ClipWrite  func(ctx context.Context, text string) error
	// ClipImage puts an image file on the clipboard.
	ClipImage func(ctx context.Context, path, mime string) error
	// ClipHistory and NoteHistory are the clipboard's and the
	// notifications' histories (internal/clipboard, internal/notify).
	ClipHistory, NoteHistory string
	// NoteHistoryOn says whether the notification history is kept; nil:
	// it is.
	NoteHistoryOn func() bool
	Tailscale     *tailscale.Client
	HTTP          *http.Client
	Processes     func() []sysmon.Process // nil reads /proc
}

func str(args map[string]any, k string) string {
	s, _ := args[k].(string)
	return strings.TrimSpace(s)
}

func num(args map[string]any, k string) (int, bool) {
	switch v := args[k].(type) {
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	}
	return 0, false
}

func boolean(args map[string]any, k string) (bool, bool) {
	switch v := args[k].(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(v) {
		case "on", "true", "yes":
			return true, true
		case "off", "false", "no":
			return false, true
		}
	}
	return false, false
}

func schema(props string, required ...string) string {
	req := ""
	if len(required) > 0 {
		b, _ := json.Marshal(required)
		req = `,"required":` + string(b)
	}
	return `{"type":"object","properties":{` + props + `}` + req + `}`
}

// Tools are inua's tools on d.
func Tools(d Desktop) []Tool {
	ts := windowTools(d)
	ts = append(ts, appTools(d)...)
	ts = append(ts, systemTools(d)...)
	ts = append(ts, infoTools(d)...)
	ts = append(ts, mediaTools(d)...)
	ts = append(ts, historyTools(d)...)
	return ts
}

// --- windows ---------------------------------------------------------------

type window struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	App       string `json:"app"`
	Workspace string `json:"workspace"`
	Focused   bool   `json:"focused,omitempty"`
}

// snapshot reads the compositor's state once.
func snapshot(ctx context.Context, d Desktop) (compositor.Snapshot, error) {
	if d.Compositor == nil {
		return compositor.Snapshot{}, errors.New("no compositor")
	}
	sn, ok := d.Compositor.(compositor.Snapshotter)
	if !ok {
		return compositor.Snapshot{}, errors.New("the compositor cannot be read")
	}
	return sn.Snapshot(ctx)
}

func windows(ctx context.Context, d Desktop) ([]window, error) {
	snap, err := snapshot(ctx, d)
	if err != nil {
		return nil, err
	}
	var ws []window
	for _, w := range snap.Windows {
		name := w.Workspace
		if s, ok := snap.Workspace(w.Workspace); ok && s.Name != "" {
			name = s.Name
		}
		ws = append(ws, window{ID: w.ID, Title: w.Title, App: w.AppID, Workspace: name, Focused: w.Focused})
	}
	return ws, nil
}

// workspaceID is the id of the workspace named or numbered q, else q
// itself for a workspace the compositor can make.
func workspaceID(ctx context.Context, d Desktop, q string) (string, error) {
	if q == "" || strings.ContainsAny(q, ",\n") {
		return "", errors.New("want a workspace")
	}
	snap, err := snapshot(ctx, d)
	if err != nil {
		return "", err
	}
	for _, w := range snap.Workspaces {
		if w.ID == q || w.Name == q {
			return w.ID, nil
		}
	}
	return q, nil
}

// findWindow takes a window's id, else the one whose title or app has the
// words; several are an error naming them.
func findWindow(ctx context.Context, d Desktop, q string) (window, error) {
	ws, err := windows(ctx, d)
	if err != nil {
		return window{}, err
	}
	if q == "" {
		for _, w := range ws {
			if w.Focused {
				return w, nil
			}
		}
		return window{}, errors.New("no window is focused")
	}
	var hits []window
	for _, w := range ws {
		if w.ID == q {
			return w, nil
		}
		if strings.Contains(strings.ToLower(w.Title+" "+w.App), strings.ToLower(q)) {
			hits = append(hits, w)
		}
	}
	switch len(hits) {
	case 0:
		return window{}, fmt.Errorf("no window matches %q", q)
	case 1:
		return hits[0], nil
	}
	var names []string
	for _, w := range hits {
		names = append(names, fmt.Sprintf("%s (%s, id %s)", w.Title, w.App, w.ID))
	}
	return window{}, fmt.Errorf("several windows match %q: %s; pass an id", q, strings.Join(names, "; "))
}

const windowArg = `"window":{"type":"string","description":"A window's id from list_windows, or words of its title or app. Empty: the focused window."}`

func windowTools(d Desktop) []Tool {
	named := func(verb string) func(map[string]any) string {
		return func(a map[string]any) string {
			return i18n.Tf(verb, cmp.Or(str(a, "window"), i18n.T("the focused window")))
		}
	}
	return []Tool{
		{
			Name: "list_windows", Use: "windows", Description: "Lists the open windows: id, title, app and workspace, and which is focused.",
			Describe: func(map[string]any) string { return i18n.T("Looking at the windows") },
			Run: func(ctx context.Context, _ map[string]any) (any, error) {
				return windows(ctx, d)
			},
		},
		{
			Name: "focus_window", Use: "windows", Description: "Brings a window forward and focuses it.", Schema: schema(windowArg),
			Describe: named(i18n.M("Focusing %s")),
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				w, err := findWindow(ctx, d, str(a, "window"))
				if err != nil {
					return nil, err
				}
				return w, d.Compositor.FocusWindow(ctx, w.ID)
			},
		},
		{
			Name: "move_window", Use: "windows", Description: "Moves a window to a workspace (a number or a name).",
			Schema: schema(windowArg+`,"workspace":{"type":"string"}`, "workspace"), Risk: Change,
			Describe: func(a map[string]any) string {
				return i18n.Tf("Moving %s to workspace %s", cmp.Or(str(a, "window"), i18n.T("the focused window")), str(a, "workspace"))
			},
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				w, err := findWindow(ctx, d, str(a, "window"))
				if err != nil {
					return nil, err
				}
				ws, err := workspaceID(ctx, d, str(a, "workspace"))
				if err != nil {
					return nil, err
				}
				return w, d.Compositor.MoveWindow(ctx, w.ID, ws, false)
			},
		},
		{
			Name: "close_window", Use: "windows", Description: "Closes a window (its app may ask to save).", Schema: schema(windowArg),
			Risk: Always, Describe: named(i18n.M("Closing %s")),
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				w, err := findWindow(ctx, d, str(a, "window"))
				if err != nil {
					return nil, err
				}
				return w, d.Compositor.CloseWindow(ctx, w.ID)
			},
		},
		{
			Name: "switch_workspace", Use: "windows", Description: "Shows a workspace (a number or a name).",
			Schema:   schema(`"workspace":{"type":"string"}`, "workspace"),
			Describe: func(a map[string]any) string { return i18n.Tf("Going to workspace %s", str(a, "workspace")) },
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				ws, err := workspaceID(ctx, d, str(a, "workspace"))
				if err != nil {
					return nil, err
				}
				return "ok", d.Compositor.FocusWorkspace(ctx, ws)
			},
		},
		{
			Name: "list_workspaces", Use: "windows", Description: "Lists the workspaces (id, name, monitor, how many windows, which is shown and focused) and the monitors (name, size, scale, the workspace shown).",
			Describe: func(map[string]any) string { return i18n.T("Looking at the workspaces") },
			Run: func(ctx context.Context, _ map[string]any) (any, error) {
				return workspaces(ctx, d)
			},
		},
	}
}

// workspaces are the compositor's workspaces and monitors.
func workspaces(ctx context.Context, d Desktop) (any, error) {
	snap, err := snapshot(ctx, d)
	if err != nil {
		return nil, err
	}
	type monitor struct {
		Name      string  `json:"name"`
		Width     int     `json:"width"`
		Height    int     `json:"height"`
		Scale     float64 `json:"scale"`
		Focused   bool    `json:"focused,omitempty"`
		Workspace string  `json:"workspace"`
	}
	type workspace struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Monitor string `json:"monitor"`
		Windows int    `json:"windows"`
		Shown   bool   `json:"shown,omitempty"`
		Focused bool   `json:"focused,omitempty"`
	}
	var outM []monitor
	for _, m := range snap.Outputs {
		outM = append(outM, monitor{m.Name, m.Width, m.Height, m.Scale, m.Focused, m.ActiveWorkspace})
	}
	count := map[string]int{}
	for _, w := range snap.Windows {
		count[w.Workspace]++
	}
	var outW []workspace
	for _, w := range snap.Workspaces {
		outW = append(outW, workspace{w.ID, w.Name, w.Output, count[w.ID], w.Active, w.Focused})
	}
	return map[string]any{"workspaces": outW, "monitors": outM}, nil
}

// --- apps -------------------------------------------------------------------

func findApp(d Desktop, q string) (*desktop.Entry, error) {
	if d.Apps == nil {
		return nil, errors.New("no apps")
	}
	q = strings.ToLower(q)
	var hits []*desktop.Entry
	for _, e := range d.Apps() {
		if e.NoDisplay {
			continue
		}
		if strings.ToLower(e.ID) == q || strings.ToLower(e.Name) == q {
			return e, nil
		}
		if strings.Contains(strings.ToLower(e.Name+" "+e.GenericName+" "+e.ID), q) {
			hits = append(hits, e)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("no app matches %q", q)
	}
	var names []string
	for _, e := range hits[:min(len(hits), 8)] {
		names = append(names, e.Name+" ("+e.ID+")")
	}
	return nil, fmt.Errorf("several apps match %q: %s", q, strings.Join(names, ", "))
}

func appTools(d Desktop) []Tool {
	return []Tool{
		{
			Name: "list_apps", Use: "apps", Description: "Lists installed apps (name, id, what it is), those matching the words when given.",
			Schema:   schema(`"query":{"type":"string"}`),
			Describe: func(map[string]any) string { return i18n.T("Looking at the apps") },
			Run: func(_ context.Context, a map[string]any) (any, error) {
				q := strings.ToLower(str(a, "query"))
				type app struct{ Name, ID, About string }
				var out []app
				for _, e := range d.Apps() {
					if !e.NoDisplay && (q == "" || strings.Contains(strings.ToLower(e.Name+" "+e.GenericName+" "+e.Comment+" "+strings.Join(e.Keywords, " ")), q)) {
						out = append(out, app{e.Name, e.ID, cmp.Or(e.GenericName, e.Comment)})
					}
				}
				return out[:min(len(out), 40)], nil
			},
		},
		{
			Name: "open_app", Use: "apps", Description: "Starts an installed app, by its name or id.",
			Schema:   schema(`"app":{"type":"string"}`, "app"),
			Describe: func(a map[string]any) string { return i18n.Tf("Opening %s", str(a, "app")) },
			Run: func(_ context.Context, a map[string]any) (any, error) {
				e, err := findApp(d, str(a, "app"))
				if err != nil {
					return nil, err
				}
				return map[string]string{"opened": e.Name}, d.Launch(e)
			},
		},
	}
}

// --- the system (through the shell) ------------------------------------------

// shell asks the shell's control socket: JSON as it is, else the text,
// or "ok".
func (d Desktop) shell(ctx context.Context, target, fn string, args ...string) (any, error) {
	if d.Call == nil {
		return nil, errors.New("the shell is not running")
	}
	out, err := d.Call(ctx, target, fn, args...)
	if err != nil {
		return nil, err
	}
	if json.Valid([]byte(out)) {
		return json.RawMessage(out), nil
	}
	return cmp.Or(out, "ok"), nil
}

func systemTools(d Desktop) []Tool {
	call := d.shell
	settings := []string{"wifi", "bluetooth", "volume", "mute", "brightness", "focus", "saver"}
	return []Tool{
		{
			Name: "system_status", Use: "system", Description: "Battery, Wi-Fi, Bluetooth, volume, brightness, power saver, Focus (quiet notifications), the time.",
			Describe: func(map[string]any) string { return i18n.T("Looking at the system") },
			Run: func(ctx context.Context, _ map[string]any) (any, error) {
				return call(ctx, "system", "status")
			},
		},
		{
			Name: "set_system", Use: "system", Description: `Changes one setting: wifi, bluetooth, mute, focus, saver (power saver) take "on" or "off"; volume and brightness take 0 to 100.`,
			Schema: schema(`"setting":{"type":"string","enum":["wifi","bluetooth","volume","mute","brightness","focus","saver"]},"value":{"type":"string"}`, "setting", "value"),
			Risk:   Change,
			Describe: func(a map[string]any) string {
				// Whole sentences: word order differs between languages.
				s, v := str(a, "setting"), strings.TrimSuffix(str(a, "value"), "%")
				switch s {
				case "volume":
					return i18n.Tf("Setting the volume to %s%%", v)
				case "brightness":
					return i18n.Tf("Setting the brightness to %s%%", v)
				}
				said := map[string]string{
					"wifi on": i18n.T("Turning Wi-Fi on"), "wifi off": i18n.T("Turning Wi-Fi off"),
					"bluetooth on": i18n.T("Turning Bluetooth on"), "bluetooth off": i18n.T("Turning Bluetooth off"),
					"mute on": i18n.T("Muting the sound"), "mute off": i18n.T("Unmuting the sound"),
					"focus on": i18n.T("Turning Focus on"), "focus off": i18n.T("Turning Focus off"),
					"saver on": i18n.T("Turning power saver on"), "saver off": i18n.T("Turning power saver off"),
				}[s+" "+v]
				return cmp.Or(said, i18n.Tf("Setting %s to %s", s, v))
			},
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				s, v := str(a, "setting"), strings.TrimSuffix(str(a, "value"), "%")
				if !slices.Contains(settings, s) {
					return nil, fmt.Errorf("no setting %q", s)
				}
				return call(ctx, "system", "set", s, v)
			},
		},
		{
			Name: "lock_screen", Use: "system", Description: "Locks the screen.", Risk: Change,
			Describe: func(map[string]any) string { return i18n.T("Locking the screen") },
			Run:      func(ctx context.Context, _ map[string]any) (any, error) { return call(ctx, "lock", "now") },
		},
		{
			Name: "screenshot", Use: "system", Description: `Takes a screenshot, saved and copied: "window" (the focused one) or "screen" at once, or "area" for the user to drag out.`,
			Schema: schema(`"of":{"type":"string","enum":["area","window","screen"]}`),
			Describe: func(a map[string]any) string {
				switch str(a, "of") {
				case "window":
					return i18n.T("Taking a screenshot of the focused window")
				case "screen":
					return i18n.T("Taking a screenshot of the screen")
				}
				return i18n.T("Taking a screenshot of an area")
			},
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				of := cmp.Or(str(a, "of"), "area")
				if !slices.Contains([]string{"area", "window", "screen"}, of) {
					return nil, fmt.Errorf("want area, window or screen")
				}
				return call(ctx, "screenshot", of)
			},
		},
		{
			Name: "power", Use: "system", Description: "Suspends, restarts, shuts down or logs out.",
			Schema: schema(`"action":{"type":"string","enum":["suspend","reboot","poweroff","logout"]}`, "action"),
			Risk:   Always,
			Describe: func(a map[string]any) string {
				return map[string]string{"suspend": i18n.T("Suspending"), "reboot": i18n.T("Restarting the computer"), "poweroff": i18n.T("Shutting down"), "logout": i18n.T("Logging out")}[str(a, "action")]
			},
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				act := str(a, "action")
				if !slices.Contains([]string{"suspend", "reboot", "poweroff", "logout"}, act) {
					return nil, fmt.Errorf("no action %q", act)
				}
				return call(ctx, "system", "power", act)
			},
		},
		{
			Name: "list_notifications", Use: "notifications", Description: "The notifications waiting in the notification center, newest first: app, summary, body, time.",
			Describe: func(map[string]any) string { return i18n.T("Reading the notifications") },
			Run:      func(ctx context.Context, _ map[string]any) (any, error) { return call(ctx, "notifications", "list") },
		},
		{
			Name: "clear_notifications", Use: "notifications", Description: "Clears every notification.", Risk: Change,
			Describe: func(map[string]any) string { return i18n.T("Clearing the notifications") },
			Run:      func(ctx context.Context, _ map[string]any) (any, error) { return call(ctx, "notifications", "clear") },
		},
		{
			Name: "dismiss_notification", Use: "notifications", Description: "Clears one notification, by its id from list_notifications.",
			Schema: schema(`"id":{"type":"integer"}`, "id"), Risk: Change,
			Describe: func(map[string]any) string { return i18n.T("Dismissing a notification") },
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				id, ok := num(a, "id")
				if !ok {
					return nil, errors.New("want a notification's id")
				}
				return call(ctx, "notifications", "dismiss", strconv.Itoa(id))
			},
		},
		{
			Name: "notification_action", Use: "notifications", Description: `Runs one of a notification's actions, by their keys from list_notifications; "default" (the default) is as clicking it.`,
			Schema: schema(`"id":{"type":"integer"},"action":{"type":"string"}`, "id"), Risk: Change,
			Describe: func(a map[string]any) string {
				if k := str(a, "action"); k != "" && k != "default" {
					return i18n.Tf("Choosing “%s” on a notification", k)
				}
				return i18n.T("Opening a notification")
			},
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				id, ok := num(a, "id")
				if !ok {
					return nil, errors.New("want a notification's id")
				}
				return call(ctx, "notifications", "invoke", strconv.Itoa(id), cmp.Or(str(a, "action"), "default"))
			},
		},
		{
			Name: "show_panel", Use: "system", Description: "Opens part of the shell for the user: the launcher, the overview, the control center, the calendar, the clipboard history, the notification center, or Settings (at a page when given).",
			Schema: schema(`"panel":{"type":"string","enum":["launcher","overview","control_center","calendar","clipboard","notifications","settings"]},"page":{"type":"string","enum":["appearance","bar","launcher","ai","notifications","clipboard","keyring","shortcuts","keyboard","mouse","about"]}`, "panel"),
			Describe: func(a map[string]any) string {
				return map[string]string{
					"launcher": i18n.T("Opening the launcher"), "overview": i18n.T("Opening the overview"),
					"control_center": i18n.T("Opening the control center"), "calendar": i18n.T("Opening the calendar"),
					"clipboard": i18n.T("Opening the clipboard history"), "notifications": i18n.T("Opening the notifications"),
					"settings": i18n.T("Opening Settings"),
				}[str(a, "panel")]
			},
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				to, ok := map[string][2]string{
					"launcher": {"launcher", "apps"}, "overview": {"overview", "open"}, "control_center": {"control", "open"},
					"calendar": {"calendar", "open"}, "clipboard": {"clipboard", "open"}, "notifications": {"notifications", "open"},
					"settings": {"settings", "open"},
				}[str(a, "panel")]
				if !ok {
					return nil, fmt.Errorf("no panel %q", str(a, "panel"))
				}
				if p := str(a, "page"); to[0] == "settings" && p != "" {
					return call(ctx, "settings", "open", p)
				}
				return call(ctx, to[0], to[1])
			},
		},
		{
			Name: "clipboard_read", Use: "clipboard-history", Description: "The text on the clipboard.",
			Describe: func(map[string]any) string { return i18n.T("Reading the clipboard") },
			Run: func(ctx context.Context, _ map[string]any) (any, error) {
				t, err := d.ClipRead(ctx)
				if len(t) > 20000 {
					t = t[:20000] + "…"
				}
				return map[string]string{"text": t}, err
			},
		},
		{
			Name: "clipboard_write", Use: "clipboard", Description: "Puts text on the clipboard.",
			Schema: schema(`"text":{"type":"string"}`, "text"), Risk: Change,
			Describe: func(a map[string]any) string {
				t := []rune(str(a, "text"))
				if len(t) > 40 {
					return i18n.Tf("Copying “%s…”", string(t[:40]))
				}
				return i18n.Tf("Copying “%s”", string(t))
			},
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				return "ok", d.ClipWrite(ctx, str(a, "text"))
			},
		},
	}
}

// --- what is going on ----------------------------------------------------------

func infoTools(d Desktop) []Tool {
	procs := func() []sysmon.Process {
		if d.Processes != nil {
			return d.Processes()
		}
		return sysmon.ReadProcesses()
	}
	return []Tool{
		{
			Name: "processes", Use: "processes", Description: "The busiest processes: by CPU (over half a second) or by memory.",
			Schema:   schema(`"by":{"type":"string","enum":["cpu","memory"]},"limit":{"type":"integer"}`),
			Describe: func(a map[string]any) string { return i18n.T("Looking at the processes") },
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				limit, _ := num(a, "limit")
				limit = min(max(cmp.Or(limit, 8), 1), 30)
				first := procs()
				byPID := map[int]uint64{}
				for _, p := range first {
					byPID[p.PID] = p.Ticks
				}
				const span = 500 * time.Millisecond
				select {
				case <-time.After(span):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				type proc struct {
					PID        int `json:"pid"`
					Name, User string
					CPU        float64 `json:"cpu_percent"`
					MemoryMB   float64 `json:"memory_mb"`
				}
				var out []proc
				for _, p := range procs() {
					cpu := 0.0
					if t, ok := byPID[p.PID]; ok && p.Ticks >= t {
						cpu = float64(p.Ticks-t) / 100 / span.Seconds() * 100 // 100 ticks a second
					}
					out = append(out, proc{p.PID, p.Name, p.User, float64(int(cpu*10)) / 10, float64(p.Mem >> 20)})
				}
				if str(a, "by") == "memory" {
					slices.SortFunc(out, func(x, y proc) int { return cmp.Compare(y.MemoryMB, x.MemoryMB) })
				} else {
					slices.SortFunc(out, func(x, y proc) int { return cmp.Compare(y.CPU, x.CPU) })
				}
				return out[:min(limit, len(out))], nil
			},
		},
		{
			Name: "end_process", Use: "processes", Description: "Asks one of the user's processes to quit (force: kill it).",
			Schema: schema(`"pid":{"type":"integer"},"force":{"type":"boolean"}`, "pid"), Risk: Always,
			Describe: func(a map[string]any) string {
				pid, _ := num(a, "pid")
				name := ""
				for _, p := range procs() {
					if p.PID == pid {
						name = p.Name + " "
					}
				}
				if f, _ := boolean(a, "force"); f {
					return i18n.Tf("Killing %s(%d)", name, pid)
				}
				return i18n.Tf("Asking %s(%d) to quit", name, pid)
			},
			Run: func(_ context.Context, a map[string]any) (any, error) {
				pid, ok := num(a, "pid")
				if !ok || pid <= 1 {
					return nil, errors.New("want a process id")
				}
				uid := os.Getuid()
				own := slices.ContainsFunc(procs(), func(p sysmon.Process) bool { return p.PID == pid && p.UID == uid })
				if !own {
					return nil, fmt.Errorf("process %d is not the user's", pid)
				}
				sig := syscall.SIGTERM
				if f, _ := boolean(a, "force"); f {
					sig = syscall.SIGKILL
				}
				return "ok", sysmon.Signal(sig, []int{pid}, func(int) bool { return true })
			},
		},
		{
			Name: "tailscale_status", Use: "network", Description: "Tailscale: connected or not, this device, the devices of the tailnet (name, address, online, OS), the exit node.",
			Describe: func(map[string]any) string { return i18n.T("Looking at Tailscale") },
			Run: func(ctx context.Context, _ map[string]any) (any, error) {
				st, err := d.Tailscale.Status(ctx)
				if err != nil {
					return nil, err
				}
				state, _ := st.State()
				type dev struct {
					Name, Address, OS string
					Online            bool
					ExitNode          bool `json:"exit_node,omitempty"`
					OffersExit        bool `json:"offers_exit,omitempty"`
				}
				var devs []dev
				for _, p := range st.Peers("") {
					devs = append(devs, dev{p.Name(), p.IPv4(), p.OS, p.Online, p.ExitNode, p.ExitNodeOption})
				}
				self := ""
				if st.Self != nil {
					self = st.Self.Name() + " " + st.Self.IPv4()
				}
				return map[string]any{"state": state, "this_device": self, "devices": devs}, nil
			},
		},
		{
			Name: "tailscale_set", Use: "network", Description: `Connects or disconnects Tailscale ("connect": true/false), or sends traffic through an exit node ("exit_node": a device's name, or "none").`,
			Schema: schema(`"connect":{"type":"boolean"},"exit_node":{"type":"string"}`), Risk: Change,
			Describe: func(a map[string]any) string {
				if e := str(a, "exit_node"); e != "" {
					if e == "none" {
						return i18n.T("Stopping the Tailscale exit node")
					}
					return i18n.Tf("Using %s as Tailscale exit node", e)
				}
				if c, _ := boolean(a, "connect"); c {
					return i18n.T("Connecting Tailscale")
				}
				return i18n.T("Disconnecting Tailscale")
			},
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				if e := str(a, "exit_node"); e != "" {
					id := ""
					if e != "none" {
						st, err := d.Tailscale.Status(ctx)
						if err != nil {
							return nil, err
						}
						for _, p := range st.ExitNodes() {
							if strings.EqualFold(p.Name(), e) {
								id = p.ID
							}
						}
						if id == "" {
							return nil, fmt.Errorf("no device %q offers to be an exit node", e)
						}
					}
					_, err := d.Tailscale.EditPrefs(ctx, tailscale.UseExitNode(id))
					return "ok", err
				}
				c, ok := boolean(a, "connect")
				if !ok {
					return nil, errors.New(`want "connect" or "exit_node"`)
				}
				_, err := d.Tailscale.EditPrefs(ctx, tailscale.Edit{WantRunning: c, WantRunningSet: true})
				return "ok", err
			},
		},
		{
			Name: "weather", Description: "The weather now, the next 12 hours and the next 4 days for a place (Open-Meteo).",
			Schema:   schema(`"place":{"type":"string","description":"A city, e.g. Kyoto"}`, "place"),
			Describe: func(a map[string]any) string { return i18n.Tf("Looking up the weather in %s", str(a, "place")) },
			Run: func(ctx context.Context, a map[string]any) (any, error) {
				places, err := weather.Search(ctx, d.HTTP, str(a, "place"))
				if err != nil {
					return nil, err
				}
				if len(places) == 0 {
					return nil, fmt.Errorf("no place called %q", str(a, "place"))
				}
				p := places[0]
				f, err := weather.Get(ctx, d.HTTP, p, false)
				if err != nil {
					return nil, err
				}
				text, _ := weather.Describe(f.Now.Code, f.Now.Day)
				type hour struct {
					Time string  `json:"time"`
					Temp float64 `json:"temp_c"`
					Sky  string  `json:"sky"`
				}
				var hours []hour
				for _, h := range f.Hours[:min(12, len(f.Hours))] {
					s, _ := weather.Describe(h.Code, true)
					hours = append(hours, hour{h.Time.Format("15:04"), h.Temp, s})
				}
				type day struct {
					Date string  `json:"date"`
					Low  float64 `json:"low_c"`
					High float64 `json:"high_c"`
					Sky  string  `json:"sky"`
					Rain int     `json:"rain_percent"`
				}
				var days []day
				for _, dd := range f.Days[:min(4, len(f.Days))] {
					s, _ := weather.Describe(dd.Code, true)
					days = append(days, day{dd.Date.Format("Mon Jan 2"), dd.Min, dd.Max, s, dd.Rain})
				}
				return map[string]any{
					"place": strings.Join(slices.DeleteFunc([]string{p.Name, p.Region, p.Country}, func(s string) bool { return s == "" }), ", "),
					"now":   map[string]any{"temp_c": f.Now.Temp, "feels_c": f.Now.Feels, "sky": text, "humidity": f.Now.Humidity, "wind_kmh": f.Now.Wind},
					"hours": hours, "days": days,
				}, nil
			},
		},
	}
}
