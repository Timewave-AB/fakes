package fakes

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// This file pins the one-draw rule for dotted sibling tokens. A format that
// addresses a sibling by path ({place.locality}) draws that sibling once per
// expansion, so every {place.*} token in the same format reads the same variant.
// That is what makes a correlated pair — a locality and a postal code that agree
// — expressible in data, without the row having to be the whole node.

// swedishPlaces is a two-variant sibling whose variants pair a locality with the
// postal-code prefix that really belongs to it.
const swedishPlaces = `{"format":"%s","place":[
	{"format":"{locality}","locality":"Stockholm","postal-code":{"format":"#100 00"}},
	{"format":"{locality}","locality":"Tranås","postal-code":{"format":"#5#7#3 00"}}
]}`

// agree reports whether a rendered "postcode locality" pair is a real pairing.
var agree = regexp.MustCompile(`^(1[0-9]{2} [0-9]{2} Stockholm|573 [0-9]{2} Tranås)$`)

func places(format string) string {
	return strings.Replace(swedishPlaces, "%s", format, 1)
}

func TestDottedTokenCorrelatesSiblingFields(t *testing.T) {
	f := engine(1)
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		got := mustRender(t, f, places("{place.postal-code} {place.locality}"))
		if !agree.MatchString(got) {
			t.Fatalf("draw %d = %q, want a postcode and locality that pair", i, got)
		}
		seen[got[len(got)-4:]] = true
	}
	if len(seen) < 2 {
		t.Fatalf("500 draws produced only %v, want both localities", seen)
	}
}

func TestRenderingALevelAndReadingIntoItIsRejected(t *testing.T) {
	// One draw, one spelling. A token that renders a level and one that reads a
	// path into it name the same draw two ways, and only the path reads what the
	// other rendered — so the pair is a load error rather than a shape whose two
	// halves can disagree.
	rejected := map[string]string{
		"bare head beside a path": `{"format":"{p} + {p.first}","p":[{"format":"{first}","first":["Anna","Bo"]}]}`,
		"prefix path beside a path": `{"format":"{p.addr}|{p.addr.city}","p":[{"format":"{addr}","addr":[` +
			`{"format":"{city}","city":["Kiruna","Boden"]},{"format":"{city}","city":["Malmö","Lund"]}]}]}`,
		"either order": `{"format":"{p.first} + {p}","p":[{"format":"{first}","first":["Anna","Bo"]}]}`,
	}
	for name, file := range rejected {
		_, err := New([]string{writeData(t, map[string]string{"cat": file})})
		if err == nil {
			t.Errorf("%s: New = nil error, want the overlapping tokens rejected", name)
			continue
		}
		if !strings.Contains(err.Error(), "reads a path into") {
			t.Errorf("%s: New = %v, want it to name the overlap", name, err)
		}
	}
	// The same path twice is one spelling, so it stays legal.
	if _, err := New([]string{writeData(t, map[string]string{
		"cat": `{"format":"{p.first} + {p.first}","p":[{"format":"{first}","first":["Anna","Bo"]}]}`,
	})}); err != nil {
		t.Errorf("New = %v, want one path read twice accepted", err)
	}
}

func TestCalcOperandNamingABoundLevelIsRejected(t *testing.T) {
	// A calc operand renders its field, so naming a bound level in one is the same
	// overlap as a bare token: {calc(item * 1)} renders what {item.price} reads a
	// path into, and the two disagree.
	_, err := New([]string{writeData(t, map[string]string{
		"cat": `{"format":"{item.price}|{calc(item * 1)}","item":[` +
			`{"format":"{price}","price":"10"},{"format":"{price}","price":"20"}]}`,
	})})
	if err == nil || !strings.Contains(err.Error(), "reads a path into") {
		t.Fatalf("New = %v, want the calc operand rejected as an overlap", err)
	}
	// Arithmetic over fields no path names is untouched.
	if _, err := New([]string{writeData(t, map[string]string{
		"cat": `{"format":"{net} x {qty} = {calc(net * qty, 2)}","net":["19.99"],"qty":["3"]}`,
	})}); err != nil {
		t.Errorf("New = %v, want plain arithmetic accepted", err)
	}
}

