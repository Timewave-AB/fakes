package fakes

import (
	"encoding/base64"
	"regexp"
	"strconv"
	"testing"
)

func TestBuiltinIDGenerators(t *testing.T) {
	cases := []struct {
		name string
		tmpl string
		re   *regexp.Regexp
	}{
		{"uuid v7", `{"format":"{uuid()}"}`, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)},
		{"ulid", `{"format":"{ulid()}"}`, regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)},
		{"objectid", `{"format":"{objectid()}"}`, regexp.MustCompile(`^[0-9a-f]{24}$`)},
		{"nanoid", `{"format":"{nanoid(21)}"}`, regexp.MustCompile(`^[A-Za-z0-9_-]{21}$`)},
		{"hex", `{"format":"{hex(16)}"}`, regexp.MustCompile(`^[0-9a-f]{16}$`)},
	}
	f := engine(1)
	for _, c := range cases {
		seen := map[string]bool{}
		for i := 0; i < 200; i++ {
			got := mustRender(t, f, c.tmpl)
			if !c.re.MatchString(got) {
				t.Fatalf("%s = %q, want %s", c.name, got, c.re)
			}
			seen[got] = true
		}
		if len(seen) < 190 {
			t.Fatalf("%s not varied: %d uniques in 200", c.name, len(seen))
		}
	}
}

// TestBuiltinGeneratorsReproducible pins the determinism guardrail: every
// generator draws only from the seeded rng, so same seed -> same value.
func TestBuiltinGeneratorsReproducible(t *testing.T) {
	for _, tmpl := range []string{
		`{"format":"{uuid()}"}`, `{"format":"{ulid()}"}`, `{"format":"{objectid()}"}`,
		`{"format":"{nanoid(12)}"}`, `{"format":"{int(1,1000000)}"}`,
		`{"format":"{float(0,1,6)}"}`, `{"format":"{base64(12)}"}`, `{"format":"{iban(SE)}"}`,
	} {
		if a, b := mustRender(t, engine(7), tmpl), mustRender(t, engine(7), tmpl); a != b {
			t.Fatalf("%s not reproducible: %q != %q", tmpl, a, b)
		}
	}
}

func TestBuiltinIntInclusiveRange(t *testing.T) {
	f := engine(1)
	lo, hi := false, false
	for i := 0; i < 1000; i++ {
		n, err := strconv.Atoi(mustRender(t, f, `{"format":"{int(3,7)}"}`))
		if err != nil || n < 3 || n > 7 {
			t.Fatalf("int(3,7) = %d (err %v), out of range", n, err)
		}
		lo, hi = lo || n == 3, hi || n == 7
	}
	if !lo || !hi {
		t.Fatalf("int(3,7) never hit a bound: lo=%v hi=%v (bounds must be inclusive)", lo, hi)
	}
}

func TestBuiltinFloat(t *testing.T) {
	f, re := engine(1), regexp.MustCompile(`^[12]\.\d{3}$`)
	for i := 0; i < 200; i++ {
		if got := mustRender(t, f, `{"format":"{float(1,2,3)}"}`); !re.MatchString(got) {
			t.Fatalf("float(1,2,3) = %q, want d.ddd in [1,2]", got)
		}
	}
}

func TestBuiltinBase64(t *testing.T) {
	got := mustRender(t, engine(1), `{"format":"{base64(9)}"}`)
	if b, err := base64.StdEncoding.DecodeString(got); err != nil || len(b) != 9 {
		t.Fatalf("base64(9) = %q decodes to %d bytes (err %v), want 9", got, len(b), err)
	}
}

// TestBuiltinChecksums fixes the payload with escapes and compares to a
// hand-computed check, like the luhn test. mod-11 emits X when it would be 10.
func TestBuiltinChecksums(t *testing.T) {
	cases := map[string]string{
		`{"format":"#1#2#3#4#5#6#7#8{mod11()}"}`:       "123456785",     // weights 2..7 from the right
		`{"format":"#6{mod11()}"}`:                     "6X",            // remainder 10 -> X
		`{"format":"#4#0#0#6#3#8#1#3#3#3#9#3{ean()}"}`: "4006381333931", // EAN-13 (= ISBN-13) check digit
	}
	f := engine(1)
	for tmpl, want := range cases {
		if got := mustRender(t, f, tmpl); got != want {
			t.Fatalf("%s = %q, want %q", tmpl, got, want)
		}
	}
}

// TestBuiltinSeqPerSession pins seq's contract: a counter from 1, advancing on
// each call, named counters independent, and the whole thing scoped to one faker
// (session) so a fresh faker restarts at 1.
func TestBuiltinSeqPerSession(t *testing.T) {
	f := engine(1)
	for i := 1; i <= 5; i++ {
		if got := mustRender(t, f, `{"format":"{seq()}"}`); got != strconv.Itoa(i) {
			t.Fatalf("seq call %d = %q, want %d", i, got, i)
		}
	}
	if got := mustRender(t, f, `{"format":"{seq(orders)}"}`); got != "1" {
		t.Fatalf("named seq(orders) = %q, want its own count from 1", got)
	}
	if got := mustRender(t, f, `{"format":"{seq()}"}`); got != "6" {
		t.Fatalf("default seq after the named one = %q, want 6 (counters are independent)", got)
	}
	// repeat drives one counter forward across its renders...
	if got := mustRender(t, engine(2), `{"format":"{seq()}","repeat":3,"separator":","}`); got != "1,2,3" {
		t.Fatalf("repeat seq = %q, want 1,2,3", got)
	}
	// ...and a fresh session restarts from 1.
	if got := mustRender(t, engine(2), `{"format":"{seq()}"}`); got != "1" {
		t.Fatalf("new session seq = %q, want 1", got)
	}
}

func TestBuiltinIBAN(t *testing.T) {
	wantLen := map[string]int{"SE": 24, "DE": 22, "NO": 15}
	f := engine(1)
	for cc, n := range wantLen {
		tmpl := `{"format":"{iban(` + cc + `)}"}`
		for i := 0; i < 100; i++ {
			got := mustRender(t, f, tmpl)
			if got[:2] != cc || len(got) != n {
				t.Fatalf("iban(%s) = %q, want %d chars prefixed %s", cc, got, n, cc)
			}
			if !ibanValid(got) {
				t.Fatalf("iban(%s) = %q fails the mod-97 check", cc, got)
			}
		}
	}
}

// ibanValid checks the IBAN mod-97 rule independently of the generator: move the
// first four chars to the end, map letters A-Z to 10-35, the number mod 97 == 1.
func ibanValid(s string) bool {
	r := s[4:] + s[:4]
	rem := 0
	for i := 0; i < len(r); i++ {
		switch c := r[i]; {
		case c >= '0' && c <= '9':
			rem = (rem*10 + int(c-'0')) % 97
		case c >= 'A' && c <= 'Z':
			rem = (rem*100 + int(c-'A') + 10) % 97
		default:
			return false
		}
	}
	return rem == 1
}
