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

// BenchmarkNew measures load+compile+validate of the whole shipped tree.
func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := New([]string{"data"}, WithSeed(1)); err != nil {
			b.Fatal(err)
		}
	}
}
