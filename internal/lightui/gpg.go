package lightui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/gpg"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/uikit"
)

// gpgApp encrypts and decrypts with GnuPG (internal/apps/gpg): a
// composer (to whom, from which of the user's keys, a text or files),
// a message or files decrypted as they come, saying who it was for and
// who signed it, and the keyring, to import keys and create one's own.
type gpgApp struct {
	c    *appui.Ctx
	win  *gtk.ApplicationWindow
	keys []gpg.Key
	mine []gpg.Key // the user's keys that can sign and encrypt
	from string    // the one chosen to sign with and encrypt to, by fingerprint
	to   []string  // the recipients chosen, by fingerprint, in order

	encFiles, decFiles []string
	encOut, decOut     string // the armored message, the decrypted text
	busy               bool
	clip               gtkx.SecretClip
	autoBlock          string   // the block pasted that was decrypted on its own
	clipBlock          string   // the encrypted message the clipboard holds
	impData            []byte   // the key data shown before importing
	decWrittenPaths    []string // the files the last decryption wrote
	chips              []*gtk.Widget

	pages, encView, keysView, msgKind, decKind gtk.Stack
	toChips                                    gtk.FlowBox
	toEntry                                    gtk.Entry
	toBrowse                                   gtk.ToggleButton
	toSuggest, fromSome, fromNone, fromList    gtk.Box
	encFiles_, decFiles_, encWritten           gtk.Box
	decWritten, signature, keyList, noKey      gtk.Box
	clipOffer, impPreview                      gtk.Box
	fromButton                                 gtk.MenuButton
	fromPop                                    gtk.Popover
	fromTitle, fromSub                         gtk.Label
	fromArrow                                  gtk.Image
	toSelf, sign                               gtk.Switch
	toSelfSub                                  gtk.Label
	encInput, encOutput, decInput, decOutput   gtk.TextView
	impInput                                   gtk.TextView
	encOutputFrame, decOutputFrame             gtk.ScrolledWindow
	toCount, encStatus, decStatus, keysMeta    gtk.Label
	encResultMeta, encResultStatus, decTo      gtk.Label
	impStatus, newStatus                       gtk.Label
	decResult                                  gtk.Box
	signatureTitle, signatureSub               gtk.Label
	signatureIcon                              gtk.Image
	encrypt, decrypt, impDo, newDo             gtk.Button
	encCopy, encSave, decCopy, decSave         gtk.Button
	newName, newEmail                          gtk.Entry
	newExpire                                  gtk.DropDown

	onButton                  func(gtk.Button)
	onToChanged, onDecChanged func(uintptr, uintptr)
	onImpChanged              func(uintptr, uintptr)
	onToActivate              func(gtk.Entry)
	onToKey                   func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onBrowse                  func(gtk.ToggleButton)
	onFromPop                 func(gtk.Widget)
	onToClick                 func(gtk.GestureClick, int32, float64, float64)
	onPage                    func(uintptr, uintptr, uintptr)
	onClipText                gio.AsyncReadyCallback
	onDrop                    map[string]*func(gtk.DropTarget, uintptr, float64, float64) bool
	dialog                    *gtk.FileDialog
	dialogFor                 string // "enc", "dec", "imp", "enc-save", "dec-save"
	onOpened, onSaved         gio.AsyncReadyCallback
	clipFor                   string // what the clipboard is read for: "dec", "imp"
	// The dialog that asks before a danger: deleting a key ("key") or
	// moving a decrypted file to the trash ("trash"), and which.
	dlgFor, dlgArg            string
	dlg                       gtk.Window
	dlgTitle, dlgKey, dlgText gtk.Label
	dlgOK                     gtk.Button
	onDlgKey                  func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onDlgClose, onWinClose    func(gtk.Window) bool
	clipReading               bool
}

