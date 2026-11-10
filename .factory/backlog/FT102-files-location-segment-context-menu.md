# Files: context menus for location-bar folder segments

## factory · 2026-11-10 16:26
Registered from the owner's Files UI request. Backlog only; no build started.

User request: "allow using context menu on the folder segment at the location bar."

- Each folder segment in the location bar supports a context menu targeting that segment's folder, not the currently selected file or a different directory.
- Reuse applicable existing folder actions and their permission/error handling. Opening the context menu must not navigate as a side effect.
- Check root, ancestors, the current folder, paths containing spaces/non-ASCII characters, and unavailable folders. Preserve normal click-to-navigate behavior.
