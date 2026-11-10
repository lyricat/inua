# Files: inua's file manager

`inua-files` is a file manager in the light apps' look, made to replace
Nautilus in an inua session. It aims to be:

- **fast**: a folder of 100 000 entries opens in a fraction of a second
  and scrolls without stutter; nothing waits on the disk on the main
  thread;
- **keyboard first**: every action has a key, typing filters, a jump list
  learns the folders you use, and the mouse is never required;
- **quiet**: the shell's colours, hairlines, monospace readouts and
  Phosphor icons for its buttons; no toolbar full of buttons.

Files, folders, places and drives are drawn with the desktop's icon theme,
in colour: the theme the XDG settings portal names
(`org.gnome.desktop.interface icon-theme`, else GTK's setting), with
GIO's icon for each MIME type (the theme's specific one, else its generic
one) and the theme's own icons for home and the user folders. They are
looked up in an icon theme of that name alone, as are the apps offered to
open a file with: the window's theme is the apps' Phosphor set, which
inherits Adwaita, and GTK would take its symbolic icons for these names.
The theme is read on a thread of its own while GTK starts.

It is a program of its own (`cmd/inua-files`), like `inua-games` and
`inua-assistant`, so `inua-apps` does not grow and the file manager can
be started, and replaced, on its own.

```
inua-files [-ui-dir DIR] [PATH|URI ...]   open windows on folders (files are selected)
inua-files -default                       make inua-files the desktop's file manager
inua-files -default=false                 give the job back (removes what -default wrote)
```

`inua-files trash:` opens the trash; `inua-files sftp://host/dir` connects
the remote disk first. A path may be relative to where it is typed, and
once inua-files is the file manager (`-default`), `open PATH` (xdg-open)
opens a folder in it: `open .`, `open ~/Downloads`.

## Window

```
┌──────────┬──────────────────────────────────────┬────────────┐
│ Files    │ ‹ › ↑  ~ / Code / inua        [≡][▦] │            │
│ PLACES   ├──────────────────────────────────────┤ preview    │
│ Home     │ NAME ↑              SIZE   MODIFIED  │            │
│ Desktop  │ ▸ cmd           12 items   14:02     │ name       │
│ …        │ ▸ docs           9 items   11:40     │ KIND       │
│ Trash    │   go.mod          3.1 kB   Oct 3     │ SIZE       │
│ BOOKMARKS│   README.md      14.2 kB   Oct 14    │ MODIFIED   │
│ Code     │                                      │ …          │
│ DEVICES  │ /flt · 23 items · 2 sel. · 9 GB free │            │
│ USB    ⏏ │ Copying photo.jpg ━━━━━━  38 MB/s  ✕ │            │
│ REMOTE   │                                      │            │
│ ◉ ▥ ⓘ  ⌘ │                                      │            │
└──────────┴──────────────────────────────────────┴────────────┘
```