func (a *gpgApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = &gtk.ApplicationWindow{}
	var encCompose, decIn, impPage, toBox gtk.Box
	ui := c.Load("gpg.ui")
	ui.Get("gpg", a.win)
	for id, w := range map[string]gobject.Ptr{
		"pages": &a.pages, "enc_view": &a.encView, "dec_kind": &a.decKind, "dec_result": &a.decResult, "keys_view": &a.keysView, "msg_kind": &a.msgKind,
		"to_chips": &a.toChips, "to_entry": &a.toEntry, "to_browse": &a.toBrowse,
		"to_suggest": &a.toSuggest, "from_some": &a.fromSome, "from_none": &a.fromNone, "from_list": &a.fromList,
		"enc_files": &a.encFiles_, "dec_files": &a.decFiles_, "enc_written": &a.encWritten,
		"dec_written": &a.decWritten, "signature": &a.signature, "keys": &a.keyList, "no_key": &a.noKey,
		"clip_offer": &a.clipOffer, "imp_preview": &a.impPreview,
		"from_button": &a.fromButton, "from_pop": &a.fromPop, "from_title": &a.fromTitle, "from_sub": &a.fromSub,
		"from_arrow": &a.fromArrow, "to_self": &a.toSelf, "to_self_sub": &a.toSelfSub, "sign": &a.sign,
		"enc_input": &a.encInput, "enc_output": &a.encOutput, "dec_input": &a.decInput, "dec_output": &a.decOutput,
		"imp_input":        &a.impInput,
		"enc_output_frame": &a.encOutputFrame, "dec_output_frame": &a.decOutputFrame,
		"to_count": &a.toCount, "enc_status": &a.encStatus, "dec_status": &a.decStatus, "keys_meta": &a.keysMeta,
		"enc_result_meta": &a.encResultMeta, "enc_result_status": &a.encResultStatus, "dec_to": &a.decTo,
		"imp_status": &a.impStatus, "new_status": &a.newStatus,
		"signature_title": &a.signatureTitle, "signature_sub": &a.signatureSub, "signature_icon": &a.signatureIcon,
		"encrypt": &a.encrypt, "decrypt": &a.decrypt, "imp_do": &a.impDo, "new_do": &a.newDo,
		"enc_copy": &a.encCopy, "enc_save": &a.encSave, "dec_copy": &a.decCopy, "dec_save": &a.decSave,
		"new_name": &a.newName, "new_email": &a.newEmail, "new_expire": &a.newExpire,
		"gpg_dialog": &a.dlg, "dialog_title": &a.dlgTitle, "dialog_key": &a.dlgKey, "dialog_text": &a.dlgText,
		"dialog_ok": &a.dlgOK,
		"to_box":    &toBox, "enc_compose": &encCompose, "dec_in": &decIn, "imp_page": &impPage,
	} {
		ui.Get(id, w)
	}
	buttons := []*gtk.Button{&a.encrypt, &a.decrypt, &a.impDo, &a.newDo, &a.encCopy, &a.encSave, &a.decCopy, &a.decSave, &a.dlgOK}
	for _, id := range []string{"b_create", "b_create2", "b_create3", "b_enc_choose", "b_enc_clear", "b_enc_back",
		"b_dec_clip", "b_dec_choose", "b_dec_clear", "b_import",
		"b_keys_back", "b_keys_back2", "b_imp_choose", "dialog_cancel"} {
		var b gtk.Button
		ui.Get(id, &b)
		buttons = append(buttons, &b)
	}
	ui.Close()

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	for _, b := range buttons {
		b.ConnectClicked(&a.onButton)
	}
	a.newExpire.SetModel(gtk.NewStringList([]string{i18n.T("1 year"), i18n.T("2 years"), i18n.T("5 years"), i18n.T("Never")}))
	a.newExpire.SetSelected(1)

	// The recipients box.
	a.onToChanged = func(uintptr, uintptr) { a.showSuggest() }
	gtkx.ConnectChanged(&a.toEntry, &a.onToChanged)
	a.onToActivate = func(gtk.Entry) {
		if fpr := a.firstSuggestion(); fpr != "" {
			a.addRecipient(fpr)
		}
	}
	a.toEntry.ConnectActivate(&a.onToActivate)
	a.onToKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		switch keyval {
		case uint32(gdk.KEY_BackSpace):
			if a.toEntry.GetText() == "" && len(a.to) > 0 {
				a.removeRecipient(a.to[len(a.to)-1])
				return true
			}
		case uint32(gdk.KEY_Escape):
			if a.toSuggest.GetVisible() {
				a.toEntry.SetText("")
				a.toBrowse.SetActive(false)
				return true
			}
		}
		return false
	}
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&a.onToKey)
	a.toEntry.AddController(&keys.EventController)
	a.onBrowse = func(gtk.ToggleButton) { a.showSuggest() }
	a.toBrowse.ConnectToggled(&a.onBrowse)
	// The list of the user's keys is as wide as the button it opens from
	// (less the popover's padding and border).
	a.onFromPop = func(gtk.Widget) { a.fromList.SetSizeRequest(max(a.fromButton.GetWidth()-10, 200), -1) }
	a.fromPop.ConnectShow(&a.onFromPop)
	// A click on the box, past the chips, is a click in the field.
	a.onToClick = func(gtk.GestureClick, int32, float64, float64) { a.toEntry.GrabFocus() }
	click := gtk.NewGestureClick()
	click.ConnectPressed(&a.onToClick)
	toBox.AddController(&click.EventController)

	// Decrypting as a message is pasted; a key shown as it is pasted.
	a.onDecChanged = func(uintptr, uintptr) { a.decInputChanged() }
	gtkx.ConnectChanged(a.decInput.GetBuffer(), &a.onDecChanged)
	a.onImpChanged = func(uintptr, uintptr) { a.previewImport(nil) }
	gtkx.ConnectChanged(a.impInput.GetBuffer(), &a.onImpChanged)
	a.onPage = func(uintptr, uintptr, uintptr) {
		switch a.pages.GetVisibleChildName() {
		case "decrypt":
			a.readClipboard("dec")
		case "keys":
			a.loadKeys() // what gpg in a terminal changed meanwhile
		}
	}
	gtkx.ConnectNotify(&a.pages, "visible-child", &a.onPage)
	a.onClipText = func(_, result, _ uintptr) {
		a.clipReading = false
		clip := a.win.GetClipboard()
		text, err := clip.ReadTextFinish(&gio.AsyncResultBase{Ptr: result})
		clip.Unref()
		if err != nil {
			text = ""
		}
		a.clipboardRead(text)
	}

	a.onOpened = func(_, result, _ uintptr) {
		d, which := a.dialog, a.dialogFor
		a.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		files, err := d.OpenMultipleFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || files == nil {
			return // cancelled
		}
		var paths []string
		for i := range files.GetNItems() {
			f := &gio.FileBase{Ptr: files.GetItem(i)}
			if p := f.GetPath(); p != "" {
				paths = append(paths, p)
			}
			unrefObject(f)
		}
		unrefObject(files)
		a.addFiles(which, paths)
	}
	a.onSaved = func(_, result, _ uintptr) {
		d, which := a.dialog, a.dialogFor
		a.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		f, err := d.SaveFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || f == nil {
			return
		}
		path := f.GetPath()
		unrefObject(f)
		a.save(which, path)
	}
	a.onDrop = map[string]*func(gtk.DropTarget, uintptr, float64, float64) bool{}
	for which, box := range map[string]*gtk.Box{"enc": &encCompose, "dec": &decIn, "imp": &impPage} {
		handler := func(dt gtk.DropTarget, _ uintptr, _, _ float64) bool {
			if v := dt.GetValue(); v != nil {
				a.addFiles(which, droppedPaths(v))
			}
			return true
		}
		a.onDrop[which] = &handler
		// Move as well as copy: Nautilus offers its files to be moved only,
		// and a drop target for copying is passed over (the files are read,
		// never moved). In the capture phase: before the text views under
		// it, which take a file manager's drag as text (its paths). Text
		// dragged in has no files, and still goes to them.
		dt := gtk.NewDropTarget(gdk.FileListGLibType(), gdk.ActionCopyValue|gdk.ActionMoveValue)
		dt.SetPropagationPhase(gtk.PhaseCaptureValue)
		dt.ConnectDrop(&handler)
		box.AddController(&dt.EventController)
	}
	// The dialog that asks before a danger: the app's class, so the
	// compositor groups it, and gone with the window.
	a.dlg.SetApplication(c.App)
	a.dlg.SetTitle(i18n.T("GPG"))
	a.dlg.SetTransientFor(&a.win.Window)
	a.onDlgClose = func(gtk.Window) bool {
		a.dlg.SetVisible(false)
		return true
	}
	a.dlg.ConnectCloseRequest(&a.onDlgClose)
	a.onWinClose = func(gtk.Window) bool {
		a.dlg.Destroy()
		return false
	}
	a.win.ConnectCloseRequest(&a.onWinClose)
	a.onDlgKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			a.dlg.SetVisible(false)
			return true
		}
		return false
	}
	dlgKeys := gtk.NewEventControllerKey()
	dlgKeys.ConnectKeyPressed(&a.onDlgKey)
	a.dlg.AddController(&dlgKeys.EventController)
	a.loadKeys()
	menuWindow(c, a.win, []appui.MenuGroup{{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "gpg-encrypt", Label: "Encrypt", Activate: func(gio.SimpleAction, uintptr) { a.ShowPage("encrypt") }}, {Name: "gpg-decrypt", Label: "Decrypt", Activate: func(gio.SimpleAction, uintptr) { a.ShowPage("decrypt") }}, {Name: "gpg-keys", Label: "Keys", Activate: func(gio.SimpleAction, uintptr) { a.ShowPage("keys") }}}}}}})
	return a.win
}

func (a *gpgApp) ThemeChanged() {}

// showPage opens a tab: `inua-apps gpg encrypt|decrypt|keys`.
func (a *gpgApp) ShowPage(name string) {
	if name == "encrypt" || name == "decrypt" || name == "keys" {
		a.pages.SetVisibleChildName(name)
	}
}

// --- the keyring ---

