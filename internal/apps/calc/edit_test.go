package calc

import "testing"

func TestEdit(t *testing.T) {
	for _, tc := range []struct {
		name, text    string
		start, end    int
		input, answer string
		answered      bool
		want          string
		caret         int
	}{
		{"empty", "", 0, 0, "π", "", false, "π", 1},
		{"unicode caret", "2×π", 2, 2, "√(4)+", "", false, "2×√(4)+π", 7},
		{"unicode selection", "2×π+3", 1, 3, "÷2", "", false, "2÷2+3", 3},
		{"backward selection", "2×π+3", 3, 1, "÷2", "", false, "2÷2+3", 3},
		{"delete unicode", "2×π", 2, 3, "", "", false, "2×", 2},
		{"delete all", "2×π", 0, 3, "", "", false, "", 0},
		{"continue answer", "2+3", 3, 3, "×", "5", true, "5×", 2},
		{"ASCII operator", "2+3", 3, 3, "*", "5", true, "5*", 2},
		{"new number", "2+3", 3, 3, "7", "5", true, "7", 1},
		{"new function", "2+3", 3, 3, "sin(", "5", true, "sin(", 4},
		{"edit answered formula", "2+3", 0, 1, "9", "5", true, "9+3", 1},
		{"insert inside answered formula", "2+3", 1, 1, "0", "5", true, "20+3", 2},
		{"backspace after answer", "2+3", 2, 3, "", "5", true, "2+", 2},
		{"edit value", "5", 1, 1, ".", "5", false, "5.", 2},
		{"paste at answered formula caret", "2+3", 3, 3, "+4", "5", false, "2+3+4", 5},
		{"invalid stays editable", "1+", 2, 2, ")", "", false, "1+)", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, caret := Edit(tc.text, tc.start, tc.end, tc.input, tc.answer, tc.answered)
			if got != tc.want || caret != tc.caret {
				t.Fatalf("Edit = (%q, %d), want (%q, %d)", got, caret, tc.want, tc.caret)
			}
		})
	}
}

func TestEditInvalidAndEmpty(t *testing.T) {
	text, caret := Edit("1+2", 2, 3, "", "", false)
	if _, err := Eval(text, true); err == nil {
		t.Fatal("incomplete edit must not evaluate")
	}
	text, _ = Edit(text, caret, caret, "3", "", false)
	if v, err := Eval(text, true); err != nil || v != 4 {
		t.Fatalf("corrected edit = %v, %v; want 4", v, err)
	}
	text, _ = Edit(text, 0, 3, "", "", false)
	if _, err := Eval(text, true); err == nil {
		t.Fatal("empty edit must not evaluate")
	}
}
