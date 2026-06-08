package fakes

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestShippedDataCategories asserts every shipped locale emits only well-formed
// values for each data category. en and sv patterns differ where the format is
// locale-specific (date, time, ssn, company, price, ...); the rest are shared.
func TestShippedDataCategories(t *testing.T) {
	letters := regexp.MustCompile(`^[\pL'-]+$`)
	semver := regexp.MustCompile(`^v?\d+\.\d+\.\d+(-(alpha|beta|rc)\.\d+)?$`)
	ip := regexp.MustCompile(`^((\d{1,3}\.){3}\d{1,3}|([0-9a-f]{4}:){7}[0-9a-f]{4})$`)
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	color := regexp.MustCompile(`^(#[0-9a-f]{6}|[\pL ]+)$`)
	url := regexp.MustCompile(`^https?://([a-z0-9-]+\.)+[a-z]{2,}(/[a-z0-9./-]*)?$`)
	email := regexp.MustCompile(`^[a-z0-9._-]+@([a-z0-9-]+\.)+[a-z]{2,}$`)
	uname := regexp.MustCompile(`^[a-z][a-z0-9._]*$`)

	cases := []struct {
		path   string
		en, sv *regexp.Regexp
	}{
		{"word", letters, letters},
		{"sentence",
			regexp.MustCompile(`^[A-Z].* .*[.!?]$`),
			regexp.MustCompile(`^[A-ZÅÄÖ].* .*[.!?]$`)},
		{"date",
			regexp.MustCompile(`^(0[1-9]|1[0-2])/(0[1-9]|[12]\d|3[01])/\d{4}$`),
			regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$`)},
		{"time",
			regexp.MustCompile(`^([1-9]|1[0-2]):[0-5]\d (AM|PM)$`),
			regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d(:[0-5]\d)?$`)},
		{"ssn",
			regexp.MustCompile(`^[1-9]\d{2}-\d{2}-\d{4}$`),
			regexp.MustCompile(`^\d{2}(0[1-9]|1[0-2])(0[1-9]|[12]\d|3[01])-\d{4}$`)},
		{"version", semver, semver},
		{"email", email, email},
		{"ip", ip, ip},
		{"company",
			regexp.MustCompile(`^.+ (Inc\.|LLC|Corp\.|Co\.|Group|Ltd\.)$`),
			regexp.MustCompile(`^.+ (AB|HB|KB)$`)},
		{"color", color, color},
		{"url", url, url},
		{"uuid", uuid, uuid},
		{"username", uname, uname},
		{"price",
			regexp.MustCompile(`^\$\d{1,3}(,\d{3})?\.\d{2}$`),
			regexp.MustCompile(`^\d{1,3}( \d{3})?(,\d{2})? kr$`)},
	}

	en := newFakes(t, "data/en_US", WithSeed(1))
	sv := newFakes(t, "data/sv_SE", WithSeed(1))
	for _, c := range cases {
		for i := 0; i < 200; i++ {
			if v := fake(t, en, c.path); !c.en.MatchString(v) {
				t.Fatalf("en_US %s = %q, want %s", c.path, v, c.en)
			}
			if v := fake(t, sv, c.path); !c.sv.MatchString(v) {
				t.Fatalf("sv_SE %s = %q, want %s", c.path, v, c.sv)
			}
		}
	}
}

// TestSwedishPersonnummer checks the two rules the shape regex can't: the date
// is a real calendar date (so month-length variants never emit e.g. Apr 31 or
// Feb 30) and the trailing digit is a valid Luhn checksum over the other nine.
func TestSwedishPersonnummer(t *testing.T) {
	sv := newFakes(t, "data/sv_SE", WithSeed(1))
	sawLongMonthEnd := false
	for i := 0; i < 2000; i++ {
		v := fake(t, sv, "ssn")
		d := digitsOnly(v)
		if len(d) != 10 {
			t.Fatalf("ssn %q has %d digits, want 10", v, len(d))
		}
		if _, err := time.Parse("060102", d[:6]); err != nil { // 2-digit year, real-date check
			t.Fatalf("ssn %q is not a valid calendar date: %v", v, err)
		}
		if !luhnValid(d) {
			t.Fatalf("ssn %q fails the Luhn check", v)
		}
		if d[4:6] == "31" {
			sawLongMonthEnd = true
		}
	}
	if !sawLongMonthEnd {
		t.Fatal("never generated a 31st — 31-day months are not reaching their last day")
	}
}

func TestShippedSwedishPhone(t *testing.T) {
	f := newFakes(t, "data/sv_SE", WithSeed(11))
	re := regexp.MustCompile(`^0\d{1,2}-\d{3} \d{2} \d{2}$`)
	for i := 0; i < 50; i++ {
		if n := fake(t, f, "phone"); !re.MatchString(n) {
			t.Fatalf("phone %q does not match %s", n, re)
		}
	}
}

func TestShippedSwedishAddress(t *testing.T) {
	f := newFakes(t, "data/sv_SE", WithSeed(3))
	digit := regexp.MustCompile(`\d`)
	for i := 0; i < 30; i++ {
		a := fake(t, f, "address")
		if !regexp.MustCompile(`\n`).MatchString(a) || !digit.MatchString(a) {
			t.Fatalf("address %q is not a multi-line address with a number", a)
		}
	}

	locality := regexp.MustCompile(`^\p{L}+( \p{L}+)*$`)
	for i := 0; i < 30; i++ {
		if c := fake(t, f, "address.locality"); !locality.MatchString(c) {
			t.Fatalf("locality %q is not a Swedish place name", c)
		}
	}
}

func TestShippedPersonHasParts(t *testing.T) {
	for _, dir := range []string{"data/sv_SE", "data/en_US"} {
		f := newFakes(t, dir, WithSeed(7))
		for i := 0; i < 30; i++ {
			if name := fake(t, f, "person"); len(name) < 3 || !regexp.MustCompile(`\S \S`).MatchString(name) {
				t.Fatalf("%s person %q lacks first and last name", dir, name)
			}
		}
	}
}

func TestShippedUSPhone(t *testing.T) {
	f := newFakes(t, "data/en_US", WithSeed(11))
	re := regexp.MustCompile(`^(\(\d{3}\) \d{3}-\d{4}|\d{3}-\d{3}-\d{4})$`)
	for i := 0; i < 50; i++ {
		if n := fake(t, f, "phone"); !re.MatchString(n) {
			t.Fatalf("phone %q does not match %s", n, re)
		}
	}
}

// TestShippedNamespacedTree loads the whole data/ tree (not a single locale) and
// reaches each locale through its folder segment: data/sv_SE/person -> sv_SE.person.
func TestShippedNamespacedTree(t *testing.T) {
	f := newFakes(t, "data", WithSeed(1))
	for _, path := range []string{"sv_SE.person", "en_US.person", "sv_SE.address.locality"} {
		if got := fake(t, f, path); got == "" {
			t.Fatalf("Fake(%q) returned empty", path)
		}
	}
	if _, err := f.Fake("person"); err == nil {
		t.Fatal("Fake(person) = nil error; categories should be namespaced under the locale folder")
	}
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// luhnValid verifies a full number (payload + trailing check digit). It doubles
// from the second-from-right, independent of the generator's own Luhn code.
func luhnValid(s string) bool {
	sum, double := 0, false
	for i := len(s) - 1; i >= 0; i-- {
		n := int(s[i] - '0')
		if double {
			if n *= 2; n > 9 {
				n -= 9
			}
		}
		double = !double
		sum += n
	}
	return sum%10 == 0
}
