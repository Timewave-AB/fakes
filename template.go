package fakes

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// node is a compiled template element: literal, choice, or template. Compiling
// JSON into these once (see compile) means rendering never re-inspects the raw
// JSON or re-sums weights.
type node interface{ isNode() }

// rng is the randomness the renderer draws from. Passing it in keeps the render
// functions a pure core over an explicit effect; *rand.Rand satisfies it.
type rng interface {
	IntN(n int) int
	Float64() float64
}

// literal is emitted verbatim, never formatted.
type literal string

func (literal) isNode() {}

// choice picks one of its items. cum holds cumulative weights for a weighted
// pick; when nil the choice is uniform and selection is O(1).
type choice struct {
	items []node
	cum   []float64
}

func (*choice) isNode() {}

// template renders a format string, substituting {tokens} from fields. repeat
// (default 1) renders that format that many times and joins the results with
// separator (default ""), each render an independent pick.
type template struct {
	format    string
	fields    map[string]node
	repeat    int
	separator string
}

func (*template) isNode() {}

// Fake generates a value for a category path. The first segment names a
// category (a JSON file); further dot-separated segments descend into named
// fields, e.g. "address" or "address.street". Choices along the way are
// resolved at random.
func (f *Fakes) Fake(path string) (string, error) {
	segments := strings.Split(path, ".")
	n, ok := f.categories[segments[0]]
	if !ok {
		return "", fmt.Errorf("fakes: unknown category %q", segments[0])
	}
	n, err := descend(f.rand, n, segments[1:])
	if err != nil {
		return "", fmt.Errorf("fakes: %s: %w", path, err)
	}
	return render(f.rand, n), nil
}

// descend walks named fields, resolving choices it meets along the way. It is
// the one render-side step that can fail, because the path comes from the
// caller and may name a field that does not exist.
func descend(r rng, n node, segments []string) (node, error) {
	if len(segments) == 0 {
		return n, nil
	}
	switch n := n.(type) {
	case *template:
		child, ok := n.fields[segments[0]]
		if !ok {
			return nil, fmt.Errorf("no field %q", segments[0])
		}
		return descend(r, child, segments[1:])
	case *choice:
		return descend(r, pick(r, n), segments) // a choice consumes no path segment
	default:
		return nil, fmt.Errorf("cannot descend into %T at %q", n, segments[0])
	}
}

// render evaluates a compiled node to a string. compile validates every node up
// front, so rendering a compiled tree cannot fail.
func render(r rng, n node) string {
	switch n := n.(type) {
	case literal:
		return string(n)
	case *choice:
		return render(r, pick(r, n))
	case *template:
		if n.repeat == 1 {
			return expand(r, n.format, n.fields)
		}
		var b strings.Builder
		for i := 0; i < n.repeat; i++ {
			if i > 0 {
				b.WriteString(n.separator)
			}
			b.WriteString(expand(r, n.format, n.fields))
		}
		return b.String()
	default:
		panic(fmt.Sprintf("fakes: uncompiled node %T", n))
	}
}

// pick selects one item. Uniform choices are O(1); weighted choices are an
// O(log n) search over precomputed cumulative weights. compile guarantees a
// non-empty choice and a finite positive total, so the index is always in range.
func pick(r rng, c *choice) node {
	if c.cum == nil {
		return c.items[r.IntN(len(c.items))]
	}
	x := r.Float64() * c.cum[len(c.cum)-1]
	i := sort.Search(len(c.cum), func(i int) bool { return c.cum[i] > x })
	return c.items[i]
}

