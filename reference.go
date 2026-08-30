package fakes

import (
	"fmt"
	"sort"
	"strings"
)

// refPrefix marks a {..path} token: a reference to a node elsewhere in the data
// root rather than a sibling field. The path is resolved across every loaded
// directory (see linkRefs), and is stricter than the one Fake takes: a reference
// binds one node, so it cannot step through a multi-variant choice even where Fake
// and List can.
const refPrefix = ".."

func isRef(name string) bool { return strings.HasPrefix(name, refPrefix) }

// linkRefs resolves every {..path} reference in the assembled tree, binding the
// target node into the referring template's fields under the token's key so the
// ordinary resolver renders it like a sibling. It runs once, after all data is
// merged, so a reference sees the final (override-resolved) tree. A path that is
// unknown, names a folder, or steps through a multi-variant choice fails here,
// keeping a bad reference a New-time error, never a random render-time one.
func linkRefs(root map[string]node) error {
	return walkNodes(root, func(path string, n node) error {
		t, ok := n.(*template)
		if !ok {
			return nil
		}
		for _, name := range refTokens(t.format) {
			target, err := lookup(root, strings.Split(name[len(refPrefix):], "."))
			if err != nil {
				return fmt.Errorf("%s: reference {%s}: %w", path, name, err)
			}
			t.fields[name] = target
		}
		return nil
	})
}

// checkBoundLevelsHeld rejects every route to a bound level except the paths that
// read it. A path holds one draw of the level; anything else that renders it draws
// again, and the two disagree. checkNoOverlap settles the spellings within one
// format (a token, a calc operand); this settles the rest — a reference, whether it
// sits in that format or in anything the format renders, however deep.
//
// It runs after checkNoCycles, whose guarantee is what lets the walk terminate.
func checkBoundLevelsHeld(root map[string]node) error {
	return walkNodes(root, func(path string, n node) error {
		t, ok := n.(*template)
		if !ok || len(t.bound) == 0 {
			return nil
		}
		heads := make([]string, 0, len(t.bound))
		for head := range t.bound {
			heads = append(heads, head)
		}
		sort.Strings(heads) // so which overlap is reported does not vary
		for _, head := range heads {
			held := map[node]bool{}
			cover(t.fields[head], held)
			// One seen set across the edges: a node that cannot reach the level
			// cannot reach it by another route either, so it is walked once here.
			seen := map[node]bool{}
			for _, e := range renderEdges(t) {
				if splitArm(e.label).key == head {
					continue // a path token reading this level, which is the one route allowed
				}
				if renders(e.to, held, seen) {
					return fmt.Errorf("%s: {%s} renders %q, which {%s} reads a path into; name the fields you want instead", path, e.label, head, t.bound[head])
				}
			}
		}
		return nil
	})
}

// cover collects what one held draw of a level answers for: the level and
// everything contained in it, since a path may read any of it. Literals are left
// out — one fixed string cannot disagree with itself, and a literal is a value, so
// two that spell the same text are indistinguishable.
func cover(n node, into map[node]bool) {
	if _, fixed := n.(literal); fixed {
		return
	}
	into[n] = true
	for _, c := range contained(n) {
		cover(c.node, into)
	}
}

// renders reports whether rendering n can reach anything in want, following the
// same edges expand does. seen keeps a node shared by several routes from being
// walked twice; checkNoCycles has already proved the graph is a DAG, so the walk
// ends.
func renders(n node, want, seen map[node]bool) bool {
	if want[n] {
		return true
	}
	if seen[n] {
		return false
	}
	seen[n] = true
	for _, e := range renderEdges(n) {
		if renders(e.to, want, seen) {
			return true
		}
	}
	return false
}

// walkNodes calls fn once per contained node, passing the dot path that reaches it,
// visiting keys in sorted order so which of several broken nodes gets reported does
// not depend on map iteration.
func walkNodes(root map[string]node, fn func(path string, n node) error) error {
	seen := map[node]bool{}
	var visit func(string, node) error
	visit = func(path string, n node) error {
		if n == nil || seen[n] {
			return nil
		}
		seen[n] = true
		if err := fn(path, n); err != nil {
			return err
		}
		for _, c := range contained(n) {
			if err := visit(join(path, c.name), c.node); err != nil {
				return err
			}
		}
		return nil
	}
	for _, name := range sortedNames(root) {
		if err := visit(name, root[name]); err != nil {
			return err
		}
	}
	return nil
}

// namedNode is a contained child and the segment reaching it; a choice's items carry
// no segment, matching how a dot path steps over a choice.
type namedNode struct {
	name string
	node node
}

func contained(n node) []namedNode {
	switch n := n.(type) {
	case *group:
		return authored(n.children)
	case *choice:
		out := make([]namedNode, len(n.items))
		for i, it := range n.items {
			out[i] = namedNode{node: it}
		}
		return out
	case *template:
		return named(n.fields)
	default:
		return nil
	}
}

