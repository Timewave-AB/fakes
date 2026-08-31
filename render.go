package fakes

import (
	"fmt"
	"sort"
	"strings"
)

// rng is the randomness the renderer draws from. Passing it in keeps the render
// functions a pure core over an explicit effect; *rand.Rand satisfies it.
type rng interface {
	IntN(n int) int
	Float64() float64
}

// Fake generates a value for a dot path. Each segment descends one level: folder
// names and the category (JSON file) come first, then named fields within it,
// e.g. "sv_SE.address" or "sv_SE.address.street". Choices along the way are
// resolved at random. A path naming a folder (no value of its own) is an error.
func (f *Fakes) Fake(path string) (string, error) {
	n, err := descend(f.rand, &group{children: f.categories}, strings.Split(path, "."))
	if err != nil {
		return "", fmt.Errorf("fakes: %s: %w", path, err)
	}
	if _, ok := n.(*group); ok {
		return "", fmt.Errorf("fakes: %s names a folder, not a value", path)
	}
	return render(f.rand, n), nil
}

// descend walks named fields to the node a path names. It is the one render-side
// step that can fail, because the path comes from the caller and may name a field
// that does not exist. A choice consumes no segment, so the rest of the path must
// be one every variant carries (the set compile stored) before a variant is picked
// — a path that resolves at all resolves on every call.
func descend(s *session, n node, segments []string) (node, error) {
	if len(segments) == 0 {
		return n, nil
	}
	switch n := n.(type) {
	case *group:
		child, ok := n.children[segments[0]]
		if !ok {
			return nil, fmt.Errorf("no entry %q", segments[0])
		}
		return descend(s, child, segments[1:])
	case *template:
		child, ok := n.fields[segments[0]]
		if !ok {
			return nil, fmt.Errorf("no field %q", segments[0])
		}
		return descend(s, child, segments[1:])
	case *choice:
		if len(n.items) > 1 {
			if want := strings.Join(segments, "."); !n.shared[want] {
				return nil, unreachableInChoice(n, want)
			}
		}
		return descend(s, pick(s, n), segments)
	case literal:
		return nil, fmt.Errorf("a plain string has no field %q", segments[0])
	default:
		return nil, fmt.Errorf("cannot descend into %T at %q", n, segments[0])
	}
}

// unreachableInChoice reports that a path cannot step through this choice, listing
// what every variant does carry. It reads the precomputed set, so a failing path
// costs no more than a rendering one.
func unreachableInChoice(c *choice, want string) error {
	if len(c.shared) == 0 {
		return fmt.Errorf("no variant of this %d-way choice carries %q", len(c.items), want)
	}
	offered := make([]string, 0, len(c.shared))
	for p := range c.shared {
		offered = append(offered, p)
	}
	sort.Strings(offered)
	return fmt.Errorf("not every variant of this %d-way choice carries %q; all carry %v", len(c.items), want, offered)
}

// render evaluates a compiled node to a string. compile validates every node up
// front, so rendering a compiled tree cannot fail.
func render(s *session, n node) string {
	switch n := n.(type) {
	case literal:
		return string(n)
	case *choice:
		return render(s, pick(s, n))
	case *template:
		if n.repeat == 1 {
			return expand(s, n)
		}
		var b strings.Builder
		b.Grow(n.repeat * (n.grow + len(n.separator)))
		for i := 0; i < n.repeat; i++ {
			if i > 0 {
				b.WriteString(n.separator)
			}
			b.WriteString(expand(s, n))
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

// classChar randomises one character-class rune: '0' digit 0-9, '1' digit 1-9,
// 'A' letter A-Z, 'a' letter a-z.
func classChar(s *session, c rune) byte {
	switch c {
	case '0':
		return byte('0' + s.IntN(10))
	case '1':
		return byte('1' + s.IntN(9))
	case 'A':
		return byte('A' + s.IntN(26))
	default: // 'a'
		return byte('a' + s.IntN(26))
	}
}

// expand renders a template's compiled ops. compile validated every token, so this
// cannot fail.
func expand(s *session, t *template) string {
	var b strings.Builder
	b.Grow(t.grow)
	// One draw per held name, for this expansion only: a nested template and each
	// repeat iteration get their own, since each is its own expansion.
	var held *draws
	if len(t.held) > 0 {
		held = &draws{
			variant: make(map[string]node, len(t.held)),
			value:   make(map[string]string, len(t.held)),
		}
	}
	for i := range t.ops {
		o := &t.ops[i]
		switch o.kind {
		case 'l':
			b.WriteString(o.lit)
		case 'c':
			b.WriteByte(classChar(s, o.r))
		case 'f':
			b.WriteString(readField(s, t, held, o.arms[s.IntN(len(o.arms))]))
		case 'b':
			// Read in the order calcVars fixed, which is what op.operands holds.
			var operands []string
			if len(o.operands) > 0 {
				operands = make([]string, len(o.operands))
				for j, name := range o.operands {
					operands[j] = readField(s, t, held, arm{name: name, key: name})
				}
			}
			b.WriteString(o.call(s, b.String(), operands)) // b.String() is the output so far
		}
	}
	return b.String()
}

// draws is what an expansion has already drawn for its held names: the variant each
// was drawn as, so every path under it reads one row, and the value each read, so
// the same name read twice reads one value.
type draws struct {
	variant map[string]node
	value   map[string]string
}

// readField renders one arm of a token. An arm's key is a sibling field or a
// {..path} reference, which linkRefs bound into fields too. A name the expansion
// holds — a level some token addresses by dotted path, or a sibling a {calc()}
// reads — is drawn once and kept, so {place.postal-code} and {place.locality} read
// one row, either read twice gives one value, and a shown operand is the operand
// computed. Every other name is drawn afresh, so {word} {word} still draws twice.
// checkTokens, checkPath and linkRefs prove every step, so this cannot fail.
func readField(s *session, t *template, held *draws, a arm) string {
	if !t.held[a.key] {
		return render(s, t.fields[a.key])
	}
	if v, read := held.value[a.name]; read {
		return v
	}
	n, drew := held.variant[a.key]
	if !drew {
		n = drawn(s, t.fields[a.key])
		held.variant[a.key] = n
	}
	// Hold the draw at every level passed through, so two paths sharing a prefix
	// share it.
	for i, seg := range a.tail {
		if i < len(a.steps) {
			step, drew := held.variant[a.steps[i]]
			if !drew {
				step = drawn(s, child(n, seg))
				held.variant[a.steps[i]] = step
			}
			n = step
			continue
		}
		n = child(n, seg)
	}
	v := render(s, n)
	held.value[a.name] = v
	return v
}

// child is the node one path segment names below an already-drawn node. It holds
// while checkPath and the set a choice shares (see sharedPaths) agree with this
// walk: both prove the segment exists and that drawn leaves a template here. Each
// way that can break panics naming the segment, so a slip in that agreement
// reports where it happened rather than surfacing a nil node a level later.
func child(n node, seg string) node {
	t, ok := n.(*template)
	if !ok {
		panic(fmt.Sprintf("fakes: %q under %T, which carries no fields", seg, n))
	}
	c, ok := t.fields[seg]
	if !ok {
		panic(fmt.Sprintf("fakes: no field %q under a drawn level", seg))
	}
	return c
}

// drawn resolves a choice to one variant, so a bound head is a concrete node the
// rest of the expansion shares. Nested choices unwrap too: a draw is one value, not
// another set to pick from.
func drawn(s *session, n node) node {
	for c, ok := n.(*choice); ok; c, ok = n.(*choice) {
		n = pick(s, c)
	}
	return n
}
