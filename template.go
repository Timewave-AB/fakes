package fakes

import (
	"fmt"
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
// current expansion (for derivations such as a checksum over preceding digits),
// the sibling fields (only calc reads them, to render its operands), and its args.
// Almost all are pure over (rng, emitted, args) — no wall-clock, no crypto/rand —
// so seeding stays reproducible; a time-based id derives its time from the rng.
// seq is the one exception: it advances per-session counter state, which is itself
// deterministic (1, 2, 3 …). arity is the exact arg count, or -1 for variadic
// (then check does all the validation). The optional check validates args at
// compile time (their values, beyond the count). A builtin supplies exactly one of
// prep (args parsed once, at compile) or call (args parsed per render). The registry
// lives in builtins.go.
type builtin struct {
	arity int
	// prep parses validated args once, at compile time, into the closure expand calls.
	prep  func(args []string) callFn
	check func(fields map[string]node, args []string) error
	call  func(s *session, emitted string, fields map[string]node, args []string) string
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
		for _, name := range strings.Split(t.body, "|") {
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
			if err := checkPath(head, a.tail); err != nil {
				return fmt.Errorf("token {%s}: field %q: %w", t.body, a.key, err)
			}
		}
		return nil
	})
}

// fieldTokens returns the field and reference names a format renders via {name}
// or {a|..b} tokens (function tokens, which carry no field edges, are excluded).
// These are exactly the child nodes expand's resolve recurses into.
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
// the arm a bound draw: its head is drawn once per expansion (see boundHeads).
type arm struct {
	name string // as written, and the key a bound draw's value is held under
	key  string
	tail []string
	// steps holds the key for each segment the walk descends to, so every level a
	// path passes through is held — including its last, which another path may
	// name in full ({p.addr} beside {p.addr.city}).
	steps []string
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
	steps := make([]string, len(segs))
	for i := range segs {
		steps[i] = head + "." + strings.Join(segs[:i+1], ".")
	}
	return arm{name: name, key: head, tail: segs, steps: steps}
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

// callFn is a builtin bound to one call site: its args already parsed.
type callFn func(s *session, emitted string, fields map[string]node) string

// op is one compiled unit of a format string: a literal run, a class char, a field
// alternation, or a builtin already bound to its args. compile builds these so
// render never re-scans the format.
type op struct {
	kind byte   // 'l' literal run, 'c' class char, 'f' field alternation, 'b' builtin
	lit  string // kind 'l'
	r    rune   // kind 'c'
	arms []arm  // kind 'f': the '|' alternatives, split into key and path once
	call callFn
}

// compileOps turns a format string into ops, and returns the smallest output it can
// produce (literals plus one byte per class char) to size the render buffer, plus
// the fields the format addresses by dotted path — each drawn once per expansion,
// so every token reading one sees the same row (see expand). bound is nil when the
// format takes no path, so data that uses none carries no render-time cost.
// Call checkTokens first: it is what proves the scan and every token are valid.
func compileOps(format string) ([]op, int, map[string]bool) {
	var ops []op
	var lit strings.Builder
	var bound map[string]bool
	grow := 0
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
				ops = append(ops, op{kind: 'b', call: builtins[name].bind(args)})
			} else {
				arms := splitArms(t.body)
				for _, a := range arms {
					if len(a.tail) > 0 {
						if bound == nil {
							bound = map[string]bool{}
						}
						bound[a.key] = true
					}
				}
				ops = append(ops, op{kind: 'f', arms: arms})
			}
		}
		return nil
	})
	flush()
	return ops, grow, bound
}

// bind returns the closure for one call site, parsing args once via prep if present.
func (b builtin) bind(args []string) callFn {
	if b.prep != nil {
		return b.prep(args)
	}
	call := b.call
	return func(s *session, emitted string, fields map[string]node) string {
		return call(s, emitted, fields, args)
	}
}
