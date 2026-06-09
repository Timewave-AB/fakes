package fakes

import "testing"

// TestRootReferenceAcrossFolders is the headline case: a category in one folder
// pulls a value from another via a {..path} reference resolved from the data root.
func TestRootReferenceAcrossFolders(t *testing.T) {
	dir := writeData(t, map[string]string{
		"en_US/person":   `["Pat Smith"]`,
		"sv_SE/greeting": `{"format":"Hej, {..en_US.person}!"}`,
	})
	f := newFakes(t, dir, WithSeed(1))
	if got := fake(t, f, "sv_SE.greeting"); got != "Hej, Pat Smith!" {
		t.Fatalf("greeting = %q, want \"Hej, Pat Smith!\"", got)
	}
}

// TestReferenceIntoAField reaches a field inside a referenced category, crossing
// a single-variant choice and then a template field (..who.last).
func TestReferenceIntoAField(t *testing.T) {
	dir := writeData(t, map[string]string{
		"who":  `[{"format":"{first} {last}","first":["Ada"],"last":["Byron"]}]`,
		"card": `{"format":"signed {..who.last}"}`,
	})
	f := newFakes(t, dir, WithSeed(1))
	if got := fake(t, f, "card"); got != "signed Byron" {
		t.Fatalf("card = %q, want \"signed Byron\"", got)
	}
}

// TestReferenceInAlternation lets a reference stand as one arm of a {a|..b}
// alternation, so a field and a cross-file value share one slot.
func TestReferenceInAlternation(t *testing.T) {
	dir := writeData(t, map[string]string{
		"far":  `["X"]`,
		"near": `{"format":"{here|..far}","here":["H"]}`,
	})
	f := newFakes(t, dir, WithSeed(2))
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		seen[fake(t, f, "near")] = true
	}
	if !seen["H"] || !seen["X"] || len(seen) != 2 {
		t.Fatalf("alternation produced %v, want both H and X", seen)
	}
}

// TestReferenceCombinesLoadedPaths is the point of references over the merge
// model: data layered from two dirs can point at each other through the root.
func TestReferenceCombinesLoadedPaths(t *testing.T) {
	a := writeData(t, map[string]string{"en_US/word": `["river"]`})
	b := writeData(t, map[string]string{"mine/slug": `{"format":"the-{..en_US.word}"}`})
	f := newFakesN(t, []string{a, b}, WithSeed(1))
	if got := fake(t, f, "mine.slug"); got != "the-river" {
		t.Fatalf("slug = %q, want the-river", got)
	}
}

// TestReferenceChain follows a reference to a node that is itself a reference, so
// linking order cannot matter.
func TestReferenceChain(t *testing.T) {
	dir := writeData(t, map[string]string{
		"a": `{"format":"{..b}"}`,
		"b": `{"format":"{..c}"}`,
		"c": `["deep"]`,
	})
	f := newFakes(t, dir, WithSeed(1))
	if got := fake(t, f, "a"); got != "deep" {
		t.Fatalf("a = %q, want deep", got)
	}
}

// TestReferenceErrors lists the references New must reject up front, so a bad path
// fails at load, never at a random render.
func TestReferenceErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"missing target": {"card": `{"format":"{..nope.gone}"}`},
		"folder target":  {"en_US/word": `["w"]`, "card": `{"format":"{..en_US}"}`},
		"multi-variant on the path": {
			"who":  `[{"format":"{f}","f":["1"]},{"format":"{f}","f":["2"]}]`,
			"card": `{"format":"{..who.f}"}`,
		},
		"empty reference path": {"card": `{"format":"{..}"}`},
		// A reference that leads back to its own value never terminates at render,
		// so New must reject the cycle up front (direct, mutual, or chained).
		"direct cycle": {"a": `{"format":"x{..a}"}`},
		"mutual cycle": {"a": `{"format":"{..b}"}`, "b": `{"format":"{..a}"}`},
		"chain cycle":  {"a": `{"format":"{..b}"}`, "b": `{"format":"{..c}"}`, "c": `{"format":"{..a}"}`},
		// calc renders its operands, so a cycle through one must be caught too.
		"calc operand cycle": {"x": `{"format":"{calc(y)}","y":[{"format":"{..x}"}]}`},
	}
	for name, files := range cases {
		if _, err := New([]string{writeData(t, files)}); err == nil {
			t.Errorf("%s: New = nil error, want a reference error", name)
		}
	}
}
