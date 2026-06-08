// Command fakes prints one fake value from one or more data directories.
//
//	fakes ./data/sv_SE person          # a full person
//	fakes ./data/sv_SE person.last     # just the surname (dotted path)
//	fakes ./data sv_SE.person          # point at the tree, address by folder
//	fakes ./data/sv_SE ./mydata person # layer custom data; last dir wins
//	fakes -seed 42 ./data/sv_SE address
//
// It is a thin CLI over the fakes library: New(dirs) then Fake(path).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Timewave-AB/fakes"
)

const usage = `Usage: fakes [-seed N] <data-dir>... <path>

  <data-dir>  one or more data directories, e.g. ./data/sv_SE (last wins on clash)
  <path>      a category, or a dotted path into one (person, person.last)
  -seed N     seed for reproducible output`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is main's testable core: it returns the process exit code (0 ok, 1
// runtime error, 2 misuse) and writes only to the given streams.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fakes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usage) }
	seed := fs.Uint64("seed", 0, "seed for reproducible output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 2 {
		fs.Usage()
		return 2
	}

	var opts []fakes.Option
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "seed" {
			opts = append(opts, fakes.WithSeed(*seed))
		}
	})

	dirs, path := fs.Args()[:fs.NArg()-1], fs.Arg(fs.NArg()-1)
	f, err := fakes.New(dirs, opts...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	v, err := f.Fake(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, v)
	return 0
}
