# Upstream PR backlog

What is already proposed to `lyricat/inua`, and what still has to be split
into a small PR after its base lands. Commits are from this fork's `master`.

## Open PRs

| PR | Branch | What it is |
| --- | --- | --- |
| #73 | `feat/appmenu-app-publish` | inua's own apps publish a dbusmenu for the bar |
| #74 | `feat/appmenu-a11y` | read menus through AT-SPI, including VS Code |
| #75 | `feat/bar-menu-switch` | the bar's application-menu switch; the clock at the end |
| #76 | `feat/appmenu-shell-term` | Terminal and Settings publish menus |
| #77 | `feat/style-animations` | animation setting in inua's own surfaces |
| #78 | `feat/compositor-animations` | animation setting through each compositor backend |
| #79 | `pr/clock-case` | the clock keeps the case its format writes |
| #80 | `pr/launcher-right-click` | right-click a launcher result for its actions |
| #81 | `pr/chromium-menu` | read Chromium's own dbusmenu over XWayland |
| #82 | `pr/monitor-focus-outline` | outline each monitor's last focused window |
| #83 | `pr/super-f-maximize` | Super+F maximizes instead of fullscreen |
| #84 | `pr/appmenu-button-bar` | open the menu of the bar whose button was clicked |

## Not split yet

These build on #74 or #75. Submitting them from `master` as one branch
would also submit all of the base work, so each waits for its base branch to
merge. Order below is the order to submit them.

### 1. Firefox's own Wayland menu (`feat/appmenu-a11y` base)

- `8acbb907` feat(appmenu): read Firefox's Wayland menu from its own bus name
- `c3235fed` fix(appmenu): find Firefox's menu through its process tree
- `c5c9d336` fix(appmenu): read Firefox's menu tree once, so its ids stay valid
- `af9cfac7` fix(appmenu): never press Firefox's sections, and read a tree built late

Firefox exposes `org.mozilla.firefox.*` dbusmenu from its main process,
while the window belongs to a content process; the PR should explain the
process-tree match and why its sections must never be pressed over AT-SPI.

### 2. The menu every window has; the system on an empty workspace (#75 base)

- `380f2572` feat(bar): the application icon and name lead its menu
- `c186d492` fix(bar): the menu's words sit level with the application's name
- `0a394033` feat(bar): the application's icon and name open the menu every window has
- `c3f6c372` fix(bar): the title menu's items act for the bar's current application
- `c322645f` feat(bar): the system menu on an empty workspace
- `cc5bc976` feat(bar): name the system on an empty workspace

The title menu needs the bar's per-monitor window and the current
application, so it stays stacked on #75 rather than duplicating that state.

### 3. Default File/Edit/View/Help menu (#74 base)

- `e111264c` feat(appmenu): a generic menu for applications without one
- `297fcec6` fix(appmenu): one honest Application menu when a menu bar cannot be read
- `e3ad4ef6` feat(appmenu): the default File/Edit/View/Help menu for windows without one

### 4. Overview settings

- `8f2e8540` feat(overview): hints and the shortcut to a new workspace's launcher
- `98f3a48a` feat(overview): a scope choice in the settings, and hide the workspace indicator

`98f3a48a` also introduces the narrow settings layout used by the next group,
so split the narrow layout into its own commit before opening #4 if the two
PRs should be independent.

### 5. Bar icon metrics and the clock's date language (#75 base)

- `eb0a094f` feat(clock): a date language setting, and the clock at the bar's end
- `6bbe553d` feat(settings): the bar's icon size and spacing, and the clock's date language
- `d397d911` fix(config): the bar icon metrics' defaults
- `7b7fd82b` fix(bar): one size and one gap for every icon in the bar
- `53e39549` fix(bar): the icon gap is the setting's, across buttons too

`eb0a094f` and `6bbe553d` can be one PR each: the clock's language, and the
bar's icon size/spacing. The three `fix` commits belong with the metrics PR.

### 6. Narrow settings layout

- `1796dda4` fix(settings): collapse the index on a small screen's width
- `98f3a48a` feat(overview): a scope choice in the settings, and hide the workspace indicator
  (only the `settings_narrow.go` part)

The threshold and the narrow rules are one change; the Overview scope change
in the same commit is not part of it.

## Housekeeping before submitting a stack

- Rebase the base PR onto `upstream/master`; keep its branch name.
- Cherry-pick only the listed commits onto the new branch, not `master`.
- Run `make i18n` and `make generate` in the new branch.
- State in the PR body which base PR it needs.
- Verify with a build and the narrowest runtime check that covers the change;
  UI changes use a nested compositor, not the user's session.
