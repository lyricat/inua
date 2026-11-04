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

## factory · review · 2026-11-04 23:44
head: 9448f7e1cce36b90b4afd55c130a450b1fed5d2b
verdict: pass

Validation: Codex reviewed all 9 changed files in origin/master...HEAD, including window ownership, reopen/multi-window behavior and CSS provider lifetime. No qualifying P0–P3 findings. No merge conflicts with origin/master.
Tests: make test passed with isolated HOME/XDG directories: 15 Python tests, go vet and race-enabled tests across 104 Go packages. Two initial attempts encountered temporary-path length and symlink semantics in the isolation launcher; adjusted the launcher, not source code, and the full suite passed. Isolated Broadway regression checks passed for repeated reopen, surviving windows, theme/user CSS reload and missing/broken configuration. An overlay restoring the baseline implementation reproduced missing CSS classes/application ownership and incorrect foreground styling.
Installs: none; no dependency downloads. Temporary probes cleaned up; tracked worktree clean and HEAD unchanged.
Limits: no manual visual inspection or nested Hyprland validation. Broadway computed styles do not replace real-desktop interaction checks. Owner should verify closing all Files windows and reopening without restarting the resident process.
Moved review/ → ready/; not merged into master.
