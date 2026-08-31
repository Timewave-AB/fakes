package fakes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// --- format string edge cases ---

func TestEscapeEdgeCases(t *testing.T) {
	f := engine(1)
	cases := map[string]string{
		`{"format":""}`:         "",     // empty format
		`{"format":"#"}`:        "#",    // trailing escape is a literal #
		`{"format":"##"}`:       "#",    // escaped hash
		`{"format":"#0#1#A#a"}`: "01Aa", // escaped class chars stay literal
		`{"format":"#{x#}"}`:    "{x}",  // escaping braces disables tokens
		`{"format":"x}y"}`:      "x}y",  // an unmatched } is literal (x, y aren't classes)
	}
	for tmpl, want := range cases {
		if got := mustRender(t, f, tmpl); got != want {
			t.Errorf("render(%s) = %q, want %q", tmpl, got, want)
		}
	}
}

func TestMultibyteFormat(t *testing.T) {
	// Scanning is rune-aware: multibyte literals coexist with class chars and
	// tokens without corrupting indices.
	got := mustRender(t, engine(2), `{"format":"Öster{x}-0å","x":["väg"]}`)
	if !regexp.MustCompile(`^Österväg-[0-9]å$`).MatchString(got) {
		t.Fatalf("multibyte format = %q", got)
	}
}

func TestAlternationThreeWay(t *testing.T) {
	f := engine(4)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		seen[mustRender(t, f, `{"format":"{a|b|c}","a":["A"],"b":["B"],"c":["C"]}`)] = true
	}
	if !seen["A"] || !seen["B"] || !seen["C"] || len(seen) != 3 {
		t.Fatalf("3-way alternation produced %v, want A, B and C", seen)
	}
}

