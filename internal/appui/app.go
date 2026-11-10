// Package appui runs inua's light apps: those of inua-apps (internal/lightui)
// and those of other programs (Files, Viewer, Mail, the terminal, the
// assistant, the games). Each runs as its own process with its own
// application ID, so the launcher, the dock and the compositor see it as
// an app, but in the shell's look: the theme and style from inua's settings, followed as
// they change, and the shell's icons.
package appui

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/compositor"
	_ "github.com/lyricat/inua/internal/compositors" // the backends Detect knows
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/portalconf"
	"github.com/lyricat/inua/internal/rundir"
	"github.com/lyricat/inua/internal/style"
	"github.com/lyricat/inua/internal/uikit"
	layouts "github.com/lyricat/inua/ui"
)

// IDBase prefixes the apps' application IDs; the shell's is IDBase itself.
const IDBase = "io.github.lyricat.Inua"

// Apps are the light apps by command name, as registered: their
// application ID suffix and window title. A many-windowed app runs once
// for each start (as Viewer, registered by inua-viewer, does); the others
// bring their window forward when started again.
var Apps = map[string]struct {
	ID, Title string
	make      func() View
	many      bool
}{}

// View is a light app's window and logic.
type View interface {
	// Build makes the window from its layout; called once, on activate.
	Build(c *Ctx) *gtk.ApplicationWindow
	// ThemeChanged runs after the theme's colours change.
	ThemeChanged()
}

// Opener is a View that takes the arguments of its first start.
type Opener interface{ Open(args []string) }

// Pager is a View with pages that a start's first argument opens:
// `inua-apps NAME PAGE`.
type Pager interface{ ShowPage(name string) }

// Preparer is a View with work to begin before the application starts,
// such as on a thread of its own while GTK starts: Prepare runs once, on
// the main thread, before Build.
type Preparer interface{ Prepare(c *Ctx) }

// Themed is a View that keeps a look of its own whatever the shell's
// theme: the name of a built-in theme ("washi"), or "" for the shell's.
type Themed interface{ Theme() string }

// opensFiles are the apps (by name) that take files while they run:
// started again with files, the running one gets them (its Opener).
var opensFiles = map[string]bool{}

// RegisterOpener is Register for a View that opens files (an Opener): a
// start with files hands them to the app already running.
func RegisterOpener(name, id, title string, make func() View) {
	Register(name, id, title, make)
	opensFiles[name] = true
}

// Register adds a light app, for Run.
func Register(name, id, title string, make func() View) { register(name, id, title, make, false) }

// RegisterMany adds a light app whose every start runs on its own, as
// Viewer's do (inua-viewer); an app that wants one process keeps to it itself
// (inua-files does, through D-Bus).
func RegisterMany(name, id, title string, make func() View) { register(name, id, title, make, true) }

func register(name, id, title string, make func() View, many bool) {
	Apps[name] = struct {
		ID, Title string
		make      func() View
		many      bool
	}{id, title, make, many}
}

// Ctx is what the views share: the application, the layouts and the
// theme.
type Ctx struct {
	App *gtk.Application
	// StartHidden keeps a background app's first window out of the way.
	StartHidden bool
	UIDir       string // read layouts from here instead of the embedded copy
	Colors      map[string]string
	refs        gtkx.Scope

	cfgPath   string
	cfgMod    time.Time
	css       *gtk.CssProvider
	userCSS   *gtk.CssProvider
	motionCSS *gtk.CssProvider
	win       *gtk.ApplicationWindow
	v         View
	onTick    glib.SourceFunc
	open      func(gio.Application, uintptr, int32, string)
	activate  func(gio.Application)
	iconTheme string // the apps' icon theme in use, "" before loadIcons
	gtkIcons  string // GTK's icon theme, the desktop's, before loadIcons set the apps'
	desktop   string // DesktopIcons, once asked
}

// Run runs the named app until its window closes. A second start of the
// same app brings its window forward instead, unless the app has many.
// args are a page to open, or files for an opener.
func Run(name string, args []string, uiDir string) int {
	a, ok := Apps[name]
	if !ok {
		names := make([]string, 0, len(Apps))
		for n := range Apps {
			names = append(names, n)
		}
		fmt.Fprintf(os.Stderr, "unknown app %q (%s)\n", name, strings.Join(names, ", "))
		return 2
	}
	return RunView(name, IDBase+"."+a.ID, a.Title, a.make(), a.many, args, uiDir)
}

