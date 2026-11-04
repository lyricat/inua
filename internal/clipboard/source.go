package clipboard

import (
	"slices"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/proc"
)

// Window is the focused window when a selection is captured, as the
// compositor names it.
type Window struct {
	Class        string `json:"class"`
	InitialClass string `json:"initialClass"`
	PID          int    `json:"pid"`
}

// Source is where a selection seems to come from. Wayland does not say
// which program copied, so it is found when the selection is captured:
//   - a wl-copy started in the last few seconds serves it: the job that
//     ran it (its process group: pass -c, passmenu, a script) and what
//     that runs under; pass keeps a process there while the password is
//     on the clipboard;
//   - the focused window, where Ctrl+C was pressed: its class and program.
type Source struct {
	Name  string   // the program to show: "pass", "firefox"; "" when not known
	Names []string // every program and window class it may be, lower case
	// Sensitive is set when one of Names is listed as sensitive: the
	// selection is not kept.
	Sensitive bool
}

// shells run the job; the program they run is the one to show.
var shells = map[string]bool{"sh": true, "bash": true, "dash": true, "zsh": true, "fish": true}

// copierAge is how recently a wl-copy must have started to be the one
// serving a selection just captured.
const copierAge = 5 * time.Second

// FindSource finds where the selection being captured comes from, and
// whether that is a program listed in sensitive (names or window classes,
// in any case).
func FindSource(w Window, sensitive []string) Source {
	return findSource(w, sensitive, proc.All())
}

// findSource is FindSource looking only among the processes in all.
func findSource(w Window, sensitive []string, all []int) Source {
	var s Source
	add := func(name string) {
		if name = strings.ToLower(name); name != "" && !slices.Contains(s.Names, name) {
			s.Names = append(s.Names, name)
		}
	}
	for _, pid := range all {
		if proc.Name(pid) != "wl-copy" || primary(proc.Args(pid)) || proc.Age(pid) > copierAge {
			continue
		}
		group := proc.Group(pid)
		for _, m := range all { // in PID order: the job's first program first
			if m == pid || group <= 1 || proc.Group(m) != group {
				continue
			}
			if name := proc.Name(m); s.Name == "" && !shells[name] {
				s.Name = name
			}
			for p, n := m, 0; p > 1 && n < 8; p, n = proc.Parent(p), n+1 {
				add(proc.Name(p))
			}
		}
	}
	add(w.Class)
	add(w.InitialClass)
	if w.PID > 0 {
		add(proc.Name(w.PID))
	}
	if s.Name == "" {
		s.Name = w.Class
	}
	for _, name := range sensitive {
		if slices.Contains(s.Names, strings.ToLower(strings.TrimSpace(name))) {
			s.Sensitive = true
		}
	}
	return s
}

// primary says whether wl-copy was asked for the primary selection, which
// the history does not record.
func primary(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool {
		return a == "--primary" || len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a, 'p')
	})
}

// PasswordHint is the type a password manager offers beside a secret it
// copies (KeePassXC, inua's Pass), asking clipboard histories to leave it
// out. wl-paste is meant to say so as CLIPBOARD_STATE=sensitive, but not
// every version does.
const PasswordHint = "x-kde-passwordManagerHint"

// MarkedSensitive reports whether a selection's offered types (wl-paste
// --list-types) mark it as a password manager's secret.
func MarkedSensitive(types []string) bool {
	return slices.ContainsFunc(types, func(t string) bool { return strings.TrimSpace(t) == PasswordHint })
}
