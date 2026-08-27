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

// literal is emitted verbatim, never formatted.
type literal string

func (literal) isNode() {}

// group is a namespace of named children, built from a directory of JSON files
// and subdirectories. It has no value of its own: descend into a named child by
// dot path; rendering one is an error (see Fake).
type group struct{ children map[string]node }

func (*group) isNode() {}

// choice picks one of its items. cum holds cumulative weights for a weighted
// pick; when nil the choice is uniform and selection is O(1). shared is the set of
// relative dot paths every item can address, so descend and List both read the one
// answer to what a path may reach through this choice.
type choice struct {
	items  []node
	cum    []float64
	shared map[string]bool
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
	ops       []op // format compiled once (see compileOps); what expand walks
	grow      int  // minimum output size, to size the render buffer
}

func (*template) isNode() {}

// compile converts parsed JSON into a node tree, validating structure up front.
// Only a choice's items carry a weight, so one here would be inert whatever its type.
func compile(v any) (node, error) {
	if m, ok := v.(map[string]any); ok {
		if _, weighted := m["weight"]; weighted {
			return nil, fmt.Errorf("weight only skews a choice's items, so it has no effect here; it is an option and can never be a field")
		}
	}
	return compileItem(v)
}

// compileItem compiles one node, allowing the weight a choice item may carry.
func compileItem(v any) (node, error) {
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
		n, err := compileItem(raw)
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
	if len(c.items) > 1 {
		// Safe to precompute: a choice's items come from one file, so no group can
		// appear inside one, and neither mergeChildren nor linkRefs can reach in.
		c.shared = sharedPaths(c.items)
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
		if repeat == 1 {
			return nil, fmt.Errorf("separator joins repeated renders, so it has no effect without a repeat above 1")
		}
	}
	t := &template{format: format, fields: make(map[string]node, len(m)), repeat: repeat, separator: sep}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // so which of several bad fields is reported does not vary
	for _, k := range keys {
		if isOption(k) {
			continue
		}
		if isRef(k) {
			return nil, fmt.Errorf("field %q starts with %q, which is reserved for {..path} bindings", k, refPrefix)
		}
		n, err := compile(m[k])
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		t.fields[k] = n
	}
	if err := checkTokens(format, t.fields); err != nil {
		return nil, err
	}
	t.ops, t.grow = compileOps(format)
	return t, nil
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
	if r > maxLen { // cap so a fat-fingered repeat can't build a multi-GB string
		return 0, fmt.Errorf("repeat %v exceeds the maximum %d", rv, maxLen)
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

// checkName rejects a category or folder name no dot path can reach. A dot separates
// path segments, so such a name is unaddressable by every route — unlike a field,
// which its parent's format still reaches by token.
func checkName(name string) error {
	if strings.Contains(name, ".") {
		return fmt.Errorf("%q contains a dot, which a dot path cannot reach", name)
	}
	return nil
}

// isOption reports whether a template key configures the node instead of naming a
// field. These four names can never be fields.
func isOption(name string) bool {
	switch name {
	case "format", "repeat", "separator", "weight":
		return true
	}
	return false
}
