package fakes

import (
	"os"
	"path/filepath"
	"testing"
)

// writeData builds a temp data directory from a map of relative path (without
// ".json") -> file content, creating parent folders as needed. The directory's
// name carries no meaning anymore, so callers pick any layout they like —
// including nested folders, which become dot-path segments.
func writeData(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name+".json")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNewLoadsAnyDirName(t *testing.T) {
	// No locale tag required: a directory named anything loads fine.
	dir := writeData(t, map[string]string{"greeting": `["hej", "hallå"]`})
	f := newFakes(t, dir, WithSeed(1))
	if got := fake(t, f, "greeting"); got != "hej" && got != "hallå" {
		t.Fatalf("greeting = %q, want hej or hallå", got)
	}
}

func TestNewEmptyDirErrors(t *testing.T) {
	if _, err := New([]string{writeData(t, nil)}); err == nil {
		t.Fatal("New(empty dir) = nil error")
	}
}

// TestFoldersBecomeDotPaths checks the core of the new model: a subfolder is a
// namespace, so data/<loc>/person.json is reachable as "<loc>.person".
func TestFoldersBecomeDotPaths(t *testing.T) {
	dir := writeData(t, map[string]string{
		"sv_SE/greeting": `["hej"]`,
		"en_US/greeting": `["hi"]`,
	})
	f := newFakes(t, dir, WithSeed(1))
	if got := fake(t, f, "sv_SE.greeting"); got != "hej" {
		t.Fatalf("sv_SE.greeting = %q, want hej", got)
	}
	if got := fake(t, f, "en_US.greeting"); got != "hi" {
		t.Fatalf("en_US.greeting = %q, want hi", got)
	}
}

// TestNestedFoldersAndJSON crosses both nesting kinds in one dot path: folders
// a/b/c, then deep JSON fields inside the file at the end of that path.
func TestNestedFoldersAndJSON(t *testing.T) {
	dir := writeData(t, map[string]string{
		"a/b/c/thing": `{"format":"{x}","x":{"format":"{y}","y":{"format":"{z}","z":["leaf"]}}}`,
	})
	f := newFakes(t, dir, WithSeed(1))
	// Folders a.b.c, file thing, then JSON fields x.y.z — one continuous path.
	if got := fake(t, f, "a.b.c.thing.x.y.z"); got != "leaf" {
		t.Fatalf("a.b.c.thing.x.y.z = %q, want leaf", got)
	}
	// Rendering the file resolves the same chain top-down.
	if got := fake(t, f, "a.b.c.thing"); got != "leaf" {
		t.Fatalf("a.b.c.thing = %q, want leaf", got)
	}
}

func TestRenderingAFolderErrors(t *testing.T) {
	dir := writeData(t, map[string]string{"sv_SE/greeting": `["hej"]`})
	f := newFakes(t, dir, WithSeed(1))
	if _, err := f.Fake("sv_SE"); err == nil {
		t.Fatal("Fake(folder) = nil error, want a not-a-value error")
	}
}

// TestMultiPathLastWins loads two dirs; on a name clash the later dir wins, and
// non-clashing entries from both are reachable (data combines).
func TestMultiPathLastWins(t *testing.T) {
	a := writeData(t, map[string]string{"greeting": `["from-a"]`, "only-a": `["a"]`})
	b := writeData(t, map[string]string{"greeting": `["from-b"]`, "only-b": `["b"]`})
	f := newFakesN(t, []string{a, b}, WithSeed(1))
	if got := fake(t, f, "greeting"); got != "from-b" {
		t.Fatalf("greeting = %q, want from-b (last loaded wins)", got)
	}
	if got := fake(t, f, "only-a"); got != "a" {
		t.Fatalf("only-a = %q, want a", got)
	}
	if got := fake(t, f, "only-b"); got != "b" {
		t.Fatalf("only-b = %q, want b", got)
	}
}

// TestMultiPathMergesFolders checks that clashing folders merge by their
// children rather than replacing wholesale: each dir adds a file to sv_SE, and
// a per-file clash inside still resolves last-wins.
func TestMultiPathMergesFolders(t *testing.T) {
	a := writeData(t, map[string]string{"sv_SE/person": `["from-a"]`, "sv_SE/shared": `["a"]`})
	b := writeData(t, map[string]string{"sv_SE/company": `["from-b"]`, "sv_SE/shared": `["b"]`})
	f := newFakesN(t, []string{a, b}, WithSeed(1))
	if got := fake(t, f, "sv_SE.person"); got != "from-a" {
		t.Fatalf("sv_SE.person = %q, want from-a (folder merged, not replaced)", got)
	}
	if got := fake(t, f, "sv_SE.company"); got != "from-b" {
		t.Fatalf("sv_SE.company = %q, want from-b", got)
	}
	if got := fake(t, f, "sv_SE.shared"); got != "b" {
		t.Fatalf("sv_SE.shared = %q, want b (last loaded wins)", got)
	}
}
