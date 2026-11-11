// Package i18n translates inua's text. Translations are gettext catalogs
// (locales/LANG.po), keyed by the English text and embedded in the
// programs: T, Tf, N and Nf translate Go strings; UI translates the
// strings a layout marks translatable (Blueprint's _("…")) before GTK
// reads it. Programs choose the language once, at start (Init); until
// then, and for a language without a catalog, the text stays English.
// po/inua.pot lists what there is to translate: make i18n.
package i18n

import (
	"embed"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
)

//go:embed locales/*.po
var locales embed.FS

var (
	mu  sync.RWMutex
	cat *catalog // nil: English
)

// Init chooses the language: preferred, the settings' choice, when it is
// set and not "auto"; else from the environment, as gettext does:
// LANGUAGE (a list), else LC_ALL, LC_MESSAGES, LANG.
func Init(preferred string) {
	langs := Wanted()
	if preferred != "" && preferred != "auto" {
		langs = append([]string{preferred}, langs...)
	}
	Use(langs...)
}

// Wanted are the languages the environment asks for, most wanted first,
// each followed by its language alone: "zh_CN.UTF-8" gives zh_CN, zh.
func Wanted() []string {
	var raw []string
	if l := os.Getenv("LANGUAGE"); l != "" {
		raw = strings.Split(l, ":")
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			raw = append(raw, v)
			break
		}
	}
	var out []string
	add := func(l string) {
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	for _, l := range raw {
		l, _, _ = strings.Cut(l, ".") // the encoding
		l, _, _ = strings.Cut(l, "@") // a variant
		if l == "" || l == "C" || l == "POSIX" {
			continue
		}
		add(l)
		if lang, _, ok := strings.Cut(l, "_"); ok {
			add(lang)
		}
	}
	return out
}

// Use loads the first of langs there is a catalog for and returns it; ""
// leaves the text English.
func Use(langs ...string) string {
	mu.Lock()
	defer mu.Unlock()
	cat = nil
	for _, l := range langs {
		l = strings.ReplaceAll(strings.ToLower(strings.Split(strings.Split(l, ".")[0], "@")[0]), "-", "_")
		switch {
		case l == "zh" || l == "zh_cn" || l == "zh_sg" || strings.HasPrefix(l, "zh_hans"):
			l = "zh_CN"
		case l == "zh_tw" || l == "zh_hk" || l == "zh_mo" || strings.HasPrefix(l, "zh_hant"):
			l = "zh_TW"
		case l == "kr" || strings.HasPrefix(l, "kr_"):
			l = "ko"
		default:
			l, _, _ = strings.Cut(l, "_")
		}
		if strings.HasPrefix(l, "en") { // English is the source: nothing to load
			return ""
		}
		data, err := locales.ReadFile("locales/" + l + ".po")
		if err != nil {
			continue
		}
		c, err := parse(string(data))
		if err != nil {
			continue
		}
		c.lang, cat = l, c
		return l
	}
	return ""
}

// Lang is the language in use, "" for English.
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	if cat == nil {
		return ""
	}
	return cat.lang
}

// T translates msgid.
func T(msgid string) string {
	mu.RLock()
	defer mu.RUnlock()
	if cat != nil {
		if s := cat.msgs[msgid]; len(s) > 0 && s[0] != "" {
			return s[0]
		}
	}
	return msgid
}

// Key returns msgid unchanged for consumers that translate the key later.
// Keep it visible to make i18n so indirect menu labels stay in the catalogs.
func Key(msgid string) string { return msgid }

// Tf translates a format and fills it in.
func Tf(format string, args ...any) string { return fmt.Sprintf(T(format), args...) }

// N translates the form of one or many that n takes.
func N(one, many string, n int) string {
	mu.RLock()
	defer mu.RUnlock()
	if cat != nil {
		if s := cat.msgs[one]; len(s) > 0 {
			if k := cat.plural(n); k < len(s) && s[k] != "" {
				return s[k]
			}
		}
	}
	if n == 1 {
		return one
	}
	return many
}

// Nf translates the form n takes and fills it in.
func Nf(one, many string, n int, args ...any) string { return fmt.Sprintf(N(one, many, n), args...) }

// translatable is a string a layout marks for translation.
var translatable = regexp.MustCompile(`(<(?:property|item|attribute)\b[^>]*?\stranslatable="yes"[^>]*>)([^<]*)(</)`)

// UI translates the strings a GtkBuilder layout marks translatable.
func UI(xml string) string {
	if Lang() == "" {
		return xml
	}
	return translatable.ReplaceAllStringFunc(xml, func(m string) string {
		p := translatable.FindStringSubmatch(m)
		return p[1] + escapeXML(T(unescapeXML(p[2]))) + p[3]
	})
}

var (
	xmlUnescaper = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&")
	xmlEscaper   = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
)

func unescapeXML(s string) string { return xmlUnescaper.Replace(s) }
func escapeXML(s string) string   { return xmlEscaper.Replace(s) }

// M marks a string for translation where it is defined, in a table, and
// leaves it as it is: T translates it where it is shown.
func M(msgid string) string { return msgid }
