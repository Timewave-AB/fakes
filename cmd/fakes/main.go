// Command fakes prints fake values from one or more data directories.
//
//	fakes -path person ./data/sv_SE              # a full person
//	fakes -path person.last ./data/sv_SE         # just the surname (dotted path)
//	fakes -path sv_SE.person ./data              # point at the tree, address by folder
//	fakes -path person -path address ./data/sv_SE # several paths, one per line
//	fakes -path person ./data/sv_SE ./mydata     # layer custom data; last dir wins
//	fakes -seed 42 -path address ./data/sv_SE
//
// It is a thin CLI over the fakes library: New(dirs) then Fake(path).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Timewave-AB/fakes"
)

const usage = `Usage: fakes -path P [-path P]... [-seed N] [-repeat N] [-separator S] <data-dir>...

  -path P       a category, or a dotted path into one (person, person.last); repeatable
  <data-dir>    one or more data directories, e.g. ./data/sv_SE (last wins on clash)
  -seed N       seed for reproducible output
  -repeat N     render each path N times (default 1)
  -separator S  string between emitted values (default newline)`

// stringList collects a repeatable string flag, preserving order.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is main's testable core: it returns the process exit code (0 ok, 1
// runtime error, 2 misuse) and writes only to the given streams.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fakes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usage) }
	seed := fs.Uint64("seed", 0, "seed for reproducible output")
	repeat := fs.Int("repeat", 1, "render each path this many times")
	sep := fs.String("separator", "\n", "string between emitted values")
	var paths stringList
	fs.Var(&paths, "path", "a category or dotted path to render (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(paths) == 0 || fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	if *repeat < 1 {
		fmt.Fprintln(stderr, "repeat must be a positive integer")
		return 2
	}

	var opts []fakes.Option
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "seed" {
			opts = append(opts, fakes.WithSeed(*seed))
		}
	})

	f, err := fakes.New(fs.Args(), opts...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	vals := make([]string, 0, *repeat*len(paths))
	for range *repeat {
		for _, p := range paths {
			v, err := f.Fake(p)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			vals = append(vals, v)
		}
	}
	fmt.Fprintln(stdout, strings.Join(vals, *sep))
	return 0
}
