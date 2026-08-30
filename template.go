package fakes

import (
	"fmt"
	"sort"
	"strings"
)

// This file is the {token} grammar of a format string: how it is scanned
// (eachToken), how a function token is parsed (funcCall) and validated
// (checkFunc, checkTokens), and the builtin contract its functions implement.
// render.go evaluates these tokens; node.go compiles the surrounding JSON;
// reference.go binds {..path} tokens across the tree.

// ftoken is one unit of a scanned format string: a literal rune to emit, a class
// char to randomise ('0' '1' 'A' 'a'), or the body of a {…} token.
type ftoken struct {
	kind byte // 'l' literal rune, 'c' class char, 'b' brace body
	r    rune // for kinds 'l' and 'c'
	body string
}

// eachToken scans a format string once and calls fn for each unit, the single
// source of truth for how '#' escapes and {…} braces are read — compileOps,
// checkTokens, fieldTokens and refTokens all drive off it so the grammar can't
// drift between the validator and the renderer. '#' escapes the next char to a
// literal ("#0" -> '0', "##" -> '#'); a '{' must reach a '}' (else an error); an
// unmatched '}' is an ordinary literal. The error stops the scan early.
func eachToken(format string, fn func(ftoken) error) error {
	rs := []rune(format)
	for i := 0; i < len(rs); i++ {
		var t ftoken
		switch c := rs[i]; c {
		case '#':
			t.kind, t.r = 'l', '#'
			if i++; i < len(rs) {
				t.r = rs[i]
			}
		case '0', '1', 'A', 'a':
			t.kind, t.r = 'c', c
		case '{':
			end := i + 1
			for end < len(rs) && rs[end] != '}' {
				end++
			}
			if end >= len(rs) {
				return fmt.Errorf("unterminated '{' in %q", format)
			}
			t.kind, t.body = 'b', string(rs[i+1:end])
			i = end
		default:
			t.kind, t.r = 'l', c
		}
		if err := fn(t); err != nil {
			return err
		}
	}
	return nil
}

// builtin is a format-string function invoked as {name(args)}. It receives the
// session (its rng, and the {seq()} counters), the output emitted so far in the
// current expansion (for derivations such as a checksum over preceding digits), and
// the values of the operands it named (only calc names any).
// Almost all are pure over (rng, emitted, args) — no wall-clock, no crypto/rand —
// so seeding stays reproducible; a time-based id derives its time from the rng.
// seq is the one exception: it advances per-session counter state, which is itself
// deterministic (1, 2, 3 …). arity is the exact arg count, or -1 for variadic
// (then check does all the validation). The optional check validates args at
// compile time (their values, beyond the count). The registry lives in builtins.go.
type builtin struct {
	arity int
	// prep parses validated args once, at compile time, into the closure expand calls.
	prep  func(args []string) callFn
	check func(fields map[string]node, args []string) error
}

// funcCall splits a "{token}" body shaped name(args) into its parts; ok is false
// for a plain field or alternation body. A '(' without a trailing ')' yields
// ok=false; checkFunc reports it as malformed at compile time.
func funcCall(body string) (name string, args []string, ok bool) {
	lp := strings.IndexByte(body, '(')
	if lp < 0 || !strings.HasSuffix(body, ")") {
		return "", nil, false
	}
	return body[:lp], splitArgs(body[lp+1 : len(body)-1]), true
}

// splitArgs parses a function arg list: comma-separated, trimmed; empty -> none.
func splitArgs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	args := strings.Split(s, ",")
	for i := range args {
		args[i] = strings.TrimSpace(args[i])
	}
	return args
}

// checkFunc validates a function token at compile time: well-formed, naming a
// known builtin, with the arg count that builtin takes and args its check accepts.
// fields is passed through for the one builtin (calc) that validates against them.
func checkFunc(body string, fields map[string]node) error {
	name, args, ok := funcCall(body)
	if !ok {
		return fmt.Errorf("malformed function token {%s}", body)
	}
	b, known := builtins[name]
	if !known {
		return fmt.Errorf("token {%s}: unknown function %q", body, name)
	}
	if b.arity >= 0 && len(args) != b.arity {
		return fmt.Errorf("token {%s}: %s takes %d args, got %d", body, name, b.arity, len(args))
	}
	if b.check != nil {
		if err := b.check(fields, args); err != nil {
			return fmt.Errorf("token {%s}: %w", body, err)
		}
	}
	return nil
}

