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
