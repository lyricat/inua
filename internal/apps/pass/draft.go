package pass

import (
	"errors"
	"io"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
)

// Draft is an entry's text as the editor's rows hold it: each line as it
// is written, its end apart, so the text comes back byte for byte (blank
// lines, spaces at the ends, "\r\n", the last newline or its lack). The
// first line is the password. Parse reads the fields out of a text;
// a draft keeps the lines and interprets none.
type Draft struct {
	Lines []Line
}

// Line is a line of a draft: its text, and what ends it ("\n", "\r\n",
// or "" for a last line without a newline).
type Line struct {
	Text, End string
}

// NewDraft cuts a text into its lines. An empty text is one empty line,
// the password still to write.
func NewDraft(text string) Draft {
	var d Draft
	for {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			break
		}
		l := Line{Text: text[:i], End: "\n"}
		if strings.HasSuffix(l.Text, "\r") {
			l.Text, l.End = l.Text[:len(l.Text)-1], "\r\n"
		}
		d.Lines = append(d.Lines, l)
		text = text[i+1:]
	}
	if text != "" || len(d.Lines) == 0 {
		d.Lines = append(d.Lines, Line{Text: text})
	}
	return d
}

// Text is the draft written out: the text it was made of, with the
// edits since.
func (d Draft) Text() string {
	var b strings.Builder
	for _, l := range d.Lines {
		b.WriteString(l.Text)
		b.WriteString(l.End)
	}
	return b.String()
}

// Password is the first line.
func (d Draft) Password() string {
	if len(d.Lines) == 0 {
		return ""
	}
	return d.Lines[0].Text
}

// eol is the newline the draft writes for a line it adds: its first
// line's, "\n" when that has none.
func (d Draft) eol() string {
	if len(d.Lines) > 0 && d.Lines[0].End != "" {
		return d.Lines[0].End
	}
	return "\n"
}

// Set puts text in line i. A text of several lines (a paste) becomes as
// many lines, the last one ending as line i did.
func (d *Draft) Set(i int, text string) {
	if i < 0 || i >= len(d.Lines) {
		return
	}
	if !strings.ContainsRune(text, '\n') {
		d.Lines[i].Text = text
		return
	}
	end := d.Lines[i].End
	parts := NewDraft(text + end).Lines
	if end == "" && strings.HasSuffix(text, "\n") {
		// The paste ended with a newline: an empty line after it.
		parts = append(parts, Line{})
	}
	d.Lines = append(d.Lines[:i], append(parts, d.Lines[i+1:]...)...)
}

// Insert adds an empty line after line i. A line added after the last
// one takes its end, so a text without a last newline still has none.
func (d *Draft) Insert(i int) {
	if i < 0 || i >= len(d.Lines) {
		return
	}
	eol := d.eol()
	l := Line{End: eol}
	if i == len(d.Lines)-1 {
		l.End = d.Lines[i].End
		d.Lines[i].End = eol
	}
	d.Lines = append(d.Lines[:i+1], append([]Line{l}, d.Lines[i+1:]...)...)
}

// Remove takes line i out; the password's line stays (Set empties it).
// The last line's end passes to the line before it.
func (d *Draft) Remove(i int) {
	if i < 1 || i >= len(d.Lines) {
		return
	}
	if i == len(d.Lines)-1 {
		d.Lines[i-1].End = d.Lines[i].End
	}
	d.Lines = append(d.Lines[:i], d.Lines[i+1:]...)
}

// PasswordLength is how long a password NewPassword makes is.
const PasswordLength = 24

// Password characters: letters and digits without the ones read alike
// (I l 1, O 0), and symbols that sites take and shells and quotes leave
// alone.
const (
	passUpper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	passLower   = "abcdefghijkmnopqrstuvwxyz"
	passDigits  = "23456789"
	passSymbols = "!#%+-.=@_"
	// PasswordAlphabet is every character NewPassword picks from.
	PasswordAlphabet = passUpper + passLower + passDigits + passSymbols
)

// NewPassword makes a password of PasswordLength characters of
// PasswordAlphabet, each picked evenly with the bytes of random
// (crypto/rand.Reader), with at least one of each kind: upper and lower
// case, a digit, a symbol. It errs only when random does.
func NewPassword(random io.Reader) (string, error) {
	n := len(PasswordAlphabet)
	// Bytes at or over limit would favour the first characters.
	limit := 256 - 256%n
	buf := make([]byte, 64)
	out := make([]byte, 0, PasswordLength)
	for {
		out = out[:0]
		for len(out) < PasswordLength {
			if _, err := io.ReadFull(random, buf); err != nil {
				clear(out)
				return "", errors.New(i18n.Tf("No password could be made: %v", err))
			}
			for _, b := range buf {
				if int(b) < limit && len(out) < PasswordLength {
					out = append(out, PasswordAlphabet[int(b)%n])
				}
			}
		}
		clear(buf)
		p := string(out)
		clear(out)
		if strings.ContainsAny(p, passUpper) && strings.ContainsAny(p, passLower) &&
			strings.ContainsAny(p, passDigits) && strings.ContainsAny(p, passSymbols) {
			return p, nil
		}
	}
}
