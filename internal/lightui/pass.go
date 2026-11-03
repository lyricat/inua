package lightui

import (
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/pass"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/uikit"
)

// masked stands for a hidden value.
const passMasked = "••••••••••"

// passResultsShown is how many of the search's results are listed.
const passResultsShown = 60

// The forms of the detail column.
const (
	formMoveCategory = "move-category"
	formMoveEntry    = "move-entry"
	formNew          = "new"
	formDelete       = "delete"
)

type passApp struct {
	c        *appui.Ctx
	prefs    pass.Prefs
	stores   []string
	store    pass.Store
	keyring  string // inua-keyring's folder, not renamed or moved
	entries  []string
	category string   // the chosen category; "" for the top level
	picked   bool     // a category is chosen
	selected string   // the open entry's name; "" for none
	found    []string // what the search finds, best first
	cursor   int      // the result Enter opens

	// The open entry, decrypted; gen drops a decryption that comes back
	// after another entry was opened.
	raw     string
	entry   pass.Entry
	otp     *pass.OTP
	gen     int
	reveal  map[string]bool   // the secret rows shown, by row
	values  map[string]string // what each row's copy button copies
	otpCode *gtk.Label
	otpLeft *gtk.Label

	form     string // the dialog's errand, one of form…; "" while it is closed
	formFrom string // what it renames, moves or deletes
	editNew  bool   // the editor writes a new entry, named in the dialog
	newName  string
	busy     bool // a pass command runs
	clip     gtkx.SecretClip
	filling  bool

	categoryList, entryList, fields, skeleton  gtk.Box
	keyringInfo, columns, resultsCard, results gtk.Box
	resultsScroll                              gtk.ScrolledWindow
	resultsNote, entriesTitle                  gtk.Label
	overlay                                    gtk.Overlay
	// The open entry's keyring item ("login/3") and the apps trusted with
	// it, as listed; "" for an entry that is not inua-keyring's.
	keyringItem                    string
	trustedHere                    []keyring.Trusted
	decrypting, decryptError       gtk.Box
	storeMenu                      gtk.DropDown
	storePath, emptyText           gtk.Label
	entryTitle, entryPath          gtk.Label
	decryptTitle, decryptErrorText gtk.Label
	editTitle, editError           gtk.Label
	spinner                        gtk.Spinner
	newEntry, moveEntry, editEntry gtk.Button
	deleteEntry                    gtk.Button
	filter                         gtk.SearchEntry
	detail                         gtk.Stack
	editor                         gtk.TextView
	win                            *gtk.ApplicationWindow

	// The dialog that names a new entry, renames or moves, or deletes.
	dlg                         gtk.Window
	dlgTitle, dlgText, dlgError gtk.Label
	dlgPath                     gtk.Entry
	dlgPreview                  gtk.Box
	dlgOK                       gtk.Button
	otpDlg                      passOTP // pass_otp.go
	otpEntry, editOTP           gtk.Button

	onButton       func(gtk.Button)
	onFilter       func(gtk.SearchEntry)
	onFilterGo     func(gtk.SearchEntry)
	onFilterKey    func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onColumnsClick func(gtk.GestureClick, int32, float64, float64)
	onStore        func(uintptr, uintptr, uintptr)
	onFormChanged  func(uintptr, uintptr)
	onFormActivate func(gtk.Entry)
	onDlgKey       func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onDlgClose     func(gtk.Window) bool
	onWinClose     func(gtk.Window) bool
	onCategoryMenu func(gtk.GestureClick, int32, float64, float64)
	onTick         glib.SourceFunc
	dialog         *gtk.FileDialog
	onChosen       gio.AsyncReadyCallback
}

