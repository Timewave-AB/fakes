package fakes

import "testing"

// TestSeededOutputIsStable pins the exact bytes a seeded faker emits for each
// piece of the token grammar, so a change to how a format is scanned or rendered
// cannot quietly shift the rng stream that reproducibility depends on.
func TestSeededOutputIsStable(t *testing.T) {
	dir := writeData(t, map[string]string{
		"alt":     `{"format":"{a|b}","a":["A"],"b":["B"]}`,
		"calc":    `{"format":"{net} x {qty} = {calc(net * qty, 2)}","net":["19.99"],"qty":["3"]}`,
		"classes": `{"format":"00-11-AA-aa"}`,
		"escapes": `{"format":"#0#1#A#a##{x}","x":["!"]}`,
		"funcs":   `{"format":"{hex(6)} {int(10,99)} {float(0,1,3)} {nanoid(5)} {seq()}"}`,
		"nested":  `{"format":"{outer}","outer":[{"format":"{inner}-00","inner":["i"]}]}`,
		"ref":     `{"format":"see {..alt}"}`,
		"repeat":  `{"format":"{w}","repeat":4,"separator":",","w":["x","y","z"]}`,
		"sums":    `{"format":"9{d}{luhn()} {e}{ean()} {m}{mod11()}","d":["012345678901234"],"e":["123456789012"],"m":["12345678"]}`,
		"weights": `[{"format":"big","weight":9},{"format":"tiny","weight":1}]`,
	})
	want := map[string][]string{
		"alt":     {"A", "B", "A", "A"},
		"calc":    {"19.99 x 3 = 59.97", "19.99 x 3 = 59.97", "19.99 x 3 = 59.97", "19.99 x 3 = 59.97"},
		"classes": {"49-74-VY-gj", "38-68-RP-xo", "47-68-IB-hs", "91-89-LP-gk"},
		"escapes": {"01Aa#!", "01Aa#!", "01Aa#!", "01Aa#!"},
		"funcs":   {"0016a2 33 0.649 k4Kwj 1", "a00c07 84 0.175 6UjGv 2", "f70eb8 12 0.390 p5p6g 3", "9048a3 90 0.531 I3Aqv 4"},
		"nested":  {"i-89", "i-67", "i-47", "i-27"},
		"ref":     {"see A", "see A", "see B", "see A"},
		"repeat":  {"z,y,z,y", "z,z,y,y", "z,z,x,z", "x,z,y,y"},
		"sums":    {"90123456789012348 1234567890124 123456780", "90123456789012348 1234567890124 123456780", "90123456789012348 1234567890124 123456780", "90123456789012348 1234567890124 123456780"},
		"weights": {"big", "big", "big", "big"},
	}
	for path := range want {
		f := newFakes(t, dir, WithSeed(42))
		for i, expect := range want[path] {
			if got := fake(t, f, path); got != expect {
				t.Errorf("%s draw %d = %q, want %q", path, i, got, expect)
			}
		}
	}
}
