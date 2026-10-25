package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/passask"
	"github.com/lyricat/inua/internal/pinentry"
)

// SSH_ASKPASS_PROMPT and the asker's words choose the question: a secret,
// a user name seen as typed, a yes or no, a note.
func TestQuestion(t *testing.T) {
	for _, tc := range []struct {
		prompt, kind string
		mode         pinentry.Mode
		visible      bool
		yesNo        bool
	}{
		{"Enter passphrase for key '/home/u/.ssh/id_ed25519': ", "", pinentry.GetPin, false, false},
		{"u@host's password: ", "", pinentry.GetPin, false, false},
		{"[sudo] password for u: ", "", pinentry.GetPin, false, false},
		{"Username for 'https://example.org': ", "", pinentry.GetPin, true, false},
		{"Password for 'https://u@example.org': ", "", pinentry.GetPin, false, false},
		{"The authenticity of host 'h' can't be established.\nAre you sure you want to continue connecting (yes/no/[fingerprint])? ", "", pinentry.Confirm, false, true},
		{"Allow use of key /home/u/.ssh/id_ed25519?", "confirm", pinentry.Confirm, false, false},
		{"Confirm user presence for key ED25519-SK", "none", pinentry.Message, false, false},
	} {
		q, yesNo := question(tc.prompt, tc.kind)
		if q.Mode != tc.mode || q.Visible != tc.visible || yesNo != tc.yesNo || q.Desc == "" || q.Desc[len(q.Desc)-1] == ' ' {
			t.Errorf("%q (%q): %+v, yes/no %v", tc.prompt, tc.kind, q, yesNo)
		}
	}
	if q, _ := question("Are you sure you want to continue connecting (yes/no/[fingerprint])? ", ""); q.Desc != "Are you sure you want to continue connecting?" {
		t.Errorf("a host's key: %q", q.Desc)
	}
}

// What it prints and how it exits: the answer and a newline with 0, or
// "yes" for a host's key; a cancel, "no" and a failure exit 1.
func TestAnswer(t *testing.T) {
	ok := passask.Reply{Answer: pinentry.Answer{OK: true, Pin: "secret"}, Result: keyring.Entered}
	secret, _ := question("u@host's password:", "")
	host, yesNo := question("Are you sure you want to continue connecting (yes/no/[fingerprint])?", "")
	confirm, _ := question("Allow use of key?", "confirm")
	note, _ := question("Touch your key", "none")
	for _, tc := range []struct {
		name  string
		q     pinentry.Request
		yesNo bool
		r     passask.Reply
		out   string
		exit0 bool
	}{
		{"a secret", secret, false, ok, "secret\n", true},
		{"a host's key", host, yesNo, passask.Reply{Answer: pinentry.Answer{OK: true}, Result: keyring.Entered}, "yes\n", true},
		{"a confirm", confirm, false, passask.Reply{Answer: pinentry.Answer{OK: true}, Result: keyring.Entered}, "", true},
		{"a note", note, false, passask.Reply{Answer: pinentry.Answer{OK: true}, Result: keyring.Entered}, "", true},
		{"cancelled", secret, false, passask.Reply{Result: keyring.Cancelled}, "", false},
		{"not ok", confirm, false, passask.Reply{Answer: pinentry.Answer{NotOK: true}, Result: keyring.Entered}, "", false},
		{"failed", secret, false, passask.Reply{Result: keyring.Failed, Err: errors.New("no shell")}, "", false},
	} {
		if out, exit0 := answer(tc.q, tc.yesNo, tc.r); out != tc.out || exit0 != tc.exit0 {
			t.Errorf("%s: %q, exit 0 %v", tc.name, out, exit0)
		}
	}
}

// For sudo, the dialog shows the command it runs as root, without sudo's
// own options.
func TestSudoCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{"sudo", []string{"/usr/bin/sudo", "-k", "-A", "apt", "install", "wtype"}, "apt install wtype", true},
		{"sudo", []string{"sudo", "-k", "-A", "-u", "postgres", "psql", "-l"}, "psql -l", true},
		{"sudo-rs", []string{"sudo", "--user=root", "-p", "", "--", "bash", "-c", "uname"}, "bash -c uname", true},
		{"sudo", []string{"sudo", "-v"}, "", false},
		{"ssh", []string{"ssh", "host"}, "", false},
	} {
		if got, ok := sudoCommand(tc.name, tc.args); got != tc.want || ok != tc.ok {
			t.Errorf("%s %v: %q %v", tc.name, tc.args, got, ok)
		}
	}
	var q pinentry.Request
	asRoot(&q, "apt install wtype")
	if q.Mode != pinentry.GetPin || q.Visible || !strings.Contains(q.Desc, "apt install wtype") {
		t.Errorf("%+v", q)
	}
}

// Through Mister Morph, a command not approved says why in its helper's
// words, which tell the assistant a declined command from a failed one.
func TestMorphMarker(t *testing.T) {
	for _, tc := range []struct {
		late bool
		r    passask.Reply
		want string
	}{
		{false, passask.Reply{Result: keyring.Cancelled}, "mistermorph: administrator approval was declined"},
		{true, passask.Reply{Result: keyring.Failed}, "mistermorph: administrator approval timed out"},
		{false, passask.Reply{Result: keyring.Failed, Err: errors.New("no shell")}, "mistermorph: the approval dialog failed: no shell"},
	} {
		if got := morphMarker(tc.late, tc.r); got != tc.want {
			t.Errorf("%v %+v: %q", tc.late, tc.r, got)
		}
	}
}
