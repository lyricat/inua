---
source: pr #71
branch: fix/hyprconf-bare-modifier
round: 1
---
# fix(hyprconf): skip bare modifier Lua binds

## factory · 2026-11-06 22:40
Owner requested: factory: move github issue #69 #71 #72 to review

GitHub #71 is a pull request: https://github.com/lyricat/inua/pull/71
Assigned FT73 and added directly to review using the existing PR branch.
PR head at intake: 33940991a269e80e75c94bbe982432cf863abc8c
This is a fork PR; fetch its head via pull/71/head for review.

Scope from the PR: Skip bare modifier shortcuts in unbind generation and Lua binds; retain classic SUPER_L/SUPER_R release handling and add renderer regression coverage.

Author-reported tests have not been independently verified at intake. Full make test and code review remain pending. No implementation changes or merge performed. UI was not checked by eye.