func TestNewErrors(t *testing.T) {
	// Pointing New at a file (not a directory) fails.
	file := filepath.Join(t.TempDir(), "xx_XX")
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New([]string{file}); err == nil {
		t.Error("New(file) = nil error, want not-a-directory error")
	}
	// Invalid JSON in a category file fails.
	if _, err := New([]string{writeData(t, map[string]string{"broken": `{ not json`})}); err == nil {
		t.Error("New(invalid JSON) = nil error")
	}
	// An option that cannot take effect, and a category or folder no dot path can
	// reach, are mistakes New must name rather than accept and ignore.
	rejected := map[string]struct {
		files map[string]string
		want  string
	}{
		"separator without repeat": {
			map[string]string{"a": `{"format":"{x}","x":["1"],"separator":","}`},
			"has no effect without a repeat above 1",
		},
		"separator with an explicit repeat of 1": {
			map[string]string{"a": `{"format":"{x}","x":["1"],"repeat":1,"separator":","}`},
			"has no effect without a repeat above 1",
		},
		"weight outside a choice": {
			map[string]string{"a": `{"format":"x","weight":5}`},
			"weight only skews a choice's items",
		},
		"non-numeric weight outside a choice": {
			map[string]string{"a": `{"format":"x","weight":"bad"}`},
			"weight only skews a choice's items",
		},
		"weight used as a field": {
			map[string]string{"a": `{"format":"{name} {weight}kg","name":["Anvil"],"weight":["7"]}`},
			"can never be a field",
		},
		"an option name used as a token": {
			map[string]string{"a": `{"format":"{weight}"}`},
			`"weight" is an option and can never be a field`,
		},
		"category name with a dot": {
			map[string]string{"a.b": `["1"]`},
			`category "a.b" contains "."`,
		},
		"folder name with a dot": {
			map[string]string{"a.b/cat": `["1"]`},
			`/a.b: folder "a.b" contains "."`,
		},
		// The token grammar reserves three more characters. A name carrying one
		// still resolves by dot path, but no format can name it, so it is rejected
		// where it is authored rather than at the token that cannot reach it.
		"field name with a pipe": {
			map[string]string{"a": `{"format":"{x}","x":["1"],"b|c":["2"]}`},
			`field "b|c" contains "|"`,
		},
		"field name with a paren": {
			map[string]string{"a": `{"format":"{x}","x":["1"],"b(c":["2"]}`},
			`field "b(c" contains "("`,
		},
		"field name with a closing brace": {
			map[string]string{"a": `{"format":"{x}","x":["1"],"b}c":["2"]}`},
			`field "b}c" contains "}"`,
		},
		"category name with a pipe": {
			map[string]string{"a|b": `["1"]`},
			`category "a|b" contains "|"`,
		},
		// An empty name is reachable by no dot path, so List cannot advertise it and
		// Fake cannot ask for it — {} is the one spelling that reaches it, which is
		// the same trap {a.} is already rejected for.
		"empty field name": {
			map[string]string{"a": `{"format":"[{}]","":"VALUE"}`},
			`field "" is empty`,
		},
		"folder name with a paren": {
			map[string]string{"a(b/cat": `["1"]`},
			`folder "a(b" contains "("`,
		},
		// A repeated arm skews an alternation, which weight is the spelling for.
		"repeated alternation arm": {
			map[string]string{"a": `{"format":"{x|x}","x":["1"]}`},
			`arm "x" is repeated`,
		},
		"repeated arm among others": {
			map[string]string{"a": `{"format":"{x|y|x}","x":["1"],"y":["2"]}`},
			`arm "x" is repeated`,
		},
		"repeated path arm": {
			map[string]string{"a": `{"format":"{p.v|p.v}","p":{"format":"{v}","v":["1"]}}`},
			`arm "p.v" is repeated`,
		},
		// A reference arm is the only kind that reaches the repeat check by passing
		// the per-arm checks rather than falling through them.
		"repeated reference arm": {
			map[string]string{"a": `["x"]`, "b": `{"format":"{..a|..a}"}`},
			`arm "..a" is repeated`,
		},
		// An arm that is broken on its own terms is reported as that, not as a
		// repeat: the repeat is a consequence of the real mistake.
		"repeated arm with no path": {
			map[string]string{"a": `{"format":"{..|..}"}`},
			"reference has no path",
		},
		"repeated empty arm": {
			map[string]string{"a": `{"format":"{|}"}`},
			`no field ""`,
		},
	}
	for name, c := range rejected {
		_, err := New([]string{writeData(t, c.files)})
		if err == nil {
			t.Errorf("%s: New = nil error, want it rejected at load", name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: New = %v, want it to mention %q", name, err, c.want)
		}
	}
	// Data that works today must keep working, and stay reachable.
	accepted := map[string]struct {
		files map[string]string
		path  string
		want  string
	}{
		"option name as a field":     {map[string]string{"a": `{"format":"{name} {Weight}kg","name":["Anvil"],"Weight":["7"]}`}, "a", "Anvil 7kg"},
		"format spelling as a field": {map[string]string{"a": `{"format":"{Format}","Format":["PDF"]}`}, "a", "PDF"},
		"hyphenated field":           {map[string]string{"a": `{"format":"{x-y}","x-y":["1"]}`}, "a.x-y", "1"},
		"category named Format":      {map[string]string{"Format": `["1"]`}, "Format", "1"},
		"folder named Repeat":        {map[string]string{"Repeat/cat": `["1"]`}, "Repeat.cat", "1"},
		"field with a closing paren": {map[string]string{"a": `{"format":"{b)c}","b)c":["2"]}`}, "a", "2"},
		"repeat without a separator": {map[string]string{"a": `{"format":"{x}","repeat":3,"x":["1"]}`}, "a", "111"},
		// One name in two separate tokens is two independent draws, not a repeated
		// arm; only a repeat within one alternation is rejected.
		"one name in two tokens": {map[string]string{"a": `{"format":"{x}{x}","x":["1"]}`}, "a", "11"},
	}
	for name, c := range accepted {
		f, err := New([]string{writeData(t, c.files)})
		if err != nil {
			t.Errorf("%s: New = %v, want it accepted", name, err)
			continue
		}
		if got, err := f.Fake(c.path); err != nil || got != c.want {
			t.Errorf("%s: Fake(%q) = %q, %v, want %q", name, c.path, got, err, c.want)
		}
	}
}

// --- deep path navigation ---

func TestDeepDottedPath(t *testing.T) {
	// A 5-segment path descends through alternating object/array nodes; choices
	// on the path are single-variant, so it resolves deterministically.
	f := engine(1)
	f.categories = map[string]node{
		"deep": compiled(t, `[{"format":"{a}","a":[{"format":"{b}","b":[{"format":"{c}","c":[{"format":"{d}","d":["leaf"]}]}]}]}]`),
	}
	if got, err := f.Fake("deep.a.b.c.d"); err != nil || got != "leaf" {
		t.Fatalf("Fake(deep.a.b.c.d) = %q, %v, want leaf", got, err)
	}
	// Rendering the whole tree resolves the same chain.
	if got, err := f.Fake("deep"); err != nil || got != "leaf" {
		t.Fatalf("Fake(deep) = %q, %v, want leaf", got, err)
	}
}

func TestDescendIntoLiteralErrors(t *testing.T) {
	f := engine(1)
	f.categories = map[string]node{"greeting": compiled(t, `["hej"]`)}
	if _, err := f.Fake("greeting.extra"); err == nil {
		t.Fatal("Fake(greeting.extra) = nil error, want descend-into-literal error")
	}
}

// TestPathThroughChoice pins the rule that keeps a dotted path from rendering on
// one call and failing on the next: every variant must carry the rest of the path.
func TestPathThroughChoice(t *testing.T) {
	dir := writeData(t, map[string]string{
		"every":  `[{"format":"{f}","f":["1"]},{"format":"{f}","f":["2"]}]`,
		"notall": `[{"format":"{f}","f":["1"]},["plain"]]`,
		"some":   `[{"format":"{f}","f":["1"]},{"format":"{f}","f":["2"],"extra":["x"]}]`,
	})
	f := newFakes(t, dir, WithSeed(1))
	for i := 0; i < 200; i++ {
		if got := fake(t, f, "every.f"); got != "1" && got != "2" {
			t.Fatalf("every.f = %q, want 1 or 2", got)
		}
	}
	// A path only some variants carry is reported against what all of them carry.
	if _, err := f.Fake("some.extra"); err == nil || !strings.Contains(err.Error(), "all carry [f]") {
		t.Errorf("Fake(some.extra) = %v, want it to name what every variant carries", err)
	}
	var first string
	for i := 0; i < 200; i++ {
		_, err := f.Fake("notall.f")
		if err == nil {
			t.Fatal("notall.f = nil error, want the same failure every call")
		}
		if i == 0 {
			first = err.Error()
		} else if err.Error() != first {
			t.Fatalf("notall.f error varies between calls:\n  %s\n  %s", first, err.Error())
		}
	}
}

// TestPathKeyIsUnambiguous pins that the one shape which could collide cannot be
// written: a field literally named "a.b" and a field "a" holding "b" would both
// spell "a.b", so a dotted field name is rejected at New and a dot means a path
// wherever it appears.
func TestPathKeyIsUnambiguous(t *testing.T) {
	dir := writeData(t, map[string]string{
		"cat": `[{"format":"{a.b}","a.b":["1"]},{"format":"{a}","a":{"format":"{b}","b":["2"]}}]`,
	})
	_, err := New([]string{dir})
	if err == nil || !strings.Contains(err.Error(), `field "a.b" contains "."`) {
		t.Fatalf("New = %v, want the dotted field name rejected", err)
	}
	// The same data without the dotted key is fine, and the path resolves.
	f := newFakes(t, writeData(t, map[string]string{
		"cat": `{"format":"{a.b}","a":{"format":"{b}","b":["2"]}}`,
	}), WithSeed(1))
	if !slices.Contains(f.List(), "cat.a.b") {
		t.Error("List() omits cat.a.b, which the data carries")
	}
	if got := fake(t, f, "cat"); got != "2" {
		t.Fatalf("cat = %q, want 2", got)
	}
}

// TestMissingFieldNamesItself keeps the precise diagnosis for the ordinary typo: a
// single-variant choice always picks the same item, so it needs no every-variant
// guard and the error can name the field that is missing.
func TestMissingFieldNamesItself(t *testing.T) {
	f := newFakes(t, "data/sv_SE", WithSeed(1))
	_, err := f.Fake("person.typo")
	if err == nil || !strings.Contains(err.Error(), `no field "typo"`) {
		t.Errorf("Fake(person.typo) = %v, want it to name the missing field", err)
	}
}

// --- category root shapes ---

func TestCategoryRootShapes(t *testing.T) {
	dir := writeData(t, map[string]string{
		"obj": `{"format":"00"}`, // object root
		"lit": `"hello"`,         // bare-string root
	})
	f := newFakes(t, dir, WithSeed(1))
	if got := fake(t, f, "obj"); !regexp.MustCompile(`^\d\d$`).MatchString(got) {
		t.Errorf("object-root category = %q, want two digits", got)
	}
	if got := fake(t, f, "lit"); got != "hello" {
		t.Errorf("string-root category = %q, want hello", got)
	}
}

// --- very long lists ---

func TestLongStringList(t *testing.T) {
	const n = 2000
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("name-%04d", i)
	}
	list, err := json.Marshal(names)
	if err != nil {
		t.Fatal(err)
	}
	f := newFakes(t, writeData(t, map[string]string{"name": string(list)}), WithSeed(1))

	valid := map[string]bool{}
	for _, v := range names {
		valid[v] = true
	}
	seen := map[string]bool{}
	for i := 0; i < 20000; i++ {
		v := fake(t, f, "name")
		if !valid[v] {
			t.Fatalf("got %q, not in the list", v)
		}
		seen[v] = true
	}
	if len(seen) < n*8/10 {
		t.Fatalf("only %d/%d distinct values seen; selection looks skewed", len(seen), n)
	}
}