// RunView runs v as the app named name, with its application ID and
// title, until its window closes: for an app known only once started, as
// a workshop app is.
func RunView(name, appID, title string, v View, many bool, args []string, uiDir string) int {
	cfg, _, _ := config.Load(config.DefaultPath()) // a broken file leaves the defaults
	i18n.Init(cfg.Language)
	return run(name, appID, title, v, many, args, uiDir)
}

// run runs view v as the app named name, with its application ID and
// title, until its window closes.
func run(name, appID, title string, v View, many bool, args []string, uiDir string) int {
	flags := gio.GApplicationDefaultFlagsValue
	if many {
		flags = gio.GApplicationNonUniqueValue
	}
	files := opensFiles[name]
	if files {
		flags |= gio.GApplicationHandlesOpenValue
	}
	quietA11y()
	restore := gtkx.ForcePortals(portalconf.PickerOn())
	// The apps' icon theme is in place before the application starts:
	// GtkApplication's start looks its icon up, which reads the whole
	// chain of the icon theme set then, and a change after reads the new
	// chain again. GTK's start already reads the desktop's theme on a
	// thread of its own, and setting the apps' waits for it to finish: the
	// view's preparation begins and the style is parsed meanwhile.
	gtk.Init()
	restore()
	c := &Ctx{UIDir: uiDir, cfgPath: config.DefaultPath(), v: v}
	var icons gobject.Value
	icons.Init(gobject.TypeStringVal)
	gtk.SettingsGetDefault().GetProperty("gtk-icon-theme-name", &icons)
	c.gtkIcons = icons.GetString()
	icons.Unset()
	if p, ok := v.(Preparer); ok {
		p.Prepare(c)
	}
	c.loadStyle()
	c.loadIcons()
	app := gtk.NewApplication(appID, flags)
	c.App = app
	opened := false // the files of the first start went to open already
	c.activate = func(gio.Application) {
		if c.win != nil { // started again: the window comes to the user
			bringHere()
			c.win.Present()
			return
		}
		c.win = c.v.Build(c)
		c.win.SetApplication(app)
		c.win.SetTitle(i18n.T(title))
		c.win.AddCssClass("app")
		c.win.AddCssClass("settings") // the settings window's controls and sections
		c.win.AddCssClass(name)
		if !c.StartHidden {
			c.win.Present()
		}
		if o, ok := c.v.(Opener); ok && !opened {
			o.Open(args)
		} else if p, ok := c.v.(Pager); ok && len(args) > 0 {
			p.ShowPage(args[0])
		}
		c.onTick = func(uintptr) bool {
			c.followConfig()
			return true
		}
		glib.TimeoutAdd(2000, &c.onTick, 0)
	}
	app.ConnectActivate(&c.activate)
	argv := []string{os.Args[0]}
	if files {
		// The files go through GApplication, so a second start's reach the
		// app already running: open, here or there.
		argv = append(argv, args...)
		c.open = func(_ gio.Application, list uintptr, n int32, _ string) {
			var paths []string
			// GFile *files[n], C memory.
			for _, ptr := range unsafe.Slice((*uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&list))), int(n)) {
				f := gio.FileBase{Ptr: ptr}
				if p := f.GetPath(); p != "" {
					paths = append(paths, p)
				} else if u := f.GetUri(); strings.HasPrefix(strings.ToLower(u), "mailto:") {
					paths = append(paths, u) // Mail's links
				}
			}
			first := c.win == nil
			opened = true
			c.activate(app.Application)
			if o, ok := c.v.(Opener); ok {
				o.Open(paths)
			}
			if !first {
				bringHere()
				c.win.Present()
			}
		}
		app.ConnectOpen(&c.open)
	}
	return int(app.Run(int32(len(argv)), argv))
}

// ReadUI is a layout or stylesheet from ui/.
func (c *Ctx) ReadUI(name string) string {
	if c.UIDir != "" {
		if b, err := os.ReadFile(filepath.Join(c.UIDir, name)); err == nil {
			return string(b)
		}
	}
	b, err := layouts.FS.ReadFile(name)
	if err != nil {
		log.Fatalf("ui: %v", err)
	}
	return string(b)
}

