# Start-up time

How long inua's apps take from launch to a window with their content, what
the time went to, and what was done about it. The apps measured are
System Monitor and Calculator (`inua-apps`), Files (`inua-files`, on a
folder of 457 items) and Mobile Link (`inua-connect`).

## Measured

In a nested Hyprland, nine launches of each program, before and after
taking turns; the medians, in milliseconds from the launch:

| | Before | After |
| --- | ---: | ---: |
| Go's package initialisation done (every program) | 435 | 265 |
| System Monitor: first frame | 1853 | 1450 |
| System Monitor: real CPU and network numbers | 2177 | 1204 (in the first frame) |
| Calculator: first frame | 1765 | 1312 |
| Mobile Link: first frame, with the devices | 1887 | 1436 |
| Files: first frame | 2113 | 1679 |
| Files: the folder's first rows drawn | 3196 | 2141 |
| Files: every row in | 3196 | 3158 |

The marks were temporary: a line printed with the time since the launch
at the start of `main`, after the window's `Present`, after each frame the
window's frame clock painted, when System Monitor showed a sample, when
Files gave the folder to its list, and when Mobile Link showed the
devices. To measure again, put such marks back and launch each program
in a nested session several times, the old and the new build in turns:
the machine's load moves every number by a few hundred milliseconds.

## Where the time went, and what was done

**Binding GTK's functions, ~165 ms in every program.** puregotk's
generated packages bind every function of GTK, GLib, Pango and the others
when the program starts: 27,054 of them, each a `dlsym` and a trampoline,
of which a program calls a few hundred. inua carries puregotk in
`third_party/puregotk`, patched to bind each function on its first call
instead (`third_party/puregotk/INUA.md`); go.mod points at it with a
`replace`. Loading the libraries themselves (~100 ms) is left.

**The icon theme, read twice, ~290 ms in the light apps.**
GtkApplication's start looks the app's icon up in the icon theme, which
reads the theme with every theme it inherits; a large desktop theme
such as Numix-Circle, which inherits five others, takes a quarter of a
second more than Adwaita. The apps then switched to their own Phosphor
theme, which inherited the desktop's, and the whole chain was read again.
Now the apps' theme is set before the application starts
(`appui.run`), and it inherits Adwaita: the apps' icons are Phosphor's,
and Adwaita has the symbolic icons the set lacks. What shows the desktop's
icons (Files' files, folders, places and the apps it offers to open a
file with) looks them up in an icon theme of the desktop's alone, as
before; Files has that theme read on a thread of its own while GTK starts
(`Preparer`), since GtkIconTheme takes lookups from any thread once made.

**System Monitor's first sample, a second late.** The window showed a
sample at once, from readings taken microseconds apart (no CPU use, no
traffic), and the next a second later. The first sample now comes a
quarter of a second after the readings, about when the window is first
drawn.

**Files' first rows, ~1 s late.** An empty list makes a row for each item
it might show, up to 200, at over a millisecond each and more for their
icons, all before the frame that shows any of them: 457 items gave one
frame of ~700 ms. An empty view now gets a screenful (48 items) first;
the rest come 48 a frame, and the selection once all are in. Every row is
in at about the same time as before, but the first are drawn a second
sooner, and no frame is longer than ~300 ms.

## A second round

Measured the same way, seven launches each, against the first round:

| | First round | Second round |
| --- | ---: | ---: |
| System Monitor: first frame | 1444 | 1340 |
| Calculator: first frame | 1304 | 1184 |
| Mobile Link: first frame, with the devices | 1446 | 1329 |
| Files: first frame | 1621 | 1466 |
| Files: the folder's first rows drawn | 2052 | 1872 |
| Files: every row in | 3054 | 2657 |

**GTK reads the desktop's icon theme at start anyway.** `gtk_init`
begins reading the desktop's theme on a thread of its own, and setting
the apps' theme waits until that read is done (~200 ms with Numix-Circle,
which is then thrown away; ~5 ms once it is done). No setting stops that
read; its time is used instead: Files' preparation begins and the style
is parsed while it runs, and the apps' theme is set after.

