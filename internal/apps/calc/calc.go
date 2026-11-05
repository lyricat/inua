// Package calc evaluates the calculator's expressions: numbers, + − × ÷,
// ^, %, parentheses, the constants π and e, and the functions sin, cos,
// tan, asin, acos, atan, ln, log, sqrt and abs. Trigonometry works in
// degrees or radians.
package calc

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// ErrSyntax is an expression that is not complete or not well formed.
var ErrSyntax = errors.New("not a complete expression")

// Eval evaluates expr. Missing closing parentheses are closed.
func Eval(expr string, degrees bool) (float64, error) {
	toks, err := lex(expr)
	if err != nil {
		return 0, err
	}
	p := &parser{toks: toks, degrees: degrees}
	v, err := p.expr()
	if err != nil {
		return 0, err
	}
	if p.pos != len(p.toks) {
		return 0, ErrSyntax
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("not a number")
	}
	return v, nil
}

// Format shows a result: up to 12 significant digits, no trailing zeros,
// and exponent form only for very large or small numbers.
func Format(v float64) string {
	if v == 0 {
		return "0" // and not "-0"
	}
	if a := math.Abs(v); a >= 1e15 || a < 1e-9 {
		s := strconv.FormatFloat(v, 'e', 9, 64)
		mant, exp, _ := strings.Cut(s, "e")
		if strings.Contains(mant, ".") {
			mant = strings.TrimRight(strings.TrimRight(mant, "0"), ".")
		}
		e, _ := strconv.Atoi(exp)
		return fmt.Sprintf("%se%d", mant, e)
	}
	s := strconv.FormatFloat(v, 'g', 12, 64)
	if strings.ContainsAny(s, "e") { // 'g' chose an exponent; spell it out
		s = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return s
}

type kind int

const (
	num kind = iota
	op
	lparen
	rparen
	fn
)

type token struct {
	kind kind
	val  float64
	text string
}

var funcs = map[string]bool{"sin": true, "cos": true, "tan": true, "asin": true, "acos": true, "atan": true, "ln": true, "log": true, "sqrt": true, "abs": true}

func lex(s string) ([]token, error) {
	r := strings.NewReplacer("×", "*", "÷", "/", "−", "-", "–", "-", "√", "sqrt", "π", "pi", ",", "")
	s = r.Replace(s)
	var out []token
	for i := 0; i < len(s); {
		c := rune(s[i])
		switch {
		case unicode.IsSpace(c):
			i++
		case c >= '0' && c <= '9' || c == '.':
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			if j < len(s) && (s[j] == 'e' || s[j] == 'E') && j+1 < len(s) && (s[j+1] >= '0' && s[j+1] <= '9' || (s[j+1] == '-' || s[j+1] == '+') && j+2 < len(s) && s[j+2] >= '0' && s[j+2] <= '9') {
				j += 2
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					j++
				}
			}
			v, err := strconv.ParseFloat(s[i:j], 64)
			if err != nil {
				return nil, ErrSyntax
			}
			out = append(out, token{kind: num, val: v})
			i = j
		case strings.ContainsRune("+-*/^%", c):
			out = append(out, token{kind: op, text: string(c)})
			i++
		case c == '(':
			out = append(out, token{kind: lparen})
			i++
		case c == ')':
			out = append(out, token{kind: rparen})
			i++
		case unicode.IsLetter(c):
			j := i
			for j < len(s) && unicode.IsLetter(rune(s[j])) {
				j++
			}
			w := strings.ToLower(s[i:j])
			switch {
			case w == "pi":
				out = append(out, token{kind: num, val: math.Pi})
			case w == "e":
				out = append(out, token{kind: num, val: math.E})
			case funcs[w]:
				out = append(out, token{kind: fn, text: w})
			default:
				return nil, fmt.Errorf("unknown name %q", w)
			}
			i = j
		default:
			return nil, fmt.Errorf("unexpected %q", c)
		}
	}
	return out, nil
}

// parser is a recursive-descent parser over
//
//	expr   = term { ("+"|"-") term }
//	term   = unary { ("*"|"/") unary | implicit }   implicit: 2π, 3(4), 2sin 30
//	unary  = ("-"|"+") unary | power
//	power  = postfix [ "^" unary ]                  right-associative
//	postfix= atom { "%" }
//	atom   = number | "(" expr [")"] | fn atom-or-unary
type parser struct {
	toks    []token
	pos     int
	degrees bool
}