func TestReferenceNamingABoundLevelIsRejected(t *testing.T) {
	// A reference can name a bound level from the data root, which renders it
	// afresh beside the path that reads its held draw — the same overlap by
	// another spelling.
	rejected := map[string]string{
		"reference names the head": `{"format":"{p.first}|{..cat.p}","p":[{"format":"{first}","first":["Anna","Bo"]}]}`,
		"reference names the leaf": `{"format":"{p.addr}|{..cat.p.addr}","p":{"format":"x","addr":["A","B","C","D"]}}`,
		// The reference need not sit in the format that binds: any field it renders
		// reaches the level just the same, however deep.
		"reference from a sibling field": `{"format":"{p.first}|{inner}","p":[` +
			`{"format":"{first}-{last}","first":"A","last":"1"},{"format":"{first}-{last}","first":"B","last":"2"}],` +
			`"inner":{"format":"{..cat.p}"}}`,
	}
	for name, file := range rejected {
		_, err := New([]string{writeData(t, map[string]string{"cat": file})})
		if err == nil || !strings.Contains(err.Error(), "reads a path into") {
			t.Errorf("%s: New = %v, want the reference rejected as an overlap", name, err)
		}
	}
	// A reference to anything this format does not bind is untouched.
	if _, err := New([]string{writeData(t, map[string]string{
		"cat":     `{"format":"{p.first} {..surname}","p":[{"format":"{first}","first":["Anna","Bo"]}]}`,
		"surname": `["Eriksson","Lindqvist"]`,
	})}); err != nil {
		t.Errorf("New = %v, want a reference outside the bound level accepted", err)
	}
}

func TestCycleReachedOnlyByAPathTokenIsRejected(t *testing.T) {
	// A path token renders what it lands on, not the level it started from, so the
	// cycle walk has to follow it there. A head whose own format names nothing
	// would otherwise hide the cycle until render, where it is fatal.
	_, err := New([]string{writeData(t, map[string]string{
		"a": `{"format":"{p.x}","p":{"format":"static","x":{"format":"{..a}"}}}`,
	})})
	if err == nil || !strings.Contains(err.Error(), "reference cycle") {
		t.Fatalf("New = %v, want the cycle through {p.x} rejected", err)
	}
}

func TestALevelRenderedOnlyByAPathTokenIsHeld(t *testing.T) {
	// {p.a} renders q, so it is a route to the level {q.x} holds — even though p's
	// own format names nothing.
	_, err := New([]string{writeData(t, map[string]string{
		"thing": `{"format":"{p.a} {q.x}","p":{"format":"static","a":{"format":"{..thing.q}"}},` +
			`"q":{"format":"{x}","x":["1","2"]}}`,
	})})
	if err == nil || !strings.Contains(err.Error(), "reads a path into") {
		t.Fatalf("New = %v, want the second route to q rejected", err)
	}
}