**The icons, written at every start.** Each app deleted and wrote again
its 218 Phosphor icons (~33 ms). They are now written only when the set,
the user's overlay or the theme's details change (`style.IconTheme`,
a sum of them kept beside the theme), and a new set goes into place whole,
so another app starting meanwhile never reads it half written.

**Accessibility kept current for no one, up to ~200 ms a frame in
Files.** GTK keeps every widget's accessible state current whether or not
an assistive program listens, and each change to a list costs in
proportion to the list's length: filling a folder of 457 items took
~1.4 s with it and ~0.6 s without. The apps now turn it off
(`GTK_A11Y=none`) when the session's AT-SPI bus says nothing uses it (no
screen reader, no other assistive program: `org.a11y.Status`), as
browsers decide; a `GTK_A11Y` set by the user is kept. An app started
before a screen reader is turned on stays without it until started again.

**Tried and left out.** Reading Files' folder before its window exists,
so the first frame has rows: the read takes 10 ms, and the rows made the
first frame ~340 ms later for rows ~80 ms sooner. The software renderer
(`GSK_RENDERER=cairo`): ~280 ms sooner first frame, but drawing on the
CPU is slower for long lists and video.

## What is left

From launch to the first frame, about 1.2 s for a light app:

- loading GTK and its libraries, ~100 ms, and the bindings' stubs;
- GTK's start, ~65 ms, and its read of the desktop's icon theme, which
  the style's parsing (~110 ms; `ui/style.css` holds the shell's and the
  apps' rules together) only partly covers;
- the application's name on the bus, ~60 ms;
- `Present`, ~300 ms, nearly all the GPU renderer's start (Vulkan;
  limiting the Vulkan drivers looked at to the machine's own,
  `VK_DRIVER_FILES`, saves ~80 ms, but that is the system's setting);
- the first frame, ~150–250 ms, with fontconfig's list of the fonts
  (~60 ms with 759 fonts);
- in Files, the first rows of a folder with many kinds of files: ~260 ms
  for each of the first two parts, mostly the icons each kind is the
  first to show.

Going further would take a process kept running that starts each app with
GTK, the style and the icon themes already loaded (a "zygote", as
Android's), which has its own costs: memory held all the time, and an app's
crash or leak reaching the next.

## Next: apps kept ready (planned)

A window in an app already running takes ~220 ms; a second Files window
took ~490 ms, ~270 ms of it the new `inua-files` process loading GTK only
to hand the request on. An app kept after its window closes, and asked
for a window over D-Bus by the shell, opens in a quarter of a second. With
its window up, an app holds 30–55 MB of its own (PSS 58–90 MB):
Calculator 29 MB, Mobile Link 33 MB, System Monitor 38 MB, Files 55 MB.

Which apps are kept is the user's choice, on the Default Apps page:

```
[KEPT READY]
Apps that stay loaded after their window closes, so they open at once.
Each one holds memory while it is kept.

Files                                   [ From login            ▾ ]
  Opens in about 0.3 s instead of 1.5 s · about 55 MB
Calculator                              [ Never                 ▾ ]
  about 30 MB
System Monitor                          [ Never                 ▾ ]
  Stops sampling while hidden · about 40 MB

Kept now: Files (54 MB)                                    [ Quit them ]
```

- Each app: Never, 30 minutes after use, until logging out, or from
  login; the same in `config.toml`. Files is kept from login by default,
  the others never.
- Pass and GPG are never kept (they hold secrets); Music and Video run
  while used, and are heavy.
- Closing a kept app's window (its button, Ctrl+W, Super+Q) hides it and
  keeps the app; with no window it is not in the dock.
- The dock's "Quit" ends the app (its `quit` action over D-Bus, else its
  process), and "Quit them" ends every kept app; Files still finishes the
  copies and moves under way first. The next start is a cold one, and the
  app is kept again as its setting says.
