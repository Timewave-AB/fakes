package fakes

import (
	"fmt"
	"math"
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
