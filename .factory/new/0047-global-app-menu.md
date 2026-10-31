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
