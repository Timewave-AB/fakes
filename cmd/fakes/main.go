// Command fakes prints one fake value from one or more data directories.
//
//	fakes -data-path ./data/sv_SE person                     # a full person
//	fakes -data-path ./data/sv_SE person.last                # just the surname (dotted path)
//	fakes -data-path ./data sv_SE.person                     # point at the tree, address by folder
//	fakes -data-path ./data/sv_SE -data-path ./mydata person # layer custom data; last dir wins
//	fakes -seed 42 -data-path ./data/sv_SE address
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

const usage = `Usage: fakes -data-path D [-data-path D]... [-seed N] [-repeat N] [-separator S] <path>

  -data-path D  a data directory, e.g. ./data/sv_SE (repeatable; last wins on clash)
  <path>        a category, or a dotted path into one (person, person.last)
  -seed N       seed for reproducible output
  -repeat N     render the path N times (default 1)
  -separator S  string between repeated values (default newline)`

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
	repeat := fs.Int("repeat", 1, "render the path this many times")
	sep := fs.String("separator", "\n", "string between repeated values")
	var dirs stringList
	fs.Var(&dirs, "data-path", "a data directory to load (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(dirs) == 0 || fs.NArg() != 1 {
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

	path := fs.Arg(0)
	f, err := fakes.New(dirs, opts...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	vals := make([]string, *repeat)
	for i := range vals {
		if vals[i], err = f.Fake(path); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintln(stdout, strings.Join(vals, *sep))
	return 0
}
