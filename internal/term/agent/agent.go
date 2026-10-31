// Package agent recognizes the agents running in inua-term's panes
// (docs/feat/inua-term.md, section 9): which one it is, from its process
// or what a wrapper says, and what it is doing, from what it reports, its
// title and its screen. Manifests say per agent how; they are TOML, built
// in and overridable in ~/.config/inua/term/agents/.
package agent

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
)

// State is what an agent is doing.
type State string

const (
	Working State = "working" // doing something
	Blocked State = "blocked" // waiting for you: a permission, a question
	Done    State = "done"    // finished its turn since you last looked
	Idle    State = "idle"    // at its prompt, nothing asked
	Exited  State = "exited"  // its process ended
)

// Manifest is how to know an agent and its state.
type Manifest struct {
	Type    string   `toml:"type"`
	Name    string   `toml:"name"`
	Icon    string   `toml:"icon"`    // ui/icons/ICON-symbolic.svg; "" for the terminal's
	Color   string   `toml:"color"`   // its mark's colour, "#rrggbb"; "" for the theme's
	Process []string `toml:"process"` // its program's names
	Resume  string   `toml:"resume"`  // the command that continues where it was
	States  []Rule   `toml:"state"`
}

// Rule is a state shown on the last Lines rows of the screen: when any of
// Any appears there, or a row matches any of Match (regular expressions,
// for a row with no fixed text, such as a spinner's).
type Rule struct {
	State State    `toml:"state"`
	Lines int      `toml:"lines"`
	Any   []string `toml:"any"`
	Match []string `toml:"match"`
	match []*regexp.Regexp
}

//go:embed manifests/*.toml
var builtin embed.FS

// Set is the manifests known.
type Set struct{ list []Manifest }

// Dir is where the user's own manifests are.
func Dir() string {
	conf := os.Getenv("XDG_CONFIG_HOME")
	if conf == "" {
		home, _ := os.UserHomeDir()
		conf = filepath.Join(home, ".config")
	}
	return filepath.Join(conf, "inua", "term", "agents")
}