func (p *parser) peek() (token, bool) {
	if p.pos < len(p.toks) {
		return p.toks[p.pos], true
	}
	return token{}, false
}

func (p *parser) isOp(s string) bool {
	t, ok := p.peek()
	return ok && t.kind == op && t.text == s
}

func (p *parser) expr() (float64, error) {
	v, err := p.term()
	for err == nil && (p.isOp("+") || p.isOp("-")) {
		o := p.toks[p.pos].text
		p.pos++
		var w float64
		if w, err = p.term(); o == "+" {
			v += w
		} else {
			v -= w
		}
	}
	return v, err
}

func (p *parser) term() (float64, error) {
	v, err := p.unary()
	for err == nil {
		t, ok := p.peek()
		switch {
		case ok && t.kind == op && (t.text == "*" || t.text == "/"):
			p.pos++
			var w float64
			if w, err = p.unary(); err != nil {
				break
			}
			if t.text == "*" {
				v *= w
			} else if w == 0 {
				err = errors.New("division by zero")
			} else {
				v /= w
			}
		case ok && (t.kind == num || t.kind == lparen || t.kind == fn): // implicit ×
			var w float64
			if w, err = p.power(); err == nil {
				v *= w
			}
		default:
			return v, nil
		}
	}
	return v, err
}

func (p *parser) unary() (float64, error) {
	if p.isOp("-") {
		p.pos++
		v, err := p.unary()
		return -v, err
	}
	if p.isOp("+") {
		p.pos++
		return p.unary()
	}
	return p.power()
}

func (p *parser) power() (float64, error) {
	v, err := p.postfix()
	if err == nil && p.isOp("^") {
		p.pos++
		var w float64
		if w, err = p.unary(); err == nil {
			v = math.Pow(v, w)
		}
	}
	return v, err
}

func (p *parser) postfix() (float64, error) {
	v, err := p.atom()
	for err == nil && p.isOp("%") {
		p.pos++
		v /= 100
	}
	return v, err
}

func (p *parser) atom() (float64, error) {
	t, ok := p.peek()
	if !ok {
		return 0, ErrSyntax
	}
	p.pos++
	switch t.kind {
	case num:
		return t.val, nil
	case lparen:
		v, err := p.expr()
		if err != nil {
			return 0, err
		}
		if t, ok := p.peek(); ok && t.kind == rparen {
			p.pos++
		} else if ok {
			return 0, ErrSyntax
		} // else: closed by the end
		return v, nil
	case fn:
		x, err := p.unary()
		if err != nil {
			return 0, err
		}
		return p.apply(t.text, x)
	}
	return 0, ErrSyntax
}

func (p *parser) apply(name string, x float64) (float64, error) {
	rad := func(v float64) float64 {
		if p.degrees {
			return v * math.Pi / 180
		}
		return v
	}
	deg := func(v float64) float64 {
		if p.degrees {
			return v * 180 / math.Pi
		}
		return v
	}
	// Exact values where floating point would show a residue: sin 180 = 0.
	clean := func(v float64) float64 {
		if math.Abs(v) < 1e-12 {
			return 0
		}
		return v
	}
	switch name {
	case "sin":
		return clean(math.Sin(rad(x))), nil
	case "cos":
		return clean(math.Cos(rad(x))), nil
	case "tan":
		if c := clean(math.Cos(rad(x))); c == 0 {
			return 0, errors.New("tan is undefined there")
		}
		return clean(math.Tan(rad(x))), nil
	case "asin":
		return deg(math.Asin(x)), nil
	case "acos":
		return deg(math.Acos(x)), nil
	case "atan":
		return deg(math.Atan(x)), nil
	case "ln":
		return math.Log(x), nil
	case "log":
		return math.Log10(x), nil
	case "sqrt":
		return math.Sqrt(x), nil
	case "abs":
		return math.Abs(x), nil
	}
	return 0, fmt.Errorf("unknown function %q", name)
}

// Pasted is pasted text as the calculator shows an expression (× ÷ − √ π),
// without spaces, thousands separators or a closing "=". Empty text is ignored;
// incomplete or invalid expressions remain available for the user to edit.
func Pasted(s string) (expr string, ok bool) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "=")
	s = strings.Join(strings.Fields(s), "")
	s = strings.NewReplacer(",", "", "*", "×", "/", "÷", "-", "−", "–", "−", "sqrt", "√", "pi", "π").Replace(s)
	if s == "" {
		return "", false
	}
	return s, true
}
