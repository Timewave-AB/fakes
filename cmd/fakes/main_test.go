package main

import (
	"bytes"
	"strings"
	"testing"
)

const (
	svSE = "../../data/sv_SE"
	enUS = "../../data/en_US"
)

// runOut runs the CLI and returns exit code, stdout, stderr.
func runOut(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRunOutputsValue(t *testing.T) {
	code, out, errb := runOut(svSE, "person")
	if code != 0 {
		t.Fatalf("run = %d, stderr=%q", code, errb)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("empty output, stderr=%q", errb)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output should end with newline, got %q", out)
	}
}

func TestRunDotPath(t *testing.T) {
	// person.last descends to just a surname — never the full "First Last".
	code, full, _ := runOut("-seed", "7", svSE, "person")
	if code != 0 {
		t.Fatalf("person run = %d", code)
	}
	code, last, errb := runOut("-seed", "7", svSE, "person.last")
	if code != 0 {
		t.Fatalf("person.last run = %d, stderr=%q", code, errb)
	}
	if strings.TrimSpace(last) == "" {
		t.Fatal("empty person.last output")
	}
	if last == full {
		t.Errorf("person.last %q should differ from person %q", last, full)
	}
}

func TestRunSeedDeterministic(t *testing.T) {
	_, a, _ := runOut("-seed", "42", svSE, "address")
	_, b, _ := runOut("-seed", "42", svSE, "address")
	if a != b {
		t.Errorf("same seed diverged: %q != %q", a, b)
	}
}

func TestRunUsageOnTooFewArgs(t *testing.T) {
	// Need at least one data dir plus a path; fewer is misuse.
	for _, args := range [][]string{{}, {svSE}} {
		code, _, errb := runOut(args...)
		if code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if !strings.Contains(errb, "Usage") {
			t.Errorf("run(%v) stderr = %q, want usage", args, errb)
		}
	}
}

func TestRunMultipleDirs(t *testing.T) {
	// Several data dirs then a final path: all but the last arg are dirs.
	code, out, errb := runOut(enUS, svSE, "person")
	if code != 0 {
		t.Fatalf("run(multi-dir) = %d, stderr=%q", code, errb)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("empty output for multi-dir run")
	}
}

func TestRunUnknownCategoryFails(t *testing.T) {
	code, _, errb := runOut(svSE, "nope")
	if code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if !strings.Contains(errb, "nope") {
		t.Errorf("stderr %q should name the unknown category", errb)
	}
}

func TestRunMissingDirFails(t *testing.T) {
	code, _, errb := runOut("../../data/nope", "person")
	if code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if errb == "" {
		t.Error("want an error message on stderr")
	}
}
