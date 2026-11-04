# Compositor experiments (#61)

Disposable prototypes that talk to niri and Wayfire directly, one vertical
slice each: list windows and workspaces, watch changes, focus a window and
switch workspace, and see what happens on disconnect. They are a separate Go
module so the shell's `go build ./...`, `go vet ./...` and `go test ./...`
never see them; they import nothing from the shell and the shell imports
nothing from them. They are evidence for
[docs/compositor-experiments.md](../../docs/compositor-experiments.md), not
support for either compositor, and they are deleted when #62 is decided.

Run from this directory, inside a session of the compositor in question:

```sh
go run ./niri                     # dump, then follow events until Ctrl-C
go run ./niri -focus-window ID    # focus a window by its id
go run ./niri -focus-workspace N  # by index (or -focus-workspace-name NAME)
go run ./niri -move-window ID -to N   # move a window to workspace index N without focusing it
go run ./niri -reconnect          # keep reconnecting with backoff after the socket closes

go run ./wayfire                  # dump, then follow events until Ctrl-C
go run ./wayfire -focus-view ID
go run ./wayfire -set-workspace X,Y [-output OUTPUT-ID]
go run ./wayfire -send-view ID -to X,Y
go run ./wayfire -reconnect
```

Each program prints what it sends and receives, with a monotonic timestamp
per line, so an event's order against a request's reply is in the log. Save
the log and the compositor version beside it:

```sh
niri --version; go run ./niri 2>&1 | tee niri-$(date +%Y%m%d-%H%M).log
wayfire --version; go run ./wayfire 2>&1 | tee wayfire-$(date +%Y%m%d-%H%M).log
```

Neither compositor is installed where this was written, so the programs were
built and vetted but not run against a compositor; the report says so per
observation. To run them nested under Hyprland, start the compositor as a
Wayland client (both pick the Wayland backend when `WAYLAND_DISPLAY` is set),
open a terminal inside it, and run the program there so it sees that
compositor's socket.