// Load is the built-in manifests and then the user's in dir (one of the
// same type takes the built-in one's place); a file that does not parse
// is reported and left out.
func Load(dir string) (*Set, []error) {
	s := &Set{}
	var errs []error
	files, _ := fs.Glob(builtin, "manifests/*.toml")
	for _, f := range files {
		b, _ := builtin.ReadFile(f)
		if err := s.add(f, b); err != nil {
			errs = append(errs, err)
		}
	}
	if dir != "" {
		mine, _ := filepath.Glob(filepath.Join(dir, "*.toml"))
		slices.Sort(mine)
		for _, f := range mine {
			b, err := os.ReadFile(f)
			if err == nil {
				err = s.add(f, b)
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	return s, errs
}

// add takes a file of one manifest, or of several as [[agent]].
func (s *Set) add(name string, b []byte) error {
	var file struct {
		Manifest
		Agents []Manifest `toml:"agent"`
	}
	if _, err := toml.Decode(string(b), &file); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	ms := file.Agents
	if file.Type != "" {
		ms = append(ms, file.Manifest)
	}
	for _, m := range ms {
		if m.Type == "" {
			return fmt.Errorf("%s: an agent without a type", name)
		}
		if m.Name == "" {
			m.Name = m.Type
		}
		if m.Color != "" && !colorRe.MatchString(m.Color) {
			return fmt.Errorf("%s: %s: color %q: want #rrggbb", name, m.Type, m.Color)
		}
		for i := range m.States {
			for _, expr := range m.States[i].Match {
				re, err := regexp.Compile(expr)
				if err != nil {
					return fmt.Errorf("%s: %s: match %q: %w", name, m.Type, expr, err)
				}
				m.States[i].match = append(m.States[i].match, re)
			}
		}
		if i := slices.IndexFunc(s.list, func(o Manifest) bool { return o.Type == m.Type }); i >= 0 {
			s.list[i] = m
		} else {
			s.list = append(s.list, m)
		}
	}
	return nil
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ByType is the manifest of a type, nil for none.
func (s *Set) ByType(t string) *Manifest {
	for i := range s.list {
		if s.list[i].Type == t {
			return &s.list[i]
		}
	}
	return nil
}

// interpreters run an agent that is a script: the agent is the script.
var interpreters = []string{"node", "nodejs", "bun", "deno", "python", "python3", "uv", "npx", "sh", "bash", "dash", "zsh"}

// Detect is the agent a process is: the type a wrapper set
// (INUA_TERM_AGENT, given as override), else its program or the script
// an interpreter runs; nil for none.
func (s *Set) Detect(argv []string, override string) *Manifest {
	if override != "" {
		if m := s.ByType(override); m != nil {
			return m
		}
		return &Manifest{Type: override, Name: override}
	}
	if len(argv) == 0 {
		return nil
	}
	prog := filepath.Base(argv[0])
	if m := s.byProcess(prog); m != nil {
		return m
	}
	// A script: `node /usr/bin/claude` is claude.
	if slices.Contains(interpreters, strings.TrimRight(prog, "0123456789.")) {
		for _, a := range argv[1:] {
			if !strings.HasPrefix(a, "-") {
				return s.byProcess(filepath.Base(a))
			}
		}
	}
	return nil
}

func (s *Set) byProcess(name string) *Manifest {
	for i := range s.list {
		if slices.Contains(s.list[i].Process, name) {
			return &s.list[i]
		}
	}
	return nil
}

// State is the state the screen shows, last rows last; "" when it shows
// none (the agent at its prompt, as far as the screen tells). A rule's
// last rows count from the last one with text: an agent drawing inline
// may be anywhere on a screen not yet full.
func (m *Manifest) State(screen []string) (State, string) {
	for len(screen) > 0 && strings.TrimSpace(screen[len(screen)-1]) == "" {
		screen = screen[:len(screen)-1]
	}
	for _, r := range m.States {
		n := r.Lines
		if n <= 0 || n > len(screen) {
			n = len(screen)
		}
		rows := screen[len(screen)-n:]
		for i, line := range rows {
			for _, a := range r.Any {
				if strings.Contains(line, a) {
					return r.State, question(rows[:i+1])
				}
			}
			for _, re := range r.match {
				if re.MatchString(line) {
					return r.State, question(rows[:i+1])
				}
			}
		}
	}
	return "", ""
}

// question is what a screen asks, its last row matching a rule: the
// nearest row up to it ending in "?" (the question over a picker's
// choices and keys), else that row.
func question(rows []string) string {
	for i := len(rows) - 1; i >= 0; i-- {
		if q := ask(rows[i]); strings.HasSuffix(q, "?") {
			return q
		}
	}
	return ask(rows[len(rows)-1])
}

// ask is a screen line as the question it asks: without a box's borders
// and blanks around.
func ask(line string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "│┃|╭╮╰╯─"))
}

// Title is an agent's title without the spinner or mark it puts first
// (⠋ ✻ ✳ ·), and whether one was there.
func Title(t string) (string, bool) {
	r := []rune(strings.TrimSpace(t))
	i := 0
	for i < len(r) && (isSpinner(r[i]) || unicode.IsSpace(r[i])) {
		i++
	}
	return strings.TrimSpace(string(r[i:])), i > 0
}

func isSpinner(r rune) bool {
	return r >= 0x2800 && r <= 0x28ff || // braille: ⠋⠙⠹
		strings.ContainsRune("✻✳✶✢✽✦✧·•●○◐◓◑◒⏺∙*", r)
}

// Tracker is a pane's agent and its state over time.
type Tracker struct {
	Type  string
	State State
	Since time.Time
	Ask   string // what a blocked agent asks
	// Reported is set while the agent reports its state: its screen only
	// adds the questions it asks.
	Reported bool
}

// Set records state at now; it reports whether anything changed. An
// agent that stops working while nobody looks is done until it is seen.
func (t *Tracker) Set(s State, ask string, now time.Time) bool {
	if s == Idle && (t.State == Working || t.State == Done) {
		s = Done
	}
	if s == t.State && ask == t.Ask {
		return false
	}
	if s != t.State {
		t.Since = now
	}
	t.State, t.Ask = s, ask
	return true
}

// Seen is the user looking at the pane: a done agent is idle again.
func (t *Tracker) Seen(now time.Time) bool {
	if t.State != Done {
		return false
	}
	t.State, t.Since = Idle, now
	return true
}
