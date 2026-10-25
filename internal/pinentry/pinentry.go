// Package pinentry speaks gpg-agent's pinentry protocol (Assuan): the
// agent describes what it needs (SETDESC, SETPROMPT, SETERROR …) and asks
// for it (GETPIN, CONFIRM, MESSAGE); something else asks the user. inua's
// pinentry has the shell ask, in its password dialog.
package pinentry

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Mode is what the agent asks for.
type Mode string

const (
	GetPin  Mode = "getpin"  // a passphrase or PIN
	Confirm Mode = "confirm" // yes or no
	Message Mode = "message" // only to be read
	// Approve is inua-keyring's, not the agent's: may an app read a
	// secret? The answer is "always", "once", or cancelled (deny).
	Approve Mode = "approve"
)

// Request is one question, with what the agent set before asking it.
type Request struct {
	Mode    Mode   `json:"mode"`
	Title   string `json:"title,omitempty"`
	Desc    string `json:"desc,omitempty"`   // what for: "Please enter the passphrase to unlock …"
	Prompt  string `json:"prompt,omitempty"` // beside the field: "Passphrase:"
	Error   string `json:"error,omitempty"`  // why it asks again: "Bad Passphrase"
	OK      string `json:"ok,omitempty"`     // the buttons' labels, mnemonics stripped
	Cancel  string `json:"cancel,omitempty"`
	NotOK   string `json:"notok,omitempty"`  // a third button for CONFIRM
	Repeat  string `json:"repeat,omitempty"` // ask twice (a new passphrase): the second field's prompt
	NoMatch string `json:"nomatch,omitempty"`
	Timeout int    `json:"timeout,omitempty"` // seconds; 0 for none
	// Visible shows the answer as it is typed: a user name, not a secret
	// (inua-askpass).
	Visible bool `json:"visible,omitempty"`

	// Owner is the process gpg-agent asks for (OPTION owner=PID/UID
	// HOST): gpg, run by whatever wants the key; 0 when it did not say.
	Owner int `json:"-"`
	// KeyGrip names the key asked for (SETKEYINFO); "" when not a key.
	KeyGrip string `json:"-"`

	// Who wants it and what for, which inua-pinentry finds, for the
	// dialog. App is the program ("pass", or the app inua-keyring serves);
	// Chain what it runs under ("zsh ← ghostty"); Keyring is set when
	// inua-keyring asks for App.
	App     string `json:"app,omitempty"`
	Chain   string `json:"chain,omitempty"`
	Keyring bool   `json:"keyring,omitempty"`
	// Entry is the pass entry gpg decrypts ("work/mail"), Secret the
	// secret inua-keyring was asked for.
	Entry  string `json:"entry,omitempty"`
	Secret string `json:"secret,omitempty"`
	// Key is the key the passphrase unlocks; nil when it is not known.
	Key *Key `json:"key,omitempty"`
	// Approval is what Approve asks about.
	Approval *Approval `json:"approval,omitempty"`
}

// Approval is the app an Approve question is about: its program, and how
// it was trusted before when it changed since.
type Approval struct {
	Program string `json:"program,omitempty"` // its path
	Script  string `json:"script,omitempty"`  // what the program, an interpreter, runs
	Flatpak string `json:"flatpak,omitempty"`
	Package string `json:"package,omitempty"` // "github-cli 2.62.0-1"
	// Changed is set when the app was trusted, but its program is not the
	// one trusted, on Since, as WasPackage.
	Changed    bool      `json:"changed,omitempty"`
	Since      time.Time `json:"since,omitzero"`
	WasPackage string    `json:"was_package,omitempty"`
	// Tampered says why it can only be allowed once.
	Tampered string `json:"tampered,omitempty"`
	// Reseal asks instead whether to trust the list of apps again, after
	// it was changed outside inua-keyring; Apps is how many it has.
	Reseal bool `json:"reseal,omitempty"`
	Apps   int  `json:"apps,omitempty"`
	// AllowPath asks instead whether programs at this path (a pattern)
	// may read every secret without asking.
	AllowPath string `json:"allow_path,omitempty"`
	// Agent asks instead whether an AI agent may use one of inua's tools
	// (inua as an MCP server): Action is what the call does ("Closing
	// Firefox"); Program is the agent's program. "always" then means for
	// the agent's session.
	Agent  bool   `json:"agent,omitempty"`
	Action string `json:"action,omitempty"`
	// Assistant is inua's own assistant as the agent: allowed once, call
	// by call, as it asked in its window.
	Assistant bool `json:"assistant,omitempty"`
}

// UserData is how inua-keyring says, in PINENTRY_USER_DATA (gpg-agent
// hands it to the pinentry), which app wants which secret when its pass
// asks for the key: UserData + app + "\t" + secret.
const UserData = "inua-keyring:"

// Answer is how the user answered.
type Answer struct {
	OK  bool   // OK (or the passphrase given); false for Cancel or Not OK
	Pin string // for GetPin
	// NotOK is the third button for Confirm, telling it from Cancel.
	NotOK bool
}