// named skips a bound {..path} key: it is a render edge, not containment, so using
// it as a path segment would report a node under a path that does not reach it. Only
// a template's fields hold bindings, so group children go through authored.
func named(m map[string]node) []namedNode {
	out := make([]namedNode, 0, len(m))
	for _, name := range sortedNames(m) {
		if isRef(name) {
			continue
		}
		out = append(out, namedNode{name: name, node: m[name]})
	}
	return out
}

func authored(m map[string]node) []namedNode {
	out := make([]namedNode, 0, len(m))
	for _, name := range sortedNames(m) {
		out = append(out, namedNode{name: name, node: m[name]})
	}
	return out
}

func sortedNames(m map[string]node) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// lookup finds the single node a reference path names, walking groups and
// template fields by segment and descending a single-variant choice as a
// transparent wrapper. A missing segment, a folder target, or a step through a
// multi-variant choice (which has no one value to bind) is an error.
func lookup(root map[string]node, segments []string) (node, error) {
	var n node = &group{children: root}
	for i := 0; i < len(segments); i++ {
		switch c := n.(type) {
		case *group:
			child, ok := c.children[segments[i]]
			if !ok {
				return nil, fmt.Errorf("no entry %q", segments[i])
			}
			n = child
		case *template:
			child, ok := c.fields[segments[i]]
			if !ok {
				return nil, fmt.Errorf("no field %q", segments[i])
			}
			n = child
		case *choice:
			if len(c.items) != 1 {
				return nil, fmt.Errorf("%q steps through a %d-way choice", segments[i], len(c.items))
			}
			n, i = c.items[0], i-1 // a choice consumes no segment; reprocess it unwrapped
		default:
			return nil, fmt.Errorf("cannot descend into %T at %q", n, segments[i])
		}
	}
	if _, ok := n.(*group); ok {
		return nil, fmt.Errorf("names a folder, not a value")
	}
	return n, nil
}

// refTokens returns just the {..path} reference names among a format's field
// tokens (linkRefs binds each into the template's fields).
func refTokens(format string) []string {
	var refs []string
	for _, name := range fieldTokens(format) {
		if isRef(name) {
			refs = append(refs, name)
		}
	}
	return refs
}

// renderEdge is a child a node renders into, labelled by the token that reaches it
// (a field name, reference, or choice index) for a readable cycle report.
type renderEdge struct {
	to    node
	label string
}

// renderEdges lists the children rendering n recurses into, mirroring expand: a
// choice's items, and a template's field/reference tokens plus its calc operands.
// A literal or group renders nothing, so it has no edges.
func renderEdges(n node) []renderEdge {
	switch n := n.(type) {
	case *choice:
		es := make([]renderEdge, len(n.items))
		for i, it := range n.items {
			es[i] = renderEdge{it, fmt.Sprintf("[%d]", i)}
		}
		return es
	case *template:
		var es []renderEdge
		for _, name := range append(fieldTokens(n.format), calcOperands(n.format)...) {
			a := splitArm(name)
			c, ok := n.fields[a.key]
			if !ok {
				continue
			}
			for _, leaf := range pathLeaves(c, a.tail) {
				es = append(es, renderEdge{leaf, name})
			}
		}
		return es
	default:
		return nil
	}
}

// pathLeaves lists what a token's dotted tail renders. A path draws the levels it
// passes through but renders only what it lands on, so the leaf is the edge — a
// bare token, whose tail is empty, lands on the field itself. A choice on the way
// contributes every variant, since any of them may be the one drawn. checkPath has
// already proved the tail resolves in every variant, so the walk drops nothing.
func pathLeaves(n node, tail []string) []node {
	if len(tail) == 0 {
		return []node{n}
	}
	if c, ok := n.(*choice); ok {
		var out []node
		for _, it := range c.items {
			out = append(out, pathLeaves(it, tail)...)
		}
		return out
	}
	return pathLeaves(child(n, tail[0]), tail[1:])
}

// checkNoCycles rejects a reference cycle: a node whose rendering can reach itself
// — directly, mutually, or through a chain — never terminates, so it must fail at
// New rather than stack-overflow at render. It is a depth-first walk of the render
// graph (renderEdges); grey marks nodes on the current path so a back-edge to one
// is the cycle, while black lets a shared node (a DAG, not a cycle) be skipped.
// Every node is a root: a field its parent's format never renders is still reachable
// by dot path, so a cycle in one would otherwise reach render and be fatal there.
func checkNoCycles(root map[string]node) error {
	const (
		grey  = 1
		black = 2
	)
	color := map[node]int{}
	var visit func(n node, path string) error
	visit = func(n node, path string) error {
		switch color[n] {
		case grey:
			return fmt.Errorf("reference cycle: %s", path)
		case black:
			return nil
		}
		color[n] = grey
		for _, e := range renderEdges(n) {
			if err := visit(e.to, path+" -> "+e.label); err != nil {
				return err
			}
		}
		color[n] = black
		return nil
	}
	return walkNodes(root, func(path string, n node) error { return visit(n, path) })
}
