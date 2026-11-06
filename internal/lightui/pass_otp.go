package lightui

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg" // images with a QR code
	_ "image/png"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/pass"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// Adding a one-time code (docs/feat/pass-otp.md): a dialog over the
// editor takes the secret or the otpauth:// link, or reads a QR code from
// the screen, an area of it, or an image; Add puts the otpauth:// line in
// the editor's text, which Save encrypts.

// passOTP is the dialog's widgets and state.
type passOTP struct {
	win                       gtk.Window
	title, code, left         gtk.Label
	status, err               gtk.Label
	input, issuer, account    gtk.Entry
	preview                   gtk.Box
	ok, scan, area, fromImage gtk.Button
	uri                       string // what the input makes, "" while it does not read
	otp                       *pass.OTP
	reading                   bool // the screen or an image is read
	open                      bool
	file                      *gtk.FileDialog
	onChanged                 func(uintptr, uintptr)
	onActivate                func(gtk.Entry)
	onKey                     func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onClose                   func(gtk.Window) bool
	onChosen                  gio.AsyncReadyCallback
}

// otpWidgets are the dialog's widgets in pass.ui, for build to get.
func (a *passApp) otpWidgets() map[string]gobject.Ptr {
	o := &a.otpDlg
	return map[string]gobject.Ptr{
		"otp_dialog": &o.win, "otp_title": &o.title, "otp_code": &o.code, "otp_left": &o.left,
		"otp_status": &o.status, "otp_error": &o.err, "otp_input": &o.input, "otp_issuer": &o.issuer,
		"otp_account": &o.account, "otp_preview": &o.preview, "otp_ok": &o.ok, "otp_scan": &o.scan,
		"otp_area": &o.area, "otp_image": &o.fromImage,
	}
}

// bindOTP connects the dialog, once its widgets are got.
func (a *passApp) bindOTP(cancel *gtk.Button) {
	o := &a.otpDlg
	o.onChanged = func(uintptr, uintptr) { a.checkOTP() }
	o.onActivate = func(gtk.Entry) { a.addOTP() }
	o.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == 0xff1b { // Escape
			a.closeOTP()
			return true
		}
		return false
	}
	o.onClose = func(gtk.Window) bool {
		a.closeOTP()
		return true // hidden, kept for the next one
	}
	o.onChosen = func(_, result, _ uintptr) {
		d := o.file
		o.file = nil
		if d == nil {
			return
		}
		defer d.Unref()
		f, err := d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || f == nil {
			return // cancelled
		}
		path := f.GetPath()
		unrefObject(f)
		a.readQR(func() (image.Image, error) {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return nil, errors.New(i18n.T("It is not an image this app reads (PNG or JPEG)."))
			}
			return img, nil
		}, false)
	}
	for _, e := range []*gtk.Entry{&o.input, &o.issuer, &o.account} {
		gtkx.ConnectChanged(e, &o.onChanged)
		e.ConnectActivate(&o.onActivate)
	}
	for _, b := range []*gtk.Button{&o.ok, &o.scan, &o.area, &o.fromImage, cancel} {
		b.ConnectClicked(&a.onButton)
	}
	o.win.SetApplication(a.c.App)
	o.win.SetTitle(i18n.T("Pass"))
	o.win.SetTransientFor(&a.win.Window)
	o.win.ConnectCloseRequest(&o.onClose)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&o.onKey)
	o.win.AddController(&keys.EventController)
}

// otpButton answers the dialog's buttons; false for another button.
func (a *passApp) otpButton(name string) bool {
	switch name {
	case "otp-entry": // on an entry: its editor, then the dialog
		if a.selected == "" || a.raw == "" {
			return true
		}
		a.openEditor(a.selected, a.raw)
		a.openOTP()
	case "edit-otp":
		a.openOTP()
	case "otp-cancel":
		a.closeOTP()
	case "otp-ok":
		a.addOTP()
	case "otp-scan":
		a.scanScreen(false)
	case "otp-area":
		a.scanScreen(true)
	case "otp-image":
		a.chooseQRImage()
	default:
		return false
	}
	return true
}