// TestACalcOperandIsHeldAgainstEveryRoute pins what a {calc()} operand's hold
// fences. A calc renders its operand whole, so the draw it holds is that one node's
// value: another route conflicts only by naming that same node, and then the value
// shown is not the value computed. Two names that merely draw from one source are
// two draws, as {word} {word} is — the rule everywhere else in the engine.
func TestACalcOperandIsHeldAgainstEveryRoute(t *testing.T) {
	rejected := map[string]map[string]string{
		"a reference beside the operand": {
			"cat": `{"format":"{..cat.net} x 2 = {calc(net * 2, 2)}","net":["10.00","20.00"]}`,
		},
		"a reference one level down": {
			"cat": `{"format":"{calc(net * 2, 2)} {q}","net":["10.00","20.00"],` +
				`"q":{"format":"{..cat.net}"}}`,
		},
		// A reference ending at the operand binds the choice itself, not a variant,
		// so wrapping the operand in one changes nothing.
		"a reference to an operand wrapped in a choice": {
			"cat": `{"format":"{calc(n * 2, 2)} {q}","n":[{"format":"{v}","v":["1","2"]}],` +
				`"q":{"format":"{..cat.n}"}}`,
		},
	}
	for name, files := range rejected {
		_, err := New([]string{writeData(t, files)})
		if err == nil || !strings.Contains(err.Error(), "a {calc()} also reads") {
			t.Errorf("%s: New = %v, want the second route to the operand rejected", name, err)
		}
	}

	accepted := map[string]map[string]string{
		// The spellings that read the one draw: the operand itself, and a bare token.
		"only the operand and a bare token": {
			"cat": `{"format":"{net} x 2 = {calc(net * 2, 2)}","net":["10.00","20.00"]}`,
		},
		// A sibling the operand's format never renders is no part of its value. A
		// path head differs — a path may read into anything the level contains —
		// which is why that half of the fence covers containment.
		"a reference to a sibling the operand never renders": {
			"cat": `{"format":"{calc(net * 2, 2)} {unit}",` +
				`"net":{"format":"{v}","v":["1","2"],"spare":["kg","lb"]},` +
				`"unit":{"format":"{..cat.net.spare}"}}`,
		},
		// Two operands drawing from one source are two names, so two draws: each is
		// held under its own name and shown once, and neither can disagree.
		"two operands sharing one source": {
			"die": `["1","2","3","4","5","6"]`,
			"cat": `{"format":"{d1} + {d2} = {calc(d1 + d2, 0)}","d1":{"format":"{..die}"},` +
				`"d2":{"format":"{..die}"}}`,
		},
		// Likewise a reference drawing from what the operand draws from: {..common}
		// and net are two names, not two spellings of one field.
		"a reference to what an operand renders through": {
			"common": `["1","2"]`,
			"cat":    `{"format":"{calc(net * 2, 2)} {..common}","net":{"format":"{..common}"}}`,
		},
		// A fixed string cannot disagree with itself, so it needs no fence.
		"a literal operand named twice": {
			"cat": `{"format":"{calc(n * 2, 0)} {..cat.n}","n":"5"}`,
		},
	}
	for name, files := range accepted {
		if _, err := New([]string{writeData(t, files)}); err != nil {
			t.Errorf("%s: New = %v, want it accepted", name, err)
		}
	}
}

func TestAPathReachesEveryVariantItMightDraw(t *testing.T) {
	// A path through a choice may land in any variant, so both walks have to see
	// all of them. Reaching only the first leaves whatever hides in a later
	// variant to be found at render — a cycle there is fatal, and a second route
	// to a held level disagrees silently.
	rejected := map[string]struct{ file, want string }{
		"a cycle in a later variant": {
			`{"format":"{p.x}","p":[{"format":"h","x":"safe"},{"format":"h","x":{"format":"{..cat}"}}]}`,
			"reference cycle",
		},
		"a second route in a later variant": {
			`{"format":"{p.x} {q.y}","p":[{"format":"h","x":"safe"},{"format":"h","x":{"format":"{..cat.q}"}}],` +
				`"q":{"format":"{y}","y":["1","2"]}}`,
			"reads a path into",
		},
	}
	for name, c := range rejected {
		_, err := New([]string{writeData(t, map[string]string{"cat": c.file})})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: New = %v, want it to mention %q", name, err, c.want)
		}
	}
}

func TestALevelAPathNeverRendersIsAccepted(t *testing.T) {
	// A path token does not expand its head's format, so a reference sitting in
	// that format is not a second route to anything: it is never rendered by the
	// path at all. Both orders must load.
	accepted := map[string]string{
		"reference in the head's own format": `{"format":"{p.first} {q.a}",` +
			`"p":{"format":"{first} {..thing.q}","first":["A","B"]},"q":{"format":"{a}","a":["1","2"]}}`,
		"the mirror shape": `{"format":"{p.first} {q.a}",` +
			`"p":{"format":"{first}","first":["A","B"]},"q":{"format":"{a} {..thing.p}","a":["1","2"]}}`,
	}
	for name, file := range accepted {
		if _, err := New([]string{writeData(t, map[string]string{"thing": file})}); err != nil {
			t.Errorf("%s: New = %v, want it accepted", name, err)
		}
	}
}

func TestADeepDiamondChainLoads(t *testing.T) {
	// A diamond chain is 2^n routes through n nodes. The search past a bound level
	// must walk each node once, not once per route, or a deep chain never loads.
	files := map[string]string{"l0": `["x"]`}
	for i := 1; i <= 30; i++ {
		files[fmt.Sprintf("l%d", i)] = fmt.Sprintf(
			`{"format":"{a}{b}","a":{"format":"{..l%d}"},"b":{"format":"{..l%d}"}}`, i-1, i-1)
	}
	files["thing"] = `{"format":"{p.first} {..l30}","p":{"format":"x","first":["A","B"]}}`
	if _, err := New([]string{writeData(t, files)}); err != nil {
		t.Fatalf("New = %v, want a deep diamond chain to load", err)
	}
}

