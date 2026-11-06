package pass

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestDraftRoundTrip(t *testing.T) {
	for _, text := range []string{
		"",
		"\n",
		"\n\n\n",
		"hunter2",
		"hunter2\n",
		"hunter2\n\n",
		"  spaced password  \n\tindented\n",
		"pw\n\nafter a blank\n\n\nend",
		"pw\r\nuser: me\r\n\r\nnotes\r\n",
		"pw\r\nmixed\nends\r\n",
		"pw\nlone\rcarriage\r",
		"pässwörd ✓\nnote: 日本語\n🔑\n",
		"pw\n---\notpauth://totp/x?secret=JBSWY3DPEHPK3PXP\nkey: value\n",
	} {
		d := NewDraft(text)
		if got := d.Text(); got != text {
			t.Errorf("NewDraft(%q).Text() = %q", text, got)
		}
		if len(d.Lines) == 0 {
			t.Errorf("NewDraft(%q) has no line for the password", text)
		}
	}
}

func TestDraftLines(t *testing.T) {
	d := NewDraft("pw\r\n\nuser: me")
	want := []Line{{"pw", "\r\n"}, {"", "\n"}, {"user: me", ""}}
	if !slices.Equal(d.Lines, want) {
		t.Fatalf("lines = %q, want %q", d.Lines, want)
	}
	if d.Password() != "pw" {
		t.Errorf("password = %q", d.Password())
	}
	if e := NewDraft(""); len(e.Lines) != 1 || e.Lines[0] != (Line{}) {
		t.Errorf("an empty text's lines = %q, want one empty line", e.Lines)
	}
}