func (a *passApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = &gtk.ApplicationWindow{}
	a.reveal, a.values = map[string]bool{}, map[string]string{}
	a.keyring = c.Config().Keyring.Folder
	var addStore, editCancel, editSave, retry, dlgCancel, otpCancel gtk.Button
	ui := c.Load("pass.ui")
	ui.Get("pass", a.win)
	for id, w := range map[string]gobject.Ptr{
		"categories": &a.categoryList, "entries": &a.entryList, "fields": &a.fields, "keyring_info": &a.keyringInfo,
		"skeleton": &a.skeleton, "decrypting": &a.decrypting, "decrypt_error": &a.decryptError,
		"store_menu": &a.storeMenu, "store_path": &a.storePath,
		"empty_text": &a.emptyText, "entry_title": &a.entryTitle, "entry_path": &a.entryPath,
		"decrypt_title": &a.decryptTitle, "decrypt_error_text": &a.decryptErrorText,
		"edit_title": &a.editTitle, "edit_error": &a.editError, "spinner": &a.spinner,
		"new_entry": &a.newEntry, "move_entry": &a.moveEntry, "edit_entry": &a.editEntry, "delete_entry": &a.deleteEntry,
		"filter": &a.filter, "detail": &a.detail, "editor": &a.editor,
		"overlay": &a.overlay, "results_card": &a.resultsCard, "results": &a.results,
		"results_scroll": &a.resultsScroll, "results_note": &a.resultsNote, "entries_title": &a.entriesTitle,
		"columns": &a.columns, "add_store": &addStore, "edit_cancel": &editCancel, "edit_save": &editSave, "retry": &retry,
		"pass_dialog": &a.dlg, "dialog_title": &a.dlgTitle, "dialog_text": &a.dlgText,
		"dialog_error": &a.dlgError, "dialog_path": &a.dlgPath, "dialog_preview": &a.dlgPreview,
		"dialog_ok": &a.dlgOK, "dialog_cancel": &dlgCancel,
		"otp_entry": &a.otpEntry, "edit_otp": &a.editOTP, "otp_cancel": &otpCancel,
	} {
		ui.Get(id, w)
	}
	for id, w := range a.otpWidgets() {
		ui.Get(id, w)
	}
	ui.Close()

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.onFilter = func(gtk.SearchEntry) { a.showResults() }
	a.onFilterGo = func(gtk.SearchEntry) {
		if a.cursor < len(a.found) && a.resultsCard.GetVisible() {
			a.openFound(a.found[a.cursor])
		}
	}
	a.onFilterKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		switch keyval {
		case 0xff52: // Up
			a.moveCursor(-1)
		case 0xff54: // Down
			a.moveCursor(1)
		case 0xff1b: // Escape
			if a.filter.GetText() == "" {
				return false
			}
			a.filter.SetText("")
		default:
			return false
		}
		return true
	}
	// A click beside the results, but in the search, puts them away.
	a.onColumnsClick = func(_ gtk.GestureClick, _ int32, x, y float64) {
		var r graphene.Rect
		if !a.resultsCard.GetVisible() || !a.filter.ComputeBounds(&a.columns.Widget, &r) {
			return
		}
		fx, fy := float64(r.GetX()), float64(r.GetY())
		if x < fx || y < fy || x >= fx+float64(r.GetWidth()) || y >= fy+float64(r.GetHeight()) {
			a.filter.SetText("")
			a.resultsCard.SetVisible(false)
		}
	}
	a.onStore = func(uintptr, uintptr, uintptr) {
		if i := int(a.storeMenu.GetSelected()); !a.filling && i < len(a.stores) && a.stores[i] != a.store.Dir {
			a.useStore(a.stores[i])
		}
	}
	a.onFormChanged = func(uintptr, uintptr) { a.checkForm() }
	a.onFormActivate = func(gtk.Entry) { a.submitForm() }
	a.onDlgKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == 0xff1b { // Escape
			a.closeForm()
			return true
		}
		return false
	}
	a.onDlgClose = func(gtk.Window) bool {
		a.closeForm()
		return true // hidden, kept for the next errand
	}
	a.onChosen = func(_, result, _ uintptr) {
		d := a.dialog
		a.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		f, err := d.SelectFolderFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || f == nil {
			return // cancelled
		}
		dir := f.GetPath()
		unrefObject(f)
		a.addStore(dir)
	}
	// A right click on a category renames or moves it.
	a.onCategoryMenu = func(g gtk.GestureClick, _ int32, _, _ float64) {
		name := strings.TrimPrefix(g.GetWidget().GetName(), "cat:")
		if name == "" || name == a.keyring {
			return
		}
		a.picked, a.category = true, name
		markSelected(&a.categoryList, "cat:"+name)
		a.showEntries()
		a.openForm(formMoveCategory, name)
	}
	a.onTick = func(uintptr) bool {
		a.tickOTP()
		a.tickOTPDialog()
		return true
	}
	for _, b := range []*gtk.Button{&addStore, &editCancel, &editSave, &retry, &dlgCancel, &a.dlgOK,
		&a.newEntry, &a.moveEntry, &a.editEntry, &a.deleteEntry, &a.otpEntry, &a.editOTP} {
		b.ConnectClicked(&a.onButton)
	}
	a.filter.ConnectSearchChanged(&a.onFilter)
	a.filter.ConnectActivate(&a.onFilterGo)
	a.filter.SetKeyCaptureWidget(&a.win.Widget)
	filterKeys := gtk.NewEventControllerKey()
	filterKeys.SetPropagationPhase(gtk.PhaseCaptureValue)
	filterKeys.ConnectKeyPressed(&a.onFilterKey)
	a.filter.AddController(&filterKeys.EventController)
	columnsClick := gtk.NewGestureClick()
	columnsClick.SetPropagationPhase(gtk.PhaseCaptureValue)
	columnsClick.ConnectPressed(&a.onColumnsClick)
	a.columns.AddController(&columnsClick.EventController)
	gtkx.ConnectNotify(&a.storeMenu, "selected", &a.onStore)
	gtkx.ConnectChanged(&a.dlgPath, &a.onFormChanged)
	a.dlgPath.ConnectActivate(&a.onFormActivate)
	// The dialog's class is the app's, so the compositor groups it; being
	// the app's, it would keep the app running hidden, so it goes with
	// the window.
	a.dlg.SetApplication(c.App)
	a.onWinClose = func(gtk.Window) bool {
		a.dlg.Destroy()
		a.otpDlg.win.Destroy()
		return false
	}
	a.win.ConnectCloseRequest(&a.onWinClose)
	a.dlg.SetTitle(i18n.T("Pass"))
	a.dlg.SetTransientFor(&a.win.Window)
	a.dlg.ConnectCloseRequest(&a.onDlgClose)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&a.onDlgKey)
	a.dlg.AddController(&keys.EventController)
	a.bindOTP(&otpCancel)
	// The rows to come while an entry decrypts: shimmering placeholders.
	for _, w := range []int{120, 180, 90} {
		row := gtk.NewBox(gtk.OrientationVerticalValue, 6)
		row.AddCssClass("pass-skel-row")
		k := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		k.AddCssClass("pass-skel")
		k.AddCssClass("short")
		k.SetHalign(gtk.AlignStartValue)
		k.SetSizeRequest(56, 8)
		gtkx.Append(row, &k.Widget)
		v := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		v.AddCssClass("pass-skel")
		v.SetHalign(gtk.AlignStartValue)
		v.SetSizeRequest(int32(w), 12)
		gtkx.Append(row, &v.Widget)
		gtkx.Append(&a.skeleton, &row.Widget)
	}

	if err := store.Load(pass.StoreName, &a.prefs); err != nil {
		log.Printf("pass: %v", err)
	}
	a.stores = pass.Stores(a.prefs)
	a.showStores()
	a.useStore(pass.Current(a.prefs, a.stores))
	glib.TimeoutAdd(1000, &a.onTick, 0)
	menuWindow(c, a.win, []appui.MenuGroup{{Title: "File", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "pass-add-entry", Label: "New entry", Activate: func(gio.SimpleAction, uintptr) { a.openForm(formNew, "") }}, {Name: "pass-add-store", Label: "Add store", Activate: func(gio.SimpleAction, uintptr) { a.chooseStore() }}, {Name: "pass-search", Label: "Search", Activate: func(gio.SimpleAction, uintptr) { a.filter.GrabFocus() }}}}}}})
	return a.win
}

