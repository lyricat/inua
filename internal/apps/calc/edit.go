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
		if strings.ContainsRune("+−-×*÷/^%", insert[0]) {
			runes = []rune(answer)
		}
		start, end = len(runes), len(runes)
	}
	return string(runes[:start]) + input + string(runes[end:]), start + len(insert)
}
