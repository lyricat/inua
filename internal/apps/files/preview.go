package files

import (
	"bytes"
	"errors"
	"io"
	"os"
	"syscall"
	"unicode/utf8"
)

// A text preview reads at most PreviewLines lines and PreviewBytes bytes
// of a file, stopping at whichever comes first.
const (
	PreviewLines = 200
	PreviewBytes = 64 << 10
)

var (
	// ErrNotText: the start of the file is not UTF-8 text (a NUL byte, or
	// a sequence that is not UTF-8).
	ErrNotText = errors.New("not UTF-8 text")
	// ErrNotRegular: a FIFO, a device, a socket or a folder, never opened
	// for a preview.
	ErrNotRegular = errors.New("not a regular file")
)

// Cut says what stopped a preview before the end of the file.
type Cut int

const (
	Whole     Cut = iota // the whole file was read
	LineLimit            // the line limit; more follows
	ByteLimit            // the byte limit; more may follow
)

// TextPreview is the start of a text file.
type TextPreview struct {
	Text string // without a byte order mark; ends at a line's end when cut at the line limit
	Cut  Cut
	Read int // the bytes taken from the file
}

// ReadTextPreview reads the start of a regular text file (a link is
// followed) within PreviewLines and PreviewBytes. Anything but a regular
// file gives ErrNotRegular without being read; it is checked before the
// file is opened and again on what was opened, without blocking, so a
// FIFO put in its place meanwhile is not waited on.
func ReadTextPreview(path string) (TextPreview, error) {
	return readTextPreview(path, PreviewLines, PreviewBytes)
}

func readTextPreview(path string, maxLines, maxBytes int) (TextPreview, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return TextPreview{}, err
	}
	if !fi.Mode().IsRegular() {
		return TextPreview{}, ErrNotRegular
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return TextPreview{}, err
	}
	defer f.Close()
	if fi, err = f.Stat(); err != nil {
		return TextPreview{}, err
	}
	if !fi.Mode().IsRegular() {
		return TextPreview{}, ErrNotRegular
	}
	return readPreview(f, fi.Size(), maxLines, maxBytes)
}

// readPreview reads r, whose size is size as far as is known, until
// maxLines line ends or maxBytes bytes, whichever is first, and takes
// not a byte from r past either: no read asks for more bytes than lines
// are still wanted, so none can reach past the last line's end. Whether
// more follows a cut is told by size, never by reading on; a file cut at
// a limit counts as cut unless its size is exactly what was read (a file
// in /proc says 0) or the read that reached the limit also met its end.
func readPreview(r io.Reader, size int64, maxLines, maxBytes int) (TextPreview, error) {
	buf := make([]byte, maxBytes)
	n, lines, eof := 0, 0, false
	for n < maxBytes && lines < maxLines && !eof {
		// m bytes hold at most m line ends: if they hold every line still
		// wanted, they are all line ends and the last one ends the read.
		m, err := r.Read(buf[n : n+min(maxLines-lines, maxBytes-n)])
		lines += bytes.Count(buf[n:n+m], []byte{'\n'})
		n += m
		if eof = err == io.EOF; err != nil && !eof {
			return TextPreview{Read: n}, err
		}
	}
	p := TextPreview{Read: n}
	text := buf[:n]
	if !eof && size != int64(n) {
		switch {
		case lines == maxLines:
			p.Cut = LineLimit
		case n == maxBytes:
			p.Cut = ByteLimit
			text = trimCutRune(text)
		}
	}
	text = bytes.TrimPrefix(text, []byte("\xef\xbb\xbf"))
	if bytes.IndexByte(text, 0) >= 0 || !utf8.Valid(text) {
		return TextPreview{Read: n}, ErrNotText
	}
	p.Text = string(text)
	return p, nil
}

// trimCutRune drops a character the byte limit cut in two; anything else
// that is not UTF-8 is left for the check.
func trimCutRune(b []byte) []byte {
	for i := 1; i < utf8.UTFMax && i <= len(b); i++ {
		if utf8.RuneStart(b[len(b)-i]) {
			if !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i]
			}
			break
		}
	}
	return b
}

// textExts are kinds of plain text shown in a preview whatever MIME type
// the system gives their names.
var textExts = map[string]bool{
	"txt": true, "text": true, "csv": true, "tsv": true, "log": true,
	"jsonl": true, "ndjson": true,
}

// TextExt reports whether a name's extension (as Entry.Ext gives it) is
// plain text to preview.
func TextExt(ext string) bool { return textExts[ext] }
