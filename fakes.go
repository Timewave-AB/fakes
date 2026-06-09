// Package fakes generates fake data from recursive JSON templates.
//
// Data lives in JSON on disk, not in Go. Point [New] at one or more data
// directories; folders and files become a dot-path namespace, then generate
// values by path:
//
//	f, _ := fakes.New([]string{"./data/sv_SE"}, fakes.WithSeed(42))
//	f.Fake("address")          // "Storgatan 12\n234 56 Göteborg"
//	f.Fake("address.locality") // "Göteborg"
//
// A subdirectory is a namespace segment, so pointing at "./data" instead reaches
// a category as "sv_SE.address". Several directories are merged left to right;
// the last one wins on a name clash, so you can layer custom data over the
// built-ins. The JSON template format is documented in the README.
//
// A [Fakes] is not safe for concurrent use; create one per goroutine.
package fakes

import (
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"sort"
)

// Fakes generates fake data from a loaded namespace tree. Create one with [New].
type Fakes struct {
	rand       *session
	categories map[string]node // root namespace: name -> compiled node tree
}

// session is one faker's mutable render state: the seeded rng plus the {seq()}
// counters. Scoping it to the Fakes means sequences (and randomness) belong to
// that faker and reset when you create a new one. Embedding *rand.Rand makes a
// *session satisfy the rng interface the renderer draws from.
type session struct {
	*rand.Rand
	counters map[string]uint64
}

// next returns the next value (counting from 1) of the named {seq()} counter.
func (s *session) next(key string) uint64 {
	s.counters[key]++
	return s.counters[key]
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

// New builds a faker from one or more data directories (e.g. "./data/sv_SE").
// Each JSON file becomes a category named after the file (address.json ->
// "address") and each subdirectory a namespace segment; directories are merged
// in order, the last winning a name clash. It errors on a missing directory,
// invalid JSON, or no data found.
func New(paths []string, opts ...Option) (*Fakes, error) {
	cats, err := loadData(paths)
	if err != nil {
		return nil, fmt.Errorf("fakes: %w", err)
	}
	var c config
	for _, opt := range opts {
		opt(&c)
	}
	return &Fakes{rand: newRand(c.seed, c.seeded), categories: cats}, nil
}

// List returns the sorted dotted paths Fake can render: every category, the
// dotted fields within a template, and folder segments — descending transparently
// through single-variant choices the way a reference does. A multi-variant choice
// is one path (its pick is random); its items are not separately addressable. It's
// the discoverable map of what a loaded data set offers, powering the CLI's -list.
func (f *Fakes) List() []string {
	var out []string
	var walk func(prefix string, n node)
	walk = func(prefix string, n node) {
		switch n := n.(type) {
		case *group:
			for name, c := range n.children {
				walk(join(prefix, name), c)
			}
		case *choice:
			if len(n.items) == 1 { // a single-variant choice is a transparent wrapper
				walk(prefix, n.items[0])
				return
			}
			out = append(out, prefix)
		case *template:
			out = append(out, prefix)
			for name, c := range n.fields {
				if isRef(name) { // a bound {..path} reference, not an authored field
					continue
				}
				walk(join(prefix, name), c)
			}
		case literal:
			out = append(out, prefix)
		}
	}
	walk("", &group{children: f.categories})
	sort.Strings(out)
	return out
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func newRand(seed uint64, seeded bool) *session {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	if !seeded {
		var b [16]byte
		_, _ = crand.Read(b[:])
		r = rand.New(rand.NewPCG(binary.LittleEndian.Uint64(b[:8]), binary.LittleEndian.Uint64(b[8:])))
	}
	return &session{Rand: r, counters: map[string]uint64{}}
}
