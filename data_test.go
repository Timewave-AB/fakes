package fakes

import (
	"regexp"
	"testing"
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

	en := newFakes(t, "locales/en_US", WithSeed(1))
	sv := newFakes(t, "locales/sv_SE", WithSeed(1))
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
