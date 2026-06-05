# fakes

A Go library for generating fake data, built for **internationalization**. It
exists because the existing Go fake libraries lacked the locale coverage and
format control we needed.

- **Standards first** — formats, names and structures follow international and
  local standards first and foremost.
- **Locale-aware** — names, addresses, postal codes and phone numbers follow
  per-locale data and formats. Locales are always full language + territory
  tags (`sv_SE`, never `sv`).
- **Data lives in JSON** — all source data is recursive JSON on disk, read when
  you create a faker and then served from memory. Add or change locales without
  touching the library.
- **Composable** — templates nest without limit: weighted choices, character
  classes and sub-templates combine to model any format.
- **Reproducible** — seed a faker and it emits the same sequence every time.
- **Zero dependencies** — standard library only.

## Install

```sh
go get github.com/Timewave-AB/fakes
```

Requires Go 1.26+.

## Usage

Point `New` at a locale directory, then generate values by category path with
`Fake`. The first path segment names a category (a JSON file); deeper dotted
segments descend into it.

```go
package main

import (
	"fmt"
	"log"

	"github.com/Timewave-AB/fakes"
)

func main() {
	f, err := fakes.New("./locales/sv_SE")
	if err != nil {
		log.Fatal(err)
	}

	for _, path := range []string{"person", "address", "phone", "address.locality"} {
		v, err := f.Fake(path)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%-18s %s\n", path, v)
	}
}
```

```
person             Sara Eriksson
address            Kungsvägen 68
                   379 17 Stockholm
phone              072-402 91 67
address.locality   Linköping
```

Seed a faker for reproducible output — same seed + locale yields an identical
sequence, handy for stable tests:

```go
a, _ := fakes.New("./locales/sv_SE", fakes.WithSeed(42))
b, _ := fakes.New("./locales/sv_SE", fakes.WithSeed(42))
av, _ := a.Fake("person")
bv, _ := b.Fake("person")
av == bv // true
```

A `*Fakes` is **not** safe for concurrent use — create one per goroutine.

### Locales

The library ships a ready-to-use set under [`locales/`](locales) (`en_US`,
`sv_SE`). Pass `New` the path to one of them, a copy, or your own directory —
anywhere on disk.

Each ships these categories, formatted per locale (e.g. `date` is `MM/DD/YYYY`
in `en_US`, `YYYY-MM-DD` in `sv_SE`; `ssn` is a US SSN vs a Swedish
personnummer): `address`, `color`, `company`, `date`, `email`, `ip`, `person`,
`phone`, `price`, `sentence`, `ssn`, `time`, `url`, `username`, `uuid`,
`version`, `word`.

The directory's name is the locale and must be a full tag (`sv_SE`, never
`sv`). Any casing or separator is accepted and canonicalised (`sv-se`,
`SV_SE` → `sv_SE`); a non-full name returns an error.

## Locale data format

Each JSON file in a locale directory is a **category** named after the file
(`address.json` → `address`), rendered by `Fake("address")`. Drop in a new file
or folder — no code change, no recompile.

Every value is a **node**, one of three shapes, nestable without limit:

| Node | JSON | Meaning |
|------|------|---------|
| literal | `"Malmö"` | emitted verbatim — never formatted |
| choice | `["a", "b", …]` | one element, picked at random |
| template | `{"format": "…", …}` | a format string plus the named sub-nodes it references |

**Weight.** A template node may carry a `weight` (default `1`) to skew its odds
within a choice:

```json
[
  { "format": "#070-000 00 00", "weight": 10 },
  { "format": "#01-000 00 00" },
  { "format": "#010-000 00 00" }
]
```

**Format string.** Every character is literal except:

| Token | Expands to |
|-------|-----------|
| `0` | digit 0–9 |
| `1` | digit 1–9 |
| `A` | letter A–Z |
| `a` | letter a–z |
| `#` | escape — the next char is literal (`#0` → `0`, `##` → `#`) |
| `{name}` | render the sibling field `name` |

`{a|b}` renders one of the sibling fields `a` or `b`, chosen at random.

**Putting it together** (`person.json`):

```json
[
  {
    "format": "{prefix}{femalefirst|malefirst} {last}",
    "femalefirst": ["Anna", "Astrid", "Elin"],
    "malefirst": ["Anders", "Erik", "Gustav"],
    "last": [
      { "format": "{first}sson", "first": ["Ander", "Erik", "Karl"] },
      ["Berg", "von Flemming"]
    ],
    "prefix": [
      "",
      { "format": "{string} ", "string": ["dr", "prof"], "weight": 0.05 }
    ]
  }
]
```

This yields e.g. `Anna Eriksson`, `Erik Berg`, or rarely `dr Astrid von Flemming`.
Any field is reachable by dotted path — `Fake("person.last")` renders just a
surname; choices along the path are resolved at random.

### Performance

Each file is parsed, validated and weight-indexed once, in `New`. After that a
`Fake` call costs about what its output costs — it scans the chosen format and
renders nested tokens, independent of how large your lists are:

- Picking from a list is **O(1)** whatever its length — a 10-name list and a
  100 000-name list cost the same.
- Giving entries a `weight` makes that list's pick **O(log n)** instead (a
  search over cumulative weights). Still tiny, but an unweighted list is the
  cheapest — only add `weight` where you actually want skew.
- Long `format` strings, deep nesting and many `{tokens}` add cost in
  proportion to the output produced.

## Development

Everything runs in Docker — **no local tooling beyond Docker is needed**.
Source is bind-mounted; build caches persist in the `gocache` volume.

```sh
docker compose run --rm test    # run tests
docker compose run --rm ci      # vet + format check + tests
docker compose run --rm cover   # tests with coverage
docker compose run --rm build   # compile the library
docker compose run --rm vet     # go vet
docker compose run --rm dev     # interactive shell
```

Commands that rewrite source keep your file ownership when run with `--user`:

```sh
docker compose run --rm --user "$(id -u):$(id -g)" fmt   # gofmt -w .
docker compose run --rm --user "$(id -u):$(id -g)" tidy  # go mod tidy
```

`docker build .` runs `go vet` and the tests, so it works as a CI gate too.

## Layout

```
fakes.go      Fakes, New, options, seeding
template.go   Fake, the recursive renderer (choices, format strings, paths)
locale.go     locale loading + tag parsing
locales/      shipped locale data (JSON)
```

To add a category, drop a JSON file into a locale directory; to add a locale,
add a directory named with its full tag.

## License

MIT — see [LICENSE](LICENSE).