func (a *passApp) ThemeChanged() {}

// --- stores ---

// showStores fills the store menu, shown when there is a choice.
func (a *passApp) showStores() {
	a.filling = true
	defer func() { a.filling = false }()
	a.storeMenu.SetModel(gtk.NewStringList(pass.ShortNames(a.stores)))
	if i := slices.Index(a.stores, a.store.Dir); i >= 0 {
		a.storeMenu.SetSelected(uint32(i))
	}
	a.storeMenu.SetVisible(len(a.stores) > 1)
}

// useStore opens a store and lists it.
func (a *passApp) useStore(dir string) {
	a.store = pass.Store{Dir: dir}
	a.storePath.SetText(pass.Tilde(dir))
	a.storePath.SetTooltipText(dir)
	if dir != "" && dir != a.prefs.Store {
		a.prefs.Store = dir
		a.save()
	}
	a.picked, a.category = false, ""
	a.closeEntry()
	a.reload("")
	if i := slices.Index(a.stores, dir); i >= 0 {
		a.filling = true
		a.storeMenu.SetSelected(uint32(i))
		a.filling = false
	}
}

func (a *passApp) save() {
	if err := store.Save(pass.StoreName, a.prefs); err != nil {
		log.Printf("pass: %v", err)
	}
}

// chooseStore asks for another store's folder.
func (a *passApp) chooseStore() {
	if a.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("A password store: a folder with a .gpg-id"))
	a.dialog = d
	d.SelectFolder(&a.win.Window, nil, &a.onChosen, 0)
}

// addStore adds a store chosen and opens it.
func (a *passApp) addStore(dir string) {
	if !pass.IsStore(dir) {
		a.showEmpty(i18n.Tf("%s is not a password store: it has no .gpg-id (pass init makes one).", dir))
		return
	}
	if !slices.Contains(a.prefs.Added, dir) {
		a.prefs.Added = append(a.prefs.Added, dir)
	}
	a.stores = pass.Stores(a.prefs)
	a.useStore(dir)
	a.showStores()
}

// --- lists ---

// reload reads the store's entries again and shows them; open, when
// given, is opened.
func (a *passApp) reload(open string) {
	if a.store.Dir == "" {
		a.entries = nil
		a.showCategories()
		a.showEntries()
		a.showEmpty(i18n.T("No password store. pass init YOUR-GPG-KEY-ID makes one in ~/.password-store; another folder can be added below."))
		return
	}
	entries, err := a.store.Entries()
	if err != nil {
		log.Printf("pass: %v", err)
	}
	a.entries = entries
	cats := pass.Categories(entries)
	if open != "" {
		a.category, _ = pass.Split(open)
		a.picked = true
	}
	if !a.picked || !slices.ContainsFunc(cats, func(c pass.Category) bool { return c.Name == a.category }) {
		a.picked, a.category = len(cats) > 0, ""
		if len(cats) > 0 {
			a.category = cats[0].Name
		}
	}
	a.showCategories()
	a.showEntries()
	switch {
	case open != "":
		a.openEntry(open)
	case a.selected != "" && slices.Contains(entries, a.selected):
	case len(entries) == 0:
		a.closeEntry()
		a.showEmpty(i18n.T("The store is empty. + makes an entry."))
	default:
		a.closeEntry()
		a.showEmpty(i18n.T("Choose an entry."))
	}
}

func (a *passApp) categoryName(c string) string {
	if c == "" {
		return i18n.T("Top level")
	}
	return c
}

func (a *passApp) showCategories() {
	gtkx.Clear(&a.categoryList)
	for _, c := range pass.Categories(a.entries) {
		row := text(a.categoryName(c.Name), i18n.Nf("%d entry", "%d entries", c.Count, c.Count))
		item := indexItem("cat:"+c.Name, &row.Widget, a.picked && c.Name == a.category, &a.onButton)
		// A right click renames or moves it, but for the top level and
		// inua-keyring's folder, whose index names its entries by path.
		if c.Name != "" && c.Name != a.keyring {
			item.SetTooltipText(i18n.T("Right-click to rename or move"))
			menu := gtk.NewGestureClick()
			menu.SetButton(3)
			menu.ConnectPressed(&a.onCategoryMenu)
			item.AddController(&menu.EventController)
		}
		gtkx.Append(&a.categoryList, &item.Widget)
	}
	a.newEntry.SetSensitive(a.store.Dir != "")
}

// showEntries lists the chosen category's entries, under its name.
func (a *passApp) showEntries() {
	gtkx.Clear(&a.entryList)
	a.entriesTitle.SetText("")
	if a.picked {
		a.entriesTitle.SetText(a.categoryName(a.category))
	}
	for _, e := range a.entries {
		cat, sub := pass.Split(e)
		if !a.picked || cat != a.category {
			continue
		}
		// The entry's own name, then its folders in the category.
		folder, name := "", sub
		if i := strings.LastIndexByte(sub, '/'); i >= 0 {
			folder, name = sub[:i], sub[i+1:]
		}
		item := indexItem("entry:"+e, &passEntryRow(e, name, folder).Widget, e == a.selected, &a.onButton)
		item.AddCssClass("pass-entry")
		gtkx.Append(&a.entryList, &item.Widget)
	}
}

