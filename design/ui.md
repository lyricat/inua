# UI and UX

How inua looks and behaves: the shell's surfaces, the settings window and
the light apps (Files, Remote Desktop, Mobile Link, GPG, Pass and the
rest). The rules are written down here; `ui/style.css` and the themes in
`ui/themes/` are where they are kept. When the two disagree, the code
wins, and this file is corrected.

## Principles

- **Restraint and empty space.** Few colours, one accent per surface, and
  that accent only for what is current, selected or on. No glows, no
  gradients, no drop shadows on controls.
- **Hairlines, not boxes.** Sections are separated by a 1 px line, not
  framed or filled. Corners are 2 px (controls), 3–4 px (cards, menus).
- **Two voices of type.** Running text in a sans; readouts, labels and
  mode names in a monospace, small and tracked wide.
- **Status is a square.** 7 px, filled when on, hollow when off.
- **Keyboard first.** Every action has a key; the keys are listed where
  the user can find them (Files' Ctrl+?, the launcher's foot).
- **Say what happened, where it happened.** An error or a result shows
  beside the control or the thing it is about, never in a distant line
  (see [Feedback](#feedback)).
- **Real content in the first frame.** No placeholder numbers; see
  `docs/performance.md`.

## Colour

Colours are tokens only. A theme (`ui/themes/<name>.css`, chosen by
`style.theme`) defines them; `ui/style.css` uses the names and never a
colour of its own. A user's theme overrides only the tokens it names.

| Token | Use |
| --- | --- |
| `app` | The ground of windows and pages |
| `panel` | The index column of two-column windows; side panels |
| `surface`, `surface2` | Controls: entries, outlined buttons, cells |
| `t0`, `t1`, `t2` | Text: primary, secondary, tertiary (hints, kickers, disabled) |
| `line` | Hairlines and control borders |
| `accent` | Current, selected, on; the main action's fill |
| `primary` | The light apps' `.primary` button fill |
| `danger`, `warn`, `ok` | Errors and destructive actions; notices; good states |
| `field` | The wallpaper's texture |
| `sb`, `sbaccent` | The bar |

The few places that tell kinds apart by colour use the three hues the
themes have besides the text: Disk Usage's map draws the developer's
folders in `accent`, the agents' and AI's in `warn` and caches in `ok`,
the rest in `t2`, with a legend of squares under it.

Themes: `yoru` (the default, dark), `washi` and `aojashin` (light),
`suimen`, `rinko`, `bunko` and `denko` (dark).

Tints of the accent, used the same way everywhere:

| State | Fill | Border |
| --- | --- | --- |
| Hover | accent at 5–9% | accent at 20% (when the item has a border) |
| Selected, checked | accent at 9–16%, or `mix(surface, primary, 0.1)` in an index | accent at 30–38% |
| Focused entry | — | accent, with a 3 px ring at 14% |

## Type

| Family | Where |
| --- | --- |
| Zen Kaku Gothic New (Noto Sans CJK after it) | Running text, 13–14 px |
| IBM Plex Sans | Titles: a window's index title (20 px), index entries |
| Geist Mono (IBM Plex Mono, JetBrains Mono) | Readouts, kickers, section labels, tags, keycaps, numbers, paths, PINs; tabular figures |

Recurring sizes and treatments:

- **Kicker** (`.kicker`): mono 10.5 px, weight 600, upper case, tracked
  0.14 em, `t2`. A group's name over its items: an index's (THIS
  COMPUTER, MACHINES), the dock drawer's, a menu's (WEB TERMINAL).
- **Section label** (`.section-label`): mono 11 px, weight 600, tracked
  0.12 em, in brackets: `[STREAM]`.
- **Tag** (`.tag`): a mode name, the section label's type, upper case, in
  the accent: `[APPS]`.

These are the only small-capital voices; a surface uses the class, not a
copy of its numbers.
- **Label / sub** (`.label`, `.sub`): a row's text, 13 px `t1`, and the
  line under it, 12 px `t2`.
- **Readout** (`.readout`): mono 11.5–12 px, `t1`.

`style.text_scale` (80–150%) scales every `font-size` in `ui/style.css`
as it loads; spacing and icons keep their sizes, so layouts must not
depend on text being a fixed size.

## Marks

The status mark (`.mark`) is a 7 px square, its 1 px border counted, with
1 px corners: hollow (`t2` border) when off, filled with the accent when
`on`. A state other than on fills it in its colour (`ok`, `warn`,
`danger`); `wait` is hollow in the accent. It marks a tile, a pane's path
(Files, with two panes), a row of a list of states. The rules are written
once in `ui/style.css`; a window adds a state class, not a rule.

The head mark is the same square before a head's `[LABEL]`, filled with
`mix(t0, accent, 0.68)`: a section's head (`.section-head`), and the heads
of the launcher, the agenda, the notification center, the clipboard, the
widget page and its cards. A head is not a state, so its mark is never the
accent.

In a layout a mark is an **empty `Box`**, never a `Label`: an empty label
is as tall as a line of text, so the "square" draws as a bar.

The one place colours leave the tokens is a service's face in Mail: its
logo on the brand's own colour (`internal/apps/mail/brands.go`, from
Simple Icons), white or black on it, and on a dark theme a brand near
black on white instead (GitHub). The rules are made for the theme in use
(`mail.BrandCSS`), not written in `ui/style.css`.

```blueprint
Box {
  styles ["mark"]
  valign: center;
}
```

## Windows

### The settings language

The settings window and the light apps share one language (their windows
carry `.settings`):

```
┌───────────────────┬──────────────────────────────────────────────┐
│ Title             │ ■ [SECTION]                     meta  [ctl]  │
│ KICKER            │ ──────────────────────────────────────────── │
│ ▣ Item       sub  │ Label                              control   │
│ ▣ Item       sub  │ sub line                                     │
│ KICKER         ↻  │ Label                              control   │
│ ▣ Item       sub  │                                              │
│ + Add…            │ ■ [SECTION]                                  │
└───────────────────┴──────────────────────────────────────────────┘
```

- **Index** (left, `panel`, a hairline on its right): the title (IBM Plex
  Sans 20 px), kickers for groups, items at least 40 px tall (`.settings button.index-item`, in every app) with an icon, a
  title and a sub line. Hover: accent at 6% with a 20% border; selected:
  `mix(surface, primary, 0.1)` with a 30% accent border.
- **Page** (right, `app`, padding 18 px 32 px): sections of rows.
- **Section**: a head (44 px; the mark, the `[LABEL]`, an optional meta
  text or control at the end) on a hairline, then the body. No frame, no
  fill. 22 px between sections. Built in Go, a section is
  `uikit.NewSection` (or `uikit.NewHead` for the head alone), the shell's
  and the apps' alike; `uikit.Bracketed` is the `[LABEL]`'s markup.
- **Row** (`.row`, at least 44 px, 12 px above and below): the text on the
  left (label, sub line), the control at the end, centred. A **field**
  (`.field`) puts its label above a wide control.
- **Rows in a list** (`.rows`): hairlines between them; a whole row can be
  a button (`.row-button`), tinted on hover.

### Light apps

A light app is one of these: a window of its own with an index and pages
(Mobile Link, Clock, Stocks), a single page with text tabs (GPG,
Tailscale), or a library with a panel (Virtual machines, Remote Desktop).

A **library** shows what the user runs or connects to as tiles on
`panel`: a card (`surface`, a `line` border, 4 px corners) with its icon
and a mark at the top (filled while it runs or is connected), then its
name and a sub line; hover and selected as an index item. The chosen one's
page is a panel at the right (`app`, about 520 px), with a head (its mark,
name and state) and sections as a page. New or Add is a button in the
library's head and asks in a dialog, never in the panel. With nothing yet,
the library says so and the panel is hidden. The layout is the app's
Blueprint in the `library` classes (`.library`, `.library-tiles`,
`.library-panel`, `.panel-head`); the tiles are `appui.NewLibraryTile`.

A **sidebar beside a pane** (Files) lines up with it: the sidebar's
head is as tall as the pane's head, and its foot as the pane's status
bar, so the hairlines under the heads and over the feet run straight
across the window. A head holds tools at the default 34 px; a foot is a
tight bar, its tools `small` and its text 12 px. A bar sized to match
another counts the buttons' borders: a 34 px tool is 36 px tall in all,
a `small` one 28 px (`window.files .files-head`, `.files-status`).

They follow `docs/performance.md` for start-up.

### The top bar

**Its height never changes with what it shows.** The bar is 30px (24px
compact) and windows are tiled under it, so a bar one pixel taller
moves every window on the monitor. Focus follows the pointer, so the
title and the app menu change as the pointer crosses windows, and
anything that can appear on the bar (a label, a button, the tray, the
recording) must fit inside it:

- A button is 24px tall with its 3px margins inside the bar: a 16px
  icon or a 16px line, 3px padding, a 1px border.
- Text in the bar has the clock's 16px `line-height`. A 13px label is
  19px tall, and a fallback font (Arabic, Thai) taller still, without it.
- Check a change with the tallest content: an app's menu shown, a title
  in Arabic, the compact density. The bar's height must not move.

### Top bar panels

What a top bar button opens (the notification center, the clipboard
history, passwords, the control center, the agenda) is a card below the
bar, over a full-screen backdrop that closes it on a click.

- **The bar stays live.** While one is open, the bar's buttons still show
  hover, and a click on another one closes the open card and opens that
  one's; a click on the open one's own button closes it, and one elsewhere
  on the bar closes it too.

- **A drop shadow.** Every such card casts `0 8px 28px` at 32% black
  (`.center .card`), and a notification banner a lighter one. A shadow
  is allowed on a surface floating over windows, not a control: these
  cards and [menus](#menus). Nothing may clip it: a `GtkScrolledWindow` around the card
  does, and so do the edges of a window only as big as the card; scroll
  inside the card, and give such a window margins as wide as the shadow.
- **Full height slides in from the side.** A card that runs from the bar
  to the screen's foot (the notification center, the clipboard history)
  is in a `GtkRevealer` that slides in from the right edge when it opens
  and back out however it closes (`revealMS`); the window goes once it is
  out.
- **A panel under a bar button hangs from the bar.** The control center,
  the agenda and the passwords, as tall as their content, are one piece
  of glass with the bar: the bar's glass and border, no border or seam
  where they meet, joined by concave arcs as round as its corners
  (`style.radius`; none at 0); a panel at the bar's end goes on flush
  from its side instead. Opening, it drops out of the bar's edge, soft:
  it falls faster and faster, stretches, and wobbles still; closing, the
  bar draws it back up (`drop.go`, `shell.DropAt`). Nothing dims behind
  it.

### Engawa, the widget page

The place before the first workspace (`widgets.blp`). It is glass over the
windows between the bar and the dock, never over them, so apps can be
dragged in from the dock. Its sides are in line with the bar's, it is as
far from the bar and the dock as the bar is from the screen's top, and it
has a floating surface's `line` border and corners. It fades in and out
(`GtkRevealer`, 200 ms), and Hyprland does not animate it as well. Its
head is a page's: the head mark, `[ENGAWA]`, how to move and remove a
card as a hint (no keys) and Add widget as a link. The widgets are cards
like a library's tiles
(`surface`, a `line` border, 4 px corners), on a grid of 60 px. A card's
outer edges are on the grid: six cells wide, its height rounded up to
whole cells, and a clear cell between it and any other. Its glass blurs
what is behind (a layer rule). Each card's head is a section's mark and
`[LABEL]`: right-click it
for its menu, drag it to move the card. A dragged card is lifted (`surface`,
an accent border at 38%, a floating surface's shadow) and follows the
pointer. The grid fades in under it (160 ms) as a small cross at each
joint, and the cells it
would land on as a dashed spot, accent or `danger` where another card is.
It snaps there when released, or goes back. The bar's mark for the page
is a dot like the workspaces' but hollow, before them. While the page
shows, it stretches into the accent pill and the workspace under the page
is drawn as active, not focused.

## Menus

Every menu, the shell's and the apps', looks and moves as the dock's
drawer does (`bubble.go`): inua-term's at the top of column 1
(`internal/termui/headmenu.go`), Files' and Mail's, the tray's, a right
click's. A popover becomes one with `uikit.Menu` (`popover.glass-menu` in
`ui/style.css`), told where it opens: under its trigger, above it, or at
the pointer. An app's menu on the dock is the drawer's bubble itself
(`showDockMenu`): its corners the dock's, from `style.radius`, the arrow
joined to the card by arcs, and `menu-item`s inside.

```
                     ≡                   ← the trigger
  ┌─────────────────/ \─────────────────┐
  │ Command palette        Ctrl Shift P │
  │ Settings                     Ctrl , │
  │ ─────────────────────────────────── │
  │ WEB TERMINAL         ■ On, Tailscale│
  │ Turn off the Web Terminal           │
  │ ─────────────────────────────────── │
  │ Restart the terminal host…          │
  └─────────────────────────────────────┘
```

- **The surface**: the glass (`@glass`), a hairline round it
  (`alpha(@line, 0.9)`), corners of `style.radius` (`style.Corners` lists
  it), 6 px inside, and the floating card's shadow (`0 8px 28px` at 32%
  black). Hyprland blurs what is behind it, as behind the shell's glass:
  `inua.conf` turns on `decoration:blur:popups`, ignoring the shadow, and
  `blur_popups` for the bar's and the dock's layer surfaces, whose menus
  are their popups.
- **An arrow at the trigger**, in the menu's glass and hairline, its tip
  at the trigger's middle. The menu is centred under the trigger: GTK
  counts the shadow in a popover's alignment, so one aligned to the
  trigger's end leaves its arrow beside it.
- **It grows out of the arrow** (at the pointer, out of its corner): from
  55% of its size at the arrow's tip, fading in, in 200 ms, the arrow
  fading in with it (140 ms). Without the desktop's animations it is
  there at once. A GTK popover plays a CSS animation once, when its
  style first has it; `uikit.Menu` trades it for a twin of another name
  at each show, so a popover kept between opens grows each time too. It
  goes at once when it closes.
- **At the pointer** (a right click), the same menu without an arrow.
- **Items**: running text, 13.5 px, at least 32 px tall and 240 px wide,
  4 px corners, clear until the pointer is over them (accent at 7%, a
  25% accent border); one that cannot be used now is `t2`. An item says
  what it does: a switch is the item "Turn on the Web Terminal", not a
  check mark. Every menu's items have this look, a framed one's too: a
  button item is `.menu-item`, GTK's own (`popover.menu`) take it as is.
- **Keys**: an item with keys shows them at its end, as a keycap in the
  menu's voice (`.menu-key`): mono 11 px, `t1`, a hairline box with no
  fill.
- **Groups**: a hairline between groups. A group about one thing has a
  title, a kicker (`.kicker`), on the hairline, and the thing's state at the
  title's end: its status square and a few words in mono 11 px, in the
  accent while it is on and running, `warn` when it is on but not, `t2`
  otherwise. A state is never a greyed item: it would read as an action
  out of reach.
- **Real state**: what the menu shows of a state that has to be asked
  for (a service running) is read when the menu opens, before it shows.
- **After an item**: the menu closes, then the item runs. An item that
  fails says so where its error can show (see [Feedback](#feedback)):
  the Web Terminal that does not start opens the settings at its page,
  with why.

## Controls

All controls are 34 px tall where they sit in a row, framed by a 1 px
`line` border with 2 px corners, on `surface`.

| Control | Look | Class |
| --- | --- | --- |
| Button | Outlined: `surface`, `line` border, `t0` text; hover: 6% accent, 38% accent border | `outlined` |
| The main action | The outlined shape filled with the accent, text in `app`; hover: the accent mixed 14% toward `t0`; disabled at 45% | `suggested-action` |
| A light app's main action | Filled with `primary`, text in `app`; hover: mixed 12% toward `app` | `primary` |
| Destructive | Outlined, text in `danger`; or filled with `danger` in a confirming dialog, hover as the main action's | `outlined danger`, `destructive-action` |
| Large | Any of the four above at 44 px, 18 px inside, 14 px text | add `large` |
| Small | Any of the four above at 26 px, 10 px inside, 12 px text: in a section's head, a notification, a tight bar | add `small` |
| Icon button | Borderless, 34 px square, `t1`; hover: 9% accent | `plain` |
| Tool | An icon button in a bar of tools, framed: a 55% hairline, clear; hover as the outlined one's, turned on (`:checked`) in a 9% tint, `.on` in the accent | `tool` |
| Icon sizes | 44 px (`large`), 34 px, 26 px (`small`), 20 px in a tight row (`tight`), for `plain`, `tool` and any `image-button` | add the size |
| Entry | `surface`, `line` border; focus: accent border and a 3 px ring at 14% | — |
| Number | An entry's box with the digits in mono; − and + as flat 32 px cells behind hairlines | `spinbutton` (styled for every `.settings` window) |
| Drop-down | The outlined button's shape, at least 150 px; its list on `app` without GTK's padding | `dropdown` |
| Switch | 36 × 18 px, 2 px corners: a 14% accent tint off, the accent on | `switch` |
| Segmented | Options in one hairline frame, 34 px in all (28 px options inside its 2 px); the chosen one tinted with the accent (16%) in the accent's colour; words in mono 12 px, or icons, square (`icons`). An option that cannot be chosen now is disabled, its words at half strength, the reason in a hint under the row. A box of toggle buttons or a stack switcher alike | `segmented` |
| Keycap | Mono 11.5 px in a hairline box | `keycap` |

One main action per page, at most: the accent fill is that page's one
accent. The other buttons beside it are outlined.

The buttons' rules are written once, for every window and surface, at
the top of `ui/style.css`; a window does not restate them. A button of
another size takes a size class, not a rule of the window's: 34 px is the
default, `large` 44 px, `small` 26 px and, for icons only, `tight` 20 px.

## Feedback

### Errors where they happen

An error shows beside what it is about, in `danger`, 12 px, wrapped,
selectable (so it can be copied), and hidden while there is none:

- a section's error under that section's rows;
- an action on an item (a machine, a device) on that item's page, kept
  per item, so moving to another item and back still shows it;
- a form's error under the form, in its dialog (Remote Desktop's *Add a
  machine*).

It is cleared when the user tries again. A message that would vanish with
its row (a device that stopped waiting to pair) stays in its section until
then. There is no window-wide status line for errors.

Remote Desktop's window follows this. The settings window and Mobile Link
still say their errors in a status line at the foot of the index
(`.settings .status`); they are to move to the same rule.

### Other feedback

- **A notice** (`.warn`): a hairline in `warn`, no fill.
- **A flash**: a result for a moment in a status line that belongs to the
  thing (Files' pane status, 4 s, in the accent).
- **Empty**: say why there is nothing (`.empty`, `t2`): "This folder is
  empty.", "No device is paired yet.", not a blank page.
- **Waiting**: say what is being waited for ("Asking…", "Starting…") in
  the place the result will show.
- **Before content**: a page that has nothing to show yet shows what it
  knows already, not a blank: Disk Usage's map is the disk's used and free
  room before a scan.
- **Working**: a long job shows its result arriving, in the place it will
  stay, with counters in mono: Disk Usage's map fills ring by ring as
  folders are measured, while a sweep of square-ended cells, stepped in
  opacity (no gradient), turns through the rings outside.
- **Asking first**: what cannot be undone (deleting for good, a command
  that deletes) is asked in a dialog over the window: the question as its
  title, the thing in mono, a line on what follows, Cancel with the focus
  and the action beside it (filled with `danger` for what is lost for
  good). What can be undone (to the trash) is done at once.
- **What a failure needs**: when something is missing (a program, a
  permission), say what and give the command that fixes it, selectable.

## Reordering

A list whose order is the user's is put in order by dragging its items:
the dock's pinned apps, inua-term's workspaces (column 1) and sessions
(column 2, by their headers, with their pane rows). It goes the same way:

- **The item lifts.** A still of it follows the pointer; its own place
  stays open as a gap, so nothing moves when the drag begins.
- **The others make room.** Under the pointer, the gap opens where the
  item would land: before the item whose first half the pointer is over,
  or after the last. The items slide aside, a step every 16 ms of 30% of
  the way left (no fixed duration), not jump.
- **The drop puts it there**, at once, before whatever keeps the order
  (the settings, inua-term's host) has answered: the list never shows the
  old order again on the way.
- **Anywhere else** (outside the list, or Escape), the item goes back
  and the gap closes. Only where it means something does a drop outside
  do more: a pinned app dropped off the dock is unpinned.
- **A click is still a click.** The drag begins past GTK's threshold, so
  pressing an item to open or show it is unchanged.
- **A filtered list stays put.** While a search narrows the list, its
  items do not drag: the places between them are not the list's.

## Icons

Phosphor's regular icons (MIT), copied by `scripts/phosphor-icons.sh` into
`ui/icons/` under the freedesktop name the code asks for. Another source
only when Phosphor has nothing fitting. Files and other apps' icons come
from the desktop's icon theme; everything else from the Phosphor set.

The inua logo and the favicons are in `design/logo/`, drawn by
`design/logo/build.py`; see `design/logo/README.md`.

App icons are objects on the desk, unlike the flat interface icons: inua's
own apps and games (`config/apps/`) and the `inua-objects` theme for other
apps (`config/icons/inua-objects/`, the default `app_icons`), all drawn by
`design/icons/build.py`; see `design/icons/README.md`. `inua-objects-soft`
draws the same objects more simply, `inua-objects-flat` flat; Settings'
"App style" picks one, "Other apps" may give the rest another theme, and
"Line icons" the flat interface icons. They may use
gradients, a light from the top left and one shadow; no filters, no live
text, under 80 KB each, with a simpler drawing for 16 px. Apps the theme
does not draw fall back to the desktop's theme; `inua icons missing` lists
them.

Where an app icon stands for the app (the dock, its drawer, the
launcher), `uikit.AppIcon` shows it with a light of its own, whatever
theme drew it, in one shape for all, `style.icon_shape` (Appearance's
"App icon shape"): a square with continuous corners (`squircle`, the
default: radius 22.4% of the side, eased into the sides as Apple's icons
are), a circle (`circle`), or a round square whose corners differ, plain
arcs joining straight sides (`pebble`). An icon that is a square or a
tile fills it; one of another shape (a circle, a figure) sits smaller on
a light plate. Over it, a fine bright line along the top edge and a
fainter one along the bottom, a faint shade from top to bottom, and a
soft shadow below; the edges' light grows with the icon's size. `none`
leaves all of that out and draws the icon as its file is. Icon files can
be plain squares; they are not changed.

## Words

All text the user reads is translatable (`docs/i18n.md`). Plain words:
what happened and what to do, in the user's terms ("Paired, and ready.",
"It does not answer: it is off, or not sharing its desktop."). Name the
thing, not the mechanism; give the command when the user has to run one.

## Checking a window

Look at a window before calling a visual change done, and compare it with
one that is right (GPG and the settings window are the references).
Without a window on the desktop, GTK's Broadway backend draws an app into
a web page that a headless browser can capture:

```sh
gtk4-broadwayd :7 &
GDK_BACKEND=broadway BROADWAY_DISPLAY=:7 dbus-run-session -- bin/inua-remote -ui-dir ui &
google-chrome --headless=new --disable-gpu --window-size=1300,900 \
  --virtual-time-budget=4000 --screenshot=remote.png http://127.0.0.1:8087/
```

`dbus-run-session` keeps the copy from handing over to the app already
open on the desktop; `-ui-dir ui` reads the layouts and styles from disk,
so a style change needs no rebuild. The private bus also means the copy
cannot see services on the session bus (Mobile Link shows "starting").

## Known differences

- Settings' bar layout (Bar & Dock, *Layout*) is dragged into and along,
  but its items do not slide aside yet: a line in the accent marks where
  the drop goes.

- The settings window and Mobile Link report errors in the index's
  status line, not where they happen.
- Two fills for a main action exist: the accent (`suggested-action`) and
  `primary` (some light apps and games). New windows use the accent.