func TestASharedNodeIsWalkedOnce(t *testing.T) {
	// Two fields reaching one node make the render graph a diamond, not a tree.
	// The search past a bound level must take that in its stride rather than walk
	// the shared node once per route.
	f, err := New([]string{writeData(t, map[string]string{
		"cat": `{"format":"{p.first}|{q}","p":[{"format":"{first}","first":["Anna","Bo"]}],` +
			`"q":{"format":"{a}{b}","a":{"format":"{..shared}"},"b":{"format":"{..shared}"}}}`,
		"shared": `["x"]`,
	})}, WithSeed(1))
	if err != nil {
		t.Fatalf("New = %v, want a shared node accepted", err)
	}
	if got, err := f.Fake("cat"); err != nil || !strings.HasSuffix(got, "|xx") {
		t.Fatalf("Fake(cat) = %q, %v, want it to end in xx", got, err)
	}
}

func TestTheEarlierReaderIsNamed(t *testing.T) {
	// A token and a calc operand can name one level. The one the format writes
	// first is the one reported, so the error points at the same place a reader
	// looking at the format would start.
	_, err := New([]string{writeData(t, map[string]string{
		"cat": `{"format":"{calc(p * 1)} {p} {p.first}","p":[{"format":"{first}","first":["1","2"]}]}`,
	})})
	if err == nil || !strings.Contains(err.Error(), `calc operand "p"`) {
		t.Fatalf("New = %v, want the calc operand named, being written first", err)
	}
}

func TestReferenceToAMatchingStringIsAccepted(t *testing.T) {
	// A literal renders one fixed string, so no draw of it can disagree with a
	// held one. Two unrelated literals that merely spell the same text must not
	// read as the same node — the trap when comparing a value type.
	if _, err := New([]string{writeData(t, map[string]string{
		"cat":   `{"format":"{p.city} {..other.tag}","p":{"format":"{city}","city":"Stockholm"}}`,
		"other": `{"format":"x","tag":"Stockholm"}`,
	})}); err != nil {
		t.Fatalf("New = %v, want a reference to a matching string accepted", err)
	}
}

func TestNestedChoiceDrawsOneVariant(t *testing.T) {
	// A choice item may itself be a choice, so drawing a bound head unwraps until
	// it reaches a value. Stopping at one level leaves a choice where the walk
	// expects a template.
	f := engine(21)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		seen[mustRender(t, f, `{"format":"{p.x}","p":[[{"format":"{x}","x":"1"}],[{"format":"{x}","x":"2"}]]}`)] = true
	}
	if !seen["1"] || !seen["2"] || len(seen) != 2 {
		t.Fatalf("200 draws produced %v, want 1 and 2", seen)
	}
}

func TestRepeatingLevelIsNamedInTheError(t *testing.T) {
	// The level carrying the repeat is the one to fix, so the error names it
	// rather than the head the path started from.
	_, err := New([]string{writeData(t, map[string]string{
		"cat": `{"format":"[{p.a.b}]","p":{"format":"{a}","a":{"format":"{b}","repeat":3,"separator":",","b":["z"]}}}`,
	})})
	if err == nil || !strings.Contains(err.Error(), `"p.a"`) {
		t.Fatalf("New = %v, want it to name the level p.a that carries the repeat", err)
	}
}

func TestPathIntoARepeatingLevelBehindAChoiceIsRejected(t *testing.T) {
	// A choice of rows is the shape this feature is for, so the repeat rule has to
	// reach inside one — otherwise the direct spelling is a load error and the same
	// mistake behind a choice silently drops the repeat.
	_, err := New([]string{writeData(t, map[string]string{
		"cat": `{"format":"[{p.a}]","p":[{"format":"{a}","repeat":3,"separator":",","a":["x"]},{"format":"{a}","a":["y"]}]}`,
	})})
	if err == nil || !strings.Contains(err.Error(), "repeat") {
		t.Fatalf("New = %v, want the repeat behind a choice rejected", err)
	}
}

