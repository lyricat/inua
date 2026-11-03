# Overview keeps clear of the dock shown above it

## lyric · 2026-11-03 10:46
Build the rest of GitHub PR #53: https://github.com/lyricat/inua/pull/53

Item 0054 merged the PR's first commit (`6d10352`). The author then pushed two
more commits on `feat/overview-dock` that are not on master:

- `07bfc94` fix(overview): keep workspace choices clear of dock
- `191374d` fix(overview): preserve dock-aware view ownership

(`1cd7f06` only merges master into the branch.) Take them over by
cherry-pick, keeping the author's commits, and regenerate the catalogs on
today's master.

## Acceptance criteria
- With the dock shown during Overview (`overview.show_dock` and the dock on,
  layer shell in use), the toolbar, workspace cards and hint sit in the area
  the dock leaves free, for every dock position (bottom, top, left, right).
- With the dock hidden during Overview, or the dock off, the layout is as it
  is on master today.
- Changing the dock's size or position re-lays the overview out (the layout
  key includes the insets).
- No other behaviour change in Overview.

## Investigation notes
- Fetch with `git fetch origin pull/53/head`. Only `internal/ui/overview.go`,
  `internal/ui/overview_render.go` and the catalogs change.
- The inset is `dock.size + 16`; check it against the dock island's real
  padding and border in `ui/style.css` and say if it differs.
- A UI change: the look is not checked by eye here; say so in the result.
- When merged, close PR #53 with a note that it was integrated through the
  factory with the author's commits kept, as was done for #52.
