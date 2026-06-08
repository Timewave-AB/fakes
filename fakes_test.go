package fakes

import (
	"strings"
	"testing"
)

// newFakes creates a faker over a single data directory, failing on error. Most
// tests load one dir; newFakesN loads several (last-loaded wins on conflicts).
func newFakes(t *testing.T, dir string, opts ...Option) *Fakes {
	t.Helper()
	return newFakesN(t, []string{dir}, opts...)
}

func newFakesN(t *testing.T, dirs []string, opts ...Option) *Fakes {
	t.Helper()
	f, err := New(dirs, opts...)
	if err != nil {
		t.Fatalf("New(%q): %v", dirs, err)
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

func TestNewMissingDirectory(t *testing.T) {
	_, err := New([]string{"data/de_DE"})
	if err == nil || !strings.Contains(err.Error(), "de_DE") {
		t.Fatalf("New(missing) error = %v, want it to name the path", err)
	}
}

func TestWithSeedIsDeterministic(t *testing.T) {
	a, b := newFakes(t, "data/sv_SE", WithSeed(42)), newFakes(t, "data/sv_SE", WithSeed(42))
	for i := 0; i < 50; i++ {
		if x, y := fake(t, a, "person"), fake(t, b, "person"); x != y {
			t.Fatalf("same seed diverged at %d: %q != %q", i, x, y)
		}
	}
}

func TestDifferentSeedsDiffer(t *testing.T) {
	a, b := newFakes(t, "data/en_US", WithSeed(1)), newFakes(t, "data/en_US", WithSeed(2))
	for i := 0; i < 50; i++ {
		if fake(t, a, "person") != fake(t, b, "person") {
			return
		}
	}
	t.Fatal("seeds 1 and 2 produced identical sequences")
}
