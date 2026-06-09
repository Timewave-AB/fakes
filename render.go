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

// descend walks named fields, resolving choices it meets along the way. It is
// the one render-side step that can fail, because the path comes from the
// caller and may name a field that does not exist.
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
		return descend(s, pick(s, n), segments) // a choice consumes no path segment
	default:
		return nil, fmt.Errorf("cannot descend into %T at %q", n, segments[0])
	}
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
			return expand(s, n.format, n.fields)
		}
		var b strings.Builder
		for i := 0; i < n.repeat; i++ {
			if i > 0 {
				b.WriteString(n.separator)
			}
			b.WriteString(expand(s, n.format, n.fields))
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

// expand renders a format string: literals verbatim, class chars randomised, a
// {name(args)} token via its builtin, any other {token} via resolve. checkTokens
// validated every token at compile time, so the scan cannot fail here (the
// eachToken error is unreachable and ignored).
func expand(s *session, format string, fields map[string]node) string {
	var b strings.Builder
	_ = eachToken(format, func(t ftoken) error {
		switch t.kind {
		case 'l':
			b.WriteRune(t.r)
		case 'c':
			b.WriteByte(classChar(s, t.r))
		case 'b':
			if name, args, ok := funcCall(t.body); ok {
				b.WriteString(builtins[name].call(s, b.String(), fields, args)) // b.String() is the output so far
			} else {
				b.WriteString(resolve(s, t.body, fields))
			}
		}
		return nil
	})
	return b.String()
}

// resolve renders a "{token}" body: one or more names separated by '|', one
// picked at random. A name is a sibling field or a {..path} reference, which
// linkRefs bound into fields too; checkTokens and linkRefs guarantee both exist.
func resolve(s *session, token string, fields map[string]node) string {
	names := strings.Split(token, "|")
	return render(s, fields[names[s.IntN(len(names))]])
}
