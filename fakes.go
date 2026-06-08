// Package fakes generates fake data from recursive, locale-specific JSON
// templates.
//
// Locale data lives in JSON on disk, not in Go. Point [New] at a locale
// directory, then generate values by category path:
//
//	f, _ := fakes.New("./locales/sv_SE", fakes.WithSeed(42))
//	f.Fake("address")          // "Storgatan 12\n234 56 Göteborg"
//	f.Fake("address.locality") // "Göteborg"
//
// The directory's name is the locale and must be a full tag ("sv_SE", never
// "sv"). The JSON template format is documented in the README.
//
// A [Fakes] is not safe for concurrent use; create one per goroutine.
package fakes

import (
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"path/filepath"
)

// Fakes generates fake data for a single locale. Create one with [New].
type Fakes struct {
	rand       *rand.Rand
	locale     string
	categories map[string]node // category name -> compiled node tree
}

type config struct {
	seed   uint64
	seeded bool
}

// Option configures a [Fakes].
type Option func(*config)

// WithSeed makes output reproducible: two fakers with the same seed and locale
// emit identical sequences.
func WithSeed(seed uint64) Option {
	return func(c *config) { c.seed, c.seeded = seed, true }
}

// New builds a faker from a locale directory (e.g. "./locales/sv_SE"). The
// directory's name is the locale tag and must be a full tag like "sv_SE"; each
// JSON file in it becomes a category named after the file (address.json ->
// "address"). It errors on a non-full tag, a missing directory, or invalid JSON.
func New(dir string, opts ...Option) (*Fakes, error) {
	name, ok := canonicalLocale(filepath.Base(dir))
	if !ok {
		return nil, fmt.Errorf("fakes: %q is not a full locale tag like \"sv_SE\"", filepath.Base(dir))
	}
	cats, err := loadLocale(dir)
	if err != nil {
		return nil, fmt.Errorf("fakes: %s: %w", name, err)
	}
	var c config
	for _, opt := range opts {
		opt(&c)
	}
	return &Fakes{rand: newRand(c.seed, c.seeded), locale: name, categories: cats}, nil
}

// Locale returns the faker's canonical locale tag.
func (f *Fakes) Locale() string { return f.locale }

func newRand(seed uint64, seeded bool) *rand.Rand {
	if !seeded {
		var b [16]byte
		_, _ = crand.Read(b[:])
		return rand.New(rand.NewPCG(binary.LittleEndian.Uint64(b[:8]), binary.LittleEndian.Uint64(b[8:])))
	}
	return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
}
