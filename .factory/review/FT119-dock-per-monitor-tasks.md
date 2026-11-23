---
source: pr #107
branch: factory/FT119-dock-per-monitor-tasks
round: 0
---
# feat(dock): optionally show tasks on their own display

## factory · 2026-11-23 15:10
Owner requested moving the open non-draft PRs to review/; this is PR #107.
Source: https://github.com/lyricat/inua/pull/107
PR head: 412240419a3d3054fa543c228c37233b4d28ce18

Imported the PR head unchanged to the factory branch above for review. No code review or tests performed during intake; the UI has not been checked by eye here.

PR description (author-reported behavior and verification, not independently verified):

> Adds an opt-in Dock setting to show running tasks only on the display that owns their windows. Pinned apps remain available on every display. Dock clicks, scrolling, and the context menu use the local window list when enabled.
> 
> Targeted config, shell, and Dock tests passed. The feature was tested in the live dual-display session before the patches were split.
