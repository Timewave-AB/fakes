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

func mustRender(t *testing.T, f *Fakes, s string) string {
	t.Helper()
	return render(f.rand, compiled(t, s))
}

func TestLiteralStringIsVerbatim(t *testing.T) {
	// A bare string is a literal, never formatted: 'a'/'A' must survive.
	if got := mustRender(t, engine(1), `"Malmö"`); got != "Malmö" {
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
			if got := mustRender(t, f, tmpl); !re.MatchString(got) {
				t.Fatalf("%s produced %q, want %s", tmpl, got, re)
			}
		}
	}
}

func TestEscapeAndLiteralChars(t *testing.T) {
	// '#' escapes the next char; non-class chars (7, x, -) are literal.
	if got := mustRender(t, engine(1), `{"format":"#0#1#A#a## x7-z"}`); got != "01Aa# x7-z" {
		t.Fatalf("escape = %q, want \"01Aa# x7-z\"", got)
	}
}

func TestTokenSubstitution(t *testing.T) {
	if got := mustRender(t, engine(1), `{"format":"{x}sson","x":["Erik"]}`); got != "Eriksson" {
		t.Fatalf("token = %q, want Eriksson", got)
	}
}

func TestAlternationPicksOneField(t *testing.T) {
	f := engine(3)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		seen[mustRender(t, f, `{"format":"{a|b}","a":["A"],"b":["B"]}`)] = true
	}
	if !seen["A"] || !seen["B"] || len(seen) != 2 {
		t.Fatalf("alternation produced %v, want both A and B", seen)
	}
}

func TestWeightZeroNeverChosen(t *testing.T) {
	f := engine(5)
	for i := 0; i < 200; i++ {
		if got := mustRender(t, f, `[{"format":"X","weight":0},"Y"]`); got != "Y" {
			t.Fatalf("weight-0 variant chosen: %q", got)
		}
	}
}

func TestWeightSkewsDistribution(t *testing.T) {
	f := engine(9)
	heavy := 0
	for i := 0; i < 1000; i++ {
		if mustRender(t, f, `[{"format":"H","weight":10},{"format":"L"}]`) == "H" {
			heavy++
		}
	}
	if heavy < 800 { // expected ~909
		t.Fatalf("heavy variant chosen %d/1000, want a clear majority", heavy)
	}
}

func TestRepeatRendersFormatNTimes(t *testing.T) {
	// repeat N concatenates N independent renders of the format ('x','y' are
	// literal, not character classes).
	if got := mustRender(t, engine(1), `{"format":"xy","repeat":3}`); got != "xyxyxy" {
		t.Fatalf("repeat literal = %q, want xyxyxy", got)
	}
	// separator joins renders (N-1 of them, no trailing one).
	if got := mustRender(t, engine(1), `{"format":"xy","repeat":3,"separator":"-"}`); got != "xy-xy-xy" {
		t.Fatalf("repeat with separator = %q, want xy-xy-xy", got)
	}
	f, re := engine(7), regexp.MustCompile(`^[0-9]{4}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		got := mustRender(t, f, `{"format":"0","repeat":4}`)
		if !re.MatchString(got) {
			t.Fatalf("repeat-4 = %q, want 4 digits", got)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatalf("repeat never varied across renders: %v", seen)
	}
}

func TestFunctionTokenLuhn(t *testing.T) {
	// {luhn()} appends a Luhn check digit over the digits emitted so far in the
	// current expansion (non-digits skipped but kept). It reads the output
	// buffer, so a value is never re-rendered. Bodies are escaped to fix input.
	f := engine(1)
	cases := map[string]string{
		`{"format":"#8#1#1#2#1#8#9#8#7{luhn()}"}`:    "8112189876",  // personnummer body
		`{"format":"#7#9#9#2#7#3#9#8#7#1{luhn()}"}`:  "79927398713", // classic Luhn vector
		`{"format":"#8#1#1#2#1#8-#9#8#7{luhn()}"}`:   "811218-9876", // '-' skipped, kept
		`{"format":"{n}{luhn()}","n":["811218987"]}`: "8112189876",  // over a rendered token
	}
	for tmpl, want := range cases {
		if got := mustRender(t, f, tmpl); got != want {
			t.Fatalf("%s = %q, want %q", tmpl, got, want)
		}
	}
}

func TestRecursionHasNoDepthLimit(t *testing.T) {
	// Build {format:{a}, a:[{format:{a}, a:[ ... "deep" ]]}} 50 levels deep.
	tmpl := `"deep"`
	for i := 0; i < 50; i++ {
		tmpl = `{"format":"{a}","a":[` + tmpl + `]}`
	}
	if got := mustRender(t, engine(1), tmpl); got != "deep" {
		t.Fatalf("deep recursion = %q, want deep", got)
	}
}

func TestCompileErrors(t *testing.T) {
	// Every structural problem is caught up front, at compile/New time, never
	// deferred to a random render that happens to hit the bad branch.
	for _, bad := range []string{
		`{"x":["Q"]}`,                 // object without "format"
		`{"format":"{y}","x":1}`,      // a field is a bare number
		`[1, 2]`,                      // a choice of numbers
		`5`,                           // unsupported node type
		`[]`,                          // empty choice
		`{"format":"{x"}`,             // unterminated brace
		`{"format":"{y}","x":["Q"]}`,  // token names a missing field
		`{"format":"{}"}`,             // empty token name
		`{"format":"{a|}","a":["Q"]}`, // empty alternation segment
		`[{"format":"A","weight":-1},{"format":"B"}]`,                   // negative weight
		`[{"format":"A","weight":0},{"format":"B","weight":0}]`,         // weights sum to zero
		`[{"format":"A","weight":1e308},{"format":"B","weight":1e308}]`, // weights overflow to +Inf
		`[{"format":"A","weight":"heavy"}]`,                             // non-numeric weight
		`{"format":"x","repeat":0}`,                                     // repeat below 1
		`{"format":"x","repeat":-2}`,                                    // negative repeat
		`{"format":"x","repeat":1.5}`,                                   // non-integer repeat
		`{"format":"x","repeat":"two"}`,                                 // non-numeric repeat
		`{"format":"x","repeat":2,"separator":5}`,                       // non-string separator
		`{"format":"{nope()}"}`,                                         // unknown function
		`{"format":"{luhn(x)}"}`,                                        // function given args it takes none of
		`{"format":"{luhn(}"}`,                                          // malformed function token
	} {
		if _, err := compile(parse(t, bad)); err == nil {
			t.Errorf("compile(%s) = nil error, want error", bad)
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
