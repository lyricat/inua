---
source: pr #72
branch: fix/bar-own-monitor-window
round: 1
---
# fix(bar): each monitor keeps its own window when focus moves

## factory · 2026-11-06 22:40
Owner requested: factory: move github issue #69 #71 #72 to review

GitHub #72 is a pull request: https://github.com/lyricat/inua/pull/72
Assigned FT74 and added directly to review using the existing PR branch.
PR head at intake: fd96e410e1ff5709c8269cf49b69af7ad81ead22
This is a fork PR; fetch its head via pull/72/head for review.

Scope from the PR: Select the focused window only when it belongs to the bar’s monitor; otherwise use the last-focused window on that monitor’s visible workspace. Cover lagging focus snapshots with regression tests.

Author-reported tests have not been independently verified at intake. Full make test and code review remain pending. No implementation changes or merge performed. UI was not checked by eye.