| Part | What | Key |
| --- | --- | --- |
| Sidebar (left) | Its toggle at the top, as Mail's (hidden, the toggle moves to the left pane's head); places and the trash, bookmarks, devices and remote disks; at its foot, framed toggles (as Calculator's) for hidden files, two panes and details, and the keys. A right click on an entry: open it in a new tab or window, and remove a bookmark, empty the trash, eject a drive, or disconnect or forget a remote disk. | F9 slides it in or out |
| Panes (centre) | One pane, or two side by side (commander style). In the list, a pane narrower than the name and the columns scrolls sideways, the column heads with it; the name keeps its width. Each has its own tabs, and each tab its own folder and history; a pane's view, order and selection are its folder's. With two panes, a status square starts each path: filled for the one the keys go to, hollow for the other, whose selection loses the accent. | F3 two panes; Tab the other pane |
| Tabs | A pane's tabs show above it once it has two: the folder's name, a button that closes it (a middle click too). | Ctrl+T, Ctrl+W, Ctrl+Tab |
| Details (right) | The selection: a thumbnail or the first lines of a text (see [Text preview](#text-preview)), its kind (MIME type), size, modified, opened and changed times, permissions, owner, where it is and where a link points; a folder's size and count, added up on a goroutine. Several selected: how many, and their size. In a window too narrow for it beside the panes it stays hidden, the toggle on, until the window widens. | Space, Ctrl+I, Alt+Enter |
| Path bar | The folder as buttons, one a segment (home as a house), each going to its folder; a middle click opens a segment's folder in a new tab, and a right click its menu, about that folder alone, the pane staying where it is: open it in a new tab or window or the other pane; make a folder or a file in it, paste into it, a terminal there, tidy it (not the root), copy its path, bookmark it (the trash's: a new tab, Empty Trash; the picker's: a new folder, the path). A folder that is not there any more is said so in the pane's status line. A click beside them, or the pencil, edits it, the next button copies it, and the last opens a terminal there. Ctrl+L turns it into an entry (`~` is home, Tab completes folder names); a file typed opens its folder with it selected; a remote disk's address (`sftp://host/dir`, `smb://server/share`, `s3://KEY@host/bucket`) connects it; `trash:` is the trash. | Ctrl+L, Esc back |
| Status | The count, the hidden count, the selection and its size, the filter typed, free space, and the zoom (− 100% +); a message for a moment after an action. The operation running shows under the panes, with its speed, time left and a cancel button. | |

### Views

A segmented control in the head (as GPG's Text | Files), with a list, a
grid and a columns icon, chooses the view:

| View | What | Key |
| --- | --- | --- |
| List | Rows: icon and name, then the columns chosen: size (a folder's item count), modified, created, opened, kind, permissions, owner, group (size, modified and kind at first). A column head sorts by it, again reverses it; a right click on the heads, or the button at their end, turns columns on and off, for every folder. The heads and the rows share fixed widths, so they line up. | Ctrl+1 |
| Grid | Icons, thumbnails for images, names below. | Ctrl+2 |
| Columns | As Finder's: a column for each folder of the path, from the first below the root (`/home/me/a/b`: `/home`, `/home/me`, `a` and `b`), the folder itself last; in the columns before it the next folder of the path is marked, and after it the folder selected shows its contents. The last column is the folder's own view, so selecting, keys and menus are as in the list; a click in another column goes to its folder with that item selected (a middle click opens a folder there in a new tab), → goes into the folder selected, ← to the one above. Browsing keeps the columns and their order from folder to folder; a search shows its results in the list. The columns before and after are read each time they show and do not follow changes. | Ctrl+3 |

All are GTK's recycling `ListView` and `GridView`: only the rows on
screen have widgets, so the size of a folder does not matter. Each folder
remembers its view and order (kept in `~/.local/share/inua/files.json`,
the last 500 folders); a new folder takes the last one used. The zoom
(Ctrl++, Ctrl+−, Ctrl+0, Ctrl with the wheel, or the status line's
buttons) goes from 80% to 150%: rows, text and icons, in every window.

Order: by name (natural: `file2` before `file10`, case folded), size,
modified or kind; folders first. Hidden files (dot files and those named in
a folder's `.hidden`) show with Ctrl+H.

### Text preview

A plain-text file selected alone (a type GIO counts as `text/plain`,
which takes in CSV, logs and source code, or a name ending in `.txt`,
`.text`, `.csv`, `.tsv`, `.log`, `.jsonl` or `.ndjson`) shows its start
in the details, read on a goroutine and shown as plain text, in mono,
never as markup; CSV is shown as it is written, not as a table. The read
stops at **200 lines or 64 KiB, whichever comes first**: no more than
64 KiB is ever read, however long a line is, and not a byte past the
200th line's end is read either: no read asks for more bytes than lines
are still wanted. A preview cut short says so under it ("Only the first
200 lines are shown.", "Only the first 64 KiB are shown."). Whether more
follows either limit is told by the file's size rather than by reading
on, so a file whose size says nothing (those in `/proc`) counts as cut
when it reaches one. A character the byte limit cuts in two is left out.

Instead of text the note says that the file is empty, that it is not
UTF-8 text (a NUL byte or a sequence that is not UTF-8 in what was read:
binaries, UTF-16, Latin-1), or, in `danger`, that it is gone or could not
be read. Only regular files are read, a link's target included: the file
is checked before it is opened and again on what was opened, and opened
without blocking, so a FIFO or a device, even one put in its place
meanwhile, is never waited on or read. Another selection, or closing the
window, drops a read still out, so an earlier file's text never replaces
the current one.

Thumbnails follow the freedesktop thumbnail standard
(`~/.cache/thumbnails/large`, `Thumb::URI` and `Thumb::MTime`), so the
ones Nautilus made are used and the ones made here serve other apps too.
JPEG, PNG, GIF, WebP, BMP and TIFF are decoded in Go (JPEGs at a reduced
size through libjpeg-turbo when it is installed), three at a time, and
turned by their EXIF orientation.

## Keys

Moving and opening

| Key | Does |
| --- | --- |
| ↑ ↓ ← → / Home End / PgUp PgDn | Move (the arrows step through the grid's cells; in the columns ← and → go to the folder above and into the folder selected) |
| Enter | Open: a folder in this pane, a file with its app; several selected, all of them |
| Shift+Enter | Open with… (choose the app) |
| Middle click | A folder in a new tab, behind this one |
| Backspace, Alt+↑ | The parent folder, with the folder left selected |
| Alt+← / Alt+→ | Back / forward |
| Alt+Home | Home |
| Alt+1 … Alt+9 | The sidebar's first nine entries |
| Ctrl+L | Type a path or a remote disk's address |
| Ctrl+P | Jump: the folders you used, by frecency (as zoxide), fuzzy matched |
| Ctrl+F | Search below this folder: names, fuzzy, results as they are found, the best first when done; Esc leaves |
| Letters | Filter this folder as you type (fuzzy; the folder keeps its order and the best match is selected); Backspace takes a letter off, Esc clears |

Selection and changes

| Key | Does |
| --- | --- |
| Shift+arrows, Ctrl+click, Shift+click, a rubber band | Select |
| Ctrl+A / Ctrl+Shift+A / Ctrl+Shift+I | Select all / none / invert |
| Ctrl+C / Ctrl+X / Ctrl+V | Copy / cut / paste, interchanged with other file managers (`x-special/gnome-copied-files`, `text/uri-list`) |
| Ctrl+Shift+C | Copy the paths as text (nothing selected: the folder's) |
| F2 | Rename (the name without its extension is selected) |
| Delete | Move to the trash (in the trash: delete for good) |
| Shift+Delete | Delete for good (asks first) |
| Ctrl+Shift+N / Ctrl+Alt+N | New folder / new empty file |
| Ctrl+Z | Undo the last operations, one at a time |
| F5 / F6 | Copy / move the selection to the other pane (one pane: F5 reloads) |
| Ctrl+D | Bookmark this folder, or remove its bookmark |
| Ctrl+Alt+T | A terminal here (the default terminal, else `xdg-terminal-exec`, `$TERMINAL`, kitty) |
| Menu, Shift+F10, right click | The selection's menu (or the folder's); on a path bar segment, the menu of its folder |

Window

| Key | Does |
| --- | --- |
| Ctrl+T / Ctrl+W | New tab (here) / close the tab; the last one closes the window |
| Ctrl+Tab / Ctrl+Shift+Tab, Ctrl+PgDn / Ctrl+PgUp | Next / previous tab |
| Ctrl+N / Ctrl+Q | New window / close it |
| Ctrl+1 / Ctrl+2 / Ctrl+3 | List / grid / columns |
| Ctrl+H | Hidden files |
| F3 / Tab | Two panes / the other pane |
| F9 / Space, Ctrl+I | Sidebar / details |
| Ctrl+R | Reload (folders follow their changes by themselves; this is for remote disks, whose changes on the server are not told) |
| Ctrl++ / Ctrl+− / Ctrl+0, Ctrl+wheel | Bigger / smaller / normal |
| Ctrl+? | The keys |

In a card (rename, a name taken, a question): Enter does what is
highlighted, Esc cancels, and a question's buttons take their first
letter (R replace, S skip, K keep both, M merge).

## Places

The sidebar is the shell's index column, as Pass and Tailscale have:

- **Places**: Home, then the XDG user folders that exist
  (`~/.config/user-dirs.dirs`: Desktop, Documents, Downloads, Music,
  Pictures, Videos), then the trash: the freedesktop trash (below; the
  picker leaves it out).
- **Bookmarks**: GTK's (`~/.config/gtk-3.0/bookmarks`), the list GTK's
  file chooser and Nautilus show, in the file's order; one that repeats
  a place above or another bookmark is not listed twice. Ctrl+D adds and
  removes them, as does an entry's menu. Without bookmarks (none yet,
  the last one removed, the file missing or unreadable) the section and
  its head are left out.
- **Devices**: the computer's root; then drives and partitions with a
  filesystem, from UDisks2 (`org.freedesktop.UDisks2` on the system bus):
  USB sticks, SD cards, other disks' partitions, LUKS volumes, removable
  ones first and each drive's together. Each shows its label (else its
  drive and size) and, mounted, its free space. Clicking an unmounted one
  mounts it (`Filesystem.Mount`; polkit asks in inua's dialog when it needs
  a password) and opens it; a locked LUKS volume asks for its passphrase
  first (`Encrypted.Unlock`, then the filesystem inside is mounted). The
  eject button moves the panes out of it, unmounts every filesystem of the
  drive, locks its containers, then ejects the media or powers the drive
  off, so it is safe to pull out; when something has a file open on it,
  the status line says so. Drives come and go as they are plugged
  (UDisks2's signals); a new removable one is announced in the status line.
  The system's own volumes (the root, swap, what UDisks2 hints to ignore)
  are left out. Other mounted filesystems (sshfs, NFS, rclone, a disk
  mounted by fstab), from `/proc/self/mountinfo`, follow; the mount table
  is followed through its `POLLPRI` mark.
- **Remote disks**: a server's folder or a bucket, mounted by
  [rclone](https://rclone.org) at `~/Remote/NAME`, so that every program
  reads and writes it as a local folder. *Remote disk…* sets one up: its
  kind (SFTP, SMB, WebDAV, FTP, S3, R2), then the fields that kind asks
  for, each labelled and saying what an empty one means (an SFTP password
  left empty signs in with the SSH keys, an SMB user left empty is a
  guest), the name offered from the server, the share or the bucket.
  *Test connection* (at the bottom left) tries the address and the
  secret, mounting and keeping nothing, and says in a notification
  whether it connects and, if not, why; Connect connects it and keeps it.
  While either works, its button says so and the form waits; a refused
  password, an unknown host key or a missing bucket is said on the form,
  to be put right there. *Edit…* in a disk's menu (a right click on its
  row) opens its setup again: a password left empty
  keeps the one in the keyring, and a disk changed while connected is
  disconnected first. The path bar and `inua-files` take
  the same disks as addresses:

  | Address | On |
  |---|---|
  | `sftp://user@host[:port]/folder` | SSH. Signs in with the SSH agent's keys, else asks for a password; the host's key must be in `~/.ssh/known_hosts` (connect once with `ssh` to check it) |
  | `smb://[user@]server/share/folder` | Windows shares, Samba; a guest without a user |
  | `ftp://`, `ftps://[user@]host/folder` | FTP; anonymous without a user |
  | `dav://`, `davs://[user@]host/folder` | WebDAV (Nextcloud, ownCloud …), over HTTP or HTTPS |
  | `s3://KEY@host/bucket/folder` | S3: `s3.amazonaws.com` or `s3.REGION.amazonaws.com`, or any S3 server (MinIO, Backblaze B2, Wasabi …) by its host |
  | `r2://KEY@ACCOUNT/bucket/folder` | Cloudflare R2, by the account's ID |

  KEY is the access key's ID; the password, or the secret key, is asked
  for and kept in the keyring (the Secret Service, by the address), never
  in the address or the settings. A disk is named after its share, bucket
  or server, and is listed under *Remote disks* with its free space while
  it is mounted; its eject button disconnects it, its menu also forgets it
  (its address and secret; nothing on the server changes).

  Connecting first lists the folder with the address, so that a refused
  password, an unknown host key or a missing bucket is said at once; then
  rclone mounts it as a systemd user unit of its own
  (`inua-remote-NAME.service`), which keeps it mounted when the window
  closes and stops it when the session ends; its log is
  `$XDG_RUNTIME_DIR/inua/remote/`. The config rclone is started with,
  which holds the secret (obscured, for the protocols whose passwords
  rclone takes so), is in the runtime folder (in memory, the user's alone)
  only while rclone starts; the secret is never on a command line.
  Files written are kept in rclone's cache
  (`~/.cache/inua/remote/`, by the address) until they are sent, so that
  an editor saves as on a local disk, even to a bucket; what was not sent
  when rclone stopped is sent the next time the same address is mounted.
  It needs `rclone` (1.60 or later), `fusermount3` (fuse3) and systemd's
  `systemd-run`. A remote disk's folder is followed only for the changes
  made through it: the server's own are seen when the folder is read
  again (Ctrl+R), once rclone's listing of it is a minute old. Buckets have no real folders: renaming a
  folder in one copies every object in it.

  Disks connected from a terminal or fstab (sshfs, NFS, CIFS, rclone) are
  listed with the devices, from the mount table.

## The trash

The freedesktop Trash specification: an item goes to the home trash
(`~/.local/share/Trash`) when it is on that filesystem, else to the trash
at the top of its own mount (`.Trash/UID` when the administrator made
one, else `.Trash-UID`); `info/NAME.trashinfo` records where it was and
when, so other file managers can restore it too. Names are reserved by
making the info file first, exclusively.

The trash shows every trash there is, newest first, each item by the name
it had, where it came from and when it was trashed. Restore puts items
back (making their folder if it went; refusing when something took the
name), Delete removes them for good, and *Empty Trash* empties every
trash after asking.

## Operations

Copying, moving, trashing, deleting, renaming and making run on a
goroutine, one queue a window, so the window can be used meanwhile. One
that takes more than a quarter second shows under the panes: what it is
doing, a bar, the speed and the time left, and a cancel button. A window
closed while its operations run hides until they end, and the app stays
until then.

- Copying moves each 16 MB chunk with the kernel's `copy_file_range` (a
  reflink on Btrfs and XFS), keeps modes and times, copies links as links,
  and writes each file to a temporary name beside it first, so a cancelled
  or failed copy leaves no half file.
- Moving within a filesystem is a rename (`RENAME_NOREPLACE`, so nothing
  is overwritten by a race); across filesystems it is a copy, then the
  removal of what was copied.
- A name that is taken asks: *Replace*, *Skip*, *Keep both* (`name
  (2).ext`), or for folders *Merge*, which asks again inside; *Do the same
  for the rest* answers the others of that kind.
- Pasting or dropping something into the folder it came from makes a copy
  beside it: `name (copy).ext`, then `name (copy 2).ext`.
- One item failing does not stop the rest; what could not be done is
  listed at the end.
- Undo puts back what can be put back: a rename or a move (moved back), a
  trashing (restored), something made or copied (trashed). Deleting for
  good cannot be undone and asks first.
- Drag and drop: drag items to another pane, onto a folder, or to another
  app (as `text/uri-list`); drop files from other apps. Within a
  filesystem a drop moves, across filesystems it copies; Ctrl copies,
  Shift moves.
- Cut files are dimmed until they are pasted; after a cut and paste the
  clipboard is spent, as in other file managers. Files another app put on
  the clipboard are read with `wl-paste`.

## Archives

**Compress…**, in the selection's menu, makes one archive of the items
selected, beside them, on a card: its name (the item's own for one,
*Archive* for more, numbered when taken) and its format, Zip, 7z,
tar.gz, tar.xz or tar.zst. Folders go in whole, with their modes, times
and links. Under **Advanced**:

- **Compression**: *Fast*, *Normal* or *Smallest* (deflate 1, 6 or 9 for
  Zip and gzip; xz's dictionary of 1, 8 or 64 MB; zstd's fastest,
  default or best; 7z's `-mx` 1, 5 or 9).
- **Password**, typed twice: a Zip or 7z locked with AES-256. A locked Zip
  opens in 7-Zip and today's file managers, not in old unzip programs.
  tar archives have no password.
- **Lock the file names too** (7z): without the password not even the list
  of files shows.
- **Leave out hidden files**: the names starting with a dot inside the
  folders (`.git`, `.cache`); an item chosen itself goes in.

The format and the options chosen last are offered first; a password
never is, and is never kept.

**Extract here**, in the menu of archives selected, takes each apart beside
itself. An archive whose items all sit in one folder, or that is one file,
gives that folder or file; any other gives a folder named as the archive.
A name taken is numbered (`photos (2)`), never merged into or replaced. A
locked archive asks for its password on a card, and asks again when it is
not the one.

Files makes and reads Zip and tar (with gzip, bzip2, xz or zstd:
`.tar.gz`, `.tgz` and the like) itself, and one file compressed alone
(`.gz`, `.bz2`, `.xz`, `.zst`). The `7z` program (7zip) makes 7z and
locked Zip archives, and reads 7z, rar and locked Zip; without it those
say so. A password goes to 7z on its input, never on its command line,
where any program on the computer could read it.

Both run as operations: progress, cancelling, and undo, which trashes
the archive made or what was extracted. Extracting works in a hidden
folder beside the result and moves it into place at the end, so a
cancelled or failed one (a wrong password too) leaves nothing. An item
named to land outside the folder (an absolute name, `..`), or written
through a link the archive made, stops the extraction; links are made
last, after every file, so none is written through. Set-user-ID bits are
dropped, and folders stay the user's to open.

## Renaming many

F2 (or **Rename N items…** in the menu) with more than one item selected
renames them together, on a card that shows every new name before it is
given. Four ways, the one used last offered first:

- **Replace text**: what is found in each name becomes the replacement
  (empty removes it); in any case unless *Match case*, and as a regular
  expression when asked, `$1` in the replacement being its first group.
- **Add text**: before or after each name.
- **Number**: a name and a number, in the order the pane shows them,
  from the number chosen: `Beach 1` … `Beach 5`, with as many digits as
  the last needs (`Beach 01` … `Beach 12`); alone, just the number.
- **Change case**: lowercase, UPPERCASE or Title Case.

A file keeps its extension (`.tar.gz` counts as one); a folder's whole
name is renamed. A new name that another file outside the selection has,
that two items would get, or that is no name (empty, with `/`) is marked
in the preview, and nothing is renamed until none is. Names the items
trade among themselves (`a` to `b`, `b` to `a`) are fine: all move to
hidden names first, then each to its own. It is one operation, undone in
one step.

## Opening files

A file opens with the app chosen for its type: its MIME type, guessed
from its name and first bytes; the choice is the desktop's, kept by GIO
in `~/.config/mimeapps.list`, so the launcher, browsers and other file
managers agree. A type with no app chosen asks. *Open with…*
(Shift+Enter, or the menu) is the app's own card: the apps for the type,
the one chosen first, the last used next, each with its icon; typing
finds any other app. *Always open … with it*, on at first, keeps the
choice for the type; off, the app is only put first next time. Several
files open together, one app for each type. The app starts from its
desktop entry with the files in its `Exec` (`%f`, `%F`, `%u`, `%U` as the
Desktop Entry specification says; `internal/desktop`), in the files'
folder; a file is opened by its type, never run, so scripts open in the
editor.

## Terminal

The head's terminal button, Ctrl+Alt+T and the folder's menu open a
terminal in the folder: the one chosen in Settings, Default Apps (the
launcher's terminal) started there, else `xdg-terminal-exec
--dir=FOLDER` (the terminal chosen for the desktop), else `$TERMINAL`
started there, else kitty with `--directory`. The folder is passed rather than only started in, as a
terminal that serves its windows from one process (GNOME Terminal) would
open in its own.

## Tidy with AI

**Tidy with AI…**, in the folder's menu (or a selected folder's), hands the
folder to inua's assistant (`inua-assistant --ask`): it looks at the files
(names, kinds, sizes, dates, the first lines of a text file whose name
says little), makes a plan with Mister Morph's `plan_create` and shows it,
then moves the files with `bash`. The question it is given sets the rules:
only inside the folder, its own files into new or existing folders there,
nothing deleted or overwritten (`mkdir -p`, `mv -n`), hidden files and the
subfolders' contents left alone, the moves in one command, and their
reverse written to `.tidy-undo.sh` in the folder, so `sh .tidy-undo.sh`
there undoes it. It needs the assistant on and its Run commands
(`bash`) in Settings → AI; Files says which is missing. The assistant has
`plan_create` whenever it has any of Mister Morph's tools.

## Window

Files is a normal window, tiled as other apps are: the light apps float,
and `inua.conf` (written by inua) tiles Files after them
(`windowrule inua-files … tile = yes`).

## Instance and D-Bus

One process serves every window. It owns `io.github.lyricat.Inua.Files`
on the session bus and asks for `org.freedesktop.FileManager1`, queued
behind a file manager that holds it (Nautilus), so it takes over when that
one leaves. Browsers ("Show in folder"), the screenshot drawer, Viewer and
the settings window call it:

| Method | Does |
| --- | --- |
| `ShowFolders(uris, startup_id)` | A window on each folder |
| `ShowItems(uris, startup_id)` | A window on each folder of the items, with them selected |
| `ShowItemProperties(uris, startup_id)` | As ShowItems, with the details shown |

`inua-files PATH` started while it runs hands the paths to the running
process, through that interface on its own name, and leaves; so a new
window opens at once. The process ends with its last window.

`inua-files -default` makes it the desktop's file manager:

- `xdg-mime default io.github.lyricat.Inua.Files.desktop inode/directory`;
- a D-Bus service file for `org.freedesktop.FileManager1` in
  `~/.local/share/dbus-1/services`, which outranks Nautilus's in
  `/usr/share`, so "Show in folder" starts inua-files when it is not
  running.

`-default=false` removes the service file and gives `inode/directory`
back to Nautilus when it is installed. The sample `hyprland.conf` binds
Super+E to `$files`, which is `inua-files`, and the default dock pins
Files.

## Architecture

As the other light apps: the logic without GTK, tested; the view thin.

| Package | What |
| --- | --- |
| `internal/apps/files` | Entries (`ReadDir`: names, then `statx`, which gives the creation time too, on up to eight goroutines for big folders; links' targets; `.hidden`), natural order without allocating, fuzzy matching and the filter, history, the jump list (frecency), settings, places (XDG user folders, GTK bookmarks read and written), mounts (`mountinfo`, `statfs`, the table followed), the trash, operations (copy, move, link, trash, restore, delete, rename, make; conflicts, progress, cancelling, undo), the clipboard's formats, searching below a folder on several goroutines, watching a folder (inotify, a burst of changes merged into one within 80 ms, at most 600 ms late), thumbnails |
| `internal/apps/files/disks` | UDisks2: volumes and their drives, mounting, unmounting, unlocking, locking, ejecting and powering off, followed by its signals |
| `internal/apps/files/remote` | Remote disks: addresses, rclone's config for each kind, connecting (a listing first, then `rclone mount` as a systemd user unit) and disconnecting, why rclone failed, the secrets in the Secret Service |
| `internal/apps/files/fm1` | `org.freedesktop.FileManager1`: serving it, and handing paths to a running instance |
| `internal/filesui` | The windows: sidebar, panes and their tabs, list (its columns), grid and columns (`trail.go`), details, path bar, cards (Open With among them), menus, drag and drop, the clipboard, operations' progress, zoom, GIO for apps by type, remote disks' cards |
| `cmd/inua-files` | Arguments, handing over to a running instance, `-default` |
| `ui/files.blp`, `ui/filespane.blp`, `ui/style.css` | The window, a pane, and their look (`window.files`) |

**The views' model.** A pane's entries live in Go, in the folder's order.
The views' model is a `GtkStringList` of the names shown, under a
`GtkMultiSelection`; a row binds to the entry at its position, so binding
is a slice index and a few labels. A reload puts in the model only the
stretch that changed (the common start and end stay), so the view keeps
its place, and the selection is put back by name. A thumbnail or a
folder's count arriving changes the row's widgets directly, never the
model, so the keyboard's place does not move.

**Signals.** purego makes a native trampoline for every Go function
connected, never freed and at most 2000. The file manager runs all
session and opens windows again and again, so its windows connect no
closures: one handler of each kind serves the process (`hub.go`) and
finds the Go function by the object that emitted the signal; a window
forgets its registrations when it closes.

**Threads.** Reading a folder, watching it, searching, counting,
thumbnails, the details' facts, drives and every operation run on
goroutines and come back with `gtkx.Post`; a pane drops a result meant for
a folder it has left (each load has a generation).

**Speed**, measured with `go test -bench` in `internal/apps/files` on a
16-thread machine, warm cache: reading and sorting a folder of 20 000
files, 21 ms (so about 0.1 s for 100 000); filtering 100 000 names by
three letters as one types, 7 ms.

## Tests

`internal/apps/files` is tested in temporary folders: natural order,
sorting, scoring and filtering, reading folders (links, broken links,
`.hidden`, the parallel path), history and the jump list, places and
bookmarks, `mountinfo`, the trash (trashing, the info
file, restoring, emptying), every operation with its conflicts, undo,
cancelling, the clipboard's formats, watching, searching, thumbnails, archives
(each format made and taken apart again, many items into a folder, one
file compressed, names that would go outside or through a link, a
cancelled extraction, 7z when it is installed, a password none, wrong and
right for Zip and 7z, the password never on 7z's command line, each
level, hidden files left out), and renaming many (each
way's names, the plan's problems, names traded among the items, a name
taken meanwhile, undo in one step).
`disks` runs against a fake UDisks2 and `fm1` against a second process
on a private bus (`internal/dbustest`). `remote` runs against stand-ins
for rclone, systemd and FUSE that record what they were given (the
secret never on a command line, the config gone once rclone started),
and keeps secrets in inua-keyring's Secret Service on a private bus. The window was checked by hand in
a nested Hyprland: moving and opening by keys, the filter, two panes,
the grid's thumbnails, details, rename, a new folder, the menu, copy and
paste, a conflict, search, the location entry, the trash, undo and the
keys.

## Not yet

In order: thumbnails of videos
and PDFs through the system's thumbnailers; reordering, renaming and
dropping on the sidebar's bookmarks (removing them is in their menu); asking a share's password in the
app's own card rather than GTK's dialog; redo; finding servers on the network (`network:///`, mDNS) rather than
typing an address. Recent and starred files are
left out on purpose: the jump list covers folders, and the launcher finds
files.