- The shell starts an app kept, or asks the running one for a window, over
  D-Bus (`org.freedesktop.Application`, `FileManager1` for Files), without
  a process of its own.
- A kept app quits when a screen reader is turned on, so that its next
  start has accessibility (see above).

## The launcher: from a key to its results

The launcher builds every result again when the results change: at each
open and each key. With 88 apps in the grid, in a nested Hyprland, the
medians in milliseconds until the frame with the new results was painted:

| | Before | After |
| --- | ---: | ---: |
| Open (not the shell's first) | 242 | 62 |
| A key leaving 89 apps | 209 | 41 |
| A key leaving 49 apps | 161 | 34 |
| A key leaving 20 apps (no foot) | 52 | 15 |
| A key leaving 4 apps | 12 | 5 |

Building the widgets was never the cost (6 to 10 ms for 88 cells). The
frame after it was:

- **Every icon decoded again, ~1 ms each.** A cell's image was given the
  icon's file, which GTK reads and decodes for each new image, the ones
  scrolled out of sight too. The grid's icons are now paintables looked
  up once and shared by the cells and the foot's copies, kept from one
  open to the next and dropped when the icon themes or the apps change
  (`gridIcon`). The shell's first open still decodes them all, ~180 ms.
- **Every icon measured again.** `uikit.AppIcon` draws an icon's art off
  screen and reads its pixels to see how it fits the shape, once for each
  widget: 330 times a rebuild. The measurement is now kept by the icon's
  file (its path, time and size) and the side drawn (`artFits`), for the
  dock and the drawer as well.
- **The blurred foot, ~70 ms.** Eight strips each held their own copy of
  the lines below the view, some 240 cells made, laid out and drawn. One
  copy is now drawn eight times, cut to each strip and blurred
  (`drawFoot`); the picture is the same to within one level of a colour.

The marks were temporary, as above: a line at each frame the launcher's
frame clock painted, with the time since the key, and a timer typing the
queries.

## Lessons for the next time

- **Time what the user sees.** Mark the first frame and the moment the
  real content shows, not only functions. The two largest gains here
  (System Monitor's numbers and Files' rows, a second each) saved little
  CPU time: they moved the content forward.
- **Measure on a real desktop's settings.** The icon theme's cost depends
  on the theme: a quarter of a second with Numix-Circle, none with
  Adwaita. A clean test machine would not have shown it.
- **Look for work done twice and for its order.** The icon theme was read
  at start and again after the apps switched theme; setting it first
  removed the second read.
- **Compare builds fairly.** Run the old and the new build in turns, many
  times, and take medians: another program's load once added more than a
  second to every number. When a number looks wrong, read one run's raw
  marks before believing the summary: a mark printed again later (Mobile
  Link's list, refreshed every three seconds) once showed a slowdown that
  was not there.
- **Try the other way before choosing.** Files' rest of the folder in one
  frame and in parts finished at the same time; the parts kept every frame
  short. Only measuring both said so.
- **Wrong first data is a bug, not slowness.** System Monitor's first
  sample compared readings microseconds apart.
- **Find who depends on what a fix changes.** Inheriting Adwaita would have
  dropped the desktop's app icons from Files' "Open with" list; searching
  for the desktop theme's users found it before a person did.
- **Tell the toolkit's cost from ours.** The same window in a few lines of
  Python and GTK 4 showed the icon theme's cost was GTK's, and
  `GODEBUG=inittrace=1` showed the bindings' share of the start.
- **Look at what the toolkit does for others.** Accessibility kept current
  with nothing listening cost Files more than all its own code; turning
  one thing off at a time (`GTK_A11Y=none`) found it when profiling could
  not be had.
- **A cost removed must be one on the critical path.** Reading Files'
  folder early saved 10 ms of reading and cost the window a third of a
  second.
- **Write down what is left and why.** Costs not worth fixing yet stay in
  the list above, so they are not measured again from nothing.
