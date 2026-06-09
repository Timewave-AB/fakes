package fakes

import (
	"fmt"
	"strings"
)

// refPrefix marks a {..path} token: a reference to a node elsewhere in the data
// root rather than a sibling field. The path after it is the same dot path Fake
// takes, resolved across every loaded directory (see linkRefs).
const refPrefix = ".."

func isRef(name string) bool { return strings.HasPrefix(name, refPrefix) }

// linkRefs resolves every {..path} reference in the assembled tree, binding the
// target node into the referring template's fields under the token's key so the
// ordinary resolver renders it like a sibling. It runs once, after all data is
// merged, so a reference sees the final (override-resolved) tree. A path that is
// unknown, names a folder, or steps through a multi-variant choice fails here,
// keeping a bad reference a New-time error, never a random render-time one. The
// seen set skips shared nodes (and keeps this walk finite if data is cyclic);
// checkNoCycles, run next, is what rejects an actual reference cycle.
func linkRefs(root map[string]node) error {
	seen := map[node]bool{}
	var visit func(node) error
	visit = func(n node) error {
		if n == nil || seen[n] {
			return nil
		}
		seen[n] = true
		switch n := n.(type) {
		case *group:
			for _, c := range n.children {
				if err := visit(c); err != nil {
					return err
				}
			}
		case *choice:
			for _, it := range n.items {
				if err := visit(it); err != nil {
					return err
				}
			}
		case *template:
			for _, name := range refTokens(n.format) {
				target, err := lookup(root, strings.Split(name[len(refPrefix):], "."))
				if err != nil {
					return fmt.Errorf("reference {%s}: %w", name, err)
				}
				n.fields[name] = target
			}
			for _, c := range n.fields {
				if err := visit(c); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, c := range root {
		if err := visit(c); err != nil {
			return err
		}
	}
	return nil
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
			if c, ok := n.fields[name]; ok {
				es = append(es, renderEdge{c, name})
			}
		}
		return es
	default:
		return nil
	}
}

// checkNoCycles rejects a reference cycle: a node whose rendering can reach itself
// — directly, mutually, or through a chain — never terminates, so it must fail at
// New rather than stack-overflow at render. It is a depth-first walk of the render
// graph (renderEdges); grey marks nodes on the current path so a back-edge to one
// is the cycle, while black lets a shared node (a DAG, not a cycle) be skipped.
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
	for name, c := range root {
		if err := visit(c, name); err != nil {
			return err
		}
	}
	return nil
}