// showResults lists what the search finds in every category (pass.Search),
// under the search and over the columns; the first is the one Enter
// opens, Up and Down move it.
func (a *passApp) showResults() {
	gtkx.Clear(&a.results)
	q := a.filter.GetText()
	a.found, a.cursor = pass.Search(a.entries, q), 0
	if strings.TrimSpace(q) == "" {
		a.resultsCard.SetVisible(false)
		return
	}
	shown := a.found
	if len(shown) > passResultsShown {
		shown = shown[:passResultsShown]
	}
	for i, e := range shown {
		cat, sub := pass.Split(e)
		folder, name := "", sub
		if j := strings.LastIndexByte(sub, '/'); j >= 0 {
			folder, name = sub[:j], sub[j+1:]
		}
		folder = strings.Trim(pass.Join(a.categoryName(cat), folder), "/")
		item := indexItem("found:"+e, &passEntryRow(e, name, folder).Widget, i == 0, &a.onButton)
		item.AddCssClass("pass-entry")
		item.SetFocusable(false) // the keys stay in the search
		gtkx.Append(&a.results, &item.Widget)
	}
	switch n := len(a.found) - len(shown); {
	case len(a.found) == 0:
		a.resultsNote.SetText(i18n.T("Nothing matches. Each word is looked for in the whole path."))
	case n > 0:
		a.resultsNote.SetText(i18n.Nf("And %d more: another word narrows them.", "And %d more: another word narrows them.", n, n))
	}
	a.resultsNote.SetVisible(len(a.found) == 0 || len(a.found) > len(shown))
	a.resultsScroll.SetVisible(len(shown) > 0)
	a.resultsScroll.GetVadjustment().SetValue(0)
	var r graphene.Rect
	if a.filter.ComputeBounds(&a.overlay.Widget, &r) {
		a.resultsCard.SetMarginStart(int32(r.GetX()))
		a.resultsCard.SetMarginTop(int32(r.GetY() + r.GetHeight() + 6))
	}
	a.resultsCard.SetVisible(true)
}

// moveCursor moves the result Enter opens by d, keeping it in sight.
func (a *passApp) moveCursor(d int) {
	n := min(len(a.found), passResultsShown)
	if n == 0 || !a.resultsCard.GetVisible() {
		return
	}
	a.cursor = (a.cursor + d + n) % n
	markSelected(&a.results, "found:"+a.found[a.cursor])
	i := 0
	for c := a.results.GetFirstChild(); c != nil; i++ {
		if i == a.cursor {
			var r graphene.Rect
			if c.ComputeBounds(&a.results.Widget, &r) {
				adj := a.resultsScroll.GetVadjustment()
				y, h, top := float64(r.GetY()), float64(r.GetHeight()), adj.GetValue()
				switch page := adj.GetPageSize(); {
				case y < top:
					adj.SetValue(y)
				case y+h > top+page:
					adj.SetValue(y + h - page)
				}
			}
		}
		next := c.GetNextSibling()
		c.Unref()
		c = next
	}
}

// openFound opens a result where it is: its category chosen, the entry
// open; the search is put away.
func (a *passApp) openFound(name string) {
	a.filter.SetText("")
	a.resultsCard.SetVisible(false)
	a.picked = true
	a.category, _ = pass.Split(name)
	a.showCategories()
	a.showEntries()
	a.openEntry(name)
}

// markSelected moves the selection to the list's item of that name in
// place: rebuilding the list would scroll it back to the top.
func markSelected(list *gtk.Box, name string) {
	for c := list.GetFirstChild(); c != nil; {
		if c.GetName() == name {
			c.AddCssClass("selected")
		} else {
			c.RemoveCssClass("selected")
		}
		next := c.GetNextSibling()
		c.Unref() // GetFirstChild and GetNextSibling return new references
		c = next
	}
}

// passEntryRow is an entry in the list: in a square, the icon its name
// calls for (a service's logo, what it is for: pass.Icon), else its
// initial, which tell entries apart at a glance; its name; and below,
// dim, where it is.
func passEntryRow(entry, name, folder string) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("pass-entry-mark")
	mark.SetValign(gtk.AlignCenterValue)
	mark.SetHexpand(false) // set, so the centred icon's expanding stops here
	mark.SetSizeRequest(28, 28)
	var inner *gtk.Widget
	if icon := pass.Icon(entry); icon != "" {
		img := gtk.NewImageFromIconName("pass-" + icon + "-symbolic")
		inner = &img.Widget
	} else {
		initial := "·"
		if r := []rune(strings.TrimLeft(name, "._-@ ")); len(r) > 0 {
			initial = strings.ToUpper(string(r[0]))
		}
		inner = &gtk.NewLabel(initial).Widget
	}
	inner.SetHexpand(true)
	inner.SetHalign(gtk.AlignCenterValue)
	inner.SetValign(gtk.AlignCenterValue)
	gtkx.Append(mark, inner)
	gtkx.Append(row, &mark.Widget)
	col := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	col.SetValign(gtk.AlignCenterValue)
	col.SetHexpand(true)
	n := gtk.NewLabel(name)
	n.AddCssClass("title")
	n.SetXalign(0)
	n.SetEllipsize(3)
	gtkx.Append(col, &n.Widget)
	if folder != "" {
		f := gtk.NewLabel(folder)
		f.AddCssClass("pass-entry-folder")
		f.SetXalign(0)
		f.SetEllipsize(1) // start: the nearest folder tells most
		gtkx.Append(col, &f.Widget)
	}
	gtkx.Append(row, &col.Widget)
	return row
}

// --- the entry ---

func (a *passApp) showEmpty(msg string) {
	a.emptyText.SetText(msg)
	a.detail.SetVisibleChildName("empty")
}

