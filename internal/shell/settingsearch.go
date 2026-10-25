package shell

import (
	"slices"
	"strings"
	"unicode"
)

// SettingsItem is what the settings search can find: a page (Title "")
// or a row on it, by what the window shows of it.
type SettingsItem struct {
	Page      string // the page's name
	PageTitle string
	Section   string // the section's label, without its brackets
	Title     string // the row's title; "" for the page itself
	Sub       string // the row's line under it
}

// SettingsKeywords are other words for each page, found as its own
// title is: what people call the things on it.
var SettingsKeywords = map[string][]string{
	"appearance":    {"theme", "dark", "light", "wallpaper", "background", "font", "text size", "icons", "language", "colour", "color"},
	"bar":           {"dock", "panel", "clock", "tray", "taskbar", "control center"},
	"launcher":      {"search", "run", "terminal"},
	"apps":          {"default", "browser", "mail", "open with", "file types"},
	"ai":            {"assistant", "model", "api key", "openai", "anthropic", "claude", "mcp"},
	"notifications": {"banner", "do not disturb", "dnd", "focus", "sound"},
	"clipboard":     {"copy", "paste", "history"},
	"keyring":       {"password", "secret", "gpg", "pass", "secret service"},
	"shortcuts":     {"keys", "hotkey", "keybinding", "binding"},
	"wifi":          {"network", "wireless", "wlan", "internet", "ssid", "password", "router"},
	"bluetooth":     {"pair", "headphones", "headset", "speaker", "devices"},
	"displays":      {"monitor", "screen", "resolution", "scale", "refresh rate", "lid", "mirror"},
	"keyboard":      {"layout", "caps lock", "ctrl", "swap", "repeat"},
	"mouse":         {"touchpad", "trackpad", "pointer", "speed", "scroll", "acceleration"},
	"sound":         {"audio", "volume", "speaker", "speakers", "headphones", "output", "microphone", "mic", "input", "mute", "pipewire"},
	"about":         {"version", "update", "upgrade", "system", "fastfetch", "neofetch", "manifest", "card", "share", "qr", "cpu", "memory", "kernel", "changelog"},
}

// SearchSettings finds the items whose words hold every word of the query,
// best first: pages by their title, rows whose title begins with the
// query, rows whose title holds it, then the rest; otherwise in the
// window's order. It returns the items' indices.
func SearchSettings(items []SettingsItem, query string) []int {
	words := strings.Fields(fold(query))
	if len(words) == 0 {
		return nil
	}
	q := strings.Join(words, " ")
	type hit struct{ i, rank int }
	var hits []hit
	for i, it := range items {
		title := fold(it.Title)
		hay := fold(it.PageTitle + " " + it.Section + " " + it.Title + " " + it.Sub)
		if it.Title == "" {
			hay += " " + fold(strings.Join(SettingsKeywords[it.Page], " ")) + " " + it.Page
		}
		if !all(hay, words) {
			continue
		}
		rank := 4
		switch {
		case it.Title == "" && all(fold(it.PageTitle), words):
			rank = 0
		case it.Title == "":
			rank = 2 // by a keyword
		case strings.HasPrefix(title, q):
			rank = 1
		case all(title, words):
			rank = 3
		}
		hits = append(hits, hit{i, rank})
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return a.rank - b.rank })
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.i
	}
	return out
}

func all(hay string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// fold lowers a text and makes "Wi-Fi" and "wifi" the same.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}

// SectionName is a section's label as a result names it: "[MY DEVICES]"
// is "My devices".
func SectionName(label string) string {
	s := strings.Trim(strings.TrimSpace(label), "[]")
	if s == "" {
		return ""
	}
	r := []rune(strings.ToLower(s))
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
