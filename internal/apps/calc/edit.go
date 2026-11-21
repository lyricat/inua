package calc

import "strings"

// Edit replaces a selection, using character offsets just like GtkEditable.
// Only insertion at the formula's end uses answer continuation: an operator
// continues the answer; other text starts a new expression. A selection or
// a caret inside the formula edits that formula instead.
func Edit(text string, start, end int, input, answer string, answered bool) (string, int) {
	runes := []rune(text)
	start, end = max(0, min(start, len(runes))), max(0, min(end, len(runes)))
	if start > end {
		start, end = end, start
	}
	insert := []rune(input)
	if answered && len(insert) > 0 && start == end && end == len(runes) {
		runes = nil
		if strings.ContainsRune("+−-×*÷/^%!", insert[0]) {
			runes = []rune(answer)
		}
		start, end = len(runes), len(runes)
	}
	return string(runes[:start]) + input + string(runes[end:]), start + len(insert)
}

// Negate changes the sign of the whole expression: a number gains or loses
// its minus, anything else goes in −( ) or comes out of it.
func Negate(text string) string {
	if text == "" {
		return ""
	}
	for _, minus := range []string{"−", "-"} {
		if rest, ok := strings.CutPrefix(text, minus); ok {
			if number(rest) {
				return rest
			}
			if wrapped(rest) {
				return rest[1 : len(rest)-1]
			}
		}
	}
	if number(text) {
		return "−" + text
	}
	return "−(" + text + ")"
}

func number(s string) bool {
	return s != "" && strings.Trim(s, "0123456789.") == ""
}

// wrapped is whether s is one pair of parentheses around everything.
func wrapped(s string) bool {
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return false
	}
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 && i < len(s)-1 {
				return false
			}
		}
	}
	return depth == 0
}