// closeEntry forgets the open entry and empties what showed it.
func (a *passApp) closeEntry() {
	a.gen++
	a.selected, a.raw, a.entry, a.otp = "", "", pass.Entry{}, nil
	a.otpCode, a.otpLeft = nil, nil
	clear(a.reveal)
	clear(a.values)
	gtkx.Clear(&a.fields)
	gtkx.Clear(&a.keyringInfo)
	a.keyringInfo.SetVisible(false)
	a.keyringItem, a.trustedHere = "", nil
	a.editor.GetBuffer().SetText("", 0)
	a.otpEntry.SetSensitive(false) // until it is decrypted
}

// openEntry decrypts an entry and shows it; gpg-agent may ask for the
// passphrase meanwhile, in inua's dialog.
func (a *passApp) openEntry(name string) {
	a.closeEntry()
	a.selected = name
	markSelected(&a.entryList, "entry:"+name)
	cat, sub := pass.Split(name)
	a.entryTitle.SetText(sub)
	a.entryPath.SetText(name)
	a.decryptTitle.SetText(i18n.Tf("Decrypting %s", name))
	a.showDecrypting(true, "")
	inKeyring := cat == a.keyring
	a.moveEntry.SetSensitive(!inKeyring)
	a.deleteEntry.SetSensitive(!inKeyring)
	a.detail.SetVisibleChildName("entry")
	if inKeyring {
		a.showKeyringInfo(name)
	}
	gen, s := a.gen, a.store
	go func() {
		text, err := s.Show(name)
		gtkx.Post(func() {
			if gen != a.gen {
				return // another entry since
			}
			if err != nil {
				a.showDecrypting(false, err.Error())
				return
			}
			a.showDecrypting(false, "")
			a.raw = text
			a.entry = pass.Parse(text)
			a.otp = nil
			if a.entry.OTP != "" {
				if o, err := pass.ParseOTP(a.entry.OTP); err == nil {
					a.otp = &o
				}
			}
			a.showFields()
		})
	}()
}

// showDecrypting shows the spinner and the shimmering rows while an
// entry decrypts, or why it could not be, with a second try.
func (a *passApp) showDecrypting(on bool, failed string) {
	a.decrypting.SetVisible(on)
	if on {
		a.spinner.Start()
	} else {
		a.spinner.Stop()
	}
	a.decryptErrorText.SetText(failed)
	a.decryptError.SetVisible(failed != "")
}

// showFields shows the open entry's values, each with its copy button;
// the secret ones as dots, with a button to show them.
func (a *passApp) showFields() {
	gtkx.Clear(&a.fields)
	clear(a.values)
	a.otpCode, a.otpLeft = nil, nil
	e := a.entry
	a.otpEntry.SetSensitive(true)
	if e.OTP != "" {
		a.otpEntry.SetTooltipText(i18n.T("Replace the one-time code"))
	} else {
		a.otpEntry.SetTooltipText(i18n.T("Add a one-time code"))
	}
	// The last part of its path is, as pass users name entries, its user.
	if cat, _ := pass.Split(a.selected); cat != a.keyring {
		if u := pass.Username(a.selected); u != "" && !slices.ContainsFunc(e.Fields, func(f pass.Field) bool { return f.Value == u }) {
			a.addRow("user", i18n.T("USERNAME"), u, false)
		}
	}
	a.addRow("password", i18n.T("PASSWORD"), e.Password, true)
	if e.OTP != "" {
		if a.otp != nil {
			a.addOTPRow()
		} else {
			a.addRow("otp", i18n.T("ONE-TIME CODE"), e.OTP, true)
		}
	}
	for i, f := range e.Fields {
		a.addRow("field:"+strconv.Itoa(i), f.Key, f.Value, f.Secret)
	}
	if e.Notes != "" {
		a.addRow("notes", i18n.T("NOTES"), e.Notes, false)
	}
}

// addRow adds a value's row: its name over it, then its buttons.
func (a *passApp) addRow(id, key, value string, secret bool) {
	a.values[id] = value
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("pass-row")
	col := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	col.SetHexpand(true)
	k := gtk.NewLabel(key)
	k.AddCssClass("kicker")
	k.SetXalign(0)
	gtkx.Append(col, &k.Widget)
	shown := !secret || a.reveal[id]
	v := gtk.NewLabel(value)
	v.AddCssClass("pass-value")
	if !shown {
		v.SetText(passMasked)
		v.AddCssClass("pass-masked")
	}
	v.SetXalign(0)
	v.SetWrap(true)
	v.SetWrapMode(2) // word or character: long tokens break anywhere
	v.SetSelectable(shown)
	gtkx.Append(col, &v.Widget)
	gtkx.Append(row, &col.Widget)
	if secret {
		icon, tip := "view-reveal-symbolic", i18n.T("Show")
		if shown {
			icon, tip = "view-conceal-symbolic", i18n.T("Hide")
		}
		gtkx.Append(row, &plainIcon("reveal:"+id, icon, tip, &a.onButton).Widget)
	}
	gtkx.Append(row, &plainIcon("copy:"+id, "edit-copy-symbolic", i18n.T("Copy"), &a.onButton).Widget)
	gtkx.Append(&a.fields, &row.Widget)
}

