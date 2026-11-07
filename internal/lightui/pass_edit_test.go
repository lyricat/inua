package lightui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/pass"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/sockdir"
)

// passTestView builds Pass, with no store, and runs check on its window.
type passTestView struct {
	passApp
	check func()
}

func (v *passTestView) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	w := v.passApp.Build(c)
	c.StartHidden = true
	c.App.Hold()
	gtkx.Post(func() {
		defer c.App.Quit()
		defer c.App.Release()
		v.check()
	})
	return w
}

// startBroadway gives the test a display of its own, and a home, a
// session bus and no system bus: nothing of the desktop it runs on.
func startBroadway(t *testing.T) {
	t.Helper()
	broadway, err := exec.LookPath("gtk4-broadwayd")
	if err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
	dir := sockdir.New(t)
	for key, value := range map[string]string{
		"HOME": dir, "XDG_RUNTIME_DIR": dir,
		"XDG_CONFIG_HOME":    filepath.Join(dir, "config"),
		"XDG_DATA_HOME":      filepath.Join(dir, "data"),
		"XDG_CACHE_HOME":     filepath.Join(dir, "cache"),
		"XDG_STATE_HOME":     filepath.Join(dir, "state"),
		"PASSWORD_STORE_DIR": filepath.Join(dir, "no-store"),
		"GDK_BACKEND":        "broadway", "BROADWAY_DISPLAY": ":0",
		"GTK_A11Y": "none", "GTK_THEME": "", "LANGUAGE": "en",
		"DBUS_SESSION_BUS_ADDRESS": dbustest.Start(t),
		"DBUS_SYSTEM_BUS_ADDRESS":  "unix:path=" + filepath.Join(dir, "no-system-bus"),
	} {
		t.Setenv(key, value)
	}
	cmd := exec.Command(broadway, "--unixsocket="+filepath.Join(dir, "http.sock"), ":0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "broadway1.socket")); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("Broadway display did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The editor's rows and its text show one draft: opening, editing a row,
// switching to the text and back, typing in the text, adding and removing
// lines and making a password keep every byte not edited.
func TestPassEditorRowsAndText(t *testing.T) {
	startBroadway(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	v := &passTestView{}
	a := &v.passApp
	ran := false
	v.check = func() {
		e := &a.ed
		rows := func() []string {
			out := []string{e.password.GetText()}
			for _, l := range e.lineEntries {
				out = append(out, l.GetText())
			}
			return out
		}
		bufText := func() string {
			buf := a.editor.GetBuffer()
			var s, end gtk.TextIter
			buf.GetBounds(&s, &end)
			return buf.GetText(&s, &end, true)
		}
		same := func(step, got, want string) {
			t.Helper()
			if got != want {
				t.Errorf("%s: %q, want %q", step, got, want)
			}
		}

		text := "pw  \r\n\n  user: me\r\notpauth://totp/x?secret=JBSWY3DPEHPK3PXP\r\n\r\n"
		a.openEditor("web/site", text)
		if e.mode != editRows || e.stack.GetVisibleChildName() != editRows || !e.rowsMode.GetActive() {
			t.Fatalf("the editor opens in %q, not its rows", e.mode)
		}
		if got := strings.Join(rows(), "|"); got != "pw  ||  user: me|otpauth://totp/x?secret=JBSWY3DPEHPK3PXP|" {
			t.Errorf("rows = %q", got)
		}
		if e.password.GetVisibility() {
			t.Error("the password shows on opening")
		}
		same("opened", a.editorText(), text)

		e.lineEntries[1].SetText("  user: you") // typed in a row
		want := "pw  \r\n\n  user: you\r\notpauth://totp/x?secret=JBSWY3DPEHPK3PXP\r\n\r\n"
		same("row edited", a.editorText(), want)

		e.textMode.SetActive(true)
		if e.mode != editText || e.stack.GetVisibleChildName() != editText || e.rowsMode.GetActive() {
			t.Fatalf("mode %q after Text", e.mode)
		}
		same("the text", bufText(), want)
		same("the text unedited", a.editorText(), want)

		buf := a.editor.GetBuffer()
		var end gtk.TextIter
		buf.GetEndIter(&end)
		buf.Insert(&end, "notes", -1) // typed in the text
		e.rowsMode.SetActive(true)
		want += "notes"
		same("back to the rows", a.editorText(), want)
		if n := len(e.lineEntries); n != 5 || e.lineEntries[4].GetText() != "notes" {
			t.Errorf("rows after the text = %q", rows())
		}

		a.button("edit-add-line")
		e.lineEntries[5].SetText("pin: 1234")
		want += "\r\npin: 1234" // the first line's newline
		same("line added", a.editorText(), want)
		a.button("edit-remove-line:1") // the blank line
		want = "pw  \r\n" + strings.TrimPrefix(want, "pw  \r\n\n")
		same("line removed", a.editorText(), want)

		a.button("edit-reveal")
		if !e.password.GetVisibility() {
			t.Error("Show left the password hidden")
		}

		// Over a password, the button asks first.
		a.button("edit-generate")
		same("asked, not answered", e.password.GetText(), "pw  ")
		a.generatePassword()
		p := e.password.GetText()
		if len(p) != pass.PasswordLength || p == "pw  " {
			t.Errorf("generated %q", p)
		}
		same("generated", a.editorText(), p+strings.TrimPrefix(want, "pw  "))
		if a.editError.GetVisible() {
			t.Errorf("error shown: %q", a.editError.GetText())
		}

		// A new entry: one empty password row, made at once.
		a.closeEditor()
		same("closed", a.editorText(), "")
		a.openEditor("new/entry", "")
		if len(e.lineEntries) != 0 || e.password.GetText() != "" {
			t.Errorf("a new entry's rows = %q", rows())
		}
		a.button("edit-generate")
		if p := a.editorText(); len(p) != pass.PasswordLength {
			t.Errorf("new entry's text = %q", p)
		}
		a.closeEditor()
		ran = true
	}
	if code := appui.RunView("pass", appui.IDBase+".PassTest", "Pass", v, true, nil, ""); code != 0 {
		t.Fatalf("application exit code = %d", code)
	}
	if !ran {
		t.Fatal("the checks did not run to their end")
	}
}