// loadKeys reads the keyring and shows it where it is chosen from; note,
// if any, says over the count what was just done.
func (a *gpgApp) loadKeys(note ...string) {
	go func() {
		keys, err := gpg.Keys()
		gtkx.Post(func() {
			if err != nil {
				log.Printf("gpg: %v", err)
				a.keysMeta.SetText(i18n.Tf("The keyring cannot be read: %s", err.Error()))
			}
			a.keys = keys
			a.mine = nil
			for _, k := range keys {
				if k.Secret && k.Usable() && k.CanSign {
					a.mine = append(a.mine, k)
				}
			}
			if !slices.ContainsFunc(a.mine, func(k gpg.Key) bool { return k.Fingerprint == a.from }) {
				a.from = ""
				if len(a.mine) > 0 {
					a.from = a.mine[0].Fingerprint
				}
			}
			a.to = slices.DeleteFunc(a.to, func(fpr string) bool {
				k := a.keyByID(fpr)
				return k == nil || !k.Usable() || !k.CanEncrypt
			})
			a.showFrom()
			a.showChips()
			a.showSuggest()
			a.showKeys()
			if len(note) > 0 {
				a.keysMeta.SetText(note[0])
			} else if err == nil {
				a.keysMeta.SetText("")
			}
		})
	}()
}

// gpgKeyLabel names a key in a line: its name, comment and address.
func gpgKeyLabel(k gpg.Key) string {
	if len(k.UIDs) > 0 && k.UIDs[0].Email != "" && k.UIDs[0].Name != "" {
		return k.UIDs[0].String()
	}
	return k.Name()
}

// gpgKeyDates says when a key was made and runs out.
func gpgKeyDates(k gpg.Key) []string {
	facts := []string{i18n.Tf("created %s", i18n.Time(k.Created.Local(), "Jan 2, 2006"))}
	switch {
	case k.Expired:
		facts = append(facts, i18n.Tf("expired %s", i18n.Time(k.Expires.Local(), "Jan 2, 2006")))
	case !k.Expires.IsZero():
		facts = append(facts, i18n.Tf("expires %s", i18n.Time(k.Expires.Local(), "Jan 2, 2006")))
	}
	return facts
}

// gpgKeyFacts tells keys of the same name apart: the short key ID, the
// algorithm, and the dates.
func gpgKeyFacts(k gpg.Key) string {
	facts := []string{gpgShortID(k)}
	if k.Algo != "" {
		facts = append(facts, k.Algo)
	}
	facts = append(facts, gpgKeyDates(k)...)
	if !k.CanEncrypt {
		facts = append(facts, i18n.T("signs only"))
	}
	return strings.Join(facts, " · ")
}

// gpgValidity says how far the keyring vouches for a key, or why it
// cannot be used.
func gpgValidity(k gpg.Key) string {
	switch {
	case k.Revoked:
		return i18n.T("revoked")
	case k.Expired:
		return i18n.T("expired")
	case k.Disabled:
		return i18n.T("disabled")
	case k.Validity == "u":
		return i18n.T("yours")
	case k.Validity == "f":
		return i18n.T("verified")
	case k.Validity == "m":
		return i18n.T("partly verified")
	}
	return i18n.T("not verified")
}

func gpgTag(k gpg.Key) *gtk.Label {
	tag := gtk.NewLabel(gpgValidity(k))
	tag.AddCssClass("gpg-tag")
	switch {
	case !k.Usable():
		tag.AddCssClass("gone")
	case k.Validity == "u" || k.Validity == "f":
		tag.AddCssClass("ok")
	}
	tag.SetValign(gtk.AlignCenterValue)
	return tag
}

func gpgKeyMatches(k gpg.Key, q string) bool {
	if q == "" || strings.Contains(strings.ToLower(k.Fingerprint), strings.ReplaceAll(q, " ", "")) {
		return true
	}
	return slices.ContainsFunc(k.UIDs, func(u gpg.UID) bool { return strings.Contains(strings.ToLower(u.String()), q) })
}

// gpgShortID is a key's last 8 hex digits, as people quote them, in two
// groups.
func gpgShortID(k gpg.Key) string {
	id := k.KeyID
	if len(id) >= 8 {
		id = id[len(id)-8:]
	}
	return pinentry.Grouped(id)
}

// keyByID finds a key by its fingerprint, its key ID, or one of its
// subkeys' (a message names the encryption subkey it was encrypted to).
func (a *gpgApp) keyByID(id string) *gpg.Key {
	if id == "" {
		return nil
	}
	for i, k := range a.keys {
		if k.Fingerprint == id || k.KeyID == id || strings.HasSuffix(k.Fingerprint, id) || slices.Contains(k.SubkeyIDs, id) {
			return &a.keys[i]
		}
	}
	return nil
}

// --- encrypt: from ---

// showFrom shows the user's key chosen, and the others to choose from.
func (a *gpgApp) showFrom() {
	has := len(a.mine) > 0
	a.fromSome.SetVisible(has)
	a.fromNone.SetVisible(!has)
	gtkx.Clear(&a.fromList)
	if !has {
		return
	}
	if k := a.keyByID(a.from); k != nil {
		a.fromTitle.SetText(gpgKeyLabel(*k))
		a.fromSub.SetText(gpgKeyFacts(*k))
		// A key only for signing cannot be encrypted to.
		a.toSelf.SetSensitive(k.CanEncrypt)
		a.toSelfSub.SetText(i18n.T("Encrypt to your key as well, so you can open it later"))
		if !k.CanEncrypt {
			a.toSelfSub.SetText(i18n.T("This key only signs; it cannot be encrypted to"))
		}
	}
	several := len(a.mine) > 1
	a.fromArrow.SetVisible(several)
	a.fromButton.SetCanTarget(several)
	for _, k := range a.mine {
		b := gtk.NewButton()
		b.AddCssClass("gpg-from-row")
		b.SetName("from:" + k.Fingerprint)
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		label := text(gpgKeyLabel(k), gpgKeyFacts(k))
		label.SetHexpand(true)
		gtkx.Append(row, &label.Widget)
		check := gtk.NewImageFromIconName("object-select-symbolic")
		check.SetOpacity(0)
		if k.Fingerprint == a.from {
			check.SetOpacity(1)
		}
		gtkx.Append(row, &check.Widget)
		b.SetChild(&row.Widget)
		row.Unref()
		b.ConnectClicked(&a.onButton)
		gtkx.Append(&a.fromList, &b.Widget)
	}
}

// --- encrypt: to ---

// showChips shows the recipients chosen as chips before the field.
func (a *gpgApp) showChips() {
	for _, w := range a.chips {
		a.toChips.Remove(w)
		w.Unref()
	}
	a.chips = nil
	for _, fpr := range a.to {
		k := a.keyByID(fpr)
		if k == nil {
			continue
		}
		chip := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		chip.AddCssClass("gpg-chip")
		chip.SetTooltipText(gpgKeyLabel(*k) + "\n" + pinentry.Grouped(k.Fingerprint))
		name := gtk.NewLabel(k.Name())
		name.SetEllipsize(3)
		name.SetMaxWidthChars(24)
		gtkx.Append(chip, &name.Widget)
		id := gtk.NewLabel(gpgShortID(*k))
		id.AddCssClass("gpg-mono-sub")
		gtkx.Append(chip, &id.Widget)
		x := plainIcon("unto:"+fpr, "window-close-symbolic", i18n.T("Take off"), &a.onButton)
		x.AddCssClass("tight")
		gtkx.Append(chip, &x.Widget)
		n := int32(len(a.chips))
		a.toChips.Insert(&chip.Widget, n)
		chip.Unref()
		child := a.toChips.GetChildAtIndex(n)
		child.SetFocusable(false)
		a.chips = append(a.chips, &child.Widget)
	}
	n := len(a.to)
	a.toCount.SetText(i18n.Nf("%d recipient", "%d recipients", n, n))
	if n == 0 {
		a.toCount.SetText("")
		a.toEntry.SetPlaceholderText(i18n.T("Name, address or key ID"))
	} else {
		a.toEntry.SetPlaceholderText(i18n.T("Add another"))
	}
}

