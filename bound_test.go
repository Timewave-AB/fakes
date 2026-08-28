package fakes

import (
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

func TestBareTokenBindsWhenTheFormatAlsoTakesAPath(t *testing.T) {
	// A head any dotted token addresses is drawn once, so a bare {place} in the
	// same format reads that same draw rather than a second one.
	f := engine(2)
	for i := 0; i < 300; i++ {
		got := mustRender(t, f, places("{place.postal-code} {place}"))
		if !agree.MatchString(got) {
			t.Fatalf("draw %d = %q, want the bare token to read the bound draw", i, got)
		}
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
			`contains a dot`,
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
