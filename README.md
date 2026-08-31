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
- **Reproducible** — seed a faker and it emits the same sequence every time, for a
  given version of the data: changing how a value is composed shifts the stream for
  that value and for everything drawn after it in the same faker.
  Every built-in draws only from that seed — no wall-clock, no `crypto/rand` —
  so determinism holds end to end.
- **Zero dependencies** — standard library only.

## CLI

Install the `fakes` command, then give it one or more `-data-path` directories
and a path — it prints one value to stdout. Each dot segment descends one level:
folders, then the category (a JSON file), then fields inside it.

```sh
go install github.com/Timewave-AB/fakes/cmd/fakes@latest

fakes -data-path ./data/sv_SE person               # Sara Eriksson
fakes -data-path ./data/sv_SE person.last          # Eriksson  (dotted path into a category)
fakes -data-path ./data sv_SE.person               # point at the tree; the folder is a segment
fakes -data-path ./data/sv_SE -data-path ./mydata word  # layer dirs; the last wins a name clash
fakes -seed 42 -data-path ./data/sv_SE address
fakes -repeat 3 -data-path ./data/sv_SE person             # three values, one per line
fakes -repeat 3 -separator ', ' -data-path ./data/sv_SE word  # nät, barn, sol
fakes -data-path ./data/sv_SE -list                        # every path this data offers
```

`-data-path` is repeatable (last wins a name clash) and the path comes last —
all flags must precede it. `-repeat N` renders the path N times — each an
independent draw — joined by `-separator` (default a newline, so values land one
per line). Not sure what a data set offers? `-list` prints every path you can ask
for; `-version` prints the build version.

Without installing, run it from a checkout with `go run ./cmd/fakes …`. Exit
codes: `0` success (including `-list`, `-version`, `-h`), `1` runtime error
(missing dir, unknown path), `2` misuse.

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
fakes -seed 1 -data-path ./data/sv_SE sql
# INSERT INTO users VALUES('zoom'),('wahoo'),('blip');
```

Raise the template's `repeat` for more rows per statement; use the CLI's
`-repeat` for more statements — together they build a whole seed file:

```sh
fakes -repeat 100 -data-path ./data/sv_SE sql > seed.sql
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

