package passask

import (
	"testing"

	"github.com/lyricat/inua/internal/pinentry"
)

// A passphrase, a yes or no and a note go to the web terminal's pages,
// with gpg's own words and who asks; a new passphrase and inua-keyring's
// approvals stay the desktop's, and so does a user name seen as typed.
func TestForPages(t *testing.T) {
	s, ok := ForPages(pinentry.Request{Mode: pinentry.GetPin, App: "ansible-playbook", Desc: "Please enter the passphrase", Prompt: "Passphrase:", Error: "Bad Passphrase", OK: "OK", Cancel: "Cancel"})
	if !ok || s.Mode != "getpin" || s.Title != "ansible-playbook asks for your passphrase" || s.Desc != "Please enter the passphrase" ||
		s.Prompt != "Passphrase:" || s.Error != "Bad Passphrase" || s.OK != "OK" || s.Cancel != "Cancel" {
		t.Errorf("a passphrase: %+v %v", s, ok)
	}
	if s, ok := ForPages(pinentry.Request{Mode: pinentry.Confirm, NotOK: "No"}); !ok || s.Title != "gpg asks" || s.NotOK != "No" {
		t.Errorf("a confirm: %+v %v", s, ok)
	}
	if s, _ := ForPages(pinentry.Request{Mode: pinentry.GetPin, App: "firefox", Keyring: true}); s.Title != "inua-keyring asks for your passphrase, for firefox" {
		t.Errorf("for inua-keyring: %q", s.Title)
	}
	for _, q := range []pinentry.Request{
		{Mode: pinentry.GetPin, Repeat: "Repeat:"},
		{Mode: pinentry.GetPin, Visible: true},
		{Mode: pinentry.Approve},
	} {
		if _, ok := ForPages(q); ok {
			t.Errorf("%+v went to the pages", q)
		}
	}
}