// Load opens a layout; objects taken with Get live as long as the app.
func (c *Ctx) Load(name string) *gtkx.Builder {
	b, err := gtkx.Load(c.ReadUI(name), &c.refs)
	if err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	return b
}

// Config is inua's settings as the file has them now, else the defaults.
func (c *Ctx) Config() config.Config {
	cfg, _, err := config.Load(c.cfgPath)
	if err != nil {
		return config.Default()
	}
	return cfg
}

// loadStyle loads the theme, the shell's style and the user's.
func (c *Ctx) loadStyle() {
	cfg := c.Config()
	if c.css == nil {
		c.css, c.userCSS, c.motionCSS = gtk.NewCssProvider(), gtk.NewCssProvider(), gtk.NewCssProvider()
		d := gdk.DisplayGetDefault()
		gtk.StyleContextAddProviderForDisplay(d, c.css, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
		gtk.StyleContextAddProviderForDisplay(d, c.userCSS, uint32(gtk.STYLE_PROVIDER_PRIORITY_USER))
		gtk.StyleContextAddProviderForDisplay(d, c.motionCSS, uint32(gtk.STYLE_PROVIDER_PRIORITY_USER+1))
	}
	theme := cfg.Style.ForApps()
	if t, ok := c.v.(Themed); ok && t.Theme() != "" {
		theme = t.Theme() // an app with a look of its own
	}
	themeCSS := cfg.ThemeCSS(theme, c.ReadUI)
	c.Colors = config.ThemeColors(themeCSS)
	gtk.SettingsGetDefault().SetPropertyGtkEnableAnimations(config.AnimationEnabled(cfg.Style.Animations))
	css := themeCSS + style.Glass(cfg.Style.Transparency) + style.ScaleText(c.ReadUI("style.css"), cfg.Style.TextScale) + style.Corners(cfg.Style.Radius)
	if runtime.GOOS == "darwin" {
		css = style.SystemFonts(css) // A Mac has none of the styles' fonts.
	}
	c.css.LoadFromString(css)
	uikit.Radius = cfg.Style.Radius // the menus' corners
	// An app's icon in a workshop app, in the dock's shape.
	uikit.SetIconShape(uikit.IconShapeFromName(cfg.Style.IconShape))
	userCSS := ""
	if p := cfg.CSSPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			userCSS = string(b)
		}
	}
	c.userCSS.LoadFromString(userCSS)
	if config.AnimationEnabled(cfg.Style.Animations) {
		c.motionCSS.LoadFromString("")
	} else {
		c.motionCSS.LoadFromString(`* { transition: none !important; animation: none !important; }`)
	}
	if st, err := os.Stat(c.cfgPath); err == nil {
		c.cfgMod = st.ModTime()
	}
}

// ReloadStyle loads the style again, for a Themed view whose theme
// changed, and has the view take the new colours.
func (c *Ctx) ReloadStyle() {
	c.loadStyle()
	c.v.ThemeChanged()
}

// followConfig reloads style and icons when settings change.
func (c *Ctx) followConfig() {
	st, err := os.Stat(c.cfgPath)
	if err != nil || st.ModTime().Equal(c.cfgMod) {
		return
	}
	c.loadStyle()
	c.loadIcons()
	c.v.ThemeChanged()
}

// loadIcons draws the apps' own icons from the Phosphor set inua ships
// (ui/icons), with the user's set over them when style.icons names one;
// "system" and an installed theme still mean Phosphor here. The apps
// write the set as an icon theme of their own, which inherits Adwaita for
// any icon the set lacks: inheriting the desktop's theme made every start
// read that theme's chain, a quarter of a second for a large one such as
// Numix-Circle.
// What shows the desktop's icons (Files' files and apps) looks them up in
// DesktopIcons.
func (c *Ctx) loadIcons() {
	cfg := c.Config()
	overlay := cfg.IconSetDir()
	// Named by the set, so an app of another inua build (another set)
	// running at the same time does not write its icons over these.
	name := appIconTheme + "-" + iconSetTag()
	if overlay != "" {
		name = "inua-apps-icons-" + cfg.Style.Icons + "-" + iconSetTag()
	}
	if overlay == "" && c.iconTheme == name {
		return // the built-in icons do not change
	}
	dir, err := rundir.Sub("app-icons")
	if err == nil {
		err = c.writeIconTheme(dir, name, overlay)
	}
	if err != nil {
		log.Printf("icons: %v", err)
		return
	}
	if c.iconTheme == name {
		// The set's files may have changed: make GTK read them again.
		gtkx.SetStringSetting("gtk-icon-theme-name", "hicolor")
	}
	gtkx.SetStringSetting("gtk-icon-theme-name", name)
	if c.iconTheme == "" {
		gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault()).AddSearchPath(dir)
	}
	c.iconTheme = name
}

