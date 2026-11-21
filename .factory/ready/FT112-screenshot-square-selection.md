---
source: pr #100
branch: factory/FT112-screenshot-square-selection
round: 0
---
# Screenshot: hold Shift for square selections

## factory · 2026-11-21 19:37
Owner requested adding non-draft PR #100 directly to review/.
Source: https://github.com/lyricat/inua/pull/100
PR head: 9773f4c8751f080be649c8e3037e831342c971fe

Requested behavior from the PR:
- Holding Shift while drawing a screenshot area constrains the selection to a square.
- The longer drag dimension sets the side length; the square stays within the current monitor.
- Existing selection resize and move behavior is unchanged.

Imported the PR head unchanged to the factory branch above for review. No code review or tests performed during intake; the UI has not been checked by eye here.

Author-reported verification (not independently verified): make i18n completed; HX90 NixOS configuration built; screenshot interaction confirmed in an existing session. A system generation switch was blocked by the build environment's sudo sandbox restriction.

## factory · review · 2026-11-21 19:57
head: 9773f4c8751f080be649c8e3037e831342c971fe
verdict: pass
notes:
- [P2] internal/shell/screenshot.go:62: A logical square can capture a non-square pixel image because Rect.Pixels rounds axes separately. At scale 1.5, SpanSquareRect(11, 20, 111, 90, 1280, 720) produces a 100×100 logical square but 151×150 captured pixels. Preserve equal pixel dimensions for constrained captures.
- [P2] internal/shell/screenshot.go:38: AGENTS.md requires regression tests for logic changes, but SpanSquareRect has none. Add tests for quadrants, zero-delta direction choices, monitor-edge clamps and fractional-scale output, including the example above.
- Validation: make test exited 0: 18 Python session tests, go vet, and Go race tests passed. Formatting checks passed. Scratch-copy make i18n reproduced catalogs. Branch merges cleanly with origin/master.
- No P0/P1 findings; moved to ready/ under factory policy. P2 notes remain for owner review before shipping.
- No installs; tracked code unchanged. UI not checked by eye; no nested session or live application interaction performed. Modifier-state delivery remains unverified at runtime.
