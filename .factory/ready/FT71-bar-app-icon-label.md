---
source: pr #69
branch: feat/bar-app-label-upstream
round: 1
---
# Top bar: show app icon and choose label

## factory · 2026-11-06 18:45
Owner requested moving GitHub PR #69 directly to review:
> 好 move to review status

Source: https://github.com/lyricat/inua/pull/69
Existing implementation branch: feat/bar-app-label-upstream
PR head at intake: 81b1cbf357418b0ac18d0cea82cca04d37811133

Scope from the PR:
- Show the focused application's icon beside its top-bar label.
- Allow choosing the application name or focused window title through settings.
- bar.title controls the icon and label together; bar.title_mode accepts app or window and defaults to app.
- Preserve per-monitor window-selection behavior.

PR author reports make generate, focused config/shell tests, a CGO-disabled shell build, make i18n and git diff --check passed. These results have not been independently verified at intake. Full make test and code review remain pending. UI was not checked by eye; the author reports no WAYLAND_DISPLAY.

No existing board item was found for this PR. Assigned FT71 and added directly to review/ using the existing PR branch. No new implementation, review verdict or merge was performed.

## factory · review · 2026-11-06 18:59
head: 81b1cbf357418b0ac18d0cea82cca04d37811133
verdict: pass
notes:
- [P2] internal/ui/bar.go:417: The bar does not refresh when the asynchronous desktop-entry index finishes loading. An isolated Broadway probe confirmed that a window snapshot arriving first shows `io.github.lyricat.Inua.Term`; loading its desktop entry leaves that label unchanged until another `setHypr` call changes it to `Terminal`. Refresh each monitor's bar from the current snapshot after the app index updates.
- Codex reviewed all 20 changed files against origin/master...HEAD. No P0/P1 findings; moved to ready under the factory review policy. The P2 remains for owner consideration.
- `make test` passed: 18 Python tests, go vet, race tests (111 packages passed; 35 had no tests). Blueprint outputs and translation references checked. Isolated UI probe confirmed the finding and checked window mode, title visibility, no-focus hiding and 16px file icons; initial probe setup failures were corrected, not repository test failures.
- No installs. UI was not checked by eye; real multi-monitor behavior was not exercised.
- PR #69 is a fork PR; its head was fetched via pull/69/head for review. No code changes or branch push.