`f.List()` returns the sorted paths the loaded data offers — the categories, their
dotted fields and folder segments (what the CLI's `-list` prints). Every path it
lists renders.

A `*Fakes` is **not** safe for concurrent use — create one per goroutine.

## Data

The library ships a ready-to-use set under [`data/`](data): one folder per locale
(`en_US`, `sv_SE`) plus a locale-neutral `misc` folder. Point either tool at the
whole tree, a single folder, a copy, or your own directory — anywhere on disk. A
category or folder name must not use `.`, `|`, `(` or `}` (see [Data
format](#data-format)), and dot-prefixed entries are skipped, so a data directory
can also be a checkout.

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
`version`, `word`.

`data/misc` carries locale-neutral categories — universal data that isn't tied to
a language or region:

- ids & networking: `uuid` (a proper random v4), `mac`, `objectid` (24 hex chars,
  MongoDB ObjectID-shaped — the leading bytes are random, not a real timestamp),
  `creditcard` (per-network numbers ending in a valid `{luhn()}` digit)
- reference codes: `currency` (ISO 4217), `country` (ISO 3166), `language` (ISO
  639), `timezone` (IANA)
- web & systems: `mimetype`, `httpstatus`, `useragent`
- misc: `coordinate` (a lat/long point), `emoji`, `car`

Many carry dotted sub-fields — `currency.symbol`, `country.alpha2`,
`mimetype.ext`, `httpstatus.code`, `car.maker`. A time-ordered v7 UUID can't be
expressed as data, so it's the `{uuid()}` builtin instead (see **Functions**).

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

`format`, `weight`, `repeat` and `separator` are the only options; **any other key
is a field**. So write `seperator` and you get a field by that name while the
option stays unset. `New` rejects an option that cannot take effect — a
`separator` without a `repeat` above 1, a `weight` outside a choice — and a name
using a character the grammars reserve: `.` separates the segments of a path, `|`
the arms of a token, `(` opens a function call and `}` ends the token, so a name
carrying one is a name no format could ever spell. An empty name goes the same
way — it is no path segment at all, so `List` never offers it. That holds for a
category, a folder and a field alike.

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

A function must be deterministic (no wall-clock), so a seeded faker stays
reproducible. A time-based id (UUID v7, ULID) therefore draws its timestamp from
the rng, not the clock — the result is a valid, reproducible value, not a real
point in time.

There are four kinds. **Derivations** read the digits emitted so far, so put them
after their payload; **generators** read only the rng, so they stand alone; one
**session counter** (`seq`) advances state held on the faker; and one
**computation** (`calc`) evaluates arithmetic over sibling fields. Arguments are
validated at `New` (a bad count, range, country, or expression fails fast); a
length, count or decimal place beyond a sane maximum is rejected there too, so a
fat-fingered `hex(2000000000)` can't try to allocate gigabytes at render.

| Function | Kind | Emits |
|----------|------|-------|
| `{luhn()}` | derivation | Luhn check digit (mod-10) over preceding digits |
| `{mod11()}` | derivation | weighted mod-11 check char (weights 2–7 from the right); `X` when it would be 10 |
| `{ean()}` | derivation | EAN-13 / UPC-A / ISBN-13 / GTIN check digit |
| `{uuid()}` | generator | UUID v7 (v4 ships as data — see [Data](#data)) |
| `{ulid()}` | generator | ULID, 26-char Crockford base32 |
| `{nanoid(n)}` | generator | URL-safe Nano ID, `n` chars |
| `{hex(n)}` | generator | `n` lowercase hex digits |
| `{base64(n)}` | generator | `n` random bytes, base64 |
| `{int(min,max)}` | generator | uniform integer in `[min, max]` |
| `{float(min,max,dp)}` | generator | number in `[min, max]` with `dp` decimals |
| `{iban(CC)}` | generator | a length- and mod-97-valid IBAN for country `CC` (BE, DE, DK, ES, FI, NO, SE) |
| `{seq()}`, `{seq(name)}` | session counter | next integer (from 1) in this faker's sequence; `name` selects an independent counter |
| `{calc(expr)}`, `{calc(expr,dp)}` | computation | value of an arithmetic expression over number literals and sibling fields; `dp` rounds |

`{ean()}` is also the ISBN-13 check (an ISBN-13 *is* an EAN-13 — build the 978/979
prefix in data and call `{ean()}`). `{iban()}` is a generator, not a derivation:
an IBAN's check digits sit *before* the account number, which a left-to-right
reader can't reach, so it emits the whole value (a generic numeric BBAN — valid
length and checksum, not real bank routing).

`{seq()}`'s counter lives on the faker, so it spans `Fake` calls (and `repeat`)
and resets when you build a new faker — `seq` is reproducible by being ordered,
not random. It's the natural fit for a primary-key column in the SQL example above.

**Computation.** `{calc(expr)}` evaluates an arithmetic expression — `+ - * /`,
parentheses and unary minus, the usual precedence — and emits the result. Operands
are number literals and **sibling field names**, each rendered then read as a
number; an optional second arg rounds to that many decimals (`{calc(net * qty, 2)}`),
otherwise the value prints in minimal form. A hyphen is always subtraction, so a
hyphenated field name can't be an operand. The expression is checked at `New`
(parse, and that every name is a real field):

```json
{ "format": "{net} x {qty} = {calc(net * qty, 2)}", "net": ["19.99"], "qty": ["3"] }
```

renders `19.99 x 3 = 59.97`. A field a `calc` reads is drawn **once per
expansion** and held, so the operand shown is the operand computed — give `net`
three prices and the line still multiplies the one it printed. The hold covers
every reading of that name in the format, so `{w} {w} {calc(w)}` is one value
three times; a name no `calc` reads is unaffected, and `{word} {word}` still draws
twice. A field that doesn't render to a number yields `NaN`, and a division by
zero yields `Inf`; both print rather than failing the render.

**References.** A `{..path}` token renders a node from the **data root** instead
of a sibling field — the dot path is the one `Fake` takes, resolved across every
loaded directory. One category can borrow another, even across folders or layered
data dirs:

```json
{ "format": "Hej, {..en_US.person}!" }
```

renders e.g. `Hej, Pat Smith!`. References are bound when you create the faker, so
a path that is unknown, names a folder, or steps through a multi-variant choice
fails at `New`. A reference that leads back to its own value (directly, mutually,
or through a chain) is a cycle that would never finish rendering, so it too is
rejected at `New`.

**Correlated fields.** A `{name.field}` token addresses a **path** into a sibling,
and a sibling addressed that way is drawn **once per expansion** — so several
tokens read one row. That is how two facts that belong together, such as a
locality and the postal code that really covers it, stay together:

```json
{ "format": "{street} {number}\n{place.postal-code} {place.locality}",
  "place": [
    { "format": "{locality}", "locality": "Stockholm", "postal-code": { "format": "#100 00" }, "weight": 975 },
    { "format": "{locality}", "locality": "Tranås",    "postal-code": { "format": "#5#7#3 00" }, "weight": 18 }
  ] }
```

renders e.g. `Kungsgatan 35` / `176 99 Stockholm`, never a Stockholm postal code
beside Tranås. Each row carries its own `weight`, so how often a place appears is data
too. The rule holds both ways: two tails of one head come from the same row, and
one path read twice reads one value (`{p.first} … {p.first}@…` gives one name).

**One draw, one spelling.** The hold above is what a dotted token reads, and a
`{calc()}` operand reads its sibling the same way (see **Computation**). A format
may not both *render* a level and *read a path into* it — `{p}` beside
`{p.first}`, or `{place}` beside `{place.locality}`.
Rendering a level expands it afresh while a path reads the level's held draw, so
the two would disagree; naming the fields you want is the one spelling that
always agrees, and the other is a load error. This covers every way a level can
be rendered: a token, a `{calc()}` operand, and a `{..path}` reference — wherever
the reference sits, including in a field the format renders.

A `{calc()}` operand is held on its own terms too, so the same fence guards it:
`{..cat.net} x 2 = {calc(net * 2, 2)}` names one field two ways and is a load
error, with no path token anywhere. A calc renders its operand whole, so the hold
pins what that render settled, following the operand's plain `{field}` tokens —
`{net.v}` and `{..cat.net.v}` are rejected alike. It stops at a `{..path}`, where
the operand's own value ends and a shared source begins: two names drawing from
one referenced category are two draws, as `{word} {word}` is, so two dice over
one `{..die}` are fine.

```text
token {p} renders a level that {p.first} reads a path into; name the fields you want instead
```

For the same reason a path may not read into a level carrying a `repeat`: it
reads one draw, so the repeat could never apply.

A path is held at **every level it passes through**, not just the first, so the
facts can nest as deeply as they belong:

```json
{ "format": "{p.geo.town.name} {p.geo.town.zip}, {p.geo.region}", "p": [ … ] }
```

renders `Kiruna 98100, Norrbotten` — the town, the zip that covers it and the
region it sits in all come from one draw. Two paths part company exactly where
they diverge: `{p.a.v}` and `{p.b.v}` share the row and nothing below it.

The binding lasts for one expansion, so each `repeat` iteration draws again and a
nested template keeps its own. A field no dotted token addresses is unaffected:
`{word} {word}` still draws twice.

`New` checks a path the way `Fake` resolves one: every variant of a multi-variant
choice must carry the whole path, so a row missing a field is named at load:

```text
token {place.postal-code}: field "place": not every variant of this 2-way choice
carries "postal-code"; all carry [locality]
```

The sub-fields stay addressable on their own — `Fake("address.place.locality")`
renders, and `List` advertises it.

**Format string.** Every character is literal except:

| Token | Expands to |
|-------|-----------|
| `0` | digit 0–9 |
| `1` | digit 1–9 |
| `A` | letter A–Z |
| `a` | letter a–z |
| `#` | escape — the next char is literal (`#0` → `0`, `##` → `#`) |
| `{name}` | render the sibling field `name` |
| `{name.field}` | render `field` of one draw of the sibling `name` (see **Correlated fields**) |
| `{name()}` | call a built-in function (see **Functions**) |
| `{..path}` | render the node at a dot path from the data root (see **References**) |

`{a|b}` renders one of the sibling fields `a` or `b`, chosen at random; an arm
may be a `{..path}` reference too (`{name|..en_US.person}`). The arms are picked
evenly and must differ — `{a|a|b}` would skew the odds, which is what `weight` is
for, so a repeated arm is a load error.

Inside a `format`, `0 1 A a` are **always** character classes — so a fixed `0`,
`1`, `A` or `a` must be escaped (`#1`, `#A`) or it becomes random. A format of
`100 Main St` renders e.g. `506 Mdin St` — the `1`, `0`, `0` and `a` were random,
the `M`, `in` and `St` were not. A half-fixed string is the trap: `555-0000`
keeps `555-` and randomises the last four digits. For a value with no
tokens at all, use a bare string node (`"100 Main St"`), emitted verbatim.

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
surname; choices along the path are resolved at random. A path may continue
*through* a choice only where every variant carries the rest of it — every
`currency` variant carries `symbol`, so `currency.symbol` resolves — which keeps a
path from rendering on one call and failing on the next.

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
docker compose run --rm bench   # benchmarks
docker compose run --rm build   # compile the library
docker compose run --rm vet     # go vet
docker compose run --rm dev     # interactive shell
```

Commands that rewrite source keep your file ownership when run with `--user`:

```sh
docker compose run --rm --user "$(id -u):$(id -g)" fmt   # gofmt -w .
docker compose run --rm --user "$(id -u):$(id -g)" tidy  # go mod tidy
```

Every pull request runs `docker compose run --rm ci` against both the latest and
the lowest supported Go, and must pass before it can be merged. `docker build .`
runs the same vet and tests locally.

Tests run against the latest Go by default. Set `GO_VERSION` to check the lowest
supported version too:

```sh
GO_VERSION=1.22.12 docker compose run --rm test   # lowest supported
docker compose run --rm test                      # latest
```

## Layout

```
fakes.go        Fakes, New, List, options, seeding
node.go         the node model and JSON -> node compilation
render.go       Fake and the recursive renderer (choices, format strings, paths, bound draws)
template.go     the {token} grammar: scanning, function and path tokens, validation
reference.go    {..path} binding across the tree, the render graph, and the walks over it
builtins.go     the {name()} function registry and its implementations
calc.go         the {calc()} arithmetic evaluator: parser, eval, validation
data.go         data loading: folders/files -> namespace tree, multi-path merge
cmd/fakes/      the `fakes` CLI (New + Fake/List over stdout)
data/           shipped data (JSON): locale folders + a misc folder
```

To add a category, drop a JSON file into a data directory; to add a locale, add
a subdirectory of JSON files.

## License

MIT — see [LICENSE](LICENSE).
