---
branch: factory/0057-overview-dock-insets
round: 0
---
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

## factory · plan · 2026-11-03 11:00
Behaviour: with the dock shown above Overview (layer shell, `overview.show_dock`, dock on), the toolbar, cards and hint go into a content box whose margin on the dock's side is the dock's inset, and the card layout gets that much less room; the layout key carries the four insets, so a dock size or position change lays the overview out again. With the dock hidden during Overview or off, the insets are 0 and the layout is master's.

Scope: PR #53's `07bfc94` and `191374d` by cherry-pick, author kept; `1cd7f06` (a merge of master) left out. Nothing else.

Decisions: the author's inset (`dock.size + 16`) is kept as written; see the note below on what the dock really takes.

Checking: `make test`; `make generate` and `make i18n` on the result.

## factory · 2026-11-03 11:00
Built on factory/0057-overview-dock-insets: `3977bf6` and `6834491`, the author's two commits, cherry-picked onto master `237e7b6` with no conflicts. `make generate` and `make i18n` change nothing; `make test` passes.

The inset is short of the dock's real footprint. From `ui/style.css` and `ui/dock.blp`: at the bottom the island is size + 20 tall (island padding 3+2, border 1+1, item padding 4+2, the 3px spacing and the 4px dot) plus its 6px bottom margin, about size + 26; on the left or right it is size + 18 wide (padding 4+4, border 1+1, item padding 4+4), plus 6 with edge gaps; at the top size + 20, plus 6 with edge gaps. So the cards can still come 4–10 px under the dock. Not changed here, to keep the author's commits as they are; a fix round can take the island's measured size instead.

Done by the factory directly: the work was a cherry-pick, so no coding agent ran.

Not checked: the UI change was not looked at in a nested session, for any dock position.

## Lyric · review · 2026-11-03 11:11
head: 68344911aa1e46e622b55b6509240440213f2535
verdict: pass

No qualifying findings. Codex reviewed origin/master...origin/factory/0057-overview-dock-insets (merge-base 237e7b6), covering all nine changed files. Both commits preserve CHEYAN's authorship; the original commit objects were unavailable locally for patch comparison.

The size + 16 inset understates the dock's footprint, but the Overview's existing 28px vertical and 48px horizontal padding provides additional clearance. For size=36 with edge gaps, the vertical dock occupies about 62px versus an 80px content boundary; the horizontal dock occupies about 60px versus 100px. The 3px auto-hide trigger still leaves clearance. The earlier overlap concern is therefore not supported by this static review. All four positions and zero-inset hidden/off/non-layer-shell cases were checked; the layout key includes the insets.

Validation: git diff --check passed. Read-only checks found the seven catalog diffs change source references only, with new references pointing to translation calls. The reviewed worktree remained clean. No build, tests, or generators were run during review. UI not checked by eye; actual GTK/Hyprland layout, scaling, and custom CSS remain unverified.
## ほたる · 2026-11-03 12:54
Moved to ready at the owner's explicit request after the code-review discussion. Visual verification in a nested session remains outstanding; this transition does not claim that it was performed. No implementation changes or merge in this step.

## factory · 2026-11-03 13:04
Owner approved merging both items in Console. Merged into master and pushed (58a19b2); branch ancestry verified against origin/master. Combined merge result: make generate and make i18n left tracked files unchanged; make test passed with throwaway HOME and XDG_RUNTIME_DIR. Initial test attempt lacked XDG_RUNTIME_DIR; rerun passed after providing a temporary runtime directory. No real desktop or nested-session visual verification was performed.
