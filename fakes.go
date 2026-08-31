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

// List returns the sorted dotted paths Fake can render: every category, the dotted
// fields within a template, and folder segments — descending transparently through
// single-variant choices the way a reference does. A choice consumes no segment, so
// a path continues through a multi-variant one only where every variant carries it,
// which is the rule Fake applies too: List is the set of paths Fake accepts.
func (f *Fakes) List() []string {
	var out []string
	for _, name := range sortedNames(f.categories) {
		for _, p := range paths(f.categories[name]) {
			out = append(out, join(name, p))
		}
	}
	sort.Strings(out)
	return out
}

// paths lists the dot paths addressable from n, relative to it, where "" is n
// itself. A group has no value of its own, so it contributes only its children's.
func paths(n node) []string {
	switch n := n.(type) {
	case *group:
		var out []string
		for _, name := range sortedNames(n.children) {
			for _, p := range paths(n.children[name]) {
				out = append(out, join(name, p))
			}
		}
		return out
	case *template:
		out := []string{""}
		for _, name := range sortedNames(n.fields) {
			if isRef(name) { // a binding, not a path segment
				continue
			}
			for _, p := range paths(n.fields[name]) {
				out = append(out, join(name, p))
			}
		}
		return out
	case *choice:
		if len(n.items) == 1 {
			return paths(n.items[0])
		}
		out := []string{""}
		for p := range n.shared {
			out = append(out, p)
		}
		return out
	case literal:
		return []string{""}
	}
	return nil
}

// sharedPaths is the sub-paths every item carries — the only ones a path may step
// through a multi-variant choice to reach. It intersects, bailing as soon as the set
// is empty, which is immediate for a choice of plain strings.
func sharedPaths(items []node) map[string]bool {
	shared := subPaths(items[0])
	for _, it := range items[1:] {
		if len(shared) == 0 {
			return nil
		}
		next := subPaths(it)
		for p := range shared {
			if !next[p] {
				delete(shared, p)
			}
		}
	}
	return shared
}

// subPaths is paths(n) as a set, without the empty path that means n itself.
func subPaths(n node) map[string]bool {
	out := map[string]bool{}
	for _, p := range paths(n) {
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func join(prefix, name string) string {
	switch {
	case prefix == "":
		return name
	case name == "":
		return prefix
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
