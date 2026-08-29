package fakes

import (
	"os"
	"path/filepath"
	"testing"
)

func benchFaker(b *testing.B, dir string) *Fakes {
	b.Helper()
	f, err := New([]string{dir}, WithSeed(1))
	if err != nil {
		b.Fatal(err)
	}
	return f
}

func benchPath(b *testing.B, dir, path string) {
	f := benchFaker(b, dir)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.Fake(path); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPerson(b *testing.B)     { benchPath(b, "data/sv_SE", "person") }
func BenchmarkAddress(b *testing.B)    { benchPath(b, "data/sv_SE", "address") }
func BenchmarkWord(b *testing.B)       { benchPath(b, "data/sv_SE", "word") }
func BenchmarkCreditcard(b *testing.B) { benchPath(b, "data/misc", "creditcard") }
func BenchmarkSSN(b *testing.B)        { benchPath(b, "data/sv_SE", "ssn") }
func BenchmarkUUIDv7(b *testing.B)     { benchPath(b, "data/misc", "uuid") }

func tmpData(b *testing.B, name, body string) string {
	b.Helper()
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
		b.Fatal(err)
	}
	return dir
}

func BenchmarkCalc(b *testing.B) {
	dir := tmpData(b, "inv", `{"format":"{net} x {qty} = {calc(net * qty, 2)}","net":["19.99"],"qty":["3"]}`)
	benchPath(b, dir, "inv")
}

func BenchmarkLongLiteral(b *testing.B) {
	dir := tmpData(b, "sql", `{"format":"INSERT INTO customers (id, name, city) V#ALUES (#1#2#3, '{word}', '{word}');","word":["alpha","beta","gamma","delta"]}`)
	benchPath(b, dir, "sql")
}

func BenchmarkRepeat(b *testing.B) {
	dir := tmpData(b, "many", `{"format":"{word}","repeat":20,"separator":", ","word":["alpha","beta","gamma","delta"]}`)
	benchPath(b, dir, "many")
}

// BenchmarkBound measures a format that binds a head and reads two paths from it —
// what a correlated pair costs against BenchmarkUnbound, the same output drawn from
// two independent fields.
func BenchmarkBound(b *testing.B) {
	dir := tmpData(b, "addr", `{"format":"{place.postal-code} {place.locality}","place":[
		{"format":"{locality}","locality":"Stockholm","postal-code":{"format":"#100 00"}},
		{"format":"{locality}","locality":"Tranås","postal-code":{"format":"#5#7#3 00"}}]}`)
	benchPath(b, dir, "addr")
}

func BenchmarkUnbound(b *testing.B) {
	dir := tmpData(b, "addr", `{"format":"{postal-code} {locality}",
		"postal-code":[{"format":"#100 00"},{"format":"#5#7#3 00"}],
		"locality":["Stockholm","Tranås"]}`)
	benchPath(b, dir, "addr")
}

// BenchmarkBoundDeep reads two paths through an intermediate level, the shape the
// depth question is about.
func BenchmarkBoundDeep(b *testing.B) {
	dir := tmpData(b, "addr", `{"format":"{p.addr.city} {p.addr.zip}","p":[
		{"format":"{addr}","addr":{"format":"{city}","city":["Stockholm","Tranås"],"zip":{"format":"#100 00"}}}]}`)
	benchPath(b, dir, "addr")
}

// BenchmarkBoundWide reads ten paths from one row: the point where the maps an
// expansion holds outgrow a single bucket.
func BenchmarkBoundWide(b *testing.B) {
	dir := tmpData(b, "row", `{"format":"{r.a}{r.b}{r.c}{r.d}{r.e}{r.f}{r.g}{r.h}{r.i}{r.j}","r":[
		{"format":"x","a":"1","b":"2","c":"3","d":"4","e":"5","f":"6","g":"7","h":"8","i":"9","j":"0"},
		{"format":"y","a":"A","b":"B","c":"C","d":"D","e":"E","f":"F","g":"G","h":"H","i":"I","j":"J"}]}`)
	benchPath(b, dir, "row")
}

// BenchmarkNew measures load+compile+validate of the whole shipped tree.
func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := New([]string{"data"}, WithSeed(1)); err != nil {
			b.Fatal(err)
		}
	}
}
