# structpeek

[![CI](https://github.com/NaomiAmethyst/structpeek/actions/workflows/ci.yml/badge.svg)](https://github.com/NaomiAmethyst/structpeek/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/NaomiAmethyst/structpeek)](https://github.com/NaomiAmethyst/structpeek/releases/latest)
[![License: GPL v3+](https://img.shields.io/badge/license-GPL--3.0--or--later-blue)](LICENSE)

See what's in a data file without seeing what's in it.

`structpeek` reads JSON, JSON Lines, YAML, TOML, INI, CSV, TSV or XML and
writes a [JSON Schema](https://json-schema.org/) (2020-12) for its structure: the
keys, types, formats, optional fields and kinds of record, but none of the
values that could be sensitive. Use it to get to know a file, or give it to
an AI agent instead of the file itself, so it can understand the data
without reading anyone's passwords.

```console
$ head -2 orders.jsonl
{"id": "4b6f1c1e-…", "status": "paid", "total": 41.5, "token": "f3a9c27e5b8d…", "tags": ["gift"]}
{"id": "9d0e2a7b-…", "status": "paid", "total": 12, "token": "0b7e4d9a2c6f…", "tags": []}
$ structpeek orders.jsonl
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "array",
  "items": {
    "type": "object",
    "properties": {
      "id": { "type": "string", "format": "uuid" },
      "status": { "type": "string" },
      "total": { "type": "number", "minimum": 0, "maximum": 100 },
      "token": { "$ref": "#/$defs/highEntropyString" },
      "tags": { "type": "array", "items": { "type": "string" } }
    },
    "required": ["id", "status", "total", "token", "tags"],
    "additionalProperties": false
  },
  "$defs": {
    "highEntropyString": {
      "description": "A string that looks randomly generated, such as a key, token, hash or opaque ID. The values are withheld.",
      "type": "string"
    }
  }
}
```

(Output squashed a little here.) The schema is always valid JSON Schema, and
the data it came from always fits it.

## How it reads data

Every value seen at one place in the data (each record's `status`, every
element of an array, the same key across several files) is an observation,
and each place's schema is inferred from all its observations together. The
aim is a schema that takes in the data's structure rather than fitting the
sample exactly.

**Objects** are closed (`additionalProperties: false`), and keys every
observation has are required. Objects whose keys overlap enough are one
kind, widened to fit: keys some lack become optional, and keys holding
different types get each type. Objects too different to merge stay separate
kinds in an `anyOf`, and if a field such as `type` tells them apart, each
kind gets its value as a `const`.

Objects that each have exactly one key, but not all the same one (a list
of XML elements, or a union tagged by its key, like `{"add": …}` and
`{"remove": …}`), are one schema allowing any of the keys, with
`minProperties` and `maxProperties` of 1.

Objects whose keys are data rather than field names (IDs, numbers, dates,
addresses, tokens, a dictionary of like records, or simply too many keys)
are **maps**: their values are pooled into one schema under
`additionalProperties`, and their keys are withheld.

**Strings** get a `format` (`date-time`, `date`, `time`, `email`, `uri`,
`uuid`, `ipv4`, `ipv6`) if they all share one. Otherwise:

- if they have few distinct values (fewer than 5% of how often they're seen)
  and every value is safe to show, they're an `enum`;
- if they have few distinct values but some aren't safe to show, they're a
  reference to `$defs/lowCardinalityString`;
- if they look randomly generated, they're a reference to
  `$defs/highEntropyString`;
- otherwise they're just strings.

**Numbers** are `integer` if they're all written as integers, else `number`,
with a range rounded out to powers of ten: it shows their scale without
giving any of them away.

**Booleans** are `boolean`, and **nulls** make a place nullable.

**Arrays** of the same length whose positions hold different things are
**tuples** (`prefixItems`). Otherwise their elements are pooled: strings
widen into one string schema, numbers into one range, objects into kinds as
above. Arrays that are always the same length (seen at least three times)
get `minItems` and `maxItems`.

## What it never writes

The only things from the data that appear in a schema are:

- **keys**, unless the object is taken for a map;
- **enum values**, which must repeat (fewer distinct values than 5% of
  observations, so a place seen fewer than 21 times never has one), be
  short, not look random or opaque, not be an address, ID or URL, not have
  six or more digits, and not be secret (see below);
- **tag values** that tell kinds of object apart, which are shown just as
  enum values are, under the same rules: each kind's value must repeat, and
  fewer kinds than 5% of the objects;
- the **power of ten** above and below each place's numbers (none at all
  for secrets, or for numbers too big for a float).

A value is secret if it sits, at any depth, under a key that sounds secret
(`password`, `token`, `apiKey`, `credentials`, `session`, `pin` and many
more), in a map with any such key (its values are pooled), or in a
name/value list where any name sounds secret (`{"name": "DB_PASSWORD",
"value": …}`, or `<param name="api_key" value="…"/>`). `--no-enums` (or
`--max-enum 0`) turns off enums and tags alike.

Anything that looks like a credential (private keys, JWTs, tokens with
well-known prefixes, URLs with passwords in) or a random string is never an
enum value, tag or key.

## Formats

The format comes from the file name (`.json`, `.jsonl`, `.ndjson`, `.yaml`,
`.yml`, `.toml`, `.ini`, `.cfg`, `.conf`, `.cnf`, `.properties`, `.csv`,
`.tsv`, `.xml`, `.svg`, `.rss`, `.atom`, `.plist`, …), or `--format`.
Anything else, including standard input, is sniffed: XML if it starts with
`<`, else the first of JSON, TOML, YAML (if it's more than a lone value),
INI, and CSV or TSV (if every row has as many fields) that reads it.

Each is read as JSON would see it:

- **JSON**: a stream of several values (JSON Lines) becomes an array.
- **YAML**: anchors, aliases and `<<` merges are followed; dates and other
  scalars that JSON lacks become strings; several documents become an array.
- **TOML**: dates and times become strings.
- **INI**: an object of sections, each an object of keys. Keys before the
  first section go at the top level. Values become booleans (`true`, `yes`,
  `on`, …), numbers or strings; a key with no value is `true`; a key given
  more than once, or written `key[]`, is an array.
- **CSV** and **TSV**: with a header row, an array of objects keyed by
  column; without one, an array of arrays. Empty cells are null, and cells
  become booleans, numbers or strings. Whether there's a header is guessed;
  `--csv-header yes` or `no` says.
- **XML**: an array of the root elements. Each element is an object with
  one key, its name (with any namespace prefix, as written), holding an
  object of its attributes, and its content under `$inner`: the text if
  that's all there is, or else an array of its child elements and any text
  between them. Text and attribute values become booleans, numbers or
  strings. Comments, processing instructions and whitespace between
  elements are dropped, and documents in other encodings are converted.

  ```xml
  <foos a="1"><foo><a>1</a><b>2</b></foo></foos>
  ```

  reads as

  ```json
  [{"foos": {"a": 1, "$inner": [
    {"foo": {"$inner": [{"a": {"$inner": 1}}, {"b": {"$inner": 2}}]}}
  ]}}]
  ```

## Usage

```
structpeek [options] [file...]
```

With no file, or `-`, it reads standard input. Several files are taken as
examples of one kind of document, and the schema fits them all:
`structpeek configs/*.yaml`.

| Option | |
|---|---|
| `-f`, `--format` *format* | read input as `json`, `yaml`, `toml`, `ini`, `csv`, `tsv` or `xml` |
| `--csv-header` *auto\|yes\|no* | whether CSV/TSV files have a header row |
| `-o`, `--output` *file* | write the schema to a file |
| `-c`, `--compact` | write the schema on one line |
| `--cardinality` *fraction* | strings with fewer distinct values than this fraction of observations are low-cardinality (default 0.05) |
| `--max-enum` *n* | list at most *n* values in an enum (default 32; 0 for none, and no tags) |
| `--no-enums` | never list values in an enum or tag: low-cardinality strings are always `$defs/lowCardinalityString` |
| `--similarity` *0–1* | how alike (by Jaccard index of their keys) kinds of object must be to merge (default 0.5) |
| `--max-properties` *n* | take objects with more distinct keys than this for maps (default 200) |

### For AI agents

Before reading a data file of unknown size or sensitivity, run
`structpeek FILE` and work from the schema. `$defs/lowCardinalityString`
means a field holds a few repeated values (like a status or a category)
that were withheld; `$defs/highEntropyString` means keys, tokens, hashes or
opaque IDs. Optional fields are the ones missing from `required`, and
`anyOf` means several kinds of record are mixed together.

## Limitations

- Telling secrets from ordinary text is heuristic. About one random token
  in a thousand passes for ordinary text: it's then typed as a plain
  `string` rather than `$defs/highEntropyString`, but a blunter check still
  keeps it out of enums and tags, so its value isn't written.
- Values that are sensitive but ordinary-looking (people's names, cities)
  can appear in an enum or tag if they repeat often enough; `--no-enums`
  turns both off.
- Keys are written as they are, unless an object looks like a map; a small
  object keyed by, say, usernames may not.
- Whether a CSV file has a header is a guess; a file that's all text and
  has none may be taken to have one.
- The whole file is read into memory; structpeek gets through about 8 MB a
  second.

## Install

**Binaries** for Linux, macOS, Windows and FreeBSD are on the
[releases page](https://github.com/NaomiAmethyst/structpeek/releases/latest),
with checksums. Unpack one and put `structpeek` on your `PATH`.

**Docker** images for amd64, arm64, armv7, riscv64, ppc64le and s390x are
on the GitHub Container Registry: `:latest` and version tags for releases,
`:edge` for the main branch.

```sh
docker run --rm -i ghcr.io/naomiamethyst/structpeek < data.json
docker run --rm -v "$PWD:/data:ro" ghcr.io/naomiamethyst/structpeek config.yaml
```

**From source**, with Go 1.24 or later:

```sh
go install github.com/NaomiAmethyst/structpeek/cmd/structpeek@latest
```

or, from a checkout, `make && sudo make install` (or `make install
PREFIX=~/.local`).

## Development

`make check` runs the tests, `go vet` and a `gofmt` check; `make fuzz`
fuzzes the decoders and the inference for a minute each. Schemas for the
files in `testdata` are compared with the ones in `testdata/golden`; after
a change that's meant to alter them, `go test ./cmd/structpeek -update`
rewrites them, and the diff shows what changed. See
[CONTRIBUTING.md](CONTRIBUTING.md), and if you find a value making it into
a schema, [SECURITY.md](SECURITY.md).

Releases are made by pushing a `v*` tag: GitHub Actions builds the binaries
with [GoReleaser](https://goreleaser.com/) and the images with Docker
Buildx.

## License

Copyright © 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

structpeek is free software: you can redistribute it and/or modify it under
the terms of the GNU General Public License as published by the Free
Software Foundation, either version 3 of the License, or (at your option)
any later version. See [LICENSE](LICENSE).
