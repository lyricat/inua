package files

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
	"time"
	"unicode/utf8"
)

// countReader counts what is taken from r.
type countReader struct {
	r io.Reader
	n int
}

func (c *countReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n += n
	return n, err
}

// endless is a byte repeated for ever.
type endless byte

func (e endless) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(e)
	}
	return len(b), nil
}

func lines(n int) string { return strings.Repeat("line of a log\n", n) }

func TestPreviewLineLimit(t *testing.T) {
	// Reading stops at the 200th line's end: not a byte is taken past it,
	// however the reader splits its reads.
	long := strings.Repeat("x", 5000)
	for _, c := range []struct {
		name string
		src  string
		keep string
	}{
		{"short lines", lines(1000), lines(200)},
		{"a long last line", lines(199) + long + "\n" + lines(10), lines(199) + long + "\n"},
		{"empty lines", strings.Repeat("\n", 300), strings.Repeat("\n", 200)},
	} {
		for _, wrap := range []struct {
			name string
			f    func(io.Reader) io.Reader
		}{
			{"whole", func(r io.Reader) io.Reader { return r }},
			{"halves", iotest.HalfReader},
			{"bytes", iotest.OneByteReader},
		} {
			r := &countReader{r: strings.NewReader(c.src)}
			p, err := readPreview(wrap.f(r), int64(len(c.src)), 200, 64<<10)
			if err != nil {
				t.Fatalf("%s, %s: %v", c.name, wrap.name, err)
			}
			if p.Cut != LineLimit || p.Text != c.keep || r.n != len(c.keep) || p.Read != r.n {
				t.Fatalf("%s, %s: cut %v, %d lines kept, read %d (reported %d), want %d",
					c.name, wrap.name, p.Cut, strings.Count(p.Text, "\n"), r.n, p.Read, len(c.keep))
			}
		}
	}
	// Line ends for ever: exactly 200 bytes are read.
	r := &countReader{r: endless('\n')}
	if p, err := readPreview(r, 0, 200, 64<<10); err != nil || p.Cut != LineLimit || r.n != 200 || len(p.Text) != 200 {
		t.Fatalf("line ends: cut %v, read %d, kept %d, %v", p.Cut, r.n, len(p.Text), err)
	}
}