// addOTPRow adds the one-time code, with the seconds it stays.
func (a *passApp) addOTPRow() {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("pass-row")
	col := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	col.SetHexpand(true)
	k := gtk.NewLabel(i18n.T("ONE-TIME CODE"))
	k.AddCssClass("kicker")
	k.SetXalign(0)
	gtkx.Append(col, &k.Widget)
	line := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	a.otpCode = gtk.NewLabel("")
	a.otpCode.AddCssClass("pass-otp")
	a.otpCode.SetXalign(0)
	gtkx.Append(line, &a.otpCode.Widget)
	a.otpLeft = gtk.NewLabel("")
	a.otpLeft.AddCssClass("sub")
	a.otpLeft.SetValign(gtk.AlignCenterValue)
	gtkx.Append(line, &a.otpLeft.Widget)
	gtkx.Append(col, &line.Widget)
	gtkx.Append(row, &col.Widget)
	gtkx.Append(row, &plainIcon("copy:otp", "edit-copy-symbolic", i18n.T("Copy"), &a.onButton).Widget)
	gtkx.Append(&a.fields, &row.Widget)
	a.tickOTP()
}

// tickOTP shows the code now and the seconds it stays.
func (a *passApp) tickOTP() {
	if a.otp == nil || a.otpCode == nil {
		return
	}
	code, left := a.otp.Code(time.Now())
	a.values["otp"] = code
	a.otpCode.SetText(code[:len(code)/2] + " " + code[len(code)/2:])
	secs := int(left.Round(time.Second) / time.Second)
	a.otpLeft.SetText(i18n.Nf("%d second left", "%d seconds left", secs, secs))
}

// --- buttons ---

func (a *passApp) button(name string) {
	if a.otpButton(name) {
		return
	}
	switch kind, arg, _ := strings.Cut(name, ":"); kind {
	case "cat":
		a.picked, a.category = true, arg
		a.filter.SetText("")
		markSelected(&a.categoryList, name)
		a.showEntries()
		if c, _ := pass.Split(a.selected); a.selected == "" || c != arg {
			a.closeEntry()
			a.showEmpty(i18n.T("Choose an entry."))
		}
	case "entry":
		a.openEntry(arg)
	case "found":
		a.openFound(arg)
	case "untrust":
		i, _ := strconv.Atoi(arg)
		if i < 0 || i >= len(a.trustedHere) || a.keyringItem == "" {
			return
		}
		item, id, name := a.keyringItem, a.trustedHere[i].Identity, a.selected
		go func() {
			if conn, err := system.SessionBus(); err == nil {
				if err := keyring.UntrustApp(conn, item, id); err != nil {
					log.Printf("pass: keyring: %v", err)
				}
				conn.Close()
			}
			gtkx.Post(func() {
				if a.selected == name {
					a.showKeyringInfo(name)
				}
			})
		}()
	case "retry":
		if a.selected != "" {
			a.openEntry(a.selected)
		}
	case "reveal":
		a.reveal[arg] = !a.reveal[arg]
		a.showFields()
	case "copy":
		a.copySecret(a.values[arg])
	case "add-store":
		a.chooseStore()
	case "move-entry":
		a.openForm(formMoveEntry, a.selected)
	case "new-entry":
		a.openForm(formNew, "")
	case "delete":
		a.openForm(formDelete, a.selected)
	case "edit":
		a.openEditor(a.selected, a.raw)
	case "edit-cancel":
		a.closeEditor()
	case "edit-save":
		a.saveEditor()
	case "dialog-cancel":
		a.closeForm()
	case "dialog-ok":
		a.submitForm()
	}
}

// copySecret copies a value as a secret (gtkx.SecretClip).
func (a *passApp) copySecret(value string) { a.clip.Copy(value) }

// --- the editor ---

func (a *passApp) openEditor(name, content string) {
	a.editTitle.SetText(name)
	a.editor.GetBuffer().SetText(content, -1)
	a.editError.SetVisible(false)
	a.detail.SetVisibleChildName("edit")
	a.editor.GrabFocus()
}

func (a *passApp) closeEditor() {
	a.editor.GetBuffer().SetText("", 0)
	if a.editNew {
		a.editNew = false
		a.showEmpty(i18n.T("Choose an entry."))
		return
	}
	a.detail.SetVisibleChildName("entry")
}

func (a *passApp) saveEditor() {
	if a.busy {
		return
	}
	buf := a.editor.GetBuffer()
	var start, end gtk.TextIter
	buf.GetBounds(&start, &end)
	content := buf.GetText(&start, &end, true)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	name := a.selected
	if a.editNew {
		name = a.newName
	}
	a.busy = true
	s := a.store
	go func() {
		err := s.Insert(name, content)
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.editError.SetText(err.Error())
				a.editError.SetVisible(true)
				return
			}
			a.editNew = false
			a.editor.GetBuffer().SetText("", 0)
			a.reload(name)
		})
	}()
}

// --- the dialog ---

// openForm opens the dialog that names a new entry, renames or moves a
// category or an entry (its path the only field, what it would do shown
// as it is typed), or asks before deleting one.
func (a *passApp) openForm(form, from string) {
	if a.form != "" {
		return
	}
	a.form, a.formFrom = form, from
	a.dlgOK.RemoveCssClass("destructive-action")
	a.dlgOK.AddCssClass("suggested-action")
	a.dlgPath.SetVisible(form != formDelete)
	a.dlgError.SetVisible(false)
	path := from
	switch form {
	case formMoveCategory:
		a.dlgTitle.SetText(i18n.Tf("Rename or move the category “%s”", from))
		a.dlgText.SetText(i18n.T("Its new path. Another name renames it; a path under another category moves it there, with all its entries."))
		a.dlgOK.SetLabel(i18n.T("Move"))
	case formMoveEntry:
		a.dlgTitle.SetText(i18n.Tf("Rename or move “%s”", from))
		a.dlgText.SetText(i18n.T("Its new path. The first part is its category: change it to move the entry, the rest to rename it."))
		a.dlgOK.SetLabel(i18n.T("Move"))
	case formNew:
		a.dlgTitle.SetText(i18n.T("New entry"))
		a.dlgText.SetText(i18n.T("Its path: the first part is its category. Its content comes next."))
		path = ""
		if a.picked && a.category != "" {
			path = a.category + "/"
		}
		a.dlgOK.SetLabel(i18n.T("Next"))
	case formDelete:
		a.dlgTitle.SetText(i18n.Tf("Delete “%s”?", from))
		a.dlgText.SetText(i18n.T("pass removes its file; a git store keeps it in its history."))
		a.dlgOK.SetLabel(i18n.T("Delete"))
		a.dlgOK.RemoveCssClass("suggested-action")
		a.dlgOK.AddCssClass("destructive-action")
	}
	a.filling = true
	a.dlgPath.SetText(path)
	a.filling = false
	a.checkForm()
	a.dlg.Present()
	if form == formDelete {
		a.dlgOK.GrabFocus()
		return
	}
	a.dlgPath.GrabFocus()
	// The part to change selected: an entry's name, all of a category.
	start := int32(0)
	if form == formMoveEntry {
		if i := strings.LastIndexByte(path, '/'); i >= 0 {
			start = int32(i + 1)
		}
	}
	a.dlgPath.SelectRegion(start, -1)
	if form == formNew {
		a.dlgPath.SetPosition(-1)
	}
}

