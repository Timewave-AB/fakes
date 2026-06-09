package fakes

import "testing"

// TestCalcArithmetic pins the operators, precedence, parentheses and unary minus
// over number literals.
func TestCalcArithmetic(t *testing.T) {
	f := engine(1)
	cases := map[string]string{
		`{"format":"{calc(2 + 3)}"}`:       "5",
		`{"format":"{calc(2 * 3 + 4)}"}`:   "10", // * binds tighter than +
		`{"format":"{calc(2 + 3 * 4)}"}`:   "14",
		`{"format":"{calc((2 + 3) * 4)}"}`: "20", // parentheses override
		`{"format":"{calc(10 / 4)}"}`:      "2.5",
		`{"format":"{calc(-2 + 5)}"}`:      "3", // unary minus
		`{"format":"{calc(2 - -3)}"}`:      "5",
		`{"format":"{calc(1.5 * 2)}"}`:     "3", // whole result drops the decimals
	}
	for tmpl, want := range cases {
		if got := mustRender(t, f, tmpl); got != want {
			t.Errorf("%s = %q, want %q", tmpl, got, want)
		}
	}
}

// TestCalcAuto pins the default (no-dp) rendering: minimal decimal form, no
// scientific notation, whole numbers without a fraction.
func TestCalcAuto(t *testing.T) {
	f := engine(1)
	cases := map[string]string{
		`{"format":"{calc(10 / 3)}"}`: "3.3333333333333335",
		`{"format":"{calc(6 / 2)}"}`:  "3",
		`{"format":"{calc(1 / 4)}"}`:  "0.25",
	}
	for tmpl, want := range cases {
		if got := mustRender(t, f, tmpl); got != want {
			t.Errorf("%s = %q, want %q", tmpl, got, want)
		}
	}
}

// TestCalcDecimals pins the optional decimals arg: rounds to dp places, dp 0
// drops the fraction.
func TestCalcDecimals(t *testing.T) {
	f := engine(1)
	cases := map[string]string{
		`{"format":"{calc(10 / 3, 2)}"}`: "3.33",
		`{"format":"{calc(10 / 3, 0)}"}`: "3",
		`{"format":"{calc(2 * 3, 2)}"}`:  "6.00",
	}
	for tmpl, want := range cases {
		if got := mustRender(t, f, tmpl); got != want {
			t.Errorf("%s = %q, want %q", tmpl, got, want)
		}
	}
}

// TestCalcFields pins that bare names resolve to sibling fields, rendered then
// parsed as numbers.
func TestCalcFields(t *testing.T) {
	if got := mustRender(t, engine(1), `{"format":"{calc(price * qty, 2)}","price":["19.99"],"qty":["3"]}`); got != "59.97" {
		t.Fatalf("calc over fields = %q, want 59.97", got)
	}
	// A field that is itself a template renders before parsing.
	if got := mustRender(t, engine(1), `{"format":"{calc(a - b)}","a":[{"format":"{n}","n":["10"]}],"b":["3"]}`); got != "7" {
		t.Fatalf("calc(a - b) = %q, want 7", got)
	}
}

// TestCalcNonNumericIsNaN pins the never-fail rule: a field that doesn't render
// to a number becomes NaN, which propagates and prints visibly.
func TestCalcNonNumericIsNaN(t *testing.T) {
	if got := mustRender(t, engine(1), `{"format":"{calc(x * 2)}","x":["abc"]}`); got != "NaN" {
		t.Fatalf("calc over non-numeric field = %q, want NaN", got)
	}
}

// TestCalcReproducible pins that a calc over a random operand stays seed-stable.
func TestCalcReproducible(t *testing.T) {
	tmpl := `{"format":"{calc(q * 2 + 1)}","q":[{"format":"{int(1,1000000)}"}]}`
	if a, b := mustRender(t, engine(7), tmpl), mustRender(t, engine(7), tmpl); a != b {
		t.Fatalf("calc not reproducible: %q != %q", a, b)
	}
}
