package calc

import (
	"math"
	"testing"
)

func TestEval(t *testing.T) {
	for _, tc := range []struct {
		expr    string
		degrees bool
		want    float64
	}{
		{"1 + 2 × 3", false, 7},
		{"(1 + 2) × 3", false, 9},
		{"2 ^ 3 ^ 2", false, 512}, // right-associative
		{"-2 ^ 2", false, -4},     // as written on paper
		{"(-2) ^ 2", false, 4},
		{"10 ÷ 4", false, 2.5},
		{"50%", false, 0.5},
		{"200 × 15%", false, 30},
		{"2π", false, 2 * math.Pi},
		{"3(4 + 1)", false, 15},
		{"sin 30", true, 0.5},
		{"sin(180)", true, 0},
		{"cos(π)", false, -1},
		{"asin 1", true, 90},
		{"√16 + log 1000", false, 7},
		{"ln e", false, 1},
		{"(1 + 2", false, 3}, // closed by the end
		{"1.5e3 + 1", false, 1501},
		{"−3 − −3", false, 0},
		{"1,000 + 1", false, 1001},
	} {
		got, err := Eval(tc.expr, tc.degrees)
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("Eval(%q) = %v, %v; want %v", tc.expr, got, err, tc.want)
		}
	}
}

func TestEvalErrors(t *testing.T) {
	for _, expr := range []string{"", "1 +", "1 / 0", "2 ) 3", "foo(2)", "sqrt(-1)", "tan 90", "*3"} {
		deg := expr == "tan 90"
		if v, err := Eval(expr, deg); err == nil {
			t.Errorf("Eval(%q) = %v, want an error", expr, v)
		}
	}
}

func TestFormat(t *testing.T) {
	for v, want := range map[float64]string{
		0: "0", 7: "7", 2.5: "2.5", 0.1 + 0.2: "0.3", -1.25: "-1.25",
		1.0 / 3: "0.333333333333", 123456789012: "123456789012",
		1e20: "1e20", 6.02214076e23: "6.02214076e23", 1.5e-12: "1.5e-12",
		math.Copysign(0, -1): "0",
	} {
		if got := Format(v); got != want {
			t.Errorf("Format(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestPasted(t *testing.T) {
	for in, want := range map[string]string{
		"12":           "12",
		" 1,234.5 * 2": "1234.5×2",
		"(3-1)/4 =":    "(3−1)÷4",
		"sqrt(2)+pi":   "√(2)+π",
		"1.5e-3":       "1.5e−3",
		"sin(30)\n":    "sin(30)",
		"1 +":          "1+",
		"si":           "si",
		"hello":        "hello",
		"2 & 3":        "2&3",
		"$5":           "$5",
	} {
		if got, ok := Pasted(in); !ok || got != want {
			t.Errorf("Pasted(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "  ", "\n\t", "="} {
		if got, ok := Pasted(in); ok {
			t.Errorf("Pasted(%q) = %q, want not ok", in, got)
		}
	}
}