// checkForm previews what the dialog would do, or why it cannot, and
// allows the action only when it can.
func (a *passApp) checkForm() {
	if a.form == "" || a.filling {
		return
	}
	gtkx.Clear(&a.dlgPreview)
	if a.form == formDelete {
		a.previewLine(i18n.T("FILE"), pass.Tilde(a.store.Dir)+"/"+a.formFrom+".gpg", "")
		a.dlgError.SetVisible(false)
		a.dlgOK.SetSensitive(!a.busy)
		return
	}
	from := a.formFrom
	if a.form == formNew {
		from = ""
	}
	moves, err := pass.PlanMove(a.entries, from, a.dlgPath.GetText())
	a.dlgPreview.SetVisible(err == nil)
	if err != nil {
		a.dlgError.SetText(passPlanError(err))
		// Nothing typed yet, or the path as it is: no reproach, only no
		// action.
		typed := strings.Trim(strings.TrimSpace(a.dlgPath.GetText()), "/")
		a.dlgError.SetVisible(typed != "" && typed != a.formFrom)
		a.dlgOK.SetSensitive(false)
		return
	}
	a.dlgError.SetVisible(false)
	to := moves[0].To
	if a.form == formMoveCategory {
		to = strings.Trim(strings.TrimSpace(a.dlgPath.GetText()), "/")
	}
	cat, sub := pass.Split(to)
	a.previewLine(i18n.T("CATEGORY"), a.categoryName(cat), "")
	a.previewLine(i18n.T("NAME"), sub, "")
	switch {
	case a.form == formNew:
		a.previewLine(i18n.T("FILE"), pass.Tilde(a.store.Dir)+"/"+to+".gpg", "")
	default:
		shown := moves
		if len(shown) > 5 {
			shown = shown[:5]
		}
		for _, m := range shown {
			a.previewLine("", m.From, m.To)
		}
		if n := len(moves) - len(shown); n > 0 {
			a.previewLine("", i18n.Nf("and %d more", "and %d more", n, n), "")
		}
	}
	a.dlgOK.SetSensitive(!a.busy)
	a.dlgPreview.SetVisible(true)
}

// previewLine adds a line to the dialog's preview: a name and a value, or
// an entry's path before and after.
func (a *passApp) previewLine(key, value, after string) {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	if key != "" {
		k := gtk.NewLabel(key)
		k.AddCssClass("kicker")
		k.SetXalign(0)
		k.SetSizeRequest(84, -1)
		gtkx.Append(row, &k.Widget)
	}
	v := gtk.NewLabel(value)
	v.AddCssClass("pass-preview-value")
	v.SetXalign(0)
	v.SetEllipsize(2)
	if after != "" {
		v.AddCssClass("before")
	}
	gtkx.Append(row, &v.Widget)
	if after != "" {
		arrow := gtk.NewLabel("→")
		arrow.AddCssClass("sub")
		gtkx.Append(row, &arrow.Widget)
		w := gtk.NewLabel(after)
		w.AddCssClass("pass-preview-value")
		w.SetXalign(0)
		w.SetEllipsize(2)
		w.SetHexpand(true)
		gtkx.Append(row, &w.Widget)
	}
	gtkx.Append(&a.dlgPreview, &row.Widget)
}

// passPlanError says, in the user's language, why a path is refused.
func passPlanError(err error) string {
	switch msg := err.Error(); {
	case strings.Contains(msg, "where it is"):
		return i18n.T("That is where it is.")
	case strings.Contains(msg, "into itself"):
		return i18n.T("A category cannot move into itself.")
	case strings.Contains(msg, "already"):
		return i18n.T("That path is already in the store.")
	case strings.Contains(msg, "not in the store"):
		return i18n.T("It is no longer in the store.")
	}
	return i18n.T("Each part of the path needs a name, without a leading dot.")
}

func (a *passApp) closeForm() {
	a.form = ""
	a.dlg.SetVisible(false)
}

func (a *passApp) submitForm() {
	if a.form == "" || a.busy || !a.dlgOK.GetSensitive() {
		return
	}
	form, from := a.form, a.formFrom
	to := strings.Trim(strings.TrimSpace(a.dlgPath.GetText()), "/")
	if form == formNew {
		a.closeForm()
		a.editNew, a.newName = true, to
		a.closeEntry()
		a.openEditor(to, "")
		return
	}
	a.busy = true
	a.dlgOK.SetSensitive(false)
	s := a.store
	go func() {
		var err error
		if form == formDelete {
			err = s.Remove(from)
		} else {
			err = s.Move(from, to)
		}
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.dlgError.SetText(err.Error())
				a.dlgError.SetVisible(true)
				a.dlgOK.SetSensitive(true)
				return
			}
			a.closeForm()
			switch form {
			case formDelete:
				a.closeEntry()
				a.reload("")
			case formMoveCategory:
				a.category, a.picked = to, true
				if c, _ := pass.Split(to); c != to {
					a.category = c
				}
				open := ""
				if a.selected != "" {
					if c, sub := pass.Split(a.selected); c == from {
						open = to + "/" + sub
					}
				}
				a.closeEntry()
				a.reload(open)
				if open == "" {
					a.showEmpty(i18n.T("Choose an entry."))
				}
			default:
				a.closeEntry()
				a.reload(to)
			}
		})
	}()
}