func TestPathIntoARepeatingLevelIsRejected(t *testing.T) {
	// A path reads one level's draw, so it can never apply that level's repeat.
	// The engine rejects options that cannot take effect, and this is one.
	_, err := New([]string{writeData(t, map[string]string{
		"cat": `{"format":"[{p.a}]","p":{"format":"{a}","repeat":3,"separator":",","a":["z"]}}`,
	})})
	if err == nil || !strings.Contains(err.Error(), "repeat") {
		t.Fatalf("New = %v, want a path into a repeating level rejected", err)
	}
}

func TestPathIntoAPlainTemplateNamesTheMissingField(t *testing.T) {
	// A head that is one template, not a choice, reports the missing segment
	// directly — there are no variants to compare.
	_, err := New([]string{writeData(t, map[string]string{
		"cat": `{"format":"{a.nope}","a":{"format":"x","b":"1"}}`,
	})})
	if err == nil || !strings.Contains(err.Error(), `no field "nope"`) {
		t.Fatalf("New = %v, want it to name the missing field", err)
	}
}

func TestFieldWithoutAPathTokenStillDrawsEachTime(t *testing.T) {
	// A format with no dotted token keeps today's behaviour: every {word} is an
	// independent draw.
	f := engine(3)
	varied := false
	for i := 0; i < 200 && !varied; i++ {
		got := mustRender(t, f, `{"format":"{w}{w}{w}","w":["a","b","c","d","e"]}`)
		varied = got[0] != got[1] || got[1] != got[2]
	}
	if !varied {
		t.Fatal("200 draws of {w}{w}{w} never varied, want independent draws")
	}
}

func TestTwoBoundHeadsDrawIndependently(t *testing.T) {
	// Binding is per head, so two correlated sets in one format compose instead
	// of needing a cross product of variants.
	f := engine(4)
	seen := map[string]bool{}
	for i := 0; i < 400; i++ {
		seen[mustRender(t, f, `{"format":"{p.v}{q.v}",
			"p":[{"format":"{v}","v":"A"},{"format":"{v}","v":"B"}],
			"q":[{"format":"{v}","v":"1"},{"format":"{v}","v":"2"}]}`)] = true
	}
	for _, want := range []string{"A1", "A2", "B1", "B2"} {
		if !seen[want] {
			t.Fatalf("400 draws produced %v, want every combination including %q", seen, want)
		}
	}
}

func TestRepeatRedrawsTheBinding(t *testing.T) {
	// The binding lives for one expansion, so each repetition is a fresh draw —
	// and each repetition is internally consistent.
	f := engine(5)
	tmpl := strings.Replace(places("{place.postal-code} {place.locality}"), `"format":"{place.postal-code} {place.locality}"`,
		`"format":"{place.postal-code} {place.locality}","repeat":6,"separator":"\n"`, 1)
	varied := false
	for i := 0; i < 40; i++ {
		got := mustRender(t, f, tmpl)
		lines := strings.Split(got, "\n")
		if len(lines) != 6 {
			t.Fatalf("repeat 6 produced %d lines", len(lines))
		}
		for _, line := range lines {
			if !agree.MatchString(line) {
				t.Fatalf("repetition %q does not pair", line)
			}
		}
		if lines[0] != lines[1] || lines[1] != lines[2] {
			varied = true
		}
	}
	if !varied {
		t.Fatal("40 renders of repeat 6 never varied within a render, want a redraw per repetition")
	}
}

func TestNestedTemplateKeepsItsOwnBinding(t *testing.T) {
	// A binding belongs to the expansion that made it: an inner template's own
	// place is not the outer one's.
	f := engine(6)
	tmpl := `{"format":"{place.v}{inner}",
		"place":[{"format":"{v}","v":"A"},{"format":"{v}","v":"B"}],
		"inner":{"format":"{place.v}","place":[{"format":"{v}","v":"A"},{"format":"{v}","v":"B"}]}}`
	seen := map[string]bool{}
	for i := 0; i < 400; i++ {
		seen[mustRender(t, f, tmpl)] = true
	}
	if !seen["AB"] && !seen["BA"] {
		t.Fatalf("400 draws produced %v, want the inner binding to be independent", seen)
	}
}

func TestBoundDrawIsSeedStable(t *testing.T) {
	tmpl := places("{place.postal-code} {place.locality}")
	a := mustRender(t, engine(42), tmpl)
	b := mustRender(t, engine(42), tmpl)
	if a != b {
		t.Fatalf("same seed gave %q and %q, want an identical draw", a, b)
	}
}

