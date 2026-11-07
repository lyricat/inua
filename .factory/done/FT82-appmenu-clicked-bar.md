---
source: pr #84
branch: factory/FT82-appmenu-clicked-bar
round: 1
---
# fix(appmenu): open the menu of the bar whose button was clicked

## factory · 2026-11-07 16:21
Imported GitHub PR #84 directly into review at the owner's request.
Source: https://github.com/lyricat/inua/pull/84
Upstream head: 1915f73e2b37e319c8d38ca787ae58c04e9582ff
Factory branch preserves the upstream head without changes.

The menu click handler previously searched all bars for the button's item ID, which could select another monitor's menu. The PR identifies the bar owning the button's parent box before looking up its menu item.

The author reports building and checking a two-monitor Kate/terminal setup. These checks have not been independently repeated by the factory.
No code review, tests, or visual UI checks were run during import. Awaiting review; not approved for merge.

## factory · review · 2026-11-07 16:55
head: 1915f73e2b37e319c8d38ca787ae58c04e9582ff
verdict: pass
notes:
- [P2] internal/ui/appmenu.go:171: GetParent acquires a parent-widget reference without automatic release; each menu click leaks a reference, retaining the menu container after bar destruction. Add defer parent.Unref() after the nil guard. Codex's isolated Broadway reproduction invoked the real clicked callback 100 times: HEAD needed 100 extra Unref calls to destroy the container; the merge base needed none.
- Codex reviewed the complete origin/master...HEAD diff: one file, nine additions, including caller paths and GTK pointer/reference semantics. No P0 or P1 findings. Branch merges cleanly with origin/master; HEAD and tracked files unchanged. Temporary untracked validation artifacts only.
- Validation: make test passed with throwaway HOME and isolated runtime directory: 18 Python tests, go vet ./..., and Go race tests; 112 packages passed.
- Non-pinned installs: none. UI not visually checked; the author's two-monitor Kate/terminal check was not independently repeated. Broadway validation covered widget hierarchy and reference lifetime only.
- Moved to ready under factory policy (no P0/P1); owner should consider the P2 reference leak before shipping. Not merged.

## factory · 2026-11-07 22:20
Merged into master at e9b45a97; released in v0.1.28 (e9b45a97). PR #84: CLOSED.
Combined make generate, make i18n, make test and git diff --check passed with isolated HOME/runtime; generated files unchanged. No installs. Existing P2 review notes remain; UI, real Chromium/XWayland and multi-monitor behavior not independently checked.
