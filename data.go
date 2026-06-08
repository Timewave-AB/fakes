package fakes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadData loads every given directory into one namespace tree and returns its
// root children. A directory becomes a group; each *.json file in it compiles to
// a node keyed by its base name (address.json -> "address"); each subdirectory
// becomes a nested group, so folders turn into dot-path segments. Paths are
// merged left to right: matching groups merge by their children, and any other
// clash (a leaf, or a leaf-vs-group) is won by the last directory loaded. Once
// merged, linkRefs binds every {..path} reference against the final tree.
func loadData(paths []string) (map[string]node, error) {
	root := map[string]node{}
	for _, p := range paths {
		g, err := loadDir(p)
		if err != nil {
			return nil, err
		}
		mergeChildren(root, g.children)
	}
	if len(root) == 0 {
		return nil, fmt.Errorf("no .json data found in %v", paths)
	}
	if err := linkRefs(root); err != nil {
		return nil, err
	}
	return root, nil
}

// loadDir compiles one directory into a group. os.ReadDir yields entries sorted
// by name, so the tree is built deterministically. Empty subdirectories (no JSON
// anywhere under them) are skipped rather than added as empty namespaces.
func loadDir(dir string) (*group, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	g := &group{children: map[string]node{}}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			child, err := loadDir(full)
			if err != nil {
				return nil, err
			}
			if len(child.children) > 0 {
				g.children[e.Name()] = child
			}
			continue
		}
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		var raw any
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("%s: %w", full, err)
		}
		n, err := compile(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", full, err)
		}
		g.children[strings.TrimSuffix(e.Name(), ".json")] = n
	}
	return g, nil
}

// mergeChildren overlays src onto dst. Two groups under the same key merge
// recursively (so locales/categories from several paths combine); every other
// key is replaced, making the last-loaded directory win on a conflict.
func mergeChildren(dst, src map[string]node) {
	for k, v := range src {
		if dg, ok := dst[k].(*group); ok {
			if sg, ok := v.(*group); ok {
				mergeChildren(dg.children, sg.children)
				continue
			}
		}
		dst[k] = v
	}
}
