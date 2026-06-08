# fakes

A Go library and CLI for generating fake data, built for
**internationalization**. It exists because the existing Go fake libraries
lacked the locale coverage and format control we needed.

- **Standards first** — formats, names and structures follow international and
  local standards first and foremost.
- **Locale-aware** — names, addresses, postal codes and phone numbers follow
  per-locale data and formats. The shipped data is organised by full locale tag
  (`sv_SE`), but the engine treats folders as plain namespaces — name yours
  anything.
- **Data lives in JSON** — all source data is recursive JSON on disk, read when
  you create a faker and then served from memory. Add or change data without
  touching the library. Behavior belongs in data too: the engine grows a
  built-in function only for what data can't express (a checksum, a time-based
  id), never for what character classes and choices already do.
- **Composable** — templates nest without limit: weighted choices, character
  classes and sub-templates combine to model any format.
- **Reproducible** — seed a faker and it emits the same sequence every time.
  Every built-in draws only from that seed — no wall-clock, no `crypto/rand` —
  so determinism holds end to end.
- **Zero dependencies** — standard library only.

## CLI

Install the `fakes` command, then give it one or more `-path` flags and one or
more data directories — it prints one value per path to stdout. Each dot segment
descends one level: folders, then the category (a JSON file), then fields inside it.

```sh
go install github.com/Timewave-AB/fakes/cmd/fakes@latest

fakes -path person ./data/sv_SE               # Sara Eriksson
fakes -path person.last ./data/sv_SE          # Eriksson  (dotted path into a category)
fakes -path sv_SE.person ./data               # point at the tree; the folder is a segment
fakes -path person -path address ./data/sv_SE # several paths, one value each
fakes -path word ./data/sv_SE ./mydata        # layer dirs; the last wins a name clash
fakes -seed 42 -path address ./data/sv_SE
fakes -repeat 3 -path person ./data/sv_SE              # three values, one per line
fakes -repeat 3 -separator ', ' -path word ./data/sv_SE  # nät, barn, sol
```

`-path` is repeatable, and flags come before the data directories. `-repeat N`
renders each path N times — every render an independent draw — and all emitted
values (`repeat` × paths) are joined by `-separator` (default a newline, so
values land one per line).

Without installing, run it from a checkout with `go run ./cmd/fakes …`. Exit
codes: `0` success, `1` runtime error (missing dir, unknown path), `2` misuse.

### Generating a file from a custom template

A category is just a JSON file in a data directory, so you can drop in your
own and render it — no code change. Save this as `data/sv_SE/sql.json`:

```json
{
  "format": "INSERT INTO users V#ALUES({sql-username});",
  "sql-username": {
    "format": "'{username}'",
    "repeat": 3,
    "separator": "),(",
    "username": ["pixelfox", "snork", "turbohund", "blip", "zoom", "wahoo"]
  }
}
```

