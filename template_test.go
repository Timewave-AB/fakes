package fakes

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// engine builds a seeded faker with no loaded categories, for rendering tests.
func engine(seed uint64) *Fakes { return &Fakes{rand: newRand(seed, true)} }

// parse unmarshals a JSON template fragment into its dynamic form.
func parse(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

// compiled parses and compiles a JSON fragment into a node.
func compiled(t *testing.T, s string) node {
	t.Helper()
	n, err := compile(parse(t, s))
	if err != nil {
		t.Fatalf("compile %q: %v", s, err)
	}
	return n
}

func render(t *testing.T, f *Fakes, s string) string {
	t.Helper()
	out, err := f.render(compiled(t, s))
	if err != nil {
		t.Fatalf("render %q: %v", s, err)
	}
	return out
}

func TestLiteralStringIsVerbatim(t *testing.T) {
	// A bare string is a literal, never formatted: 'a'/'A' must survive.
	if got := render(t, engine(1), `"Malmö"`); got != "Malmö" {
		t.Fatalf("literal = %q, want Malmö", got)
	}
}

func TestCharacterClasses(t *testing.T) {
	cases := map[string]*regexp.Regexp{
		`{"format":"0"}`: regexp.MustCompile(`^[0-9]$`),
		`{"format":"1"}`: regexp.MustCompile(`^[1-9]$`),
		`{"format":"A"}`: regexp.MustCompile(`^[A-Z]$`),
		`{"format":"a"}`: regexp.MustCompile(`^[a-z]$`),
	}
	f := engine(7)
	for tmpl, re := range cases {
		for i := 0; i < 100; i++ {
			if got := render(t, f, tmpl); !re.MatchString(got) {
				t.Fatalf("%s produced %q, want %s", tmpl, got, re)
			}
		}
	}
}

func TestEscapeAndLiteralChars(t *testing.T) {
	// '#' escapes the next char; non-class chars (7, x, -) are literal.
	if got := render(t, engine(1), `{"format":"#0#1#A#a## x7-z"}`); got != "01Aa# x7-z" {
		t.Fatalf("escape = %q, want \"01Aa# x7-z\"", got)
	}
}

func TestTokenSubstitution(t *testing.T) {
	if got := render(t, engine(1), `{"format":"{x}sson","x":["Erik"]}`); got != "Eriksson" {
		t.Fatalf("token = %q, want Eriksson", got)
	}
}

func TestAlternationPicksOneField(t *testing.T) {
	f := engine(3)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		seen[render(t, f, `{"format":"{a|b}","a":["A"],"b":["B"]}`)] = true
	}
	if !seen["A"] || !seen["B"] || len(seen) != 2 {
		t.Fatalf("alternation produced %v, want both A and B", seen)
	}
}

func TestWeightZeroNeverChosen(t *testing.T) {
	f := engine(5)
	for i := 0; i < 200; i++ {
		if got := render(t, f, `[{"format":"X","weight":0},"Y"]`); got != "Y" {
			t.Fatalf("weight-0 variant chosen: %q", got)
		}
	}
}

func TestWeightSkewsDistribution(t *testing.T) {
	f := engine(9)
	heavy := 0
	for i := 0; i < 1000; i++ {
		if render(t, f, `[{"format":"H","weight":10},{"format":"L"}]`) == "H" {
			heavy++
		}
	}
	if heavy < 800 { // expected ~909
		t.Fatalf("heavy variant chosen %d/1000, want a clear majority", heavy)
	}
}

func TestRecursionHasNoDepthLimit(t *testing.T) {
	// Build {format:{a}, a:[{format:{a}, a:[ ... "deep" ]]}} 50 levels deep.
	tmpl := `"deep"`
	for i := 0; i < 50; i++ {
		tmpl = `{"format":"{a}","a":[` + tmpl + `]}`
	}
	if got := render(t, engine(1), tmpl); got != "deep" {
		t.Fatalf("deep recursion = %q, want deep", got)
	}
}

func TestCompileErrors(t *testing.T) {
	// Structural problems are caught up front, at compile/New time.
	for _, bad := range []string{
		`{"x":["Q"]}`,            // object without "format"
		`{"format":"{y}","x":1}`, // a field is a bare number
		`[1, 2]`,                 // a choice of numbers
		`5`,                      // unsupported node type
	} {
		if _, err := compile(parse(t, bad)); err == nil {
			t.Errorf("compile(%s) = nil error, want error", bad)
		}
	}
}

func TestRenderErrors(t *testing.T) {
	// These compile, but fail when rendered.
	f := engine(1)
	for _, bad := range []string{
		`{"format":"{x"}`,            // unterminated brace
		`{"format":"{y}","x":["Q"]}`, // token names a missing field
		`[]`,                         // empty choice
	} {
		if _, err := f.render(compiled(t, bad)); err == nil {
			t.Errorf("render(%s) = nil error, want error", bad)
		}
	}
}

func TestFakePathNavigation(t *testing.T) {
	f := engine(1)
	f.categories = map[string]node{
		"addr": compiled(t, `[{"format":"{street}","street":["Main"]}]`),
	}
	if got, err := f.Fake("addr"); err != nil || !strings.Contains(got, "Main") {
		t.Fatalf("Fake(addr) = %q, %v", got, err)
	}
	if got, err := f.Fake("addr.street"); err != nil || got != "Main" {
		t.Fatalf("Fake(addr.street) = %q, %v, want Main", got, err)
	}
	if _, err := f.Fake("addr.nope"); err == nil {
		t.Error("Fake(addr.nope) = nil error, want missing-field error")
	}
	if _, err := f.Fake("missing"); err == nil {
		t.Error("Fake(missing) = nil error, want unknown-category error")
	}
}