// suggestions are the keys to offer for what is typed: those that can be
// encrypted to first, then expired or revoked ones (shown, to say why
// they cannot be chosen); not those chosen already, nor the user's key
// under From, nor keys only for signing.
func (a *gpgApp) suggestions() []gpg.Key {
	q := strings.ToLower(strings.TrimSpace(a.toEntry.GetText()))
	var ok, gone []gpg.Key
	for _, k := range a.keys {
		if k.Fingerprint == a.from || slices.Contains(a.to, k.Fingerprint) || !gpgKeyMatches(k, q) {
			continue
		}
		switch {
		case k.Usable() && k.CanEncrypt:
			ok = append(ok, k)
		case !k.Usable():
			gone = append(gone, k)
		}
	}
	return append(ok, gone...)
}

func (a *gpgApp) firstSuggestion() string {
	if strings.TrimSpace(a.toEntry.GetText()) == "" {
		return ""
	}
	for _, k := range a.suggestions() {
		if k.Usable() && k.CanEncrypt {
			return k.Fingerprint
		}
	}
	return ""
}

// showSuggest lists the keys matching what is typed under the box, or all
// of them while the list button is down; nothing when neither.
func (a *gpgApp) showSuggest() {
	gtkx.Clear(&a.toSuggest)
	typed := strings.TrimSpace(a.toEntry.GetText()) != ""
	if !typed && !a.toBrowse.GetActive() {
		a.toSuggest.SetVisible(false)
		return
	}
	list := a.suggestions()
	for i, k := range list {
		if i == gpgSuggestMax {
			l := gtk.NewLabel(i18n.Nf("%d more: type to narrow them down", "%d more: type to narrow them down", len(list)-i, len(list)-i))
			l.AddCssClass("sub")
			l.SetXalign(0)
			l.SetMarginTop(8)
			l.SetMarginBottom(8)
			l.SetMarginStart(12)
			gtkx.Append(&a.toSuggest, &l.Widget)
			break
		}
		b := gtk.NewButton()
		b.AddCssClass("gpg-suggest")
		b.SetName("to:" + k.Fingerprint)
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		email := ""
		if len(k.UIDs) > 0 {
			email = k.UIDs[0].Email
		}
		label := text(k.Name(), strings.TrimSpace(email+"  "+gpgShortID(k)))
		label.SetHexpand(true)
		gtkx.Append(row, &label.Widget)
		switch {
		case k.Expired:
			tag := gtk.NewLabel(i18n.Tf("expired %s", i18n.Time(k.Expires.Local(), "Jan 2, 2006")))
			tag.AddCssClass("gpg-tag")
			tag.AddCssClass("gone")
			tag.SetValign(gtk.AlignCenterValue)
			gtkx.Append(row, &tag.Widget)
		case k.Secret && k.Usable():
			tag := gtk.NewLabel(i18n.T("yours"))
			tag.AddCssClass("gpg-tag")
			tag.AddCssClass("ok")
			tag.SetValign(gtk.AlignCenterValue)
			gtkx.Append(row, &tag.Widget)
		default:
			gtkx.Append(row, &gpgTag(k).Widget)
		}
		b.SetChild(&row.Widget)
		row.Unref()
		if !k.Usable() || !k.CanEncrypt {
			b.SetSensitive(false)
			b.AddCssClass("gone")
			switch {
			case k.Expired:
				b.SetTooltipText(i18n.Tf("This key expired on %s, so gpg cannot encrypt to it. Ask %s for an updated key, then import it.",
					i18n.Time(k.Expires.Local(), "Jan 2, 2006"), k.Name()))
			case k.Revoked:
				b.SetTooltipText(i18n.Tf("%s revoked this key; ask for their new one.", k.Name()))
			default:
				b.SetTooltipText(i18n.T("This key cannot encrypt."))
			}
		} else {
			b.SetTooltipText(gpgKeyLabel(k) + "\n" + pinentry.Grouped(k.Fingerprint))
			b.ConnectClicked(&a.onButton)
		}
		gtkx.Append(&a.toSuggest, &b.Widget)
	}
	if len(list) == 0 {
		q := strings.ToLower(strings.TrimSpace(a.toEntry.GetText()))
		msg := i18n.T("No key matches. Import theirs on the Keys tab.")
		if slices.ContainsFunc(a.to, func(fpr string) bool { k := a.keyByID(fpr); return k != nil && gpgKeyMatches(*k, q) }) {
			msg = i18n.T("Already added.")
		}
		if len(a.keys) == 0 {
			msg = i18n.T("Your keyring is empty. Import their key on the Keys tab.")
		} else if !typed {
			msg = i18n.T("Every key is chosen.")
		}
		l := gtk.NewLabel(msg)
		l.AddCssClass("sub")
		l.SetXalign(0)
		l.SetMarginTop(10)
		l.SetMarginBottom(10)
		l.SetMarginStart(12)
		gtkx.Append(&a.toSuggest, &l.Widget)
	}
	a.toSuggest.SetVisible(true)
}

// gpgSuggestMax is how many matching keys are listed under the box.
const gpgSuggestMax = 8

func (a *gpgApp) addRecipient(fpr string) {
	if !slices.Contains(a.to, fpr) {
		a.to = append(a.to, fpr)
	}
	a.toEntry.SetText("")
	a.toBrowse.SetActive(false)
	a.showChips()
	a.showSuggest()
	a.toEntry.GrabFocus()
}

func (a *gpgApp) removeRecipient(fpr string) {
	a.to = slices.DeleteFunc(a.to, func(f string) bool { return f == fpr })
	a.showChips()
	a.showSuggest()
}

// --- the keys tab ---

// showKeys lists the keyring in groups: the user's keys, other people's,
// and keys that cannot be used (expired, revoked, disabled); with no key
// of the user's, the offer to create one.
func (a *gpgApp) showKeys() {
	gtkx.Clear(&a.keyList)
	var mine, others, invalid []gpg.Key
	for _, k := range a.keys {
		switch {
		case !k.Usable():
			invalid = append(invalid, k)
		case k.Secret:
			mine = append(mine, k)
		default:
			others = append(others, k)
		}
	}
	a.noKey.SetVisible(len(a.mine) == 0)
	for _, g := range []struct {
		title string
		keys  []gpg.Key
	}{{i18n.T("MY KEYS"), mine}, {i18n.T("OTHERS"), others}, {i18n.T("INVALID KEYS"), invalid}} {
		if len(g.keys) == 0 {
			continue
		}
		// A section as the other pages' are: [TITLE] and its count.
		section := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		section.AddCssClass("section")
		head := uikit.NewHead(g.title, i18n.Nf("%d key", "%d keys", len(g.keys), len(g.keys)))
		gtkx.Append(section, &head.Box.Widget)
		for _, k := range g.keys {
			gtkx.Append(section, &a.keyRow(k).Widget)
		}
		gtkx.Append(&a.keyList, &section.Widget)
	}
}

