package fakes

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

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

// TestCalcTokenOperandsReadsOnlyACalc pins what the helper answers for a body that
// is not a calc, and for one whose expression does not parse: nothing, either way.
// checkCalc is what reports a bad expression, so the callers that run after it
// never meet one — but they must not have to depend on that order to be safe.
func TestCalcTokenOperandsReadsOnlyACalc(t *testing.T) {
	for _, body := range []string{"plain", "luhn()", "calc()", "calc(1 +)", "calc(()"} {
		if got := calcTokenOperands(body); got != nil {
			t.Errorf("calcTokenOperands(%q) = %v, want none", body, got)
		}
	}
	if got := calcTokenOperands("calc(net * qty)"); len(got) != 2 {
		t.Errorf("calcTokenOperands(calc(net * qty)) = %v, want both operands", got)
	}
}

// --- one draw, one value: a calc operand reads the expansion's draw ---

// TestCalcOperandReadsTheExpansionsDraw pins the one-draw rule for a calc operand.
// A field the format renders and a calc reads is drawn once per expansion, so the
// operand shown is the operand computed — the correlation the dotted-path rule
// already gives a level (see bound_test.go), applied to a plain sibling.
func TestCalcOperandReadsTheExpansionsDraw(t *testing.T) {
	dir := writeData(t, map[string]string{
		"inv": `{"format":"{net} x {qty} = {calc(net * qty, 2)}","net":["19.99","5.00","100.00"],"qty":["2","3","7"]}`,
	})
	f := newFakes(t, dir, WithSeed(3))
	for i := 0; i < 300; i++ {
		got := fake(t, f, "inv")
		var net, qty, want float64
		if _, err := fmt.Sscanf(got, "%g x %g = %g", &net, &qty, &want); err != nil {
			t.Fatalf("inv = %q, unparseable: %v", got, err)
		}
		if math.Abs(net*qty-want) > 1e-9 {
			t.Fatalf("inv = %q: the shown %g x %g is not the computed %g", got, net, qty, want)
		}
	}
}

// TestCalcOperandSharesOneDraw pins the reach of that hold: the draw belongs to the
// expansion, not to the calc, so a bare token rendering the same name reads it too.
func TestCalcOperandSharesOneDraw(t *testing.T) {
	dir := writeData(t, map[string]string{
		"same": `{"format":"{w} {w} {calc(w)}","w":["1","2","3","4","5"]}`,
	})
	f := newFakes(t, dir, WithSeed(5))
	for i := 0; i < 200; i++ {
		got := fake(t, f, "same")
		if p := strings.Fields(got); len(p) != 3 || p[0] != p[1] || p[0] != p[2] {
			t.Fatalf("same = %q, want one value three times", got)
		}
	}
}

// TestFieldNoCalcReadsDrawsEachTime guards the boundary: only a name a calc reads is
// held, so an ordinary {w} {w} still draws twice.
func TestFieldNoCalcReadsDrawsEachTime(t *testing.T) {
	dir := writeData(t, map[string]string{"two": `{"format":"{w} {w}","w":["1","2","3","4","5"]}`})
	f := newFakes(t, dir, WithSeed(5))
	for i := 0; i < 200; i++ {
		if p := strings.Fields(fake(t, f, "two")); p[0] != p[1] {
			return
		}
	}
	t.Fatal("{w} {w} never differed in 200 draws, want two independent draws")
}

// TestCalcHoldIsPerExpansion pins the scope of the hold: each repeat iteration is
// its own expansion, so it draws again while staying self-consistent.
func TestCalcHoldIsPerExpansion(t *testing.T) {
	dir := writeData(t, map[string]string{
		"rep": `{"format":"{n}={calc(n * 1)}","repeat":8,"separator":" ","n":["2","3","4","5","6","7","8","9"]}`,
	})
	f := newFakes(t, dir, WithSeed(11))
	varied := false
	for i := 0; i < 50; i++ {
		got := fake(t, f, "rep")
		seen := map[string]bool{}
		for _, pair := range strings.Fields(got) {
			shown, computed, ok := strings.Cut(pair, "=")
			if !ok || shown != computed {
				t.Fatalf("rep = %q: %q disagrees within one iteration", got, pair)
			}
			seen[shown] = true
		}
		varied = varied || len(seen) > 1
	}
	if !varied {
		t.Fatal("every repeat iteration drew alike, want an independent draw each")
	}
}

// TestCalcHoldIsPerTemplate pins that a nested template holds its own: the inner
// {v} and its calc agree with each other, not with the outer pair.
func TestCalcHoldIsPerTemplate(t *testing.T) {
	dir := writeData(t, map[string]string{
		"nest": `{"format":"{v}={calc(v * 1)} {inner}","v":["2","3","4","5","6","7","8","9"],
			"inner":{"format":"{v}={calc(v * 1)}","v":["2","3","4","5","6","7","8","9"]}}`,
	})
	f := newFakes(t, dir, WithSeed(13))
	differed := false
	for i := 0; i < 200; i++ {
		got := fake(t, f, "nest")
		outer, inner, ok := strings.Cut(got, " ")
		if !ok {
			t.Fatalf("nest = %q, want two pairs", got)
		}
		for _, pair := range []string{outer, inner} {
			if shown, computed, ok := strings.Cut(pair, "="); !ok || shown != computed {
				t.Fatalf("nest = %q: %q disagrees within its own expansion", got, pair)
			}
		}
		differed = differed || outer != inner
	}
	if !differed {
		t.Fatal("the nested template never differed from its parent, want its own draw")
	}
}
