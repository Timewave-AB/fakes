package fakes

import (
	"strings"
	"testing"
)

// newFakes creates a faker for a locale directory, failing the test on error.
func newFakes(t *testing.T, dir string, opts ...Option) *Fakes {
	t.Helper()
	f, err := New(dir, opts...)
	if err != nil {
		t.Fatalf("New(%q): %v", dir, err)
	}
	return f
}

// fake generates a value, failing the test on error.
func fake(t *testing.T, f *Fakes, path string) string {
	t.Helper()
	s, err := f.Fake(path)
	if err != nil {
		t.Fatalf("Fake(%q): %v", path, err)
	}
	return s
}

func TestNewRejectsNonFullLocale(t *testing.T) {
	for _, bad := range []string{"locales/sv", "locales/se", "locales/swedish", "locales/sv_S"} {
		if _, err := New(bad); err == nil {
			t.Errorf("New(%q) = nil error, want full-locale error", bad)
		}
	}
}

func TestNewMissingDirectory(t *testing.T) {
	_, err := New("locales/de_DE")
	if err == nil || !strings.Contains(err.Error(), "de_DE") {
		t.Fatalf("New(missing) error = %v, want it to name the locale", err)
	}
}

func TestWithSeedIsDeterministic(t *testing.T) {
	a, b := newFakes(t, "locales/sv_SE", WithSeed(42)), newFakes(t, "locales/sv_SE", WithSeed(42))
	for i := 0; i < 50; i++ {
		if x, y := fake(t, a, "person"), fake(t, b, "person"); x != y {
			t.Fatalf("same seed diverged at %d: %q != %q", i, x, y)
		}
	}
}

func TestDifferentSeedsDiffer(t *testing.T) {
	a, b := newFakes(t, "locales/en_US", WithSeed(1)), newFakes(t, "locales/en_US", WithSeed(2))
	for i := 0; i < 50; i++ {
		if fake(t, a, "person") != fake(t, b, "person") {
			return
		}
	}
	t.Fatal("seeds 1 and 2 produced identical sequences")
}
