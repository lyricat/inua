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

## factory · 2026-11-06 22:40
Owner requested: factory: move github issue #69 #71 #72 to review

Returned PR #69 from ready to review. Current PR head: 059d35b759414f3012735cacacc6e674abbea13d; the previous review covered 81b1cbf357418b0ac18d0cea82cca04d37811133, not this head. Preserve the existing branch and review history. No new review or merge performed.

## factory · review · 2026-11-06 23:02
head: 44431c881c8cb1f418171531d42b49303e261071
verdict: pass
notes:
- [P2] internal/ui/bar.go:417: Focusing Settings in default app mode displays `io.github.lyricat.Inua` and a generic icon, even after desktop-entry loading, because Settings has no matching desktop entry. An isolated GTK check reproduced it. Handle shell.AppID with the built-in Settings entry, translated name and icon, as the Dock does.
- Codex reviewed all 21 changed files against origin/master...HEAD. No P0/P1 findings; moved to ready under factory policy. The P2 remains for owner consideration.
- The previous asynchronous-index refresh P2 is fixed: dockApps changes the label to Terminal without another window event. Window-title mode, combined title/icon visibility and existing per-monitor selection were checked.
- Merged origin/master into fork PR head 059d35b, resolved catalog conflicts preserving both sides, and pushed merge 44431c8 to this repository's feat/bar-app-label-upstream branch. The fork PR itself is unchanged; use the board branch for shipping.
- `make test` passed with temporary HOME and isolated runtime/network: 18 Python tests, go vet, 112 Go packages with race tests, uncached. 53 skip records cover missing terminal-engine dependencies, environmental conditions and helper cases. Generated layouts, translations and formatting checked.
- No installs. UI was not checked by eye; real multi-monitor sessions were not exercised. Initial merge-validation AppArmor sandbox query failure was resolved by isolating the mount before a successful rerun.

## factory · 2026-11-07 00:33
Merged into master at 6f1a24e; pushed successfully. PR #69 is marked merged on GitHub.
Combined validation: make generate, make i18n, make test (uncached Go race tests, go vet, 18 Python tests), and git diff --check passed. Tests used throwaway HOME and isolated runtime/network/D-Bus/securityfs.
FT74 integration resolved the BarWindow conflict preserving FT71 label selection and FT74 per-monitor fallback, with the corresponding test assertion updated. No unpinned installs. UI was not checked by eye; real multi-monitor rendering remains unverified. FT71 Settings label/icon P2 remains.