// openOTP opens the dialog over the editor: with the code the text has,
// to replace; else empty, its issuer and account guessed from the
// entry's name and fields.
func (a *passApp) openOTP() {
	o := &a.otpDlg
	if o.open {
		return
	}
	o.open = true
	e := pass.Parse(a.editorText())
	a.filling = true
	o.input.SetText(e.OTP)
	issuer, account := pass.OTPLabel(e.OTP)
	if e.OTP == "" {
		name := a.selected
		if a.editNew {
			name = a.newName
		}
		_, sub := pass.Split(name)
		issuer = sub[strings.LastIndex(sub, "/")+1:]
		for _, f := range e.Fields {
			switch strings.ToLower(f.Key) {
			case "user", "username", "login", "email", "account":
				account = f.Value
			}
		}
	}
	o.issuer.SetText(issuer)
	o.account.SetText(account)
	a.filling = false
	if e.OTP != "" {
		o.title.SetText(i18n.T("Replace the one-time code"))
		o.ok.SetLabel(i18n.T("Replace"))
	} else {
		o.title.SetText(i18n.T("Add a one-time code"))
		o.ok.SetLabel(i18n.T("Add"))
	}
	o.status.SetVisible(false)
	a.checkOTP()
	o.err.SetVisible(false) // nothing typed yet is not an error
	o.win.Present()
	o.input.GrabFocus()
}

func (a *passApp) closeOTP() {
	o := &a.otpDlg
	o.open, o.otp, o.uri = false, nil, ""
	a.filling = true
	o.input.SetText("")
	a.filling = false
	o.win.SetVisible(false)
}

// checkOTP reads what is typed: the code it makes, shown as it would be
// now, or why it does not read.
func (a *passApp) checkOTP() {
	o := &a.otpDlg
	if a.filling {
		return
	}
	o.uri, o.otp = "", nil
	uri, err := pass.OTPURI(o.input.GetText(), o.issuer.GetText(), o.account.GetText())
	if err == nil {
		if otp, perr := pass.ParseOTP(uri); perr == nil {
			o.uri, o.otp = uri, &otp
		} else {
			err = perr
		}
	}
	o.ok.SetSensitive(o.otp != nil && !o.reading)
	o.preview.SetVisible(o.otp != nil)
	typed := strings.TrimSpace(o.input.GetText()) != ""
	o.err.SetVisible(err != nil && typed)
	if err != nil {
		o.err.SetText(err.Error())
	}
	a.tickOTPDialog()
}

// tickOTPDialog shows the dialog's code now; each second while it is
// open.
func (a *passApp) tickOTPDialog() {
	o := &a.otpDlg
	if !o.open || o.otp == nil {
		return
	}
	code, left := o.otp.Code(time.Now())
	o.code.SetText(code[:len(code)/2] + " " + code[len(code)/2:])
	secs := int(left.Round(time.Second) / time.Second)
	o.left.SetText(i18n.Nf("%d second left", "%d seconds left", secs, secs))
}

// addOTP puts the code's otpauth:// line in the editor's text.
func (a *passApp) addOTP() {
	o := &a.otpDlg
	if o.uri == "" || o.reading {
		return
	}
	a.setEditorText(pass.WithOTP(a.editorText(), o.uri))
	a.closeOTP()
	a.focusEditor(-1)
}

// scanScreen reads the QR codes on the screen, or on an area of it the
// user drags out, while the Pass windows step aside.
func (a *passApp) scanScreen(area bool) {
	a.readQR(func() (image.Image, error) {
		if _, err := exec.LookPath("grim"); err != nil {
			return nil, errors.New(i18n.T("Reading the screen needs grim."))
		}
		if area {
			if _, err := exec.LookPath("slurp"); err != nil {
				return nil, errors.New(i18n.T("Selecting an area needs slurp."))
			}
			out, err := exec.Command("slurp").Output()
			if err != nil {
				return nil, errors.New(i18n.T("No area was chosen."))
			}
			return grab("-g", strings.TrimSpace(string(out)))
		}
		return nil, nil // each monitor, below
	}, true)
}