// keyRow is a key in the keyring: its name and tags, addresses,
// fingerprint and dates, and what can be done with it.
func (a *gpgApp) keyRow(k gpg.Key) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	row.AddCssClass("gpg-key")
	if !k.Usable() {
		row.AddCssClass("gone")
	}
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	title := k.Name()
	if len(k.UIDs) > 0 && k.UIDs[0].Comment != "" {
		title += " (" + k.UIDs[0].Comment + ")"
	}
	name := gtk.NewLabel(title)
	name.AddCssClass("title")
	name.SetXalign(0)
	gtkx.Append(head, &name.Widget)
	if k.Secret && !k.Usable() {
		tag := gtk.NewLabel(i18n.T("yours"))
		tag.AddCssClass("gpg-tag")
		gtkx.Append(head, &tag.Widget)
	}
	if !k.Secret || !k.Usable() {
		gtkx.Append(head, &gpgTag(k).Widget)
	}
	gtkx.Append(row, &head.Widget)
	for _, u := range k.UIDs {
		if u.Email == "" {
			continue
		}
		e := gtk.NewLabel(u.Email)
		e.AddCssClass("sub")
		e.SetXalign(0)
		gtkx.Append(row, &e.Widget)
	}
	fpr := gtk.NewLabel(pinentry.Grouped(k.Fingerprint))
	fpr.AddCssClass("gpg-mono-sub")
	fpr.SetXalign(0)
	fpr.SetSelectable(true)
	gtkx.Append(row, &fpr.Widget)
	facts := gpgKeyDates(k)
	if k.Algo != "" {
		facts = append([]string{k.Algo}, facts...)
	}
	if !k.CanEncrypt && k.Usable() {
		facts = append(facts, i18n.T("signs only"))
	}
	f := gtk.NewLabel(strings.Join(facts, " · "))
	f.AddCssClass("sub")
	f.SetXalign(0)
	gtkx.Append(row, &f.Widget)
	hint := ""
	switch {
	case k.Usable():
	case k.Secret && k.Expired && !k.Revoked:
		hint = i18n.T("Extend it to use it again. Keep it either way: it opens what was encrypted to it.")
	case k.Secret:
		hint = i18n.T("Keep it: it opens what was encrypted to it.")
	case k.Expired:
		hint = i18n.Tf("gpg cannot encrypt to it. Ask %s for an updated key, then import it.", k.Name())
	case k.Revoked:
		hint = i18n.Tf("%s revoked this key; ask for their new one.", k.Name())
	}
	if hint != "" {
		h := gtk.NewLabel(hint)
		h.AddCssClass("sub")
		h.AddCssClass("gpg-hint")
		h.SetXalign(0)
		h.SetWrap(true)
		gtkx.Append(row, &h.Widget)
	}
	acts := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
	acts.AddCssClass("gpg-acts")
	act := func(name, label string) *gtk.Button {
		b := gtk.NewButtonWithLabel(label)
		b.AddCssClass("gpg-act")
		b.SetName(name + ":" + k.Fingerprint)
		b.ConnectClicked(&a.onButton)
		acts.Append(&b.Widget)
		b.Unref()
		return b
	}
	act("fpr", i18n.T("Copy fingerprint"))
	act("pub", i18n.T("Copy public key"))
	if k.Usable() && k.CanEncrypt {
		act("encrypt-to", i18n.T("Encrypt to…"))
	}
	if k.Secret && k.Expired && !k.Revoked {
		act("extend", i18n.T("Extend 2 years"))
	}
	// Another person's key that cannot be used may go; the user's own
	// never does here (its secret key opens what was sent to it).
	if !k.Usable() && !k.Secret {
		act("delete", i18n.T("Delete…")).AddCssClass("danger")
	}
	gtkx.Append(row, &acts.Widget)
	return row
}

// --- buttons ---

func (a *gpgApp) button(name string) {
	switch kind, arg, _ := strings.Cut(name, ":"); kind {
	case "to":
		a.addRecipient(arg)
	case "unto":
		a.removeRecipient(arg)
	case "from":
		a.from = arg
		a.fromPop.Popdown()
		a.showFrom()
		a.removeRecipient(arg) // "Let me read it too" stands for it
	case "enc-choose":
		a.choose("enc")
	case "dec-choose":
		a.choose("dec")
	case "imp-choose":
		a.choose("imp")
	case "enc-clear":
		a.encFiles = nil
		a.encInput.GetBuffer().SetText("", 0)
		a.showFiles("enc")
		a.encStatus.SetText("")
	case "dec-clear":
		if a.decKind.GetVisibleChildName() == "files" {
			a.decFiles = nil
			a.showFiles("dec")
		} else {
			a.autoBlock = ""
			a.decInput.GetBuffer().SetText("", 0)
		}
		a.decResult.SetVisible(false)
		a.decStatus.SetText("")
	case "rm-enc":
		a.encFiles = slices.DeleteFunc(a.encFiles, func(p string) bool { return p == arg })
		a.showFiles("enc")
	case "rm-dec":
		a.decFiles = slices.DeleteFunc(a.decFiles, func(p string) bool { return p == arg })
		a.showFiles("dec")
	case "encrypt":
		a.runEncrypt()
	case "decrypt":
		a.runDecrypt()
	case "dec-clip":
		a.clipOffer.SetVisible(false)
		a.decKind.SetVisibleChildName("text")
		a.decInput.GetBuffer().SetText(a.clipBlock, -1) // decrypted as it comes
	case "enc-back":
		a.encView.SetVisibleChildName("compose")
	case "enc-copy":
		clip := a.win.GetClipboard()
		clip.SetText(a.encOut)
		clip.Unref()
		a.encResultStatus.SetText(i18n.T("Copied"))
	case "dec-copy":
		a.clip.Copy(a.decOut)
		a.decStatus.SetText(i18n.T("Copied; taken off the clipboard in 45 seconds"))
	case "enc-save":
		a.saveAs("enc-save", "message.asc")
	case "dec-save":
		a.saveAs("dec-save", "message.txt")
	case "keys-refresh":
		a.loadKeys()
	case "keys-back":
		a.keysView.SetVisibleChildName("list")
	case "import":
		a.impData = nil
		a.impInput.GetBuffer().SetText("", 0)
		a.impStatus.SetText("")
		a.pages.SetVisibleChildName("keys")
		a.keysView.SetVisibleChildName("import")
		a.readClipboard("imp")
	case "imp-do":
		a.runImport()
	case "create":
		a.newStatus.SetText("")
		if a.newName.GetText() == "" {
			a.newName.SetText(gpgDefaultName())
		}
		a.pages.SetVisibleChildName("keys")
		a.keysView.SetVisibleChildName("create")
		a.newName.GrabFocus()
	case "new-do":
		a.runCreate()
	case "extend":
		a.runExtend(arg)
	case "delete":
		a.askDelete(arg)
	case "trash":
		a.askTrash(arg)
	case "dialog-cancel":
		a.dlg.SetVisible(false)
	case "dialog-ok":
		a.dlg.SetVisible(false)
		if a.dlgFor == "trash" {
			a.trashDecrypted(a.dlgArg)
		} else {
			a.runDelete(a.dlgArg)
		}
	case "encrypt-to":
		a.pages.SetVisibleChildName("encrypt")
		a.encView.SetVisibleChildName("compose")
		if arg != a.from {
			a.addRecipient(arg)
		}
	case "fpr":
		clip := a.win.GetClipboard()
		clip.SetText(arg)
		clip.Unref()
		a.keysMeta.SetText(i18n.T("Fingerprint copied"))
	case "pub":
		go func() {
			pub, err := gpg.Export(arg)
			gtkx.Post(func() {
				if err != nil {
					a.keysMeta.SetText(err.Error())
					return
				}
				clip := a.win.GetClipboard()
				clip.SetText(pub)
				clip.Unref()
				a.keysMeta.SetText(i18n.T("Public key copied"))
			})
		}()
	}
}