`sql-username` renders `'{username}'` `repeat` times and joins the results with
the `),(` separator; the outer `V#ALUES(…)` wraps that into one valid row list.
(`#A` escapes the literal `A`, which a format string would otherwise read as a
letter token — see [Data format](#data-format).)

```sh
fakes -seed 1 -path sql ./data/sv_SE
# INSERT INTO users VALUES('zoom'),('wahoo'),('blip');
```

Raise the template's `repeat` for more rows per statement; use the CLI's
`-repeat` for more statements — together they build a whole seed file:

```sh
fakes -repeat 100 -path sql ./data/sv_SE > seed.sql
```

## Library

```sh
go get github.com/Timewave-AB/fakes   # requires Go 1.22+ (for math/rand/v2)
```

Point `New` at one or more data directories, then generate values by path with
`Fake`. Each dot segment descends one level: folders, then the category (a JSON
file), then fields inside it.

```go
package main

import (
	"fmt"
	"log"

	"github.com/Timewave-AB/fakes"
)

func main() {
	f, err := fakes.New([]string{"./data/sv_SE"})
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
a, _ := fakes.New([]string{"./data/sv_SE"}, fakes.WithSeed(42))
b, _ := fakes.New([]string{"./data/sv_SE"}, fakes.WithSeed(42))
av, _ := a.Fake("person")
bv, _ := b.Fake("person")
av == bv // true
```

A `*Fakes` is **not** safe for concurrent use — create one per goroutine.

## Data

The library ships a ready-to-use set under [`data/`](data), organised by locale
(`en_US`, `sv_SE`). Point either tool at the whole tree, a single locale, a
copy, or your own directory — anywhere on disk; no naming rules.

A directory is just a namespace. Each JSON file is a category named after the
file; each subdirectory is a dot-path segment — folders nest exactly like JSON
objects do. So `data/sv_SE/person.json` is `Fake("person")` when you point at
`data/sv_SE`, or `Fake("sv_SE.person")` when you point at `data`.

Pass several directories and they merge, left to right: matching folders combine
by their children, and any other clash is won by the last directory loaded. That
lets you layer your own data over the built-ins without copying them:

```go
fakes.New([]string{"./data/sv_SE", "./mydata"}) // mydata overrides on a clash
```

Each shipped locale carries these categories, formatted per locale (e.g. `date`
is `MM/DD/YYYY` in `en_US`, `YYYY-MM-DD` in `sv_SE`; `ssn` is a US SSN vs a
Swedish personnummer): `address`, `color`, `company`, `date`, `email`, `ip`,
`person`, `phone`, `price`, `sentence`, `ssn`, `time`, `url`, `username`,
`uuid`, `version`, `word`.

## Data format

Each JSON file in a data directory is a **category** named after the file
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

Only template (object) nodes carry `weight` — a bare string or nested array in a
choice always counts as `1`. Weights are checked when you create the faker: a
negative, non-numeric, or all-zero set is rejected at `New`, so a typo fails
fast instead of silently skewing output.

**Repeat.** A template node may carry a `repeat` (default `1`) to render its
`format` that many times — each render an independent pick — joined by
`separator` (default `""`):

```json
{ "format": "{word}", "repeat": 3, "separator": " ", "word": ["foo", "bar", "baz"] }
```

This yields e.g. `bar foo baz`. `repeat` must be a positive integer and
`separator` a string, both checked at `New`.

**Functions.** A `{name()}` token calls a built-in function instead of rendering
a field. `{luhn()}` appends a Luhn check digit over the digits emitted **so far**
in the current format (non-digits skipped but kept); unknown functions or wrong
argument counts are rejected at `New`. This is what makes a generated Swedish
personnummer valid — its last digit is a Luhn checksum over the nine before it:

```json
{ "format": "00{mmdd}-000{luhn()}", "mmdd": [ … ] }
```

renders e.g. `811218-987`, then `{luhn()}` appends `6` → `811218-9876`. Place it
after its payload (it reads what is to its left). The buffer it reads is
per-expansion, so nesting keeps fixed parts out of the sum — e.g. a 12-digit
form prefixes the century outside the checksummed core:

```json
{ "format": "{century}{core}", "century": ["19", "20"],
  "core": { "format": "00{mmdd}-000{luhn()}", "mmdd": [ … ] } }
```

A function must be deterministic in the seeded rng (no wall-clock), so a seeded
faker stays reproducible.

**References.** A `{..path}` token renders a node from the **data root** instead
of a sibling field — the dot path is the one `Fake` takes, resolved across every
loaded directory. One category can borrow another, even across folders or layered
data dirs:

```json
{ "format": "Hej, {..en_US.person}!" }
```

renders e.g. `Hej, Pat Smith!`. References are bound when you create the faker, so
a path that is unknown, names a folder, or steps through a multi-variant choice
fails at `New`. A reference must not lead back to its own value (directly or
through a chain), or rendering won't terminate.

**Format string.** Every character is literal except:

| Token | Expands to |
|-------|-----------|
| `0` | digit 0–9 |
| `1` | digit 1–9 |
| `A` | letter A–Z |
| `a` | letter a–z |
| `#` | escape — the next char is literal (`#0` → `0`, `##` → `#`) |
| `{name}` | render the sibling field `name` |
| `{name()}` | call a built-in function (see **Functions**) |
| `{..path}` | render the node at a dot path from the data root (see **References**) |

`{a|b}` renders one of the sibling fields `a` or `b`, chosen at random; an arm
may be a `{..path}` reference too (`{name|..en_US.person}`).

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

Tests run against the latest Go by default. Set `GO_VERSION` to check the lowest
supported version too:

```sh
GO_VERSION=1.22 docker compose run --rm test   # lowest supported
docker compose run --rm test                   # latest
```

## Layout

```
fakes.go        Fakes, New, options, seeding
template.go     Fake, the recursive renderer (choices, format strings, paths)
data.go         data loading: folders/files -> namespace tree, multi-path merge
cmd/fakes/      the `fakes` CLI (New + Fake over stdout)
data/           shipped data (JSON), organised by locale
```

To add a category, drop a JSON file into a data directory; to add a locale, add
a subdirectory of JSON files.

## License

MIT — see [LICENSE](LICENSE).
