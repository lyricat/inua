---
source: pr #88
branch: factory/FT87-appmenu-toml-packs
round: 3
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

## factory · plan · 2026-11-09 05:55
Owner requested: “Can you fix FT87? By using Claude code”. Fix round on the existing branch, addressing all seven review findings. Bind actions to the focused window, capture snapshot data before background work, distinguish pending discovery from absent menus, restore generic fallback, correct kitty keysyms and clarify unsupported flags/install behavior. Add regression tests; no accessibility discovery implementation or unrelated features.

## factory · 2026-11-09 05:55
Claude Code completed fix round 2 in 26a88e9a, b4452c6f and 07e98b83; pushed head 07e98b83 on factory/FT87-appmenu-toml-packs. All seven prior review findings addressed by implementation or documentation corrections. Added focused-window/stale-action, quit capture, fallback/discovery selection and punctuation regression tests. Pack validation additionally rejects single-character punctuation keys.
Validation: Claude Code ran make test and make i18n; manager independently reran make test with throwaway HOME/XDG directories successfully (18 Python session tests, go vet and full Go race tests). No protected paths changed; clean worktree. No tools/dependencies installed. No layout files changed.
Not checked: visual UI behavior, real application key delivery, GTK discovery-completion rerender. Accessibility flags remain intentionally inert and documented as such. Generic menus now appear for any focused window lacking a published menu/usable pack; registration timing requires live verification.
Remains in review/ for review of the new head; not approved or merged into master.


## factory · review · 2026-11-09 06:10
head: 07e98b83c60fad6d7f0e5972a7f28ae8acd51568
verdict: fix
fix:
- [P1] internal/ui/appmenu_packs.go:118: Each About window allocates a permanent native callback; repeated opening eventually crashes the shell with purego callback exhaustion. Isolated Broadway reproduction failed after 2000 open/destroy cycles. Reuse a long-lived shared click handler and release window/child references correctly.
notes:
- [P2] internal/appmenu/packs.go:354: The 256 KiB limit is checked only after reading the entire file, and non-regular files are accepted. A 16 MiB TOML allocated about 16.8 MB before rejection; a TOML FIFO blocked waiting for a writer on a UI-thread-reachable path. Accept only regular files, limit reads before allocation, and keep configuration I/O off the GTK thread.
- [P2] internal/ui/appmenu.go:95: Rediscovery of the same window retains searched=true, allowing fallback pack actions while a newly registered published menu is pending. A private D-Bus reproduction registered /menu after confirmed absence and observed pack selection/action authorization during rediscovery. Clear confirmed-absence state on every search and withdraw fallback while pending.

Validation: Codex reviewed all 23 changed files in origin/master...HEAD, including round 2. Base tip: 0de61d7987d0241cd69becf00060301a5a86da60; merge base: 655b7e896e604c6c579b098289cef80d8fa6acfb. Branch merges cleanly. Initial isolated make test failed at an AppArmor read-only probe; a rerun with only the temporary sandbox AppArmor view adjusted passed (exit 0): 18 Python tests, go vet, full Go race tests. Additional About regression probe failed with `purego: the maximum number of callbacks has been reached`. No tracked source edits, commits, or installs by reviewer.
Residual risks: No visual inspection, real-application key delivery, or version-specific shortcut verification. Security-sensitive config/parser/action/IPC/concurrency/FFI paths were traced; the resumed coder could not locate the full review-code reference locally, so exhaustive security-checklist coverage is not claimed. The first coder invocation timed out; its isolated test artifacts were retained and reused by the resumed Codex review. No live user apps/settings were changed.
Decision: stays in review/ for owner direction; not approved or merged. Add an owner note and move to new/ to request fixes.

## factory · 2026-11-09 15:12
Moved review/ → new/ at the owner's request: “move FT87 back to new”. The next build is fix round 3 on factory/FT87-appmenu-toml-packs, for the fix: list of the review at head 07e98b83.

## factory · plan · 2026-11-09 15:42
Round 3 addendum. Behaviour: repeatedly opening About reuses one native Close handler; Close destroys only its own window, compositor close remains supported, and window/child references are released. Multiple About windows remain independent. Scope: latest required P1 callback exhaustion and ownership fix only; the two P2 notes on bounded regular-file config reads and same-window rediscovery remain deferred. Decisions: use existing shared-handler and gtkx ownership patterns, no new callback registry or visual redesign. Checking: isolated Broadway regression for independent close, simulated compositor close and 2500 open/close cycles with finalization checks; make test and make i18n.

