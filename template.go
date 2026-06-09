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
// source of truth for how '#' escapes and {…} braces are read — expand,
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
// compile time (their values, beyond the count). The registry lives in builtins.go.
type builtin struct {
	arity int
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
			if _, ok := fields[name]; !ok {
				return fmt.Errorf("token {%s}: no field %q", t.body, name)
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
