// Command fakes prints one fake value from a locale directory.
//
//	fakes ./locales/sv_SE person        # a full person
//	fakes ./locales/sv_SE person.last   # just the surname (dotted path)
//	fakes -seed 42 ./locales/sv_SE address
//
// It is a thin CLI over the fakes library: New(dir) then Fake(path).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Timewave-AB/fakes"
)

const usage = `Usage: fakes [-seed N] <locale-dir> <path>

  <locale-dir>  a locale directory, e.g. ./locales/sv_SE
  <path>        a category, or a dotted path into one (person, person.last)
  -seed N       seed for reproducible output`

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
	if fs.NArg() != 2 {
		fs.Usage()
		return 2
	}

	var opts []fakes.Option
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "seed" {
			opts = append(opts, fakes.WithSeed(*seed))
		}
	})

	f, err := fakes.New(fs.Arg(0), opts...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	v, err := f.Fake(fs.Arg(1))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, v)
	return 0
}