// expand renders a format string. Character classes: '0' digit 0-9, '1' digit
// 1-9, 'A' letter A-Z, 'a' letter a-z. '#' escapes the next character to a
// literal ("#0" -> "0", "##" -> "#"). A "{name(args)}" token calls a builtin;
// any other "{token}" is substituted via resolve; every other rune is literal.
// checkTokens validated the braces and tokens at compile time, so this scan
// cannot fail.
func expand(r rng, format string, fields map[string]node) string {
	var b strings.Builder
	rs := []rune(format)
	for i := 0; i < len(rs); i++ {
		switch c := rs[i]; c {
		case '#':
			if i++; i < len(rs) {
				b.WriteRune(rs[i])
			} else {
				b.WriteRune('#')
			}
		case '0':
			b.WriteByte(byte('0' + r.IntN(10)))
		case '1':
			b.WriteByte(byte('1' + r.IntN(9)))
		case 'A':
			b.WriteByte(byte('A' + r.IntN(26)))
		case 'a':
			b.WriteByte(byte('a' + r.IntN(26)))
		case '{':
			end := i + 1
			for rs[end] != '}' { // checkTokens guarantees a closing '}'
				end++
			}
			body := string(rs[i+1 : end])
			if name, args, ok := funcCall(body); ok {
				b.WriteString(builtins[name].call(r, b.String(), args)) // b.String() is the output so far
			} else {
				b.WriteString(resolve(r, body, fields))
			}
			i = end
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// resolve renders a "{token}" body: one or more field names separated by '|',
// of which one is chosen at random. checkTokens guarantees every name exists.
func resolve(r rng, token string, fields map[string]node) string {
	names := strings.Split(token, "|")
	return render(r, fields[names[r.IntN(len(names))]])
}

// builtin is a format-string function invoked as {name(args)}. It receives the
// rng, the output emitted so far in the current expansion (for derivations such
// as a checksum over preceding digits), and its args. A builtin must be pure
// over those inputs — no wall-clock, no crypto/rand — so seeding stays
// reproducible; a time-based id derives its time from the rng.
type builtin struct {
	arity int
	call  func(r rng, emitted string, args []string) string
}

var builtins = map[string]builtin{
	// luhn emits a Luhn check digit over the digits emitted so far. Put it after
	// its payload (its input is everything to its left); prepend fixed parts,
	// e.g. a century, in an enclosing template so they stay out of the sum.
	"luhn": {0, func(_ rng, emitted string, _ []string) string {
		return string(rune('0' + luhnCheck(emitted)))
	}},
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
// known builtin, with the arg count that builtin takes.
func checkFunc(body string) error {
	name, args, ok := funcCall(body)
	if !ok {
		return fmt.Errorf("malformed function token {%s}", body)
	}
	b, known := builtins[name]
	if !known {
		return fmt.Errorf("token {%s}: unknown function %q", body, name)
	}
	if len(args) != b.arity {
		return fmt.Errorf("token {%s}: %s takes %d args, got %d", body, name, b.arity, len(args))
	}
	return nil
}

// luhnCheck returns the Luhn check digit (0-9) over the digits of s; non-digit
// runes are skipped. Doubling runs from the rightmost digit, so the result is
// correct whatever the payload length.
func luhnCheck(s string) int {
	sum, double := 0, true
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		d := int(c - '0')
		if double {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		double = !double
		sum += d
	}
	return (10 - sum%10) % 10
}

// compile converts parsed JSON into a node tree, validating structure up front.
func compile(v any) (node, error) {
	switch v := v.(type) {
	case string:
		return literal(v), nil
	case []any:
		return compileChoice(v)
	case map[string]any:
		return compileTemplate(v)
	default:
		return nil, fmt.Errorf("unsupported node type %T", v)
	}
}

func compileChoice(items []any) (node, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("empty choice")
	}
	c := &choice{items: make([]node, len(items))}
	cum := make([]float64, len(items))
	var total float64
	weighted := false
	for i, raw := range items {
		w, err := weightOf(raw)
		if err != nil {
			return nil, err
		}
		if w != 1 {
			weighted = true
		}
		total += w
		cum[i] = total
		n, err := compile(raw)
		if err != nil {
			return nil, err
		}
		c.items[i] = n
	}
	if weighted { // uniform choices skip the weight table and pick in O(1)
		if total <= 0 || math.IsInf(total, 1) {
			return nil, fmt.Errorf("choice weights must sum to a finite positive number, got %v", total)
		}
		c.cum = cum
	}
	return c, nil
}

func compileTemplate(m map[string]any) (node, error) {
	format, ok := m["format"].(string)
	if !ok {
		return nil, fmt.Errorf("template object missing string \"format\"")
	}
	repeat, err := repeatOf(m)
	if err != nil {
		return nil, err
	}
	sep := ""
	if sv, ok := m["separator"]; ok {
		if sep, ok = sv.(string); !ok {
			return nil, fmt.Errorf("separator must be a string, got %T", sv)
		}
	}
	t := &template{format: format, fields: make(map[string]node, len(m)), repeat: repeat, separator: sep}
	for k, v := range m {
		if k == "format" || k == "weight" || k == "repeat" || k == "separator" {
			continue
		}
		n, err := compile(v)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		t.fields[k] = n
	}
	if err := checkTokens(format, t.fields); err != nil {
		return nil, err
	}
	return t, nil
}

// checkTokens validates a format string the way expand scans it, so every
// "{token}" is balanced and names an existing field. This makes a typo'd or
// dangling reference a New-time error, never a random render-time one.
func checkTokens(format string, fields map[string]node) error {
	rs := []rune(format)
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '#':
			i++ // an escaped char is literal, never a token delimiter
		case '{':
			end := i + 1
			for end < len(rs) && rs[end] != '}' {
				end++
			}
			if end >= len(rs) {
				return fmt.Errorf("unterminated '{' in %q", format)
			}
			body := string(rs[i+1 : end])
			if strings.IndexByte(body, '(') >= 0 { // a function token, not a field
				if err := checkFunc(body); err != nil {
					return err
				}
			} else {
				for _, name := range strings.Split(body, "|") {
					if _, ok := fields[name]; !ok {
						return fmt.Errorf("token {%s}: no field %q", body, name)
					}
				}
			}
			i = end
		}
	}
	return nil
}

// repeatOf reads a template's "repeat" (default 1): how many times its format
// is rendered and concatenated. A present one must be a positive integer.
func repeatOf(m map[string]any) (int, error) {
	rv, ok := m["repeat"]
	if !ok {
		return 1, nil
	}
	r, ok := rv.(float64)
	if !ok {
		return 0, fmt.Errorf("repeat must be a number, got %T", rv)
	}
	if math.IsNaN(r) || math.IsInf(r, 0) || r < 1 || r != math.Trunc(r) {
		return 0, fmt.Errorf("repeat must be a positive integer, got %v", rv)
	}
	return int(r), nil
}

// weightOf reads a node's "weight" (default 1) from its raw JSON form. Only
// template objects carry weight; a present one must be finite and non-negative.
func weightOf(raw any) (float64, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return 1, nil
	}
	wv, ok := m["weight"]
	if !ok {
		return 1, nil
	}
	w, ok := wv.(float64)
	if !ok {
		return 0, fmt.Errorf("weight must be a number, got %T", wv)
	}
	if w < 0 || math.IsNaN(w) || math.IsInf(w, 0) {
		return 0, fmt.Errorf("weight must be finite and non-negative, got %v", w)
	}
	return w, nil
}
