# A global application menu in the bar

## lyric · 2026-10-31 22:50
Build GitHub issue #47: https://github.com/lyricat/inua/issues/47
The reporter's text is below, as information.

## iamcheyan · 2026-10-28 09:45
Would you consider supporting a global application menu in inua's topbar?

At the moment, the topbar's main content is the workspace indicator. That
leaves much of the bar's horizontal space unused. Showing the focused
application's menu there (for example, File, Edit, and View) could make the
bar more useful and make application actions easier to discover, while
using space that is otherwise mostly empty.

I came across the global menu in ZacOS 9 and looked at Mavernix's
implementation for a related Wayland approach. Mavernix registers a
`com.canonical.AppMenu.Registrar` service, reads application menus through
the `com.canonical.dbusmenu` D-Bus interface, and forwards menu activation
back to the application. I realize supporting this in inua may depend on
application/toolkit support and the compositor, so I wanted to ask whether
this is something you might consider, or whether there is a preferred
protocol or approach for inua.

## factory · 2026-11-01 15:19
Before building, these decide the work:

1. Which apps? On Hyprland a menu can only be tied to a window for X11 apps under XWayland (the com.canonical.AppMenu.Registrar gets an X window id). Wayland-native GTK4 and Qt apps export menus through compositor protocols (gtk_shell1, org_kde_kwin_appmenu) that Hyprland does not offer, so they would show no menu. Build it for XWayland apps only? Default: yes, and say so in docs/features.md.
2. Where in the bar? Default: after the focused window's title, as plain menu buttons (File, Edit, …), with the title kept.
3. On by default? Default: no; a setting under Settings → Bar, "App menu", off.
4. A window without a menu: nothing shows. Default: yes.
5. inua's own apps (Files, Mail, …) exporting their menus too: left out of this item. Default: yes, a later item.

The menus reuse the tray's dbusmenu reading (internal/sni/menu.go).