// DesktopIcons is the icon theme of apps and files: style.app_icons when
// it names one, else the desktop's, the XDG settings portal's, else
// GTK's, else Adwaita.
func (c *Ctx) DesktopIcons() string {
	if name := c.Config().Style.AppIconTheme(); name != "system" && name != "" {
		return name
	}
	if c.desktop == "" {
		c.desktop = cmp.Or(desktopIconTheme(), c.gtkIcons, "Adwaita")
		if strings.HasPrefix(c.desktop, "inua-") {
			c.desktop = "Adwaita"
		}
	}
	return c.desktop
}

// quietA11y turns GTK's accessibility off (GTK_A11Y=none) while nothing
// uses it: no screen reader and no other assistive program, as the
// session's AT-SPI bus says (org.a11y.Status), the way browsers decide.
// GTK keeps every widget's accessible state current all the same, and
// each change to a list costs in proportion to the list's length: Files
// filled a folder of 457 items in 1.4 s with it and 0.6 s without. A
// GTK_A11Y already set, and an answer not had, leave it on.
func quietA11y() {
	if os.Getenv("GTK_A11Y") != "" {
		return
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	status := conn.Object("org.a11y.Bus", "/org/a11y/bus")
	for _, p := range []string{"IsEnabled", "ScreenReaderEnabled"} {
		var v dbus.Variant
		err := status.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.a11y.Status", p).Store(&v)
		if on, ok := v.Value().(bool); err != nil || !ok || on {
			return
		}
	}
	os.Setenv("GTK_A11Y", "none")
}

// desktopIconTheme is the icon theme the desktop's settings choose, read
// from the XDG settings portal (org.gnome.desktop.interface icon-theme),
// or "" without one.
func desktopIconTheme() string {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return ""
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var v dbus.Variant
	err = conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").
		CallWithContext(ctx, "org.freedesktop.portal.Settings.ReadOne", 0, "org.gnome.desktop.interface", "icon-theme").Store(&v)
	if err != nil {
		return ""
	}
	name, _ := v.Value().(string)
	return name
}

// appIconTheme names the apps' icon theme.
const appIconTheme = "inua-phosphor-apps"

// iconSetTag names the built-in icon set: a sum of its icons' names.
func iconSetTag() string {
	entries, _ := layouts.FS.ReadDir("icons")
	h := sha256.New()
	for _, e := range entries {
		io.WriteString(h, e.Name()+"\n")
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

func (c *Ctx) writeIconTheme(dir, name, overlay string) error {
	entries, err := layouts.FS.ReadDir("icons")
	if err != nil {
		return err
	}
	t := style.IconTheme{
		Name:     name,
		Comment:  "Icons for inua's light apps (written by inua-apps)",
		Inherits: "Adwaita",
		Read:     func(file string) string { return c.ReadUI("icons/" + file) },
		Overlay:  overlay,
	}
	for _, e := range entries {
		t.Builtin = append(t.Builtin, e.Name())
	}
	return t.Write(dir)
}

// bringHere moves the app's window to the workspace in view, where the
// compositor places windows for us, rather than leaving it where it was
// opened.
func bringHere() {
	b, err := compositor.Detect()
	if err != nil {
		return // no compositor we know
	}
	placer, ok := b.(compositor.Placer)
	if !ok || !b.Capabilities().Has(compositor.CapPlacement) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := placer.BringHere(ctx, os.Getpid()); err != nil {
		log.Printf("bringing the window here: %v", err)
	}
}
