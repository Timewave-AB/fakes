package fakes

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// writeLocale creates a locale directory named tag under a temp root, with one
// JSON file per category, and returns its path.
func writeLocale(t *testing.T, tag string, categories map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range categories {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNewLoadsCustomLocale(t *testing.T) {
	dir := writeLocale(t, "is_IS", map[string]string{"greeting": `["hej", "hallå"]`})
	f := newFakes(t, dir, WithSeed(1))
	if got := fake(t, f, "greeting"); got != "hej" && got != "hallå" {
		t.Fatalf("greeting = %q, want hej or hallå", got)
	}
}

func TestNewCanonicalizesDirName(t *testing.T) {
	// A directory named with a lowercase region resolves to the canonical tag.
	dir := writeLocale(t, "sv_se", map[string]string{"x": `["a"]`})
	if got := newFakes(t, dir).Locale(); got != "sv_SE" {
		t.Fatalf("Locale() = %q, want sv_SE", got)
	}
}

func TestNewEmptyLocaleErrors(t *testing.T) {
	if _, err := New(writeLocale(t, "xx_XX", nil)); err == nil {
		t.Fatal("New(empty locale) = nil error")
	}
}

func TestShippedSwedishPhone(t *testing.T) {
	f := newFakes(t, "locales/sv_SE", WithSeed(11))
	re := regexp.MustCompile(`^0\d{1,2}-\d{3} \d{2} \d{2}$`)
	for i := 0; i < 50; i++ {
		if n := fake(t, f, "phone"); !re.MatchString(n) {
			t.Fatalf("phone %q does not match %s", n, re)
		}
	}
}

func TestShippedSwedishAddress(t *testing.T) {
	f := newFakes(t, "locales/sv_SE", WithSeed(3))
	digit := regexp.MustCompile(`\d`)
	for i := 0; i < 30; i++ {
		a := fake(t, f, "address")
		if !regexp.MustCompile(`\n`).MatchString(a) || !digit.MatchString(a) {
			t.Fatalf("address %q is not a multi-line address with a number", a)
		}
	}

	localities := map[string]bool{
		"Göteborg": true, "Linköping": true, "Malmö": true, "Stockholm": true,
		"Uppsala": true, "Västerås": true, "Örebro": true,
	}
	for i := 0; i < 30; i++ {
		if c := fake(t, f, "address.locality"); !localities[c] {
			t.Fatalf("locality %q not in sv_SE data", c)
		}
	}
}

func TestShippedPersonHasParts(t *testing.T) {
	for _, locale := range []string{"locales/sv_SE", "locales/en_US"} {
		f := newFakes(t, locale, WithSeed(7))
		for i := 0; i < 30; i++ {
			if name := fake(t, f, "person"); len(name) < 3 || !regexp.MustCompile(`\S \S`).MatchString(name) {
				t.Fatalf("%s person %q lacks first and last name", locale, name)
			}
		}
	}
}

func TestShippedUSPhone(t *testing.T) {
	f := newFakes(t, "locales/en_US", WithSeed(11))
	re := regexp.MustCompile(`^(\(\d{3}\) \d{3}-\d{4}|\d{3}-\d{3}-\d{4})$`)
	for i := 0; i < 50; i++ {
		if n := fake(t, f, "phone"); !re.MatchString(n) {
			t.Fatalf("phone %q does not match %s", n, re)
		}
	}
}
