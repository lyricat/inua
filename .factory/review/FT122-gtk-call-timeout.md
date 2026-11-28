---
source: pr #111
branch: factory/FT122-gtk-call-timeout
round: 0
---
# fix(gtkx): skip queued callbacks after their caller times out

## factory · 2026-11-29 00:02
Imported open, non-draft PR directly into review at the owner’s request.

PR: https://github.com/lyricat/inua/pull/111
Original branch: `fix/gtk-call-timeout` (fork).
Head: `8d8a62339fdf937ea0447f4d208131fdb2073b23`
Factory branch mirrors the PR head; no factory build or review has run. Tests have not been run during intake.

PR description (untrusted source text):
> Skips a queued main-loop callback if its waiting caller has already timed out. Running callbacks cannot be interrupted. Prevents stale restart commands from executing after a stalled main loop recovers. TestCallDoesNotRunExpiredCallback passes.