// checkTokens validates a format string the way expand scans it, so every
// "{token}" is balanced and names an existing field (or a known function). This
// makes a typo'd or dangling reference a New-time error, never a random
// render-time one.
func checkTokens(format string, fields map[string]node) error {
	return eachToken(format, func(t ftoken) error {
		if t.kind != 'b' {
			return nil
		}
		if strings.IndexByte(t.body, '(') >= 0 { // a function token, not a field
			return checkFunc(t.body, fields)
		}
		names := strings.Split(t.body, "|")
		if err := checkNoRepeatedArm(t.body, names); err != nil {
			return err
		}
		for _, name := range names {
			if isRef(name) {
				if name == refPrefix {
					return fmt.Errorf("token {%s}: reference has no path", t.body)
				}
				continue // a root reference; its target is checked at New (see linkRefs)
			}
			a := splitArm(name)
			if err := checkSegments(a); err != nil {
				return fmt.Errorf("token {%s}: %w", t.body, err)
			}
			head, ok := fields[a.key]
			if !ok {
				if isOption(a.key) {
					return fmt.Errorf("token {%s}: %q is an option and can never be a field", t.body, a.key)
				}
				return fmt.Errorf("token {%s}: no field %q", t.body, a.key)
			}
			if err := checkPath(head, a.tail, a.key); err != nil {
				return fmt.Errorf("token {%s}: field %q: %w", t.body, a.key, err)
			}
		}
		return nil
	})
}

// checkNoRepeatedArm rejects {a|a|b}. An alternation picks its arms evenly, so a
// repeated one skews the odds — a third spelling of what weight is for, and one an
// author is far likelier to have typed by accident than meant. The error names the
// spelling that does skew a pick.
func checkNoRepeatedArm(body string, names []string) error {
	if len(names) < 2 {
		return nil
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			return fmt.Errorf("token {%s}: arm %q is repeated; an alternation picks its arms evenly, so skew the odds with a choice's weights instead", body, name)
		}
		seen[name] = true
	}
	return nil
}

// fieldTokens returns the field and reference names a format renders via {name}
// or {a|..b} tokens (function tokens, which carry no field edges, are excluded).
// These are exactly the child nodes expand recurses into, through readField.
func fieldTokens(format string) []string {
	var names []string
	_ = eachToken(format, func(t ftoken) error {
		if t.kind == 'b' && strings.IndexByte(t.body, '(') < 0 {
			names = append(names, strings.Split(t.body, "|")...)
		}
		return nil
	})
	return names
}

// arm is one alternative of a {a|b} token, split into the key naming the node in
// a template's fields (a sibling field, or the whole "..path" string a reference is
// bound under) and the tail of a dotted path into it. A non-empty tail is what makes
// the arm a bound draw: its head is drawn once per expansion (see compileOps).
type arm struct {
	name  string // as written, and the key a bound draw's value is held under
	key   string
	tail  []string
	steps []string // key per level passed through; the head and leaf hold their own
}

// splitArm splits one token alternative into key and tail. A reference keeps its
// dots — linkRefs binds it whole — so only a sibling name reads as a path.
func splitArm(name string) arm {
	if isRef(name) {
		return arm{name: name, key: name}
	}
	head, tail, dotted := strings.Cut(name, ".")
	if !dotted {
		return arm{name: name, key: name}
	}
	segs := strings.Split(tail, ".")
	var steps []string
	for i := 0; i < len(segs)-1; i++ { // every level except the leaf's own
		steps = append(steps, head+"."+strings.Join(segs[:i+1], "."))
	}
	return arm{name: name, key: head, tail: segs, steps: steps}
}

// checkNoOverlap rejects a format that both renders a level and reads a path into
// it — {p} beside {p.first}, or {p.addr} beside {p.addr.city}. The two spell one
// draw two ways: the path reads the level's held draw, while rendering the level
// expands it afresh, so their values disagree. One spelling, so there is nothing
// to get wrong. Names are compared in sorted order, so which pair is reported
// does not depend on where the tokens sit.
func checkNoOverlap(format string, bound map[string]string) error {
	names := boundReaders(format, bound)
	// Stable over one format-order scan, so two readers of one name (a token and a
	// calc operand both naming "p") are reported as the format writes them.
	sort.SliceStable(names, func(i, j int) bool { return names[i].name < names[j].name })
	for i, level := range names {
		for _, path := range names[i+1:] {
			if strings.HasPrefix(path.name, level.name+".") {
				return fmt.Errorf("%s renders a level that {%s} reads a path into; name the fields you want instead", level.label, path.name)
			}
		}
	}
	return nil
}

// reader is one way a format reaches a bound field, and how to name that spelling.
type reader struct{ name, label string }