func TestPreviewByteLimit(t *testing.T) {
	r := &countReader{r: strings.NewReader(lines(100000))}
	p, err := readPreview(r, 14*100000, 1<<30, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if p.Cut != ByteLimit || r.n != 1000 || p.Read != 1000 || len(p.Text) != 1000 {
		t.Fatalf("cut %v, read %d, kept %d", p.Cut, r.n, len(p.Text))
	}
}

func TestPreviewLongLine(t *testing.T) {
	// One line with no end: only the byte limit stops it.
	r := &countReader{r: endless('x')}
	p, err := readPreview(r, 0, 200, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if p.Cut != ByteLimit || r.n != 64<<10 || len(p.Text) != 64<<10 {
		t.Fatalf("cut %v, read %d, kept %d", p.Cut, r.n, len(p.Text))
	}
}

func TestPreviewExactLimits(t *testing.T) {
	for _, c := range []struct {
		name     string
		src      string
		maxLines int
		maxBytes int
		cut      Cut
		text     string
	}{
		{"lines exactly", lines(5), 5, 1000, Whole, lines(5)},
		{"one more line", lines(6), 5, 1000, LineLimit, lines(5)},
		{"last line without end", lines(4) + "end", 5, 1000, Whole, lines(4) + "end"},
		{"line past the end", lines(5) + "x", 5, 1000, LineLimit, lines(5)},
		{"bytes exactly", "0123456789", 100, 10, Whole, "0123456789"},
		{"one byte more", "0123456789a", 100, 10, ByteLimit, "0123456789"},
		{"both at once", "abc\n", 1, 4, Whole, "abc\n"},
		{"empty", "", 5, 10, Whole, ""},
		{"byte order mark", "\xef\xbb\xbfa,b\n1,2\n", 5, 100, Whole, "a,b\n1,2\n"},
	} {
		p, err := readPreview(strings.NewReader(c.src), int64(len(c.src)), c.maxLines, c.maxBytes)
		if err != nil || p.Cut != c.cut || p.Text != c.text {
			t.Errorf("%s: %q %v %v, want %q %v", c.name, p.Text, p.Cut, err, c.text, c.cut)
		}
	}
}

func TestPreviewSizeUnknown(t *testing.T) {
	// A file that says 0 (as /proc's do) and fills the limit counts as cut.
	p, err := readPreview(strings.NewReader("0123456789"), 0, 100, 10)
	if err != nil || p.Cut != ByteLimit {
		t.Fatalf("%v %v", p.Cut, err)
	}
	p, err = readPreview(strings.NewReader("short"), 0, 100, 10)
	if err != nil || p.Cut != Whole || p.Text != "short" {
		t.Fatalf("%q %v %v", p.Text, p.Cut, err)
	}
	// So does one that reaches the line limit, as nothing past it is read.
	p, err = readPreview(strings.NewReader(lines(5)), 0, 5, 1000)
	if err != nil || p.Cut != LineLimit || p.Text != lines(5) {
		t.Fatalf("%q %v %v", p.Text, p.Cut, err)
	}
	// Unless the read that reached it also met the end.
	p, err = readPreview(iotest.DataErrReader(strings.NewReader(lines(5))), 0, 5, 1000)
	if err != nil || p.Cut != Whole || p.Text != lines(5) {
		t.Fatalf("%q %v %v", p.Text, p.Cut, err)
	}
}

func TestPreviewRuneAtLimit(t *testing.T) {
	src := "abcdefgh€€" // € is three bytes: the limit of 10 cuts the first
	for limit := 8; limit <= len(src); limit++ {
		p, err := readPreview(strings.NewReader(src), int64(len(src)), 100, limit)
		if err != nil {
			t.Fatalf("limit %d: %v", limit, err)
		}
		if !utf8.ValidString(p.Text) || !strings.HasPrefix(src, p.Text) || len(p.Text) < limit-2 {
			t.Fatalf("limit %d: %q", limit, p.Text)
		}
	}
	// Cut at the end of the file is not a cut character but bad text.
	if _, err := readPreview(strings.NewReader("abc\xe2\x82"), 5, 100, 10); !errors.Is(err, ErrNotText) {
		t.Fatalf("a character cut by the file's end: %v", err)
	}
}

func TestPreviewNotText(t *testing.T) {
	for _, src := range []string{
		"abc\x00def",              // NUL
		"caf\xe9 au lait\n",       // Latin-1 inside
		"\xff\xfeh\x00i\x00",      // UTF-16
		"ok\n\xe2\x82\n" + "more", // a cut character inside
	} {
		if _, err := readPreview(strings.NewReader(src), int64(len(src)), 100, 1000); !errors.Is(err, ErrNotText) {
			t.Errorf("%q: %v", src, err)
		}
	}
	// A bad byte past the limit is never seen.
	src := "abc\n\xff"
	if p, err := readPreview(strings.NewReader(src), int64(len(src)), 1, 1000); err != nil || p.Text != "abc\n" {
		t.Errorf("past the line limit: %q %v", p.Text, err)
	}
}

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, syscall.EIO }

func TestPreviewReadError(t *testing.T) {
	if _, err := readPreview(io.MultiReader(strings.NewReader("abc"), failing{}), 100, 10, 100); !errors.Is(err, syscall.EIO) {
		t.Fatal(err)
	}
}

func TestReadTextPreviewFiles(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "data.csv")
	write(t, text, "a,b\n1,2\n")
	p, err := ReadTextPreview(text)
	if err != nil || p.Text != "a,b\n1,2\n" || p.Cut != Whole {
		t.Fatalf("%q %v %v", p.Text, p.Cut, err)
	}

	link := filepath.Join(dir, "link.log")
	if err := os.Symlink(text, link); err != nil {
		t.Fatal(err)
	}
	if p, err := ReadTextPreview(link); err != nil || p.Text != "a,b\n1,2\n" {
		t.Fatalf("through a link: %q %v", p.Text, err)
	}

	empty := filepath.Join(dir, "empty.log")
	write(t, empty, "")
	if p, err := ReadTextPreview(empty); err != nil || p.Text != "" || p.Cut != Whole {
		t.Fatalf("empty: %q %v %v", p.Text, p.Cut, err)
	}

	big := filepath.Join(dir, "big.log")
	write(t, big, strings.Repeat("x", 3*PreviewBytes))
	if p, err := ReadTextPreview(big); err != nil || p.Cut != ByteLimit || p.Read != PreviewBytes {
		t.Fatalf("big: %v read %d %v", p.Cut, p.Read, err)
	}

	bin := filepath.Join(dir, "blob.txt")
	write(t, bin, "\x7fELF\x02\x01\x01\x00\x00")
	if _, err := ReadTextPreview(bin); !errors.Is(err, ErrNotText) {
		t.Fatalf("binary: %v", err)
	}

	if err := os.Remove(text); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTextPreview(text); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("deleted: %v", err)
	}
	if _, err := ReadTextPreview(link); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("link to a deleted file: %v", err)
	}

	if os.Geteuid() != 0 {
		locked := filepath.Join(dir, "locked.txt")
		write(t, locked, "secret")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadTextPreview(locked); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("unreadable: %v", err)
		}
	}
}

func TestReadTextPreviewSpecial(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe.log")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("no FIFO:", err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}
	paths := []string{fifo, link, dir}
	if _, err := os.Stat("/dev/null"); err == nil {
		paths = append(paths, "/dev/null")
	}
	for _, path := range paths {
		done := make(chan error, 1)
		go func() {
			_, err := ReadTextPreview(path)
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, ErrNotRegular) {
				t.Errorf("%s: %v", filepath.Base(path), err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: still waiting", filepath.Base(path))
		}
	}
}

func TestTextExt(t *testing.T) {
	for ext, want := range map[string]bool{"csv": true, "log": true, "tsv": true, "txt": true, "png": false, "": false} {
		if TextExt(ext) != want {
			t.Errorf("%q: %v", ext, !want)
		}
	}
}
