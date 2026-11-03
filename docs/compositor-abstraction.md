# Compositor abstraction: current state and refactor plan

Inua's compositor-neutral interface exists, but the shell is not yet built on
it. The Wayfire port works through adapters. This document records that
boundary and the refactor needed before adding another compositor.

## Current layering

| Layer | State |
| --- | --- |
| `internal/compositor` | Defines `Client`, `Snapshot`, `Toplevel`, `Workspace`, `Monitor`, `Capabilities` and `Event`. This is the intended seam. |
| `internal/wayfire` | `Client` (IPC actions, live `Windows` snapshots) and `Wayland` (foreign-toplevel) implement `compositor.Client`. |
| `internal/hypr` | Keeps its own `hypr.Snapshot`, `hypr.Toplevel`, `hypr.Monitor` and `hypr.Workspace`; it does not expose the neutral types. |
| `internal/shell` | `Dock`, the launcher's window mode and `Overview` take `hypr.Snapshot` / `hypr.Toplevel`. |
| `internal/ui` | `shellUI.client` is `*hypr.Client`; `shellUI.snap` is `hypr.Snapshot`; Wayfire state is converted by `wayfireSnapshot`. Actions branch on `s.wayfire != nil` or `s.hyprLua`. |
| Settings and shortcuts | Hyprland writers (`hyprconf`) and Hyprland dispatchers remain the default. Wayfire has branch-specific writers and commands. |

The Wayfire path currently works through two adapters:

1. `wayfire.Client` implements the neutral action interface (focus, close,
   move, workspace, Windows snapshot).
2. `wayfireSnapshot` maps `compositor.Snapshot` into `hypr.Snapshot` so the
   existing Dock, launcher and overview can consume it.

That was deliberate for bringing Wayfire up quickly, but it means a third
compositor would still need a third `xxxSnapshot` conversion and more UI
branches.

## Target architecture

- `internal/shell` and `internal/ui` depend on `compositor.Snapshot` and
  `compositor.Toplevel`, never on `hypr` types.
- `shellUI` holds one `compositor.Client` plus one `compositor.Snapshot`.
  Hyprland is adapted once by a `hyprSnapshot()` conversion; Wayfire supplies
  its native snapshot.
- Window focus, close, move, workspace switching, capture and settings writes
  go through `compositor.Client` and `Capabilities`. UI code asks
  `Capabilities` instead of testing `s.wayfire` / `s.hyprLua`.
- Settings have a compositor-neutral model. Each backend writes its own file
  (`hypr/inua.conf` for Hyprland, `wayfire.ini` sections for Wayfire), and the
  settings view hides or disables unsupported controls with the capability
  reason.
- Shortcut recording writes the backend's binding format; the key labels stay
  in Inua's model.

## Refactor checklist

1. Move `internal/shell` APIs from `hypr.Snapshot`/`hypr.Toplevel` to
   `compositor` types. Keep `hyprSnapshot()` as the only Hyprland adapter.
2. Change `shellUI.snap` to `compositor.Snapshot`; replace `s.client` with
   `s.compositor compositor.Client`.
3. Replace `dispatchEither`, `focusQuietly`, overview Lua and screenshot
   `j/clients` calls with client methods and capability checks.
4. Give the overview a per-window thumbnail service supplied by the
   compositor backend (Wayfire: output capture plus crop; Hyprland: its
   capture protocol), rather than Hyprland-specific capture only.
5. Move settings writes behind a backend interface; remove per-control
   `if Wayfire` branches from `internal/ui`.
6. Remove `wayfireSnapshot` once the neutral snapshot is the only state type.
7. Add contract tests with a fake `compositor.Client`; run the same shell
   tests against fake Hyprland and fake Wayfire snapshots.

## Acceptance

- No package outside `internal/hypr` imports `internal/hypr` except the
  adapter.
- The shell builds and passes its tests with a fake compositor client.
- Dock, launcher window mode, overview, focus/close/move, settings and
  shortcuts run without `if Wayfire` / `if Hyprland` branches in views.
- Adding a compositor means implementing `compositor.Client` and a settings
  writer, not editing Dock/overview/settings logic.

## Non-goals for the first pass

- A plugin system or runtime loading of compositor backends.
- Replacing compositor-specific overview capture with a portable protocol
  before the neutral state migration is done.
- Promising identical features on every compositor; capabilities and
  reasons still decide what the UI offers.