// gpgDefaultName is the user's full name from the passwd file, to start
// a new key with.
func gpgDefaultName() string {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	uid := fmt.Sprint(os.Getuid())
	for line := range strings.SplitSeq(string(data), "\n") {
		if f := strings.Split(line, ":"); len(f) > 4 && f[2] == uid {
			name, _, _ := strings.Cut(f[4], ",")
			return name
		}
	}
	return ""
}

// --- the clipboard ---

// readClipboard looks at the clipboard for what the page can use: an
// encrypted message to decrypt, a key to import.
func (a *gpgApp) readClipboard(which string) {
	if a.clipReading {
		return
	}
	a.clipFor, a.clipReading = which, true
	clip := a.win.GetClipboard()
	clip.ReadTextAsync(nil, &a.onClipText, 0)
	clip.Unref()
}

func (a *gpgApp) clipboardRead(text string) {
	kind, block, ok := gpg.Armored(text)
	switch a.clipFor {
	case "dec":
		offer := ok && (kind == "MESSAGE" || kind == "SIGNED MESSAGE") && block != a.autoBlock &&
			strings.TrimSpace(a.textOf(&a.decInput)) == ""
		if offer {
			a.clipBlock = block
		}
		a.clipOffer.SetVisible(offer)
	case "imp":
		if ok && kind == "PUBLIC KEY BLOCK" && strings.TrimSpace(a.textOf(&a.impInput)) == "" {
			a.impInput.GetBuffer().SetText(block, -1)
		}
	}
}

func (a *gpgApp) textOf(v *gtk.TextView) string {
	buf := v.GetBuffer()
	var start, end gtk.TextIter
	buf.GetBounds(&start, &end)
	return buf.GetText(&start, &end, true)
}

// --- files ---

func (a *gpgApp) choose(which string) {
	if a.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(map[string]string{"enc": i18n.T("Files to encrypt"), "dec": i18n.T("Files to decrypt"), "imp": i18n.T("Key file to import")}[which])
	a.dialog, a.dialogFor = d, which
	d.OpenMultiple(&a.win.Window, nil, &a.onOpened, 0)
}

// addFiles takes files chosen or dropped: to encrypt (the Files tab), to
// decrypt (at once), or a key file to import (shown first).
func (a *gpgApp) addFiles(which string, paths []string) {
	var regular []string
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			regular = append(regular, p)
		}
	}
	switch which {
	case "imp":
		if len(regular) == 0 {
			return
		}
		data, err := os.ReadFile(regular[0])
		if err != nil {
			a.impStatus.SetText(err.Error())
			return
		}
		a.impInput.GetBuffer().SetText("", 0)
		a.previewImport(data)
		return
	case "dec":
		// Files dropped are decrypted at once: they are the list.
		a.pages.SetVisibleChildName("decrypt")
		a.decKind.SetVisibleChildName("files")
		a.decFiles = nil
	default:
		a.pages.SetVisibleChildName("encrypt")
		a.encView.SetVisibleChildName("compose")
		a.msgKind.SetVisibleChildName("files")
	}
	list := &a.encFiles
	if which == "dec" {
		list = &a.decFiles
	}
	for _, p := range regular {
		if !slices.Contains(*list, p) {
			*list = append(*list, p)
		}
	}
	a.showFiles(which)
	if which == "dec" && len(a.decFiles) > 0 {
		a.runDecrypt()
	}
}

// showFiles lists the files chosen, each with a button to leave it out.
func (a *gpgApp) showFiles(which string) {
	box, list := &a.encFiles_, a.encFiles
	if which == "dec" {
		box, list = &a.decFiles_, a.decFiles
	}
	gtkx.Clear(box)
	for _, p := range list {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.AddCssClass("gpg-file")
		row.SetMarginTop(6)
		l := text(filepath.Base(p), pinentryTilde(filepath.Dir(p)))
		l.SetHexpand(true)
		gtkx.Append(row, &l.Widget)
		gtkx.Append(row, &plainIcon("rm-"+which+":"+p, "window-close-symbolic", i18n.T("Leave it out"), &a.onButton).Widget)
		gtkx.Append(box, &row.Widget)
	}
	box.SetVisible(len(list) > 0)
}

