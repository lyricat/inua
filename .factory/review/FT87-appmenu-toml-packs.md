---
source: pr #88
branch: factory/FT87-appmenu-toml-packs
round: 1
---
# feat(appmenu): per-application menus from TOML packs

## factory · 2026-11-08 22:42
Imported GitHub PR #88 directly into review at the owner's request: “那把 #88 纳入 review”.
Source: https://github.com/lyricat/inua/pull/88
Upstream head: 8912c4630dc29b1a8722c3df7a5495c3fd00961d
Factory branch preserves the upstream head without changes.

Adds per-application TOML menu packs loaded from embedded files, XDG data directories and user configuration. Matches window class and desktop ID; supports shortcuts, key sequences, shell actions, submenus and disabled items. Provides fallback bar menus for applications that do not publish menus, shortcut keycap labels, shipped application packs and docs/appmenus.md describing user overrides.

Review should check pack parsing and precedence, application matching, fallback behavior versus published menus, shortcut and sequence delivery, disabled items and the compatibility of existing accessibility exceptions.

The author reports go build ./..., go vet ./internal/..., appmenu/UI package tests, and live Zed/Chromium checks; these claims have not been independently repeated by the factory.
No code review, tests, or visual UI checks were run during import. Awaiting review; not approved for merge.

## factory · review · 2026-11-08 22:58

head: 8912c4630dc29b1a8722c3df7a5495c3fd00961d
verdict: fix
fix:
- [P1] internal/ui/appmenu_packs.go:55: Actions target the first window sharing the PID rather than the focused window. Example: two Chromium windows share a PID, focus is second, but CTRL T is sent to first; close-window uses the same incorrect target. Bind, cache and validate actions by focused window ID.
notes:
- [P2] internal/ui/appmenu_packs.go:260: Background actions read s.snap while the UI replaces it. Example: quit-app iterates Windows concurrently with a snapshot update; an overlay race test confirmed the race. Capture required window data on the UI thread before launching the goroutine; copy-title/class need the same treatment.
- [P2] internal/ui/appmenu.go:159: Fallback appears while published-menu discovery is pending. Example: first focus of a Chromium window with a published menu shows actionable pack entries before the async query returns, bypassing the real menu's disabled states. Distinguish pending from confirmed absent.
- [P2] internal/ui/appmenu.go:160: Empty packs omit the documented generic fallback. Example: bundled code/firefox packs produce zero sections while genericAppSections produces four; the caller renders the empty result. Use generic sections when no published menu or pack content exists.
- [P2] share/inua/menus/kitty.toml:78: Four punctuation keys are invalid XKB keysym names. Example: Move Tab Forward uses a punctuation name resolving to NoSymbol. Replace '.', ',', ']', '[' with period, comma, bracketright, bracketleft.
- [P2] docs/appmenus.md:306: Accessibility flags are documented as implemented but have no runtime consumers. Example: changing VS Code skip_a11y does not affect discovery; SkipA11y/NeverPress are only parsed/stored. Document the actual supported behavior.
- [P3] docs/appmenus.md:293: make install does not copy TOML packs as documented (also line 77). Example: installing does not create the advertised editable pack files in the data directory. Correct the installation/override instructions.

Validation: Codex reviewed all 22 changed files in origin/master...HEAD. make test passed with isolated HOME: 18 Python tests, go vet, and full Go race tests. Additional temporary overlay reproductions confirmed wrong-window routing, empty fallback and the snapshot race. Branch merges cleanly with origin/master (0de61d7987d0241cd69becf00060301a5a86da60). No tracked files changed, commits made, tools installed or dependencies changed by the reviewer.
Residual risks: No visual UI inspection or real-application key-delivery end-to-end checks. Zed upstream bindings for the documented version could not be verified. The coder could not locate the review-code skill/security reference; complete security checklist coverage of TOML parsing, compositor IPC and action execution is therefore not claimed.
Decision: stays in review/ pending owner direction; not approved for merge. To request fixes, add an owner note and move to new/.
