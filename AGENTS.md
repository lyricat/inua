# Repository Guidelines

## Project Structure

inua is a Hyprland desktop shell written in Go and GTK4. Read `README.md` and `docs/architecture.md` first.

- `cmd/`: entry points for the shell, the command line, the light apps, the games, the assistant and the keyring service.
- `internal/shell`, `internal/apps`, `internal/games`: state and logic without GTK; their views are in `internal/ui`, `internal/lightui` (run by `internal/appui`) and `internal/gamesui`.
- `internal/hypr`, `internal/system` and others: Hyprland IPC, D-Bus and system services.
- `ui/`: Blueprint layouts, the generated `.ui` files, CSS, themes and icons.
- `config/`: sample configuration and desktop entries; `scripts/`: build and session tools; `website/`: the static website.
- `design/ui.md`: the look and the interaction rules (tokens, type, marks, controls, where errors show); follow it for UI changes. Controls are shared CSS classes (`outlined`, `suggested-action`, `plain`, `tool`, `segmented`, `menu-item`, `mark`, `kicker`) sized by `small`, `tight` or `large`, and pieces of several widgets (sections, menus) come from `internal/uikit`: use them, never restate a control's look or size in a window's rules.
- Tests live next to the Go sources, named `*_test.go`.

## Build and Development Commands

Use Go 1.26 or later; running the UI needs Hyprland, GTK4 and gtk4-layer-shell.

- `make build`: builds the programs with `CGO_ENABLED=0` into `bin/`.
- `make test`: runs the Python session script tests, `go vet` and the Go tests with the race detector; needs a C compiler.
- `go test ./internal/shell`: checks one package only.
- `make generate`: compiles `.blp` into `.ui` with `blueprint-compiler`; after changing a layout, commit both.
- `make i18n`: gathers the translatable text into `po/inua.pot` and updates the catalogs in `internal/i18n/locales`.
- `make nested`: runs in a nested Hyprland window; `make run`: runs in the current Hyprland session.
- `bin/inua-shell -ui-dir ui`: reads layouts and styles from disk.

## Code Style and Architecture

Format Go with `gofmt` and follow standard naming: exported identifiers start with a capital letter, package names are short and lower case. Blueprint and CSS use two-space indentation, and colours refer to theme tokens.

Services and state must not import GTK. GTK calls run on the main thread; background results come back through the existing `gtkx.Post` or `gtkx.Call`. Windows, workspaces and focus follow the compositor's state. Implement the needed behaviour directly; do not add wrappers that only rename, or duplicate state.

Text the user reads is translatable: in Go through `internal/i18n` (`i18n.T`, `i18n.Tf`, `i18n.Nf`, `i18n.M`), in Blueprint as `_("…")`; run `make i18n` and commit `po/inua.pot` and the catalogs with the change. See `docs/i18n.md`. Run `make i18n` before every commit that touches a `.go` or `.blp` file, even one that adds no text: the catalogs record each string's file and line, so moving code shifts them, and CI fails when the committed catalogs differ from what `make i18n` writes.

Icons: when an icon is needed, use the SVG from Phosphor Icons (https://phosphoricons.com, MIT, the regular weight) by default. The shell's own icons are listed in `scripts/phosphor-icons.sh`, which copies them into `ui/icons/` under the freedesktop name the shell asks for (`NAME-symbolic.svg`); add a line to its list, run it, and commit the SVG. Use another source only when Phosphor has nothing fitting, and say so.

## Start-up Time

A light app's window should be up with its content about a second after launch. `docs/performance.md` has where the time goes, what was done and how to measure it; for a new app or view:

- Run through `appui.Run` (or `appui.Register`): it binds GTK lazily, sets the icon theme before the application starts, parses the style while GTK reads the desktop's icon theme, and turns accessibility off when nothing uses it. Do not set `gtk-icon-theme-name` or load style sheets again yourself.
- Before the first frame, only build the window: files, D-Bus calls, processes and network go on goroutines, their results back through `gtkx.Post`. Work that can begin while GTK starts (reading a large icon theme) goes in `Prepare` (`appui.Preparer`).
- Show real content in the first frame or soon after it, never placeholder numbers (System Monitor's first sample came from readings microseconds apart).
- Long lists use `GtkListView`/`GtkGridView` with a factory; fill an empty view a screenful first and the rest in parts (`filesui` `refill`), and change a model with one `Splice` per change, not item by item.
- Desktop-theme icons (files, other apps) come from a `GtkIconTheme` of the desktop's alone, as Files does; everything else from the Phosphor set.
- Do not write files at every start when their content has not changed (`style.IconTheme` keeps a sum).
- Check a start-up change by measuring it: temporary marks at the first frame and when the content shows, the old and the new build in turns in a nested session, medians of several launches; keep the marks out of commits.

## Testing

Use the Go standard library's `testing`; name test functions `TestXxx`. Add behaviour regression tests for logic changes, using temporary directories, fake services and the private D-Bus of `internal/dbustest`; tests that need `dbus-daemon` are skipped without it. There is no coverage threshold.

Documentation, wording, simple logging, and visual or interaction changes to the UI do not get new automated tests. Check UI changes by hand in a nested session and note the steps. Do not verify an implementation by connecting to a database or starting a PostgreSQL container.

**Never test in the user's own session.** The session someone is working in is not a test bed: no restarting its shell, no moving its workspaces, no stopping or restarting its applications, and no reading its windows to check a change. A test that needs a running desktop uses one of its own - a VM, a nested compositor, or a machine set aside for it - and the user is told its address and how to watch it. Killing or restarting an application in the user's session loses their tabs and their work, and "I will put it back" does not restore it. When no separate desktop is available, ask before touching theirs, and say exactly what will happen.

Two more things follow from that: a change is not verified until it has been exercised somewhere, and that somewhere is never the user's screen; and the checks that read the shell's own state (`bar state`, `bar appmenu`, `bar hover` over the control socket) are how a UI change is verified, so a test does not depend on a pointer, on focus, or on a screenshot.

## Commits and Pull Requests

History uses both `fix(dock): ...`, `feat: ...`, `docs: ...` and "Component: what changed". A commit title says briefly what behaviour changed; keep each commit to one topic.

A PR explains the problem, the behaviour after the change and how it was verified; link the related issue if there is one. Attach screenshots for UI changes, and note any environment the change was not run in. When configuration or command behaviour changes, update the samples and documentation with it.

## Configuration and Operational Boundaries

Do not commit secrets, local user names or local absolute paths. Do not run binaries downloaded from the internet without explicit permission. Install and session-setup commands write to user or system directories; do not run them as ordinary build checks.
