# Compositor contracts and the migration plan

The fourth step of #58 (issue #62). It defines the smallest set of
interfaces inua needs between its shell and a compositor, from what the
[audit](hyprland-coupling.md) found the code using, what the
[capability comparison](compositor-capabilities.md) says the candidates
offer, and the [experiments'](compositor-experiments.md) constraints. It
then gives the plan for moving Hyprland behind them (#63) and validating
them with niri (#64). It is a contract, not a universal compositor API: a
backend implements the base and whichever optional pieces its compositor
can do, and says which.

Package: `internal/compositor`, toolkit-free, imported by `internal/shell`,
`internal/ui`, `internal/desk` and the light apps. The backends
(`internal/hypr` today, `internal/niri` in #64) import it and register
themselves; nothing in `internal/compositor` imports a backend.

## 1. Shared types

Only what a consumer reads today. Ids are opaque strings chosen by the
backend and compared for equality only.

```go
type Output struct {
    Name        string  // connector name, equal to GDK's
    Description string
    X, Y        int     // logical position in the global space
    Width, Height int   // logical size after scale and transform
    Scale       float64
    Transform   int     // wl_output transform; odd values swap width and height when a backend reports the mode's size
    RefreshRate float64 // Hz, 0 unknown
    Focused     bool
    ActiveWorkspace  string // workspace id shown on it
    SpecialWorkspace string // Hyprland's special workspace shown on it, "" elsewhere
}

type Workspace struct {
    ID      string
    Name    string
    Output  string
    Order   int    // sort key among the output's workspaces; backends make it stable while nothing moves
    Active  bool   // shown on its output
    Focused bool   // active on the focused output
    Urgent  bool
    HasFullscreen bool
}

type Window struct {
    ID         string
    PID        int
    Title      string
    AppID      string  // Hyprland's class, niri's app_id, Wayfire's app-id
    Workspace  string
    Output     string
    Focused    bool
    Urgent     bool
    Floating   bool
    FocusRank  int     // 1 the most recently focused, larger older; 0 unknown
    Geometry   *Rect   // global logical pixels; nil when the backend has none (CapGeometry)
}

type Rect struct{ X, Y, W, H int }

type Snapshot struct {
    Outputs       []Output    // by name
    Workspaces    []Workspace // by output, then Order
    Windows       []Window    // backend order
    FocusedWindow string
    FocusedOutput string
}
```

Differences from `hypr.Snapshot`: `Raw` maps are gone, their seven read
fields are typed (`at`/`size` → `Geometry`, `floating`, `focusHistoryID` →
`FocusRank` as the id plus one, `transform`, `refreshRate`,
`specialWorkspace`); ids are strings; `Monitor` is `Output`; `Toplevel` is
`Window`; `Class` is `AppID`; `Activated` is `Focused`. Special and negative
Hyprland workspaces are not in `Workspaces`; a backend filters what its
compositor treats as not a user workspace, so consumers drop the
`ID <= 0` checks.

## 2. The base contract

```go
type Handler interface {
    OnSnapshot(Snapshot)              // after a change; from the backend's goroutine
    OnConnection(up bool, err error)  // the control channel going up or down
    OnCapabilities(Capabilities)      // when a probe changes a state (a plugin loaded, a Lua probe)
}

type Backend interface {
    Name() string                     // "hyprland", "niri", "wayfire"
    Run(ctx context.Context, h Handler) error // connects, delivers the full state, follows changes, reconnects with backoff
    Refresh()                         // ask for a re-read; consumers never poll otherwise
    Capabilities() Capabilities

    FocusWindow(ctx context.Context, id string) error
    CloseWindow(ctx context.Context, id string) error
    FocusWorkspace(ctx context.Context, id string) error
    MoveWindow(ctx context.Context, id, workspace string, follow bool) error
}
```

Semantics:

- `Run` delivers the first `OnSnapshot` before `OnConnection(true)`. On
  Hyprland that is the four queries after the event socket is up; on niri the
  event stream's first events; on Wayfire the lists then the watch. A change
  racing the initial read is seen as an event and causes another snapshot.
- Snapshots are whole and consistent as far as the compositor allows; a
  backend coalesces bursts (Hyprland's 15 ms debounce) and publishes only
  when something changed. Consumers never see raw events.
- A disconnect keeps the last snapshot in the consumers (as today) and
  `OnConnection(false, err)`; the backend retries from 250 ms doubling to
  5 s and resets after a session longer than 5 s. A reconnect rebuilds the
  state and re-publishes it.
- An action returns the backend's error, wrapped, or `nil` once the
  compositor accepted it. Acceptance is not completion: the resulting state
  arrives as a snapshot, and consumers that show a projected result (the
  Overview's moves) settle it on the next snapshot, as they do now.
  `MoveWindow(follow=false)` returns `ErrUnsupported` where the compositor
  cannot keep focus; callers check `CapMoveWithoutFollow` first.
- `Refresh` is a request, not a promise; the backend may already have one
  pending.

## 3. Capabilities and their states

```go
type Capability string
type State int // Supported, Unavailable, Unsupported
type Capabilities map[Capability]State

var ErrUnsupported = errors.New("compositor: not supported")
var ErrUnavailable = errors.New("compositor: not available now")
```

A capability absent from the map is `Unsupported`. `Unavailable` is a probe
that failed or a part not answering while the channel is up (a Wayfire
plugin missing from `list-methods`, Hyprland's Lua probe failing, a
reconnect in progress); it may become `Supported` later and
`OnCapabilities` says so. Consumers decide what to show from the state, by
the rules in the capability comparison: Unsupported hides the control or
names what is missing on a settings page; Unavailable keeps the control and
shows the error where the control shows errors.

The capabilities, each the name of an optional interface or a data field:

| Capability | What it enables | Hyprland | niri (from docs) |
| --- | --- | --- | --- |
| `CapGeometry` | `Window.Geometry` present: Overview card layout, picker, dictation hint | yes | no: floating windows only (Wayfire: yes) |
| `CapMoveWithoutFollow` | Overview drag of a window | yes (Lua) | yes |
| `CapWorkspaces` (`WorkspaceMover`) | New card, drag onto another output | yes | yes |
| `CapQuietFocus` (`QuietFocuser`) | dock click without pointer warp | yes | n/a: never warps, so `FocusWindow` is quiet and the backend reports Supported |
| `CapPreviews` (`Previewer`) | live Overview previews | yes | no |
| `CapKeystrokes` (`Keystroker`) | paste from history, dictation insert | yes | yes, by virtual keyboard after focus (two steps) |
| `CapBinds` (`Binder`) | Settings → Shortcuts | yes | no (config only) |
| `CapKeyObserver` (`KeyObserver`) | Super tap, Super+Tab, number slots | yes (Lua) | no |
| `CapDisplays` (`Displayer`) | Settings → Displays, lid rules | yes | not wired (Wayfire: yes, without the lid rule) |
| `CapVirtualOutput` (`VirtualOutputs`) | remote desktop without a monitor | yes | no |
| `CapPointer` (`Pointer`) | read and move the cursor | yes | no |
| `CapPlacement` (`Placer`) | bring here, place beside, tile (apps) | yes | partial (floating only) |
| `CapModes` (`Moder`) | a key mode that swallows keys (recording, dictation) | yes | no |
| `CapRules` | compositor styling the sample config installs | yes | layer rules, no blur |
| `CapKeyboard`, `CapTouchpad` | inua's keyboard and touchpad settings reach the compositor (`Settings`, `Configurer`) | yes | yes, once `config.kdl` includes `inua.kdl` (Wayfire: yes, over IPC) |
| `CapOnDemandFocus` | a layer surface asking for the keyboard on demand is focused as it appears; without it the overview asks exclusively | yes | no (Wayfire: yes) |
| `CapArrange` (`Arranger`) | the launcher's Toggle Fullscreen and Center Window | yes | yes (Wayfire: yes) |
| `CapFloating` (`Floater`) | the launcher's Toggle Floating; not offered without it | yes | yes (Wayfire: no, its windows all float) |

## 4. Optional interfaces

Each is a Go interface a backend may implement; consumers assert it and read
the capability's state. Signatures are what the current callers pass.

```go
type WorkspaceMover interface {
    // NewWorkspace names the workspace a window moved to "new" on output
    // would land on, skipping ids unavailable says are taken (reserved,
    // in flight). Hyprland: the lowest free positive integer; niri: the
    // output's trailing empty workspace; Wayfire: an empty grid cell.
    NewWorkspace(snap Snapshot, output string, unavailable func(id string) bool) (Workspace, bool)
    MoveWorkspace(ctx context.Context, id, output string) error
}

type QuietFocuser interface {
    FocusWindowQuietly(ctx context.Context, id string) error // no pointer warp
}

type Keystroker interface {
    SendKey(ctx context.Context, window, mods, key string) error // "CTRL SHIFT", "V"; focuses first where needed
}

type Previewer interface {
    // Previews returns a source of window images by id; the capture
    // package's stream today, keyed by Hyprland addresses.
    Previews() capture.Source
}

type Pointer interface {
    CursorPos(ctx context.Context) (x, y int, err error)
    MoveCursor(ctx context.Context, x, y int) error
}

type Moder interface {
    EnterMode(ctx context.Context, name string) error // "" leaves
}

type Placer interface {
    BringHere(ctx context.Context, pid int) error
    WindowOf(ctx context.Context, pid int) (Rect, error)
    PlaceWindow(ctx context.Context, pid int, r Rect) error
    RoomRight(ctx context.Context, pid int) (Rect, int, error)
    Tile(ctx context.Context, pid int) (bool, error)
}

type KeyObserver interface {
    // Observe installs the compositor-side listener for Super taps,
    // Super+Tab and the number slots; events come back on h.
    Observe(ctx context.Context, h func(KeyEvent)) (stop func(), err error)
}

type Binder interface {
    Binds(ctx context.Context) ([]Bind, error)
    Apply(ctx context.Context, cfg config.Config) (changed bool, err error) // hyprconf.Apply today
}

type Displayer interface {
    Outputs(ctx context.Context) ([]display.Output, error)
    ApplyDisplays(ctx context.Context, cfg config.Config) error
}

type VirtualOutputs interface {
    SyncVirtualScreen(ctx context.Context, mode string) (changed bool, err error)
}
```

`Previewer`, `KeyObserver`, `Binder`, `Displayer` and `VirtualOutputs` wrap
code that exists in `internal/hypr`, `internal/capture/hyprland` and
`internal/hyprconf` and stay Hyprland-only until a second backend has them;
the contract only gives the UI one place to ask and one state to show.

## 5. Discovery and the session

```go
func Register(name string, open func() (Backend, error))
func Detect() (Backend, error) // ErrNoCompositor when nothing registered matches the environment
```

A backend registers in its package `init` and `Detect` tries them in
registration order on the environment: `HYPRLAND_INSTANCE_SIGNATURE` for
Hyprland, `NIRI_SOCKET` for niri, `WAYFIRE_SOCKET` for Wayfire. The programs
that need a compositor import the backends they ship with
(`_ "…/internal/hypr"`). Light apps that only bring a window here call
`Detect` and the `Placer`; without one they do nothing, as today when there
is no Hyprland.

The session script gains a compositor argument (`inua-session [hyprland|niri]`)
with the Hyprland path unchanged; the niri path, its sample config and what
`inua init` writes for it are #64's.

## 6. Error handling, states, UI

- Every action's error reaches the control that asked. The Launcher keeps
  showing the window list with the message in its line; the Dock and the
  Overview log and keep their state, as the audit records; nothing new is
  invented here, the contract only forbids a backend from swallowing an
  error.
- A capability's state is data on the snapshot's side, not derived from the
  compositor's name anywhere in `internal/ui` or `internal/shell`. The one
  allowed name check is the session script.
- A backend never simulates: an action it cannot do returns
  `ErrUnsupported`; a value it does not know is the zero value or nil
  (`Geometry`), never an estimate.

## 7. Migration plan (#63)

Each step compiles, passes `make test` and changes no behaviour on
Hyprland; each is one commit or a few.

1. **Types and contract.** Add `internal/compositor` with sections 1 to 4
   and a `compositortest` fake backend (snapshots pushed by the test,
   actions recorded). No consumer yet.
2. **Hyprland implements it.** `hypr.BuildSnapshot` produces
   `compositor.Snapshot`; `hypr.Store` becomes the backend's `Run`; the
   Lua-or-classic choice (`dispatchEither`) moves into `hypr.Backend` with
   the probe reporting `CapKeyObserver`, `CapMoveWithoutFollow` and
   `CapQuietFocus`; `Keystroker`, `Pointer`, `Moder`, `Placer`,
   `VirtualOutputs` wrap the existing methods. `cmd/inua` keeps its debug
   printer on the new types. Existing `internal/hypr` tests keep passing
   with the type change.
3. **Shell state on the contract.** `internal/shell` (bar, dock, launcher,
   overview, screenshot, shotpick) and their tests use the shared types;
   workspace ids become strings; `Overview.ReserveWorkspace` keeps its
   reservation bookkeeping and asks `WorkspaceMover.NewWorkspace` for the
   id; the saved Overview history reads old integer ids as strings.
4. **Shell UI on the contract.** `shellUI` holds a `compositor.Backend`
   instead of `*hypr.Client`; `focusWindow`, `switchWorkspace`, Dock's
   quiet focus, the Overview's activate, move, route and New, the cursor,
   submaps, dictation and clipboard paste go through the interfaces;
   controls check the capability state. The Hyprland-only pages
   (Displays, Shortcuts, input options, screenshot's `j/clients`) take the
   backend's `Binder`/`Displayer` when present, else show the "not
   available" line.
5. **Other consumers.** `internal/desk` takes a `Backend` for its window
   and workspace tools (its `HyprRequest`/`HyprDispatch` stay for the
   Hyprland-only tools, now obtained through the backend's
   `hypr`-specific method when the backend is Hyprland); `appui`,
   `lightui`, `mailui`, `term/desktop` use `Detect` and `Placer`;
   `remotedesk` uses `VirtualOutputs`; `clipboard` and `cmd/inua fetch`
   use the backend.
6. **Regression coverage.** The existing tests move with the types. New:
   `compositor` contract tests run against the fake and against `hypr`
   with the fake socket of `hypr_test.go` (snapshot on connect, change on
   event, reconnect, action errors); shell tests use the fake backend. The
   nested-session checks before merging: bar workspaces and titles on two
   monitors; dock click and quiet focus; launcher window focus and cancel;
   Overview open, select, drag to New, drag across monitors, Super tap and
   number slots; screenshot and picker; dictation paste; lock; display
   settings; remote desktop virtual screen.

### What #63 did

Steps 1 to 5 are in; what they left where it was, on purpose:

- `internal/shell`'s screenshot and picker models still parse Hyprland's
  `j/clients` (`ShotWindows`, `PickGroups`): they need stacking order and
  special workspaces the snapshot does not carry, so the UI asks the
  Hyprland backend for the list and hides the window picker without it.
- The Hyprland-only pages and paths take the backend's client directly
  through `shellUI.hyprClient`: Displays, Shortcuts, keyboard options,
  the Lua key listeners, the mouse-binding guard, the virtual screen, the
  dictation card's placement, floating-window placement in the Overview.
  Each checks for Hyprland and does nothing without it; the capability
  states for them (`CapBinds`, `CapDisplays`, `CapKeyObserver`,
  `CapVirtualOutput`) are reported but not yet read by those pages.
- `internal/apps/remotedesk` and `internal/term/desktop` find a Hyprland
  instance by its lock file (another session's) and keep `internal/hypr`.
- The Overview is shown when the compositor is there and, on Hyprland,
  runs Lua (`overviewAvailable`); its New card needs `CapWorkspaces`, its
  drag `CapMoveWithoutFollow`.
- The saved Overview history now holds workspace ids as strings; an old
  file with Hyprland's numbers is read as their text.
- The desk tools' `list_workspaces` ids are strings now (Hyprland's numbers
  as text), and `move_window` and `switch_workspace` take a name or an id.

## 8. Validation with niri (#64)

`internal/niri`: the JSON-lines client, the event stream into snapshots,
the base actions and `WorkspaceMover` (`NewWorkspace` = the output's
trailing empty workspace; `MoveWorkspace` = `MoveWorkspaceToMonitor` by
name), the capability map from section 3, contract tests on a fake niri
socket, a session script path, and a sample `config.kdl` with inua's
binds and layer rules. The first real session on niri runs the baseline's
feature list and the #61 tables, and fills the "observed" columns; the
contract changes with what that finds, which is the point of a second
implementation.

### What #64 did

- `internal/niri` is in, with `Snapshotter`, `OutputFocuser`,
  `WorkspaceMover`, `QuietFocuser` (niri does not warp unless configured)
  and a partial `Placer` (`BringHere`, `WindowOf`, `Tile`; `PlaceWindow`
  and `RoomRight` return `ErrUnsupported`, so the settings panel pins
  without growing the window). `Keystroker` is not implemented: clipboard
  paste falls back to `wtype`, dictation says it cannot paste. The window
  capture, binds, key observer, pointer, displays and virtual output
  capabilities are Unsupported, as the comparison found.
- `internal/compositors` links both backends; `inua-session niri` starts
  `niri --session` after checking the config starts the shell;
  `config/niri/config.kdl` is the sample; the login screen gets "Inua on
  niri" where niri is installed.
- Outputs have no event in niri's stream: the backend re-reads them when
  the workspaces change and on `Refresh`.
- **Run on niri 26.04** (headless, nested; see the experiments document):
  the backend against the real IPC, then the shell with its bar, dock,
  launcher, overview, lock and settings. What that changed: geometry is not
  a niri capability, the overview asks for the keyboard exclusively where
  on-demand focus waits for a click, the preview capture and
  `hypr/inua.conf` are Hyprland's only, the sample config turns the hot
  corners off.

### Wayfire (#68)

`internal/wayfire` is the second non-Hyprland backend: the plugin IPC
(`ipc`, `ipc-rules`, `vswitch`, `wsets`, `wm-actions`, `command`), the
state read again after each event as the Hyprland store does,
capabilities from `list-methods` (a missing plugin makes its capability
Unavailable, and it comes back when the plugin does), the grid of each
output's workspace set as workspaces with ids `SET:X,Y`, a view's cell
derived from its place, `NewWorkspace` as the first empty cell, the
placer by `configure-view`, fullscreen by `wm-actions/set-fullscreen`
and centring by `configure-view` (`Arranger`). `inua-session wayfire`,
`config/wayfire/wayfire.ini` and the one "Inua" entry are the session
side.

It runs on Wayfire 0.11 (Arch) and on Wayfire 0.9 (Debian 13), which
differ: 0.9 has no `vswitch/send-view`, so there a view goes to another
cell by `configure-view` with its geometry moved by whole outputs; it
cannot list its options, so its own shortcuts are not shown; it writes a
layer surface's `wset-index` as 2^64-1; its socket is in `/tmp`, which
`WAYFIRE_SOCKET` names either way. Debian 13's gtk4-layer-shell is 1.0,
which has no session lock: the shell starts and says that locking is
not there.

Displays: Wayfire's IPC lists an output's place but not its modes, so
the backend reads the displays through wlr-output-management
(`internal/wlroutput`, the protocol spoken directly) and sets them as the
options of each output's section (`output:NAME/mode`, `/scale`,
`/position`; `off` and `mirror NAME` are modes) by
`wayfire/set-config-options`. What an option was before inua first set
it is kept in the runtime directory and put back when the rule goes.
Wayfire lays the outputs with no place of their own out again to the
right of the placed ones, so when one is placed the others are pinned
where they are; "beside the others" (`auto-left`, …) is computed from
the layout once the modes and scales took. A headless output is left
out of the list unless the session has no other.

Not wired: keystrokes to one window (a paste is typed into the focused
window by `wtype`, after the panel has given the keyboard back), live
previews (Wayfire's image capture has an output source but none for a
window), the key observer, the virtual output for remote desktop
(`wayfire/create-headless-output` is there), and turning the built-in
display off with the lid. It shares nothing with the abstraction in
PR #67, which predates the contract and which this replaces; that PR's
Wayfire knowledge (the plugin list, the paired `binding_`/`command_`
options, Debian's Wayfire 0.9) is in the sample config and in the
checks above.

### Settings through the backends

inua's settings are the same on every compositor and each backend applies
them its own way:

```go
type Settings struct {
    KbOptions     *string   // XKB options; nil leaves the compositor's
    TouchpadSpeed *float64
    TouchpadAccel string
    Binds         []KeyBind // {Keys "SUPER+SHIFT+S", Command, Replaces}
}

type Configurer interface {
    ApplySettings(ctx context.Context, s Settings) error
    Binds(ctx context.Context) ([]Bind, error) // the compositor's own shortcuts
    Source() string                            // where they go, to show
    Hint(c Capability) string                  // what to do about an Unavailable one
}

type Exiter interface{ Exit(ctx context.Context) error } // log out
```

`internal/compsettings` makes `Settings` from `config.toml`. Keys keep the
form they always had (`SUPER+SHIFT+S`, an XKB key name); each backend
writes them as its compositor names them.

| Backend | Keyboard and touchpad | Shortcuts | The compositor's own shortcuts | Persistence |
| --- | --- | --- | --- | --- |
| Hyprland | `hypr/inua.conf` or `inua.lua` (`hyprconf`, unchanged) | binds and unbinds in the same file | `j/binds` | the file, sourced by the user's config |
| niri | `inua.kdl` beside `config.kdl`, which includes it last; `xkb` is written with the main file's layout, since niri replaces that block as a whole | `binds {}` in the same file; a later bind of a key wins | parsed from `config.kdl` | the file; niri loads it again by itself |
| Wayfire | `wayfire/set-config-options` (`input/xkb_options`, `input/touchpad_cursor_speed`, `input/touchpad_accel_profile`) | `command/register-binding`, the ids kept in the runtime directory so the next apply takes them off first | the `command` plugin's `binding_*`/`command_*` options | none in `wayfire.ini`: applied again each time the shell starts |

Known limits: niri cannot take a key away from `config.kdl`, and Wayfire
cannot take one away from `wayfire.ini` while it runs, so a shortcut
moved in inua keeps its old key in the compositor's file until the user
removes it there. Display settings (`Settings.Displays`, from
`[displays]`) are applied by Hyprland's file and by Wayfire's backend;
niri's are not wired and its Displays page says so.

### The settings pages

Every option that depends on the compositor follows a capability. Where
the compositor takes it, the option works as on Hyprland. Where it does
not, the option stays visible, disabled and dimmed, with the reason and
what to do in the page's notice: `shellUI.gatePage` for Keyboard
(`CapKeyboard`), Mouse & Touch (`CapTouchpad`) and Shortcuts (`CapBinds`),
"Not available" in place of the outputs on Displays. The capabilities are
read again when a page is shown, so a line added to niri's config or a
plugin loaded in Wayfire brings the page alive without a restart. The
words that name the compositor (the SOURCE line, "Use niri's own", the
compositor's own shortcuts) are set from the backend.

### Choosing the compositor

Settings → Compositor lists Hyprland, niri and Wayfire: the one running,
which are installed, the choice for the next login. One that is not
installed is disabled with the reason under it. The choice is `[session]
compositor` in `config.toml`; `internal/session` and `inua session
compositor` answer the session script, which is why one Inua entry in the
login manager serves every compositor and choosing needs no administrator.
A first choice of niri or Wayfire writes its sample config when the user
has none. A chosen compositor that is missing, or whose config does not
start `inua-shell`, is passed over for Hyprland at login and the shell
says why in a notification. The page also shows the login manager and
whether the installed session script is one that reads the choice, and
lists what the running compositor does not give inua.
