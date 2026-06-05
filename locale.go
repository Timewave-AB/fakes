package fakes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// loadLocale parses and compiles every JSON file in dir into a category node
// tree, keyed by the file's base name without extension (address.json ->
// "address"). Compiling here means structural errors surface at New, not at
// Fake time.
func loadLocale(dir string) (map[string]node, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no .json files in %s", dir)
	}
	categories := make(map[string]node, len(files))
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var raw any
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
		n, err := compile(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
		categories[strings.TrimSuffix(filepath.Base(file), ".json")] = n
	}
	return categories, nil
}

var localeTag = regexp.MustCompile(`^[a-z]{2,3}_[A-Z]{2}$`)

// canonicalLocale normalises a tag to "lang_REGION" form, accepting '-' or '_'
// separators and any casing. It reports whether the result is a full tag.
func canonicalLocale(s string) (string, bool) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "-", "_"), "_")
	if len(parts) != 2 {
		return "", false
	}
	tag := strings.ToLower(parts[0]) + "_" + strings.ToUpper(parts[1])
	if !localeTag.MatchString(tag) {
		return "", false
	}
	return tag, true
}