// Asker asks the user; an error means they could not be asked at all.
type Asker func(Request) (Answer, error)

// gpg-error codes with the pinentry source (5 << 24).
const (
	errCanceled     = 5<<24 | 99
	errNotConfirmed = 5<<24 | 114
	errUnknownCmd   = 5<<24 | 275
	errGeneral      = 5<<24 | 1
)

// Serve answers the agent on r and w until it says BYE or goes away.
func Serve(r io.Reader, w io.Writer, ask Asker) error {
	bw := bufio.NewWriter(w)
	say := func(format string, a ...any) error {
		fmt.Fprintf(bw, format+"\n", a...)
		return bw.Flush()
	}
	if err := say("OK Pleased to meet you, process %d", os.Getpid()); err != nil {
		return err
	}
	var st Request
	owner := 0 // the agent says it once, for the connection
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		cmd, arg, _ := strings.Cut(line, " ")
		arg = unescape(arg)
		cmd = strings.ToUpper(cmd)
		var err error
		switch cmd {
		case "BYE":
			return say("OK closing connection")
		case "OPTION":
			if v, ok := strings.CutPrefix(arg, "owner="); ok {
				pid, _, _ := strings.Cut(v, "/")
				owner, _ = strconv.Atoi(pid)
			}
			err = say("OK")
		case "SETKEYINFO": // "n/GRIP", "s/GRIP" for ssh, or "--clear"
			_, st.KeyGrip, _ = strings.Cut(arg, "/")
			err = say("OK")
		case "NOP", "SETQUALITYBAR", "SETQUALITYBAR_TT", "SETGENPIN", "SETGENPIN_TT", "CLEARPASSPHRASE":
			err = say("OK")
		case "RESET":
			st = Request{}
			err = say("OK")
		case "SETTITLE":
			st.Title, err = arg, say("OK")
		case "SETDESC":
			st.Desc, err = arg, say("OK")
		case "SETPROMPT":
			st.Prompt, err = arg, say("OK")
		case "SETERROR":
			st.Error, err = arg, say("OK")
		case "SETOK":
			st.OK, err = label(arg), say("OK")
		case "SETCANCEL":
			st.Cancel, err = label(arg), say("OK")
		case "SETNOTOK":
			st.NotOK, err = label(arg), say("OK")
		case "SETREPEAT":
			st.Repeat, err = arg, say("OK")
			if st.Repeat == "" {
				st.Repeat = "Repeat:"
			}
		case "SETREPEATERROR":
			st.NoMatch, err = arg, say("OK")
		case "SETTIMEOUT":
			fmt.Sscan(arg, &st.Timeout)
			err = say("OK")
		case "GETINFO":
			switch arg {
			case "flavor":
				err = data(say, "inua")
			case "version":
				err = data(say, "1.0")
			case "pid":
				err = data(say, fmt.Sprint(os.Getpid()))
			case "ttyinfo":
				err = data(say, "- - - - 0/0 -")
			default:
				err = say("ERR %d Unknown command <Pinentry>", errUnknownCmd)
			}
		case "GETPIN":
			q := st
			q.Owner = owner
			q.Mode = GetPin
			err = answer(say, ask, q)
			st.Error = "" // shown once
		case "CONFIRM":
			q := st
			q.Owner = owner
			q.Mode = Confirm
			if arg == "--one-button" {
				q.Mode = Message
			}
			err = answer(say, ask, q)
			st.Error = ""
		case "MESSAGE":
			q := st
			q.Owner = owner
			q.Mode = Message
			err = answer(say, ask, q)
		default:
			err = say("ERR %d Unknown command <Pinentry>", errUnknownCmd)
		}
		if err != nil {
			return err
		}
	}
	return sc.Err()
}

// answer asks the user and tells the agent.
func answer(say func(string, ...any) error, ask Asker, q Request) error {
	a, err := ask(q)
	switch {
	case err != nil:
		return say("ERR %d %s <Pinentry>", errGeneral, oneLine(err.Error()))
	case q.Mode == GetPin && a.OK:
		if a.Pin != "" {
			if err := say("D %s", escape(a.Pin)); err != nil {
				return err
			}
		}
		return say("OK")
	case a.OK:
		return say("OK")
	case q.Mode == Confirm && a.NotOK:
		return say("ERR %d Not confirmed <Pinentry>", errNotConfirmed)
	}
	return say("ERR %d Operation cancelled <Pinentry>", errCanceled)
}

func data(say func(string, ...any) error, s string) error {
	if err := say("D %s", escape(s)); err != nil {
		return err
	}
	return say("OK")
}

// label drops a button label's mnemonic marks: "_OK" is "OK", "__" is "_".
func label(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			if i+1 < len(s) && s[i+1] == '_' {
				b.WriteByte('_')
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// unescape undoes Assuan's %XX escapes.
func unescape(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// escape writes %, CR and LF as Assuan's %XX for a data line.
func escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '%', '\r', '\n':
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func oneLine(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ") }