## factory · 2026-11-09 15:42
Claude Code completed fix round 3 in 5dce048e; pushed factory/FT87-appmenu-toml-packs. About windows share a long-lived Close handler, resolve the clicked button's own window, and release constructor/getter references. Regression exercises 2500 cycles and checks all 5000 window/button objects are freed; coder reports the same probe fails on old code with callback exhaustion and missing finalization.
Validation: coder ran make i18n before commit and make test successfully. Manager independently reran make test with throwaway HOME/XDG directories: exit 0 (18 Python tests, go vet, full Go race suite). Commit history contains only this item's fixes; no protected paths changed. No unpinned tools or dependencies installed; pinned Go modules restored. No layouts changed.
Not checked: visual UI inspection, real compositor close, real application key delivery. Broadway's gtk_window_close simulation is not a real compositor check. Latest two P2 notes remain deliberately unresolved, as this round targets the required fix list. Moved new/ → review/; not approved or merged into master.


## factory · review · 2026-11-09 16:03
head: 5dce048e7ad75ffdf33a0ba44ec5b58e6d06de6d
verdict: pass
notes:
- [P2] internal/appmenu/packs.go:354: Pack files are fully read before the size check, and special files are accepted. Example: a 16 MiB TOML allocated 16,810,208 bytes before rejection; a FIFO without a writer blocked. Validate opened regular files without blocking, bound reads, and move I/O off the GTK thread. Previously deferred, still present.
- [P2] internal/ui/appmenu.go:95: Same-window rediscovery retains searched=true, allowing fallback actions while discovery is pending. Example: after confirmed absence then registration of /menu, a private D-Bus reproduction sent CTRL T to a fake compositor before the published-menu result arrived. Reset confirmed-absence state for every search and withdraw fallback while pending. Previously deferred, still present.
- [P2] internal/ui/appmenu.go:266: Comparing array indices instead of published entry IDs leaves old popups and requests valid after menu reconstruction. Example: File ID 10/action 11 became File ID 20/action 21 with unchanged label/position; clicking the retained popup sent Event(11), while baseline closes it. Include entry.ID in change detection and invalidate old popups/requests.

Validation: Codex reviewed all 25 changed files in origin/master...HEAD; base bf80760714bab3ba064afe709c72027e559e1578, merge base 655b7e896e604c6c579b098289cef80d8fa6acfb. Latest About callback-exhaustion P1 is resolved: independent race-enabled TestAppAboutWindows passed 2500 cycles and 5000 object-finalization checks. Final make test exited 0 (18 Python tests, go vet, all-package race tests). Two initial isolation-environment failures also reproduced on baseline: AppArmor read-only policy probing and missing portal GSettings schemas. Only the disposable sandbox probe view/schema availability was adjusted; no host security settings or test source changed. Additional isolated overlay probes confirmed the three P2 findings. git diff --check passed; review worktree remained clean. No installs or dependency changes.
Residual risks: No visual UI inspection, real-application key delivery, real compositor-close validation, or application-version shortcut verification. Parser/file, native FFI, process/IPC, configuration and concurrency paths traced; extreme parser/directory resource loads, replacement races and backend-specific key parsing not exhaustively tested.
Decision: no remaining P0/P1; moved review/ → ready/ under the factory gate. Three P2 findings remain for owner consideration. Not merged into master.


## factory · 2026-11-09 22:09
Shipped at owner request after Claude Code fixes. Merge commit: c8944b0d. Fixed all three outstanding P2 findings: nonblocking regular-file-only bounded pack reads off the GTK thread; confirmed-absence reset and fallback withdrawal during rediscovery; published entry-ID comparison invalidating stale popups/requests. Fix commits: 0e2493e5, 76b15fb5, c1b8da97. Linked PR #88 closed after master push.
Claude Code added regression tests and passed make test; manager independently reran make test with isolated absolute HOME/XDG directories: exit 0 (18 Python tests, go vet, full Go race suite). No installs; protected paths unchanged. master and annotated tag v0.1.30 atomically pushed at 2cd0dbb3.
Not checked: visual UI, actual drag/drop, multi-monitor behavior, restart persistence, real-application key delivery and real compositor behavior. Moved ready/ → done/.