func TestDottedTokenErrors(t *testing.T) {
	// A path token is validated where every other token is: at New, naming what
	// is wrong, so a typo can never reach a render.
	rejected := map[string]struct {
		file string
		want string
	}{
		"unknown head": {
			`{"format":"{nope.x}","place":[{"format":"{v}","v":"A"}]}`,
			`no field "nope"`,
		},
		"unknown tail": {
			`{"format":"{place.nope}","place":[{"format":"{v}","v":"A"},{"format":"{v}","v":"B"}]}`,
			`"nope"`,
		},
		"tail only some variants carry": {
			`{"format":"{place.locality}","place":[{"format":"{locality}","locality":"A"},{"format":"x"}]}`,
			`locality`,
		},
		"path into a literal": {
			`{"format":"{x.y}","x":"plain"}`,
			`"y"`,
		},
		"dotted field key": {
			`{"format":"[{a.b}]","a.b":["V"]}`,
			`contains "."`,
		},
	}
	for name, c := range rejected {
		_, err := New([]string{writeData(t, map[string]string{"cat": c.file})})
		if err == nil {
			t.Errorf("%s: New = nil error, want it rejected at load", name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: New = %v, want it to mention %q", name, err, c.want)
		}
	}
}

func TestSamePathReadTwiceReadsOneValue(t *testing.T) {
	// A path is drawn once per expansion, so reading it twice reads one value —
	// what a name shown in a display form and again in an address needs.
	f := engine(7)
	for i := 0; i < 300; i++ {
		got := mustRender(t, f, `{"format":"{p.first}|{p.first}",
			"p":[{"format":"{first}","first":["Anna","Astrid","Elin","Karin"]}]}`)
		parts := strings.Split(got, "|")
		if parts[0] != parts[1] {
			t.Fatalf("draw %d = %q, want one value read twice", i, got)
		}
	}
}

func TestDifferentTailsUnderOneHeadShareTheRow(t *testing.T) {
	// Two tails of one head stay in the same variant, each keeping its own value.
	f := engine(8)
	for i := 0; i < 300; i++ {
		got := mustRender(t, f, `{"format":"{p.a}{p.b}",
			"p":[{"format":"x","a":"A","b":"1"},{"format":"y","a":"B","b":"2"}]}`)
		if got != "A1" && got != "B2" {
			t.Fatalf("draw %d = %q, want a row's own pair", i, got)
		}
	}
}

// deepPlaces nests the correlated facts a level down: the row holds an addr, and
// the addr holds the city and the zip that belongs to it.
const deepPlaces = `{"format":"%s","p":[{"format":"{addr}","addr":[
	{"format":"{city}","city":"Stockholm","zip":"11111"},
	{"format":"{city}","city":"Kiruna","zip":"98100"}]}]}`

func TestPathHoldsItsDrawAtEveryLevel(t *testing.T) {
	// A choice below the head is drawn once too, so two paths sharing a prefix
	// share every level of it — not just the row they started from.
	f := engine(11)
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		got := mustRender(t, f, strings.Replace(deepPlaces, "%s", "{p.addr.city}/{p.addr.zip}", 1))
		if got != "Stockholm/11111" && got != "Kiruna/98100" {
			t.Fatalf("draw %d = %q, want a city and the zip that belongs to it", i, got)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatalf("500 draws produced only %v, want both", seen)
	}
}

func TestPathHoldsItsDrawThreeLevelsDown(t *testing.T) {
	// The rule does not run out at two: every level a path passes through is held.
	f := engine(12)
	tmpl := `{"format":"{p.geo.town.name}/{p.geo.town.zip} {p.geo.region}","p":[{"format":"{geo}","geo":[
		{"format":"{town}","region":"Norrbotten","town":[
			{"format":"{name}","name":"Kiruna","zip":"98100"},
			{"format":"{name}","name":"Luleå","zip":"97200"}]},
		{"format":"{town}","region":"Skåne","town":[
			{"format":"{name}","name":"Malmö","zip":"21100"},
			{"format":"{name}","name":"Lund","zip":"22100"}]}]}]}`
	want := map[string]bool{
		"Kiruna/98100 Norrbotten": true, "Luleå/97200 Norrbotten": true,
		"Malmö/21100 Skåne": true, "Lund/22100 Skåne": true,
	}
	seen := map[string]bool{}
	for i := 0; i < 600; i++ {
		got := mustRender(t, f, tmpl)
		if !want[got] {
			t.Fatalf("draw %d = %q, want a town with its own zip and region", i, got)
		}
		seen[got] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("600 draws produced %v, want all four towns", seen)
	}
}

func TestPathWalksVariantsOfDifferentShape(t *testing.T) {
	// One variant holds addr as a plain template where another holds a choice.
	// Both carry the path, so both must walk — the walk reads what it drew, never
	// assuming a shape.
	f := engine(13)
	tmpl := `{"format":"{p.addr.city}/{p.addr.zip}","p":[
		{"format":"{addr}","addr":{"format":"{city}","city":"Kiruna","zip":"98100"}},
		{"format":"{addr}","addr":[
			{"format":"{city}","city":"Malmö","zip":"21100"},
			{"format":"{city}","city":"Lund","zip":"22100"}]}]}`
	for i := 0; i < 400; i++ {
		got := mustRender(t, f, tmpl)
		if got != "Kiruna/98100" && got != "Malmö/21100" && got != "Lund/22100" {
			t.Fatalf("draw %d = %q, want a city with its own zip", i, got)
		}
	}
}

func TestDeepPathsUnderOneHeadStayIndependentWhereTheyDiverge(t *testing.T) {
	// Two paths share only what they actually share: a common prefix is one draw,
	// and each keeps its own draw past the point they part.
	f := engine(14)
	tmpl := `{"format":"{p.a.v}{p.b.v}","p":[{"format":"x",
		"a":[{"format":"{v}","v":"1"},{"format":"{v}","v":"2"}],
		"b":[{"format":"{v}","v":"1"},{"format":"{v}","v":"2"}]}]}`
	seen := map[string]bool{}
	for i := 0; i < 400; i++ {
		seen[mustRender(t, f, tmpl)] = true
	}
	for _, want := range []string{"11", "12", "21", "22"} {
		if !seen[want] {
			t.Fatalf("400 draws produced %v, want every combination including %q", seen, want)
		}
	}
}

func TestEmptyPathSegmentIsRejected(t *testing.T) {
	// "{a.}", "{.b}" and "{a..b}" are unfinished paths. A field really named ""
	// would otherwise make them resolve, which reads as a typo that worked.
	rejected := map[string]string{
		"trailing dot": `{"format":"[{a.}]","a":{"format":"x","":["V"]}}`,
		"leading dot":  `{"format":"[{.b}]","":{"format":"{b}","b":["V"]}}`,
		"double dot":   `{"format":"[{a..b}]","a":{"format":"x","":{"format":"{b}","b":["V"]}}}`,
	}
	for name, file := range rejected {
		_, err := New([]string{writeData(t, map[string]string{"cat": file})})
		if err == nil {
			t.Errorf("%s: New = nil error, want the unfinished path rejected", name)
			continue
		}
		if !strings.Contains(err.Error(), "empty segment") {
			t.Errorf("%s: New = %v, want it to name the empty segment", name, err)
		}
	}
}

func TestCycleThroughAPathTokenIsRejected(t *testing.T) {
	// A path token is a render edge like any other, so a cycle routed through one
	// must be caught at New. Reaching render would be fatal: the recursion never
	// terminates, and a stack overflow cannot be recovered.
	_, err := New([]string{writeData(t, map[string]string{
		"a": `{"format":"{p.x}","p":{"format":"{x}","x":{"format":"{..a}"}}}`,
	})})
	if err == nil || !strings.Contains(err.Error(), "reference cycle") {
		t.Fatalf("New = %v, want the cycle through {p.x} rejected", err)
	}
}

func TestBoundPathIsReachableByFake(t *testing.T) {
	// Binding changes how a format reads a sibling, not what List and Fake offer:
	// the sub-fields stay addressable on their own.
	dir := writeData(t, map[string]string{"address": places("{place.postal-code} {place.locality}")})
	f, err := New([]string{dir}, WithSeed(9))
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	for _, path := range []string{"address", "address.place", "address.place.locality", "address.place.postal-code"} {
		if _, err := f.Fake(path); err != nil {
			t.Errorf("Fake(%q) = %v, want it to render", path, err)
		}
	}
}
