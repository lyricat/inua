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

## factory · review · 2026-11-06 23:40
head: fd96e410e1ff5709c8269cf49b69af7ad81ead22
verdict: pass
notes:
- Codex reviewed origin/master...HEAD, UI callers and all three compositor backends; no confirmed introduced defects found.
- Fork PR head fetched through origin pull/72/head; clean merge with origin/master verified. No code changes or commits made.
- Codex reports make test passed (exit 0): 18 script tests, go vet and race tests across 111 packages. Initial private D-Bus AppArmor query failed against read-only securityfs; rerun passed after isolating that interface inside the test namespace, without changing real system settings.
- 24 temporary overlay probe scenarios passed, including lagging focus snapshots, explicit output priority, workspace fallback and missing focused window. git diff --check passed; tracked worktree remains clean.
- No unpinned tools installed. UI was not checked by eye; real multi-monitor rendering remains unverified.
- Moved review/ → ready/; owner approval required before ship.

## factory · 2026-11-07 00:33
Merged into master at e97ebcc; pushed successfully. PR #72 is marked merged on GitHub.
Combined validation: make generate, make i18n, make test (uncached Go race tests, go vet, 18 Python tests), and git diff --check passed. Tests used throwaway HOME and isolated runtime/network/D-Bus/securityfs.
FT74 integration resolved the BarWindow conflict preserving FT71 label selection and FT74 per-monitor fallback, with the corresponding test assertion updated. No unpinned installs. UI was not checked by eye; real multi-monitor rendering remains unverified. FT71 Settings label/icon P2 remains.
