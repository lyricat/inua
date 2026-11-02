---
branch: factory/54-overview-dock
round: 0
---
# Show Dock during workspace Overview

## lyric · 2026-11-01 20:52
Import the author's PR #53 directly into factory review.

## factory · 2026-11-01 20:55
Source: https://github.com/lyricat/inua/pull/53
Author: iamcheyan
PR branch: feat/overview-dock
Imported commit: 6d10352e7d9a860bd02ebd94556b3fb20ba10657

The factory branch preserves the author's commits. Item 0053 already belongs to the completed terminal pane close confirmation task; this separate request uses item 0054.

Requested behaviour: keep the Dock visible above workspace Overview, provide a setting to disable this behaviour, and restore the previous Dock autohide state when Overview closes.

The author reports running `go test ./internal/config`, `go test ./internal/shell`, `CGO_ENABLED=0 go build -buildvcs=false -o /tmp/inua-overview-dock-test ./cmd/inua-shell`, `make generate`, and `make i18n`. These results have not been independently verified during import. The author did not exercise the UI in a nested desktop session; neither has the factory during import.

Pending review: inspect the implementation and regression coverage, verify configuration defaults and persistence, check Dock layering and autohide restoration across Overview open/close and setting changes, and run the configured `make test` against the merge result with current master. Follow repository generation and translation requirements when fixing code. Do not treat this import as review approval or merge authorization.

## Morta · 2026-11-01 testing
Tested imported commit `6d10352` merged without committing with local master `749bc25`, in detached worktree `/tmp/inua-pr53-check`; production desktop untouched.

- PASS: `make test` (15 Python session tests, go vet, Go race tests), with temporary HOME and private XDG_RUNTIME_DIR. Initial run lacked XDG_RUNTIME_DIR and failed Hyprland config checks; rerun with the private directory passed.
- PASS: `CGO_ENABLED=0 go build -o /tmp/inua-pr53-shell ./cmd/inua-shell`.
- PASS: `make i18n`; no unstaged generated differences against the temporary merge result.
- BLOCKED: `make generate`, because blueprint-compiler is not installed.
- NOT VERIFIED: nested desktop visuals, actual stacking, clicks and multiple monitors. This execution environment has no WAYLAND_DISPLAY set; no production session was targeted.

Code-inspection concern (not graphically reproduced): open Overview while an autohiding Dock is revealed, move the pointer away, wait past its hide timeout, then close Overview. `onDockHide` skips hiding while `overviewShown` and returns false (one-shot), but closing restores `overviewWasRevealed || hover` without scheduling another hide. This can leave the Dock visible until another enter/leave cycle. Check and fix this before approval.

No implementation changes, approval, commit, push or merge into master performed during this test.

## factory · 2026-11-02 00:43
Owner explicitly requested in Console: “然后 54 也可以去 ready 了”.
Moved to ready on the owner's decision, awaiting merge. This status change
does not claim that the Dock autohide concern was fixed, that make generate
passed, or that visual interaction was verified. The previous testing notes
and limitations are retained unchanged. No tests were rerun, implementation
changed, or code merged for this status change.

## factory · 2026-11-02 15:05
Progress reconciliation: origin/factory/54-overview-dock remains at
6d10352 and is not an ancestor of origin/master (c42ac4e). Source PR #53
remains open. Keep ready/ under the previously recorded owner decision;
this is factory item 0054, not GitHub issue #54.

No implementation, merge or test run was performed in this board-only
migration. Previous test results, the Dock autohide concern and visual
verification limitations remain unchanged. The old testing section is
preserved verbatim as historical evidence.
