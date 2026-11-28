---
source: pr #109
branch: factory/FT121-appmenu-cascade-lifecycle
round: 0
---
# fix(appmenu): manage cascading popup lifecycle and dismissal

## factory · 2026-11-29 00:02
Imported open, non-draft PR directly into review at the owner’s request.

PR: https://github.com/lyricat/inua/pull/109
Original branch: `fix/appmenu-cascade-lifecycle` (fork).
Head: `f42b9d8a0a940d422ae1f7e7a50fbf531c42857e`
Factory branch mirrors the PR head; no factory build or review has run. Tests have not been run during intake.

PR description (untrusted source text):
> Uses the latest local cascading-popup implementation, including submenu toggling, deferred detach and outside-click tracking. This is no longer the earlier single-popup two-column implementation. Supersedes #99. TestAppMenuCascade passes on Broadway; the previously reported Wayland hang requires further live confirmation.
