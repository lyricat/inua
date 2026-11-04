---
branch: factory/FT63-term-hover-pane-focus-option
round: 1
---
# Terminal: option to focus the pane under the pointer

## lyric · 2026-11-04 22:24
Add an option to inua-term: enable the hovered pane to take the focus. Default is off.

## factory · plan · 2026-11-05 00:57
Behaviour: default hover focus off for first-run and existing settings. A persistent terminal preference enables focus when entering a visible pane in the active window, through existing GTK/host synchronization. Disabling takes effect immediately. Dialogs, search, menus and selection/drag retain focus; stale or disconnected panes are ignored. Save errors follow existing handling.

Scope: terminal settings and preferences, pointer entry handling, documentation, settings regression tests and catalogs. No web terminal, compositor policy, host protocol or session rearranging changes.

Decisions: use terminal.focus_follows_mouse = false and a translated switch. Reuse existing focus path, without timers. Pointer entry rather than continuous motion preserves subsequent keyboard focus within a pane.

Checking: test defaults, omitted key, explicit true/false and round trips; run make i18n and make test. Manual checks: split panes, toggle, crossing, keyboard focus, selection/drag, dialogs/search/menus, inactive windows and restart persistence.

## factory · 2026-11-05 00:57
Built b94e3f8f: optional focus-follows-mouse switch, persisted settings, guarded pane entry handling, documentation, regression tests and six translated catalogs. No dependency installs or manifest changes.

Checks: make i18n and targeted race tests passed. Manager make test initially failed in unrelated mail test: `TestBodyStructureOpaqueMessagePart: bodystructure_test.go:42: unexpected EOF`. Coder investigated unchanged baseline mail/go-imap sources (suspected CAPABILITY/FETCH test timing) and retried the complete make test with a throwaway HOME and private runtime directory: passed, including an uncached mail run. Initial coder test setup also needed a private runtime directory, then passed.

UI changed and was not visually checked. All manual interaction checks listed above remain unperformed. Branch pushed; new/ → review/ for review, not merged into master.

## factory · review · 2026-11-05 01:06
head: b94e3f8f0a0feb6018a72f6d075921f75ad9f75c
verdict: pass
notes:
- Codex reviewed all 12 changed files in origin/master...HEAD (base d6a2e5a2). No confirmed introduced defects; no outstanding P0/P1 findings. Clean merge with base; no tracked files changed.
- Final make test passed: 15 Python tests, go vet, and uncached Go race tests, using throwaway HOME and private runtime directory. Initial environment failures also reproduced on base (overlong Unix socket paths and private D-Bus EOF); shortening test paths resolved them. Previous mail unexpected EOF did not recur.
- No unpinned installs or dependency manifest changes.
- UI not visually checked; hover, drag and window activation interactions still require manual checks. libinua-vt.so unavailable, so engine-dependent tests used existing skip behavior.
- review/ → ready/; not merged into master.