// passReadsShown is how many reads of a keyring entry the Pass app lists.
const passReadsShown = 5

// showKeyringInfo shows, under an entry of inua-keyring's, the apps
// trusted with its secret, each with Remove, and its last reads; read
// from inua-keyring's files, off the main loop.
func (a *passApp) showKeyringInfo(name string) {
	dir, folder := a.store.Dir, a.keyring
	go func() {
		var item string
		var apps []keyring.Trusted
		if dir == keyring.StoreDir() {
			if st, err := keyring.OpenPass(keyring.Pass{Dir: dir, Folder: folder}); err == nil {
				if it, ok := st.ByEntry(strings.TrimPrefix(name, folder+"/")); ok {
					item = keyring.ItemKey(it.Collection, it.ID)
					apps = keyring.ReadTrust(keyring.TrustPath())[item]
				}
			}
		}
		var reads []keyring.Reads
		if record, err := keyring.ReadAccess(keyring.AccessPath()); err == nil {
			var mine []keyring.Access
			for _, r := range record {
				if r.Pass == name && r.Kind == "" {
					mine = append(mine, r)
				}
			}
			reads = keyring.GroupReads(mine, passReadsShown)
		}
		gtkx.Post(func() {
			if a.selected != name {
				return // another entry since
			}
			a.keyringItem, a.trustedHere = item, apps
			gtkx.Clear(&a.keyringInfo)
			a.keyringInfo.SetVisible(item != "")
			if item == "" {
				return
			}
			gtkx.Append(&a.keyringInfo, &passSection(i18n.T("TRUSTED APPS"), i18n.Nf("%d app", "%d apps", len(apps), len(apps))).Widget)
			if len(apps) == 0 {
				gtkx.Append(&a.keyringInfo, &passNote(i18n.T("None: an app reading it is asked about first.")).Widget)
			}
			for i, t := range apps {
				where := t.Exe
				switch {
				case t.Flatpak != "":
					where = i18n.Tf("Flatpak: %s", t.Flatpak)
				case t.Script != "":
					where = t.Exe + " " + t.Script
				}
				if t.Package != "" {
					where = i18n.Tf("%s · %s", where, t.Package)
				}
				row := passInfoRow(t.Name(), where)
				rm := gtk.NewButtonWithLabel(i18n.T("Remove"))
				rm.AddCssClass("outlined")
				rm.SetValign(gtk.AlignCenterValue)
				rm.SetName("untrust:" + strconv.Itoa(i))
				rm.SetTooltipText(i18n.T("Stop trusting it: it is asked about at its next read"))
				rm.ConnectClicked(&a.onButton)
				gtkx.Append(row, &rm.Widget)
				gtkx.Append(&a.keyringInfo, &row.Widget)
			}
			gtkx.Append(&a.keyringInfo, &passSection(i18n.T("LAST READS"), "").Widget)
			if len(reads) == 0 {
				gtkx.Append(&a.keyringInfo, &passNote(i18n.T("No app has read it yet.")).Widget)
			}
			for _, r := range reads {
				what := passReadWhat(r)
				when := i18n.Time(r.Time.Local(), "Jan 2 15:04")
				if r.N > 1 {
					when = i18n.Nf("%s · %d times", "%s · %d times", r.N, when, r.N)
				}
				gtkx.Append(&a.keyringInfo, &passInfoRow(r.App, i18n.Tf("%s · %s", what, when)).Widget)
			}
		})
	}()
}

// passReadWhat says how a read of a keyring entry went.
func passReadWhat(r keyring.Reads) string {
	switch r.Decision {
	case keyring.DecTrusted:
		return i18n.T("trusted")
	case keyring.DecOnce:
		return i18n.T("allowed once")
	case keyring.DecAllowed:
		return i18n.T("allowed, and trusted from now on")
	case keyring.DecDenied:
		return i18n.T("denied")
	case keyring.DecUnasked:
		return i18n.T("allowed: no one to ask")
	}
	return i18n.T("read")
}

// passSection is a section head, as the other light apps have: [TITLE]
// and a dim note.
func passSection(title, meta string) *gtk.Box {
	head := uikit.NewHead(title, meta)
	head.Box.AddCssClass("pass-keyring-head")
	return head.Box
}

// passInfoRow is a row of a keyring entry's sections: a name over a dim
// line in the monospace.
func passInfoRow(title, sub string) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("pass-row")
	col := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	col.SetHexpand(true)
	t := gtk.NewLabel(title)
	t.AddCssClass("title")
	t.SetXalign(0)
	t.SetEllipsize(3)
	gtkx.Append(col, &t.Widget)
	l := gtk.NewLabel(sub)
	l.AddCssClass("pass-entry-folder")
	l.SetXalign(0)
	l.SetEllipsize(2) // middle: a path's both ends tell most
	l.SetTooltipText(sub)
	gtkx.Append(col, &l.Widget)
	gtkx.Append(row, &col.Widget)
	return row
}

// passNote is a dim line under a section head.
func passNote(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.AddCssClass("sub")
	l.AddCssClass("pass-keyring-note")
	l.SetXalign(0)
	l.SetWrap(true)
	return l
}
