---
source: pr #87
branch: factory/FT86-appmenu-all-apps
round: 1
---
# feat(appui): every inua app publishes a menu for the bar

## factory · 2026-11-08 19:39
Imported GitHub PR #87 directly into review at the owner's request: “好 85 87 加到 review”.
Source: https://github.com/lyricat/inua/pull/87
Upstream head: 12c3502dda6b8ca17cb29b021ca5ffe8a1329340
Factory branch preserves the upstream head without changes.

Publishes app-specific menus for VM, Mail, Viewer, Disk Usage, Games, Remote Desktop, Connect and Assistant using appui.NewMenuBar and existing actions. Shares the Assistant newConversation helper between button and menu.

The author's verification claims have not been independently repeated by the factory.
No code review, tests, or visual UI checks were run during import. Awaiting review; not approved for merge.

## factory · review · 2026-11-08 20:27
head: d86bf68b3adf53dae0baa1f2c877212ce85cd992
verdict: fix
fix:
- [P1] internal/viewerui/menubar.go:48-49: Previous/Next File bypasses the unsaved-edit guard. Example: annotate an image in a multi-image directory, then choose Next File; action -> show -> leave -> endEdit discards the editor without leaveEdit's warning. Disable navigation during editing or run the existing unsaved-change confirmation first.
notes:
- [P2] internal/viewerui/menubar.go:40: Set as Wallpaper is available for PDF/Markdown/text; unlike the image-only toolbar, the callback writes these unsupported paths into Background.Image and reloads. Guard on viewer.Image.
- [P2] internal/diskui/menubar.go:38 and internal/viewerui/menubar.go:39: New raw menu labels (e.g. Copy Cleanup Command, Stop Scanning, Copy Image, Zoom In) are absent from extracted catalogs. Non-English sessions show English labels. Mark them with i18n.M and regenerate catalogs.
- [P2] internal/remoteui/menubar.go:18: With no remote machines and This Computer selected, Connect treats the page ID sharing as a hostname and opens a connection attempt. Require a real remote-machine selection and preserve existing availability/pairing guards.

Merged origin/master (cca5859) into the item branch, resolved seven generated catalog conflicts preserving translations, and pushed merge d86bf68. Review scope: origin/master...HEAD. No protected paths changed in the item diff.
Validation: manager make test passed; Codex independently passed make test with throwaway HOME, isolated runtime and D-Bus, Go 1.26.1, GOPROXY=off, and GOFLAGS='-mod=readonly -count=1': 18 Python tests, go vet, and race tests (115 Go packages). An initial run had four Hyprland config validation failures without isolated XDG_RUNTIME_DIR; the corrected full rerun passed. make i18n was stable; git diff --check passed. No installs. Interaction findings verified by code paths; UI and live desktop menu interactions were not visually tested. Item remains in review/ for the owner's decision.
