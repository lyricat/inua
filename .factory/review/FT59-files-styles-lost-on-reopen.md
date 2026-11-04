---
branch: factory/FT59-files-styles-lost-on-reopen
round: 1
---
# Files loses its styles when its window is reopened

## lyric · 2026-11-04 22:24
Sometimes after closing the inua-files window and opening it again, all styles are gone: only GTK's default style remains. Killing the inua-files process that stayed in memory and opening it again recovers the styles. Inspect the bug and fix it.

## factory · plan · 2026-11-04 22:58
Behaviour: First launch and every reopen in the resident process retain the built-in theme and user styling; closing one or all windows must not invalidate styling. Missing or broken configuration keeps existing defaults.
Scope: Inspect Files window initialization and shared appui CSS lifetime; minimally fix the demonstrated cause and cover repeated reopen. No redesign or unrelated features.
Decisions: Preserve the resident process instead of restarting it to mask the bug. Change shared appui only if the cause lies there.
Checking: Regression-check first launch, surviving windows, repeated reopen, theme/user CSS reload and configuration fallback; run make test. Manual visual checking is not performed here.

## factory · 2026-11-04 22:58
Built 9448f7e1: initialize application ownership, title and CSS classes for every Files window. The previous window-count guard incorrectly treated a reopened window as the first, although appui only initializes the initial window. Display-wide CSS providers remain valid and needed no change.
Added isolated Broadway regression coverage for multiple windows, repeated reopen, theme and user CSS reload, and missing/broken configuration. The regression failed before the fix and passed afterward. Translation source references refreshed with make i18n.
Tests: make test passed in a throwaway HOME, including an independent manager run. The coder's first full run had an intermittent Mail TestBodyStructureMessagePart EOF; targeted repeats and full rerun passed.
Installs: none.
Not checked: no manual visual inspection or nested Hyprland check; computed styles were checked in isolated GTK/Broadway.
Moved new/ → review/; branch pushed. Owner should check closing all Files windows and reopening without restarting its process.
