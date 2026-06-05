package fakes

import (
	"fmt"
	"sort"
	"strings"
)

// node is a compiled template element: literal, choice, or template. Compiling
// JSON into these once (see compile) means rendering never re-inspects the raw
// JSON or re-sums weights.
type node interface{ isNode() }

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

// template renders a format string, substituting {tokens} from fields.
type template struct {
	format string
	fields map[string]node
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
	n, err := f.descend(n, segments[1:])
	if err != nil {
		return "", fmt.Errorf("fakes: %s: %w", path, err)
	}
	s, err := f.render(n)
	if err != nil {
		return "", fmt.Errorf("fakes: %s: %w", path, err)
	}
	return s, nil
}

// descend walks named fields, resolving choices it meets along the way.
func (f *Fakes) descend(n node, segments []string) (node, error) {
	if len(segments) == 0 {
		return n, nil
	}
	switch n := n.(type) {
	case *template:
		child, ok := n.fields[segments[0]]
		if !ok {
			return nil, fmt.Errorf("no field %q", segments[0])
		}
		return f.descend(child, segments[1:])
	case *choice:
		chosen, err := f.pick(n)
		if err != nil {
			return nil, err
		}
		return f.descend(chosen, segments) // a choice consumes no path segment
	default:
		return nil, fmt.Errorf("cannot descend into %T at %q", n, segments[0])
	}
}

// render evaluates a node to a string.
func (f *Fakes) render(n node) (string, error) {
	switch n := n.(type) {
	case literal:
		return string(n), nil
	case *choice:
		chosen, err := f.pick(n)
		if err != nil {
			return "", err
		}
		return f.render(chosen)
	case *template:
		return f.expand(n.format, n.fields)
	default:
		return "", fmt.Errorf("unsupported node %T", n)
	}
}

// pick selects one item. Uniform choices are O(1); weighted choices are an
// O(log n) search over precomputed cumulative weights.
func (f *Fakes) pick(c *choice) (node, error) {
	if len(c.items) == 0 {
		return nil, fmt.Errorf("empty choice")
	}
	if c.cum == nil {
		return c.items[f.intn(len(c.items))], nil
	}
	r := f.rand.Float64() * c.cum[len(c.cum)-1]
	i := sort.Search(len(c.cum), func(i int) bool { return c.cum[i] > r })
	if i >= len(c.items) {
		i = len(c.items) - 1
	}
	return c.items[i], nil
}

// expand renders a format string. Character classes: '0' digit 0-9, '1' digit
// 1-9, 'A' letter A-Z, 'a' letter a-z. '#' escapes the next character to a
// literal ("#0" -> "0", "##" -> "#"). "{token}" is substituted via resolve;
// every other rune is literal.
func (f *Fakes) expand(format string, fields map[string]node) (string, error) {
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
			b.WriteByte(byte('0' + f.intn(10)))
		case '1':
			b.WriteByte(byte('1' + f.intn(9)))
		case 'A':
			b.WriteByte(byte('A' + f.intn(26)))
		case 'a':
			b.WriteByte(byte('a' + f.intn(26)))
		case '{':
			end := i + 1
			for end < len(rs) && rs[end] != '}' {
				end++
			}
			if end >= len(rs) {
				return "", fmt.Errorf("unterminated '{' in %q", format)
			}
			s, err := f.resolve(string(rs[i+1:end]), fields)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
			i = end
		default:
			b.WriteRune(c)
		}
	}
	return b.String(), nil
}

// resolve evaluates a "{token}" body: one or more field names separated by '|',
// of which one is chosen at random and rendered.
func (f *Fakes) resolve(token string, fields map[string]node) (string, error) {
	names := strings.Split(token, "|")
	name := names[f.intn(len(names))]
	child, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("token {%s}: no field %q", token, name)
	}
	return f.render(child)
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
	c := &choice{items: make([]node, len(items))}
	cum := make([]float64, len(items))
	var total float64
	weighted := false
	for i, raw := range items {
		w := weightOf(raw)
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
		c.cum = cum
	}
	return c, nil
}

func compileTemplate(m map[string]any) (node, error) {
	format, ok := m["format"].(string)
	if !ok {
		return nil, fmt.Errorf("template object missing string \"format\"")
	}
	t := &template{format: format, fields: make(map[string]node, len(m))}
	for k, v := range m {
		if k == "format" || k == "weight" {
			continue
		}
		n, err := compile(v)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		t.fields[k] = n
	}
	return t, nil
}

// weightOf reads a node's "weight" (default 1) from its raw JSON form.
func weightOf(raw any) float64 {
	if m, ok := raw.(map[string]any); ok {
		if w, ok := m["weight"].(float64); ok {
			return w
		}
	}
	return 1
}