func TestDraftEdits(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		edit       func(*Draft)
		want       string
	}{
		{"set password", "old\nuser: me\n", func(d *Draft) { d.Set(0, "new") }, "new\nuser: me\n"},
		{"set blank line", "pw\n\nnote\n", func(d *Draft) { d.Set(1, "  x ") }, "pw\n  x \nnote\n"},
		{"set keeps crlf", "pw\r\na\r\n", func(d *Draft) { d.Set(1, "b") }, "pw\r\nb\r\n"},
		{"set out of range", "pw\n", func(d *Draft) { d.Set(3, "x") }, "pw\n"},
		{"paste lines", "pw\nnote\n", func(d *Draft) { d.Set(1, "a\nb") }, "pw\na\nb\n"},
		{"paste lines at the end", "pw\nnote", func(d *Draft) { d.Set(1, "a\nb\n") }, "pw\na\nb\n"},
		{"insert in the middle", "pw\na\nb\n", func(d *Draft) { d.Insert(1) }, "pw\na\n\nb\n"},
		{"insert after the last", "pw\na\n", func(d *Draft) { d.Insert(1); d.Set(2, "b") }, "pw\na\nb\n"},
		{"insert after the last, no newline", "pw\na", func(d *Draft) { d.Insert(1); d.Set(2, "b") }, "pw\na\nb"},
		{"insert into an empty entry", "", func(d *Draft) { d.Set(0, "pw"); d.Insert(0); d.Set(1, "user: me") }, "pw\nuser: me"},
		{"insert with crlf", "pw\r\n", func(d *Draft) { d.Insert(0); d.Set(1, "x") }, "pw\r\nx\r\n"},
		{"remove a middle line", "pw\na\n\nb\n", func(d *Draft) { d.Remove(2) }, "pw\na\nb\n"},
		{"remove the last line", "pw\na\nb", func(d *Draft) { d.Remove(2) }, "pw\na"},
		{"remove the last line, newline kept", "pw\na\nb\n", func(d *Draft) { d.Remove(2) }, "pw\na\n"},
		{"the password stays", "pw\na\n", func(d *Draft) { d.Remove(0) }, "pw\na\n"},
		{"remove out of range", "pw\n", func(d *Draft) { d.Remove(5) }, "pw\n"},
	} {
		d := NewDraft(tc.text)
		tc.edit(&d)
		if got := d.Text(); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The rows and the text are two views of one draft: going from one to
// the other and back keeps every byte, edits made in either included.
func TestDraftModeSwitch(t *testing.T) {
	text := "pw  \r\n\n  user: me\n\n"
	d := NewDraft(text)
	d.Set(2, "  user: you")
	inText := d.Text() // shown in the text editor
	if inText != "pw  \r\n\n  user: you\n\n" {
		t.Fatalf("text = %q", inText)
	}
	edited := inText + "notes" // typed in the text editor
	back := NewDraft(edited)   // back to the rows
	if back.Text() != edited || len(back.Lines) != 5 || back.Lines[4].Text != "notes" {
		t.Fatalf("rows = %q", back.Lines)
	}
}

func TestNewPassword(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		p, err := NewPassword(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(p) != PasswordLength {
			t.Fatalf("%q is %d long, want %d", p, len(p), PasswordLength)
		}
		for _, r := range p {
			if !strings.ContainsRune(PasswordAlphabet, r) {
				t.Fatalf("%q has %q, not in the alphabet", p, r)
			}
		}
		for _, set := range []string{passUpper, passLower, passDigits, passSymbols} {
			if !strings.ContainsAny(p, set) {
				t.Fatalf("%q has none of %q", p, set)
			}
		}
		if seen[p] {
			t.Fatalf("%q made twice", p)
		}
		seen[p] = true
	}
	if strings.ContainsAny(PasswordAlphabet, "Il1O0 '\"`\\$") {
		t.Error("the alphabet has a look-alike, a space, a quote or a shell character")
	}
}

// Bytes at or above the cut-off are skipped, not folded onto the first
// characters, so every character is as likely.
func TestNewPasswordUnbiased(t *testing.T) {
	n := len(PasswordAlphabet)
	limit := 256 - 256%n
	var in []byte
	for b := limit; b < 256; b++ {
		in = append(in, byte(b)) // all skipped
	}
	upper, lower, digits := len(passUpper), len(passLower), len(passDigits)
	in = append(in, 0, byte(upper), byte(n+upper+lower), byte(upper+lower+digits))
	for range PasswordLength - 4 {
		in = append(in, byte(n)) // n % n: the first character
	}
	in = append(in, make([]byte, 128-len(in))...) // whole reads
	p, err := NewPassword(bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if want := "Aa2!" + strings.Repeat("A", PasswordLength-4); p != want {
		t.Fatalf("%q, want %q", p, want)
	}
	// Counting over many: each character near the mean.
	counts := map[byte]int{}
	for range 3000 {
		p, err := NewPassword(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		for i := range len(p) {
			counts[p[i]]++
		}
	}
	if len(counts) != n {
		t.Fatalf("%d characters seen, want %d", len(counts), n)
	}
	mean := 3000 * PasswordLength / n
	for c, k := range counts {
		if k < mean*7/10 || k > mean*13/10 {
			t.Errorf("%q seen %d times, the mean %d", c, k, mean)
		}
	}
}

type failingReader struct{ after int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.after <= 0 {
		return 0, errors.New("no entropy")
	}
	n := min(len(p), r.after)
	r.after -= n
	return n, nil
}

func TestNewPasswordError(t *testing.T) {
	for _, after := range []int{0, 10, 64, 100} {
		p, err := NewPassword(&failingReader{after: after})
		if err == nil || p != "" {
			t.Errorf("after %d bytes: %q, %v; want an error and no password", after, p, err)
		}
	}
	if _, err := NewPassword(bytes.NewReader(nil)); err == nil {
		t.Error("an empty source: no error")
	}
	// A failure leaves the draft as it was.
	d := NewDraft("old\nuser: me\n")
	if p, err := NewPassword(io.LimitReader(rand.Reader, 3)); err == nil {
		d.Set(0, p)
	}
	if d.Text() != "old\nuser: me\n" {
		t.Errorf("draft = %q after a failure", d.Text())
	}
}