// --- composition against the shipped sv_SE data ---

// swedishName matches one or more letter-words, optionally space/hyphen joined
// ("Storgatan", "Norra Promenaden", "von Flemming"). Used by the composition
// tests so shipped name lists can grow without re-enumerating them here.
var swedishName = regexp.MustCompile(`^\p{L}+([ -]\p{L}+)*$`)

func TestShippedStreetComposition(t *testing.T) {
	// street is a choice of composed {first}{last} templates and literal names.
	f := newFakes(t, "data/sv_SE", WithSeed(5))
	for i := 0; i < 300; i++ {
		if s := fake(t, f, "address.street"); !swedishName.MatchString(s) {
			t.Fatalf("street %q is not a Swedish street name", s)
		}
	}
}

func TestShippedLastNameComposition(t *testing.T) {
	// last is a choice of patronymic {first}sson templates, compound
	// {first}{last} templates and literal surnames.
	f := newFakes(t, "data/sv_SE", WithSeed(6))
	for i := 0; i < 300; i++ {
		if s := fake(t, f, "person.last"); !swedishName.MatchString(s) {
			t.Fatalf("last name %q is not a Swedish surname", s)
		}
	}
}

func TestShippedStreetNumberFormats(t *testing.T) {
	// Reachable via a hyphenated path; covers all five weighted number variants.
	f := newFakes(t, "data/sv_SE", WithSeed(8))
	re := regexp.MustCompile(`^[1-9]\d{0,2}[A-Z]?$`)
	for i := 0; i < 300; i++ {
		if n := fake(t, f, "address.street-number"); !re.MatchString(n) {
			t.Fatalf("street-number %q does not match %s", n, re)
		}
	}
}