// grab takes the screen with grim (its arguments before the output), as
// an image.
func grab(args ...string) (image.Image, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "grim", append(args, "-t", "png", "-")...).Output()
	if err != nil {
		return nil, errors.New(i18n.T("The screen could not be read."))
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	return img, err
}

// monitorImages are the monitors, one image each (Hyprland's), or the
// whole screen at once elsewhere; those that cannot be taken left out.
func monitorImages() ([]image.Image, error) {
	var names []string
	if b, err := compositor.Detect(); err == nil {
		if sn, ok := b.(compositor.Snapshotter); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			snap, err := sn.Snapshot(ctx)
			cancel()
			if err == nil {
				for _, m := range snap.Outputs {
					names = append(names, m.Name)
				}
			}
		}
	}
	if len(names) == 0 {
		img, err := grab()
		if err != nil {
			return nil, err
		}
		return []image.Image{img}, nil
	}
	imgs := make([]image.Image, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() { imgs[i], _ = grab("-o", name) })
	}
	wg.Wait()
	imgs = slices.DeleteFunc(imgs, func(img image.Image) bool { return img == nil })
	if len(imgs) == 0 {
		return nil, errors.New(i18n.T("The screen could not be read."))
	}
	return imgs, nil
}

// chooseQRImage asks for an image with a QR code.
func (a *passApp) chooseQRImage() {
	o := &a.otpDlg
	if o.file != nil || o.reading {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("An image with a QR code"))
	f := gtk.NewFileFilter()
	f.SetName(i18n.T("Images"))
	f.AddMimeType("image/png")
	f.AddMimeType("image/jpeg")
	filters := gio.NewListStore(gtk.FileFilterGLibType())
	filters.Append(&f.Object)
	d.SetFilters(filters)
	o.file = d
	d.Open(&o.win, nil, &o.onChosen, 0)
}

// readQR reads the one-time code from an image got off the main thread
// (nil: every monitor's); hide puts the Pass windows aside meanwhile, as
// the screen is taken.
func (a *passApp) readQR(get func() (image.Image, error), hide bool) {
	o := &a.otpDlg
	if o.reading {
		return
	}
	o.reading = true
	o.ok.SetSensitive(false)
	o.err.SetVisible(false)
	o.status.SetText(i18n.T("Reading the QR code…"))
	o.status.SetVisible(true)
	if hide {
		o.win.SetVisible(false)
		a.win.SetVisible(false)
	}
	go func() {
		if hide {
			time.Sleep(300 * time.Millisecond) // the windows gone from the screen
		}
		img, err := get()
		imgs := []image.Image{img}
		if err == nil && img == nil { // the whole screen
			imgs, err = monitorImages()
		}
		uri := ""
		if err == nil {
			var texts []string
			for _, img := range imgs {
				texts = append(texts, pass.ReadQR(img)...)
			}
			uri, err = pass.OTPFromQR(texts)
		}
		gtkx.Post(func() {
			o.reading = false
			o.status.SetVisible(false)
			if hide {
				// The window first; the dialog once it is back, so it
				// comes over it.
				a.win.SetVisible(true)
				go func() {
					time.Sleep(150 * time.Millisecond)
					gtkx.Post(func() {
						if o.open {
							o.win.Present()
						}
					})
				}()
			}
			if !o.open {
				return
			}
			if err != nil {
				a.checkOTP()
				o.err.SetText(err.Error())
				o.err.SetVisible(true)
				return
			}
			issuer, account := pass.OTPLabel(uri)
			a.filling = true
			o.input.SetText(uri)
			if issuer != "" {
				o.issuer.SetText(issuer)
			}
			if account != "" {
				o.account.SetText(account)
			}
			a.filling = false
			a.checkOTP()
		})
	}()
}