// boundReaders lists every way a format reaches a bound field, in the order the
// format writes them. A calc operand renders its field, so it names a level exactly
// as a token does; one scan finds both, which is what puts them in one order.
func boundReaders(format string, bound map[string]string) []reader {
	var names []reader
	_ = eachToken(format, func(t ftoken) error {
		if t.kind != 'b' {
			return nil
		}
		if _, _, isFunc := funcCall(t.body); isFunc {
			for _, operand := range calcTokenOperands(t.body) {
				if _, isBound := bound[operand]; isBound {
					names = append(names, reader{operand, fmt.Sprintf("calc operand %q", operand)})
				}
			}
			return nil
		}
		for _, a := range splitArms(t.body) {
			if _, isBound := bound[a.key]; isBound {
				names = append(names, reader{a.name, "token {" + a.name + "}"})
			}
		}
		return nil
	})
	return names
}

// checkSegments rejects an unfinished path: "{a.}", "{.b}" and "{a..b}" each have
// a segment naming nothing. A field really named "" would otherwise make them
// resolve, so a typo would read as a path that worked.
func checkSegments(a arm) error {
	if len(a.tail) == 0 {
		return nil
	}
	if a.key == "" {
		return fmt.Errorf("path has an empty segment")
	}
	for _, seg := range a.tail {
		if seg == "" {
			return fmt.Errorf("path has an empty segment")
		}
	}
	return nil
}

// splitArms splits a token body's '|' alternatives.
func splitArms(body string) []arm {
	parts := strings.Split(body, "|")
	arms := make([]arm, len(parts))
	for i, p := range parts {
		arms[i] = splitArm(p)
	}
	return arms
}

// callFn is a builtin bound to one call site: its args already parsed. It reads the
// output emitted so far in the current expansion (a derivation's payload) and the
// values of the operands it named, which expand read for it.
type callFn func(s *session, emitted string, operands []string) string

// op is one compiled unit of a format string: a literal run, a class char, a field
// alternation, or a builtin already bound to its args. compile builds these so
// render never re-scans the format.
type op struct {
	kind byte   // 'l' literal run, 'c' class char, 'f' field alternation, 'b' builtin
	lit  string // kind 'l'
	r    rune   // kind 'c'
	arms []arm  // kind 'f': the '|' alternatives, split into key and path once
	call callFn
	// operands names the sibling fields a {calc()} reads, in the order its
	// expression first names them. expand reads them before the call, so the
	// evaluator never touches the node tree. nil for every other builtin.
	operands []string
}

// compileOps turns a format string into ops, and returns the smallest output it can
// produce (literals plus one byte per class char) to size the render buffer, plus
// two sets. bound is the levels the format addresses by dotted path, each mapped to
// the first path reading it, which is what the overlap fences name. held is every
// name drawn once per expansion — those levels, plus the siblings a {calc()} reads,
// so an operand shown is the operand computed. Both are nil when the format needs
// neither, so data that uses neither carries no render-time cost. Call checkTokens
// first: it is what proves the scan and every token are valid.
func compileOps(format string) ([]op, int, map[string]string, map[string]bool) {
	var ops []op
	var lit strings.Builder
	var bound map[string]string
	var held map[string]bool
	grow := 0
	hold := func(name string) {
		if held == nil {
			held = map[string]bool{}
		}
		held[name] = true
	}
	flush := func() {
		if lit.Len() > 0 {
			grow += lit.Len()
			ops = append(ops, op{kind: 'l', lit: lit.String()})
			lit.Reset()
		}
	}
	_ = eachToken(format, func(t ftoken) error {
		switch t.kind {
		case 'l':
			lit.WriteRune(t.r)
		case 'c':
			flush()
			grow++
			ops = append(ops, op{kind: 'c', r: t.r})
		case 'b':
			flush()
			if name, args, ok := funcCall(t.body); ok {
				operands := calcTokenOperands(t.body)
				for _, operand := range operands {
					hold(operand) // a calc renders its operand, so the expansion holds that draw
				}
				ops = append(ops, op{kind: 'b', call: builtins[name].prep(args), operands: operands})
			} else {
				arms := splitArms(t.body)
				for _, a := range arms {
					if len(a.tail) > 0 {
						if bound == nil {
							bound = map[string]string{}
						}
						if _, named := bound[a.key]; !named {
							bound[a.key] = a.name // the first path reading it, for error messages
						}
						hold(a.key)
					}
				}
				ops = append(ops, op{kind: 'f', arms: arms})
			}
		}
		return nil
	})
	flush()
	return ops, grow, bound, held
}
