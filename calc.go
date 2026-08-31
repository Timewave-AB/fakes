package fakes

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// calc is the {calc(expr[, dp])} token: an arithmetic expression over number
// literals and sibling-field names, with + - * /, unary minus and parentheses. It is
// a registry builtin like any other {name(args)} function — the one that names
// operands, which expand reads for it (once per expansion, so the value computed is
// the value the format showed) and hands over as strings. The value prints in
// minimal decimal form, or rounded to dp when given. An operand that isn't a number
// becomes NaN, which propagates and prints as "NaN" — visible, never a render error.
// The expression is parsed once at New into an AST every render shares and none
// mutates, so render cannot fail and must not carry per-render state.

// calcNode is a parsed expression node. It evaluates over the operand values expand
// read, so the evaluator touches neither the rng nor the node tree.
type calcNode interface {
	eval(operands []string) float64
}

type calcNum float64 // a number literal
type calcVar string  // a sibling-field name, before indexVars places it
type calcIdx int     // an operand, by its position in the values expand read
type calcNeg struct{ x calcNode }
type calcBin struct { // a + - * / b
	op   byte
	l, r calcNode
}

func (n calcNum) eval([]string) float64 { return float64(n) }

func (n calcIdx) eval(operands []string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(operands[n]), 64)
	if err != nil {
		return math.NaN() // a non-numeric operand stays visible, never an error
	}
	return v
}

// eval on an unplaced name cannot happen: calcPrep runs indexVars over every
// expression it compiles, so only a calcIdx reaches a render. It panics rather
// than returning NaN, so a node kind indexVars forgets is a stack trace and not a
// silently wrong number.
func (n calcVar) eval([]string) float64 {
	panic(fmt.Sprintf("fakes: calc operand %q was never placed", string(n)))
}

func (n calcNeg) eval(operands []string) float64 { return -n.x.eval(operands) }

func (n calcBin) eval(operands []string) float64 {
	l, r := n.l.eval(operands), n.r.eval(operands)
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
// operands all name existing fields, and an optional non-negative integer dp. It
// is a builtin check (fields first), so calc dispatches through the registry like
// every other {name(args)} function.
func checkCalc(fields map[string]node, args []string) error {
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
		if dp, err := strconv.Atoi(args[1]); err != nil || dp < 0 || dp > maxDecimals {
			return fmt.Errorf("calc decimals %q must be an integer in 0..%d", args[1], maxDecimals)
		}
	}
	return nil
}

// calcPrep parses the expression and decimals once, at compile time, and places each
// operand name at the position expand will read it into. checkCalc proved both args
// valid, so no step here can fail; dp -1 prints the minimal form.
func calcPrep(args []string) callFn {
	expr, _ := parseCalc(args[0])
	at := make(map[string]int)
	for i, name := range calcVars(expr) {
		at[name] = i
	}
	placed := indexVars(expr, at)
	dp := -1
	if len(args) == 2 {
		dp = atoi(args[1])
	}
	return func(_ *session, _ string, operands []string) string {
		return strconv.FormatFloat(placed.eval(operands), 'f', dp, 64)
	}
}

// indexVars replaces each operand name with its position in the values expand reads.
// Both sides take that order from calcVars, so they cannot drift.
func indexVars(n calcNode, at map[string]int) calcNode {
	switch n := n.(type) {
	case calcVar:
		i, placed := at[string(n)]
		if !placed { // calcVars named every operand, so a miss means the two disagree
			panic(fmt.Sprintf("fakes: calc operand %q is not among the names read for it", string(n)))
		}
		return calcIdx(i)
	case calcNeg:
		return calcNeg{indexVars(n.x, at)}
	case calcBin:
		return calcBin{n.op, indexVars(n.l, at), indexVars(n.r, at)}
	}
	return n
}

// calcOperands lists the sibling-field names every {calc(...)} token in a format
// reads, so cycle detection sees the field edges calc renders through (a function
// token otherwise carries no field edge).
func calcOperands(format string) []string {
	var names []string
	_ = eachToken(format, func(t ftoken) error {
		if t.kind == 'b' {
			names = append(names, calcTokenOperands(t.body)...)
		}
		return nil
	})
	return names
}

// calcTokenOperands lists the sibling-field names one {token} body reads, empty
// for anything that is not a {calc(...)}. checkCalc reports an expression that does
// not parse, so one that does not simply names nothing here.
func calcTokenOperands(body string) []string {
	name, args, ok := funcCall(body)
	if !ok || name != "calc" || len(args) == 0 {
		return nil
	}
	expr, err := parseCalc(args[0])
	if err != nil {
		return nil
	}
	return calcVars(expr)
}

// calcVars lists the distinct field names an expression reads, in the order it first
// names each. That order is the contract between expand, which reads the operands
// into a slice, and indexVars, which places each name at its position in it.
func calcVars(n calcNode) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(calcNode)
	walk = func(n calcNode) {
		switch n := n.(type) {
		case calcVar:
			if name := string(n); !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		case calcNeg:
			walk(n.x)
		case calcBin:
			walk(n.l)
			walk(n.r)
		}
	}
	walk(n)
	return out
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
