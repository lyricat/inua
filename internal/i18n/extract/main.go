// Command extract gathers inua's translatable text into po/inua.pot, from
// the Go sources' i18n.T, Tf, N, Nf and M calls and the layouts' strings
// marked translatable, then brings each catalog in internal/i18n/locales
// up to date with it: new text added untranslated, gone text dropped,
// translations kept. Run it from the repository's root: make i18n.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
)

const (
	potPath    = "po/inua.pot"
	localesDir = "internal/i18n/locales"
)

type message struct {
	id, plural string
	where      []string
}

func main() {
	log.SetFlags(0)
	var order []string
	msgs := map[string]*message{}
	add := func(id, plural, where string) {
		if id == "" {
			return
		}
		m := msgs[id]
		if m == nil {
			m = &message{id: id}
			msgs[id] = m
			order = append(order, id)
		}
		if plural != "" {
			m.plural = plural
		}
		m.where = append(m.where, where)
	}

	// The Go sources.
	fset := token.NewFileSet()
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var function string
				switch fn := call.Fun.(type) {
				case *ast.SelectorExpr:
					if pkg, ok := fn.X.(*ast.Ident); !ok || pkg.Name != "i18n" {
						return true
					}
					function = fn.Sel.Name
				case *ast.Ident:
					if f.Name.Name != "i18n" {
						return true
					}
					function = fn.Name
				default:
					return true
				}
				lit := func(i int) string {
					if i >= len(call.Args) {
						return ""
					}
					b, ok := call.Args[i].(*ast.BasicLit)
					if !ok || b.Kind != token.STRING {
						return ""
					}
					s, _ := strconv.Unquote(b.Value)
					return s
				}
				where := fmt.Sprintf("%s:%d", path, fset.Position(call.Pos()).Line)
				switch function {
				case "Time":
					add(lit(1), "", where)
				case "T", "Tf", "M", "Key":
					add(lit(0), "", where)
				case "N", "Nf":
					add(lit(0), lit(1), where)
				}
				return true
			})
			return nil
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	// The layouts.
	uis, _ := filepath.Glob("ui/*.ui")
	marked := regexp.MustCompile(`(?s)<(?:property|item|attribute)\b[^>]*?\stranslatable="yes"[^>]*>([^<]*)</`)
	unescape := strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&")
	for _, path := range uis {
		b, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		text := string(b)
		for _, m := range marked.FindAllStringSubmatchIndex(text, -1) {
			line := strings.Count(text[:m[0]], "\n") + 1
			add(unescape.Replace(text[m[2]:m[3]]), "", fmt.Sprintf("%s:%d", path, line))
		}
	}

	// The template.
	// Desktop launchers use the same catalog as the application windows.
	desktops, _ := filepath.Glob("config/apps/*.desktop")
	desktopKeys := map[string]bool{"Name": true, "GenericName": true, "Comment": true, "Keywords": true}
	for _, path := range desktops {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		for n, line := range strings.Split(string(data), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok && desktopKeys[key] {
				add(value, "", fmt.Sprintf("%s:%d", path, n+1))
			}
		}
	}

	var b strings.Builder
	b.WriteString("# inua's translatable text: made by make i18n, not edited by hand.\n")
	b.WriteString("msgid \"\"\nmsgstr \"\"\n\"Content-Type: text/plain; charset=UTF-8\\n\"\n")
	for _, id := range order {
		m := msgs[id]
		b.WriteString("\n")
		for _, w := range m.where {
			b.WriteString("#: " + w + "\n")
		}
		b.WriteString("msgid " + i18n.Quote(m.id) + "\n")
		if m.plural != "" {
			b.WriteString("msgid_plural " + i18n.Quote(m.plural) + "\nmsgstr[0] \"\"\nmsgstr[1] \"\"\n")
		} else {
			b.WriteString("msgstr \"\"\n")
		}
	}
	if err := os.MkdirAll(filepath.Dir(potPath), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(potPath, []byte(b.String()), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %d messages\n", potPath, len(order))

	// The catalogs.
	pos, _ := filepath.Glob(filepath.Join(localesDir, "*.po"))
	translations := map[string]map[string]string{}
	for _, path := range pos {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		entries, err := i18n.ParsePO(string(data))
		if err != nil {
			log.Fatalf("%s: %v", path, err)
		}
		have := map[string]i18n.Entry{}
		var head i18n.Entry
		for _, e := range entries {
			if e.ID == "" {
				head = e
				continue
			}
			have[e.Key()] = e
		}
		forms := 2 // plural forms, as the catalog's header says
		if len(head.Str) > 0 && strings.Contains(head.Str[0], "nplurals=1") {
			forms = 1
		}
		var out strings.Builder
		lang := strings.TrimSuffix(filepath.Base(path), ".po")
		translations[lang] = map[string]string{}
		writeEntry(&out, head, nil, forms)
		done, todo := 0, 0
		for _, id := range order {
			m := msgs[id]
			e, ok := have[id]
			if !ok {
				e = i18n.Entry{ID: m.id, Plural: m.plural}
			}
			e.Plural = m.plural
			if len(e.Str) > forms {
				e.Str = e.Str[:forms]
			}
			if len(e.Str) == 0 || slices.Contains(e.Str, "") {
				todo++
			} else {
				done++
			}
			out.WriteString("\n")
			writeEntry(&out, e, m.where, forms)
			if !i18n.Fuzzy(e) && len(e.Str) > 0 && e.Str[0] != "" {
				translations[lang][e.ID] = e.Str[0]
			}
		}
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %d translated, %d to do\n", path, done, todo)
	}
	for _, path := range desktops {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		var out strings.Builder
		for _, line := range strings.SplitAfter(string(data), "\n") {
			key, value, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "=")
			if base, suffix, localized := strings.Cut(key, "["); localized && desktopKeys[base] {
				if _, managed := translations[strings.TrimSuffix(suffix, "]")]; managed {
					continue
				}
			}
			out.WriteString(line)
			if !ok || !desktopKeys[key] {
				continue
			}
			for _, po := range pos {
				lang := strings.TrimSuffix(filepath.Base(po), ".po")
				if translated := translations[lang][value]; translated != "" {
					translated = strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r").Replace(translated)
					fmt.Fprintf(&out, "%s[%s]=%s\n", key, lang, translated)
				}
			}
		}
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

// writeEntry writes a catalog entry: its translator's comments and flags,
// where it is used, the message and its translation, in forms plural
// forms.
func writeEntry(b *strings.Builder, e i18n.Entry, where []string, forms int) {
	for _, c := range e.Comments {
		if !strings.HasPrefix(c, "#:") { // where it is used is written anew
			b.WriteString(c + "\n")
		}
	}
	for _, w := range where {
		b.WriteString("#: " + w + "\n")
	}
	if e.Context != "" {
		b.WriteString("msgctxt " + i18n.Quote(e.Context) + "\n")
	}
	if e.ID == "" { // the header, its lines kept apart
		b.WriteString("msgid \"\"\nmsgstr \"\"\n")
		if len(e.Str) > 0 {
			for _, l := range strings.SplitAfter(e.Str[0], "\n") {
				if l != "" {
					b.WriteString(i18n.Quote(l) + "\n")
				}
			}
		}
		return
	}
	b.WriteString("msgid " + i18n.Quote(e.ID) + "\n")
	if e.Plural != "" {
		b.WriteString("msgid_plural " + i18n.Quote(e.Plural) + "\n")
		strs := append([]string(nil), e.Str...)
		for len(strs) < forms {
			strs = append(strs, "")
		}
		for i, s := range strs {
			fmt.Fprintf(b, "msgstr[%d] %s\n", i, i18n.Quote(s))
		}
		return
	}
	s := ""
	if len(e.Str) > 0 {
		s = e.Str[0]
	}
	b.WriteString("msgstr " + i18n.Quote(s) + "\n")
}
