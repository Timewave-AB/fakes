package fakes

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// calc is the {calc(expr[, dp])} token: an arithmetic expression over number
// literals and sibling-field names (rendered, then parsed as numbers), with
// + - * /, unary minus and parentheses. Unlike a builtin it reads the field
// environment, so expand and checkTokens route it here rather than the builtins
// registry. The value prints in minimal decimal form, or rounded to dp decimals
// when given. A field that doesn't render to a number becomes NaN, which
// propagates and prints as "NaN" — visible, never a render error. The expression
// is re-parsed each render, mirroring how expand re-scans the format; checkCalc
// proves it parses (and names real fields) at New, so render never fails.

// calcNode is a parsed expression node.
type calcNode interface {
	eval(s *session, fields map[string]node) float64
}

type calcNum float64 // a number literal
type calcVar string  // a sibling-field name
type calcNeg struct{ x calcNode }
type calcBin struct { // a + - * / b
	op   byte
	l, r calcNode
}

func (n calcNum) eval(*session, map[string]node) float64 { return float64(n) }

func (n calcVar) eval(s *session, fields map[string]node) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(render(s, fields[string(n)])), 64)
	if err != nil {
		return math.NaN() // a non-numeric operand stays visible, never an error
	}
	return v
}

func (n calcNeg) eval(s *session, fields map[string]node) float64 { return -n.x.eval(s, fields) }

func (n calcBin) eval(s *session, fields map[string]node) float64 {
	l, r := n.l.eval(s, fields), n.r.eval(s, fields)
	switch n.op {
	case '+':
		return l + r
	case '-':
		return l - r
	case '*':
		return l * r
	default: // '/'
		return l / r
	}
}

// checkCalc validates a calc token at compile time: a parseable expression whose
// operands all name existing fields, and an optional non-negative integer dp.
func checkCalc(args []string, fields map[string]node) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("calc takes an expression and an optional decimals count, got %d args", len(args))
	}
	expr, err := parseCalc(args[0])
	if err != nil {
		return fmt.Errorf("calc(%q): %w", args[0], err)
	}
	for _, name := range calcVars(expr) {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("calc(%q): no field %q", args[0], name)
		}
	}
	if len(args) == 2 {
		if dp, err := strconv.Atoi(args[1]); err != nil || dp < 0 {
			return fmt.Errorf("calc decimals %q must be a non-negative integer", args[1])
		}
	}
	return nil
}

// calcEval renders a calc token. checkCalc proved the expression parses and the
// decimals arg is valid, so neither step here can fail. dp -1 prints the minimal
// form; a given dp rounds to that many places.
func calcEval(s *session, args []string, fields map[string]node) string {
	expr, _ := parseCalc(args[0])
	dp := -1
	if len(args) == 2 {
		dp = atoi(args[1])
	}
	return strconv.FormatFloat(expr.eval(s, fields), 'f', dp, 64)
}

// calcVars lists the field names an expression references, for the existence
// check in checkCalc.
func calcVars(n calcNode) []string {
	switch n := n.(type) {
	case calcVar:
		return []string{string(n)}
	case calcNeg:
		return calcVars(n.x)
	case calcBin:
		return append(calcVars(n.l), calcVars(n.r)...)
	default:
		return nil
	}
}

// calcParser is a recursive-descent parser over the expression runes, threading
// expr -> term -> factor for the standard * / before + - precedence.
type calcParser struct {
	rs  []rune
	pos int
}

// parseCalc parses a whole expression, requiring it to consume all input.
func parseCalc(expr string) (calcNode, error) {
	p := &calcParser{rs: []rune(expr)}
	if p.space(); p.pos >= len(p.rs) {
		return nil, fmt.Errorf("empty expression")
	}
	n, err := p.expr()
	if err != nil {
		return nil, err
	}
	if p.space(); p.pos != len(p.rs) {
		return nil, fmt.Errorf("unexpected %q", string(p.rs[p.pos:]))
	}
	return n, nil
}

func (p *calcParser) space() {
	for p.pos < len(p.rs) && unicode.IsSpace(p.rs[p.pos]) {
		p.pos++
	}
}

func (p *calcParser) expr() (calcNode, error) { return p.binary(p.term, '+', '-') }
func (p *calcParser) term() (calcNode, error) { return p.binary(p.factor, '*', '/') }

// binary parses a left-associative run of next() operands joined by the given
// operators, the one shape expr and term share.
func (p *calcParser) binary(next func() (calcNode, error), ops ...byte) (calcNode, error) {
	n, err := next()
	if err != nil {
		return nil, err
	}
	for {
		p.space()
		if p.pos >= len(p.rs) || !contains(ops, byte(p.rs[p.pos])) {
			return n, nil
		}
		op := byte(p.rs[p.pos])
		p.pos++
		r, err := next()
		if err != nil {
			return nil, err
		}
		n = calcBin{op, n, r}
	}
}

func (p *calcParser) factor() (calcNode, error) {
	p.space()
	if p.pos >= len(p.rs) {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	switch c := p.rs[p.pos]; {
	case c == '-':
		p.pos++
		x, err := p.factor()
		if err != nil {
			return nil, err
		}
		return calcNeg{x}, nil
	case c == '(':
		p.pos++
		n, err := p.expr()
		if err != nil {
			return nil, err
		}
		if p.space(); p.pos >= len(p.rs) || p.rs[p.pos] != ')' {
			return nil, fmt.Errorf("missing ')'")
		}
		p.pos++
		return n, nil
	case c == '.' || c >= '0' && c <= '9':
		return p.number()
	case c == '_' || unicode.IsLetter(c):
		return p.ident()
	default:
		return nil, fmt.Errorf("unexpected %q", string(c))
	}
}

func (p *calcParser) number() (calcNode, error) {
	start, dot := p.pos, false
	for p.pos < len(p.rs) {
		if c := p.rs[p.pos]; c >= '0' && c <= '9' {
			p.pos++
		} else if c == '.' && !dot {
			dot, p.pos = true, p.pos+1
		} else {
			break
		}
	}
	v, err := strconv.ParseFloat(string(p.rs[start:p.pos]), 64)
	if err != nil {
		return nil, fmt.Errorf("bad number %q", string(p.rs[start:p.pos]))
	}
	return calcNum(v), nil
}

// ident reads a field name: a letter or '_', then letters, digits or '_'. A '-'
// is always the minus operator, so a hyphenated field name can't be an operand.
func (p *calcParser) ident() (calcNode, error) {
	start := p.pos
	for p.pos < len(p.rs) {
		if c := p.rs[p.pos]; c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c) {
			p.pos++
		} else {
			break
		}
	}
	return calcVar(string(p.rs[start:p.pos])), nil
}

func contains(bs []byte, b byte) bool {
	for _, x := range bs {
		if x == b {
			return true
		}
	}
	return false
}