// pinentryTilde writes a folder under home with "~".
func pinentryTilde(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && (dir == home || strings.HasPrefix(dir, home+"/")) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

func (a *gpgApp) saveAs(which, name string) {
	if a.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetInitialName(name)
	a.dialog, a.dialogFor = d, which
	d.Save(&a.win.Window, nil, &a.onSaved, 0)
}

// save writes the armored message, or the decrypted text, readable only
// by the user.
func (a *gpgApp) save(which, path string) {
	data, status := a.encOut, &a.encResultStatus
	if which == "dec-save" {
		data, status = a.decOut, &a.decStatus
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		status.SetText(err.Error())
		return
	}
	status.SetText(i18n.Tf("Saved to %s", pinentryTilde(path)))
}

// --- encrypt ---

func (a *gpgApp) options(armor bool) (gpg.Options, error) {
	o := gpg.Options{Armor: armor, Recipients: slices.Clone(a.to)}
	if k := a.keyByID(a.from); k != nil {
		if a.toSelf.GetActive() && k.CanEncrypt && !slices.Contains(o.Recipients, a.from) {
			o.Recipients = append(o.Recipients, a.from)
		}
		if a.sign.GetActive() {
			o.Signer = a.from
		}
	}
	if len(o.Recipients) == 0 {
		return o, fmt.Errorf("%s", i18n.T("Add someone to encrypt to."))
	}
	return o, nil
}

// runEncrypt encrypts the text, or the files: the tab shown.
func (a *gpgApp) runEncrypt() {
	if a.busy {
		return
	}
	var files []string
	if a.msgKind.GetVisibleChildName() == "files" {
		files = slices.Clone(a.encFiles)
		if len(files) == 0 {
			a.encStatus.SetText(i18n.T("Choose or drop files first."))
			return
		}
	}
	o, err := a.options(files == nil)
	if err != nil {
		a.encStatus.SetText(err.Error())
		return
	}
	msg := a.textOf(&a.encInput)
	if files == nil && strings.TrimSpace(msg) == "" {
		a.encStatus.SetText(i18n.T("Type a message first."))
		return
	}
	a.busy = true
	a.encrypt.SetSensitive(false)
	a.encStatus.SetText(i18n.T("Encrypting…"))
	go func() {
		var out []byte
		var written, errs []string
		if files != nil {
			for _, f := range files {
				if w, err := gpg.EncryptFile(f, o); err != nil {
					errs = append(errs, filepath.Base(f)+": "+err.Error())
				} else {
					written = append(written, w)
				}
			}
		} else if out, err = gpg.Encrypt([]byte(msg), o); err != nil {
			errs = append(errs, err.Error())
		}
		gtkx.Post(func() {
			a.busy = false
			a.encrypt.SetSensitive(true)
			a.encStatus.SetText(strings.Join(errs, "\n"))
			if out == nil && written == nil {
				return
			}
			isText := files == nil
			a.encOut = string(out)
			a.encOutput.GetBuffer().SetText(a.encOut, -1)
			a.encOutputFrame.SetVisible(isText)
			a.encCopy.SetVisible(isText)
			a.encSave.SetVisible(isText)
			a.showWritten(&a.encWritten, written, false)
			var names []string
			for _, fpr := range o.Recipients {
				if fpr == o.Signer || fpr == a.from {
					names = append(names, i18n.T("you"))
				} else if k := a.keyByID(fpr); k != nil {
					names = append(names, k.Name())
				}
			}
			who := i18n.Tf("For %s", strings.Join(names, ", "))
			if o.Signer != "" {
				who += " · " + i18n.T("signed")
			}
			a.encResultMeta.SetText(who)
			a.encResultStatus.SetText(strings.Join(errs, "\n"))
			a.encView.SetVisibleChildName("result")
		})
	}()
}

// showWritten lists the files written; decrypted ones (trash) each with
// a button to move it to the trash.
func (a *gpgApp) showWritten(box *gtk.Box, written []string, trash bool) {
	gtkx.Clear(box)
	for _, w := range written {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.AddCssClass("gpg-file")
		row.SetMarginTop(6)
		l := text(filepath.Base(w), i18n.Tf("written in %s", pinentryTilde(filepath.Dir(w))))
		l.SetHexpand(true)
		gtkx.Append(row, &l.Widget)
		if trash {
			b := gtk.NewButtonWithLabel(i18n.T("Move to Trash…"))
			b.AddCssClass("gpg-act")
			b.AddCssClass("danger")
			b.SetName("trash:" + w)
			b.SetValign(gtk.AlignCenterValue)
			b.ConnectClicked(&a.onButton)
			gtkx.Append(row, &b.Widget)
		}
		gtkx.Append(box, &row.Widget)
	}
	box.SetVisible(len(written) > 0)
}

// trashDecrypted moves a decrypted file to the trash.
func (a *gpgApp) trashDecrypted(path string) {
	f := gio.FileNewForPath(path)
	_, err := f.Trash(nil)
	unrefObject(f)
	if err != nil {
		a.decStatus.SetText(filepath.Base(path) + ": " + err.Error())
		return
	}
	a.decWrittenPaths = slices.DeleteFunc(a.decWrittenPaths, func(p string) bool { return p == path })
	a.showWritten(&a.decWritten, a.decWrittenPaths, true)
	a.decStatus.SetText(i18n.Tf("%s moved to the trash", filepath.Base(path)))
}

// --- decrypt ---

// decInputChanged decrypts a message once it is whole in the paste area.
func (a *gpgApp) decInputChanged() {
	a.clipOffer.SetVisible(false)
	kind, block, ok := gpg.Armored(a.textOf(&a.decInput))
	if !ok || (kind != "MESSAGE" && kind != "SIGNED MESSAGE") || block == a.autoBlock || a.busy {
		return
	}
	a.autoBlock = block
	a.runDecrypt()
}

// runDecrypt decrypts the message pasted, or the files: the tab shown.
func (a *gpgApp) runDecrypt() {
	if a.busy {
		return
	}
	var files []string
	msg := ""
	if a.decKind.GetVisibleChildName() == "files" {
		files = slices.Clone(a.decFiles)
		if len(files) == 0 {
			a.decStatus.SetText(i18n.T("Choose or drop files first."))
			return
		}
	} else {
		msg = a.textOf(&a.decInput)
		if _, block, ok := gpg.Armored(msg); ok {
			msg = block // a mail's greeting around it is not gpg's
		}
		if strings.TrimSpace(msg) == "" {
			a.decStatus.SetText(i18n.T("Paste a message first."))
			return
		}
	}
	a.decResult.SetVisible(false)
	a.busy = true
	a.decrypt.SetSensitive(false)
	a.decStatus.SetText(i18n.T("Decrypting… gpg may ask for your passphrase."))
	go func() {
		var out []byte
		var res gpg.Result
		var written, errs []string
		if len(files) > 0 {
			for _, f := range files {
				w, r, err := gpg.DecryptFile(f)
				if err != nil {
					errs = append(errs, filepath.Base(f)+": "+err.Error())
					continue
				}
				written = append(written, w)
				res = r
			}
		} else {
			var err error
			if out, res, err = gpg.Decrypt([]byte(msg)); err != nil {
				errs = append(errs, err.Error())
			}
		}
		gtkx.Post(func() {
			a.busy = false
			a.decrypt.SetSensitive(true)
			a.decStatus.SetText(strings.Join(errs, "\n"))
			if out == nil && written == nil {
				return
			}
			isText := len(files) == 0
			a.decOut = string(out)
			a.decOutput.GetBuffer().SetText(a.decOut, -1)
			a.decOutputFrame.SetVisible(isText)
			a.decCopy.SetVisible(isText)
			a.decSave.SetVisible(isText)
			a.decWrittenPaths = written
			a.showWritten(&a.decWritten, written, true)
			a.showSignature(res)
			a.decResult.SetVisible(true)
		})
	}()
}

// showSignature says who signed it, and whether the signature holds, and
// who it was encrypted to.
func (a *gpgApp) showSignature(r gpg.Result) {
	for _, c := range []string{"good", "bad", "unknown", "none"} {
		a.signature.RemoveCssClass(c)
	}
	state, icon := "none", "dialog-information-symbolic"
	title, sub := i18n.T("Not signed: who sent it is not known"), ""
	signer := r.Signer
	if k := a.keyByID(r.SignerKey); k != nil {
		signer = gpgKeyLabel(*k)
	}
	switch r.Signature {
	case "good":
		state, icon = "good", "pass-certificate-symbolic"
		title = i18n.Tf("Signed by %s", signer)
		sub = pinentry.Grouped(r.SignerKey)
		if !r.SignedAt.IsZero() {
			sub += "  ·  " + i18n.Time(r.SignedAt.Local(), "Jan 2, 2006 15:04")
		}
	case "bad":
		state, icon = "bad", "dialog-warning-symbolic"
		title = i18n.Tf("The signature by %s does not match: it was changed after signing", signer)
		sub = r.SignerKey
	case "unknown":
		state, icon = "unknown", "dialog-warning-symbolic"
		title = i18n.T("Signed by a key not in your keyring, so it cannot be checked")
		sub = r.SignerKey
	case "expired", "revoked":
		state, icon = "unknown", "dialog-warning-symbolic"
		title = i18n.Tf("Signed by %s, with a key that is no longer valid", signer)
		sub = r.SignerKey
	}
	a.signature.AddCssClass(state)
	a.signatureIcon.SetFromIconName(icon)
	a.signatureTitle.SetText(title)
	a.signatureSub.SetText(sub)
	a.signatureSub.SetVisible(sub != "")
	var to []string
	for _, id := range r.To {
		if k := a.keyByID(id); k != nil {
			to = append(to, gpgKeyLabel(*k))
		} else {
			to = append(to, id)
		}
	}
	a.decTo.SetText("")
	if len(to) > 0 {
		a.decTo.SetText(i18n.Tf("Encrypted to %s", strings.Join(to, ", ")))
	}
	a.decTo.SetVisible(len(to) > 0)
}

// --- import ---

// previewImport shows the keys in data (a file), or else in the paste
// area, before importing them.
func (a *gpgApp) previewImport(data []byte) {
	if data == nil {
		data = []byte(a.textOf(&a.impInput))
	}
	gtkx.Clear(&a.impPreview)
	a.impData = nil
	a.impDo.SetSensitive(false)
	a.impDo.SetLabel(i18n.T("Import"))
	a.impStatus.SetText("")
	if strings.TrimSpace(string(data)) == "" {
		return
	}
	go func() {
		keys, err := gpg.ShowKeys(data)
		gtkx.Post(func() {
			gtkx.Clear(&a.impPreview)
			if err != nil {
				a.impStatus.SetText(i18n.T("No key found in it."))
				return
			}
			a.impData = data
			a.impDo.SetSensitive(true)
			a.impDo.SetLabel(i18n.Nf("Import %d key", "Import %d keys", len(keys), len(keys)))
			for _, k := range keys {
				row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
				row.AddCssClass("gpg-file")
				row.SetMarginTop(6)
				email := ""
				if len(k.UIDs) > 0 {
					email = k.UIDs[0].Email
				}
				label := text(k.Name(), strings.TrimSpace(email+"  "+pinentry.Grouped(k.Fingerprint)))
				label.SetHexpand(true)
				gtkx.Append(row, &label.Widget)
				what := i18n.T("new")
				switch {
				case k.Secret:
					what = i18n.T("holds a secret key")
				case a.keyByID(k.Fingerprint) != nil:
					what = i18n.T("updates one you have")
				}
				if !k.Usable() {
					gtkx.Append(row, &gpgTag(k).Widget)
				}
				tag := gtk.NewLabel(what)
				tag.AddCssClass("gpg-tag")
				tag.SetValign(gtk.AlignCenterValue)
				if k.Secret {
					tag.AddCssClass("warn")
				}
				gtkx.Append(row, &tag.Widget)
				gtkx.Append(&a.impPreview, &row.Widget)
			}
		})
	}()
}

func (a *gpgApp) runImport() {
	if a.busy || a.impData == nil {
		return
	}
	data := a.impData
	a.busy = true
	a.impDo.SetSensitive(false)
	a.impStatus.SetText(i18n.T("Importing…"))
	go func() {
		fprs, err := gpg.Import(data)
		gtkx.Post(func() {
			a.busy = false
			a.impDo.SetSensitive(true)
			if err != nil {
				a.impStatus.SetText(err.Error())
				return
			}
			a.keysView.SetVisibleChildName("list")
			a.loadKeys(i18n.Nf("%d key imported", "%d keys imported", len(fprs), len(fprs)))
		})
	}()
}

// --- create and extend ---

func (a *gpgApp) runCreate() {
	if a.busy {
		return
	}
	name, email := strings.TrimSpace(a.newName.GetText()), strings.TrimSpace(a.newEmail.GetText())
	if name == "" || email == "" {
		a.newStatus.SetText(i18n.T("Give a name and an address."))
		return
	}
	if !strings.Contains(email, "@") {
		a.newStatus.SetText(i18n.T("That address has no @."))
		return
	}
	expire := gpg.Expiries[min(int(a.newExpire.GetSelected()), len(gpg.Expiries)-1)]
	a.busy = true
	a.newDo.SetSensitive(false)
	a.newStatus.SetText(i18n.T("Creating… gpg asks for a passphrase for the new key."))
	go func() {
		fpr, err := gpg.Generate(name, email, expire)
		gtkx.Post(func() {
			a.busy = false
			a.newDo.SetSensitive(true)
			if err != nil {
				a.newStatus.SetText(err.Error())
				return
			}
			a.from = fpr
			a.newName.SetText("")
			a.newEmail.SetText("")
			a.keysView.SetVisibleChildName("list")
			a.loadKeys(i18n.T("Your key is created"))
		})
	}()
}

// ask opens the dialog: what, which, what it means, and the button that
// does it.
func (a *gpgApp) ask(what, arg, title, which, text, ok string) {
	if a.dlg.GetVisible() {
		return
	}
	a.dlgFor, a.dlgArg = what, arg
	a.dlgTitle.SetText(title)
	a.dlgKey.SetText(which)
	a.dlgText.SetText(text)
	a.dlgOK.SetLabel(ok)
	a.dlg.Present()
	a.dlgOK.GrabFocus()
}

// askDelete asks before another person's key goes.
func (a *gpgApp) askDelete(fpr string) {
	if k := a.keyByID(fpr); k != nil {
		a.ask("key", fpr, i18n.Tf("Delete the key of %s?", k.Name()),
			gpgKeyLabel(*k)+"\n"+pinentry.Grouped(k.Fingerprint),
			i18n.T("It goes from your keyring. Messages it signed then show as signed by a key not in your keyring; importing it again brings that back."),
			i18n.T("Delete"))
	}
}

// askTrash asks before a decrypted file goes to the trash.
func (a *gpgApp) askTrash(path string) {
	a.ask("trash", path, i18n.Tf("Move %s to the trash?", filepath.Base(path)), pinentryTilde(path),
		i18n.T("The decrypted copy goes to the trash, where it can be restored from; the encrypted file stays."),
		i18n.T("Move to Trash"))
}

func (a *gpgApp) runDelete(fpr string) {
	if a.busy {
		return
	}
	a.busy = true
	go func() {
		err := gpg.Delete(fpr)
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.keysMeta.SetText(err.Error())
				return
			}
			a.loadKeys(i18n.T("Key deleted"))
		})
	}()
}

func (a *gpgApp) runExtend(fpr string) {
	if a.busy {
		return
	}
	a.busy = true
	a.keysMeta.SetText(i18n.T("Extending… gpg asks for your passphrase."))
	go func() {
		err := gpg.Extend(fpr, "2y")
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.keysMeta.SetText(err.Error())
				return
			}
			a.loadKeys(i18n.T("Extended by 2 years"))
		})
	}()
}
