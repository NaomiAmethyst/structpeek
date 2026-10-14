# structpeek

[![CI](https://github.com/NaomiAmethyst/structpeek/actions/workflows/ci.yml/badge.svg)](https://github.com/NaomiAmethyst/structpeek/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/NaomiAmethyst/structpeek)](https://github.com/NaomiAmethyst/structpeek/releases/latest)
[![License: GPL v3+](https://img.shields.io/badge/license-GPL--3.0--or--later-blue)](LICENSE)

See what's in a data file without seeing what's in it.

`structpeek` reads JSON, YAML, TOML, INI, CSV or XML and writes a
[JSON Schema](https://json-schema.org/) describing its structure: the
fields, their types and formats, which are optional, and what kinds of
record there are.

**Values are left out by default; field names stay visible.** (Two
options show a little more; see [What it writes](#what-it-writes).)

## What it's for

- **Checking a credentials or config file's layout** without printing the
  secrets in it.
- **Getting to know an unfamiliar dataset**: what fields records have,
  which are optional, which hold dates, IDs or categories.
- **Giving an AI agent the structure instead of the raw data**, so it can
  write code against a file without reading anyone's passwords.

## Example

Given a credentials file holding a URL, a user name, an API token and an
expiry date:

```console
$ structpeek credentials.json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "url": { "type": "string", "format": "uri" },
    "user": { "type": "string" },
    "token": { "$ref": "#/$defs/highEntropyString" },
    "expires": { "type": "string", "format": "date" }
  },
  "required": ["url", "user", "token", "expires"],
  "additionalProperties": false,
  "$defs": {
    "highEntropyString": {
      "description": "A string that looks randomly generated, such as a key, token, hash or opaque ID. The values are withheld.",
      "type": "string"
    }
  }
}
```

(Squashed a little here.) You can see the token is there, and that it
looks like a real secret, without seeing it.

## Quick start

Install it one of these ways:

- **Download a binary** for Linux, macOS, Windows or FreeBSD from the
  [releases page](https://github.com/NaomiAmethyst/structpeek/releases/latest),
  unpack it, and put `structpeek` on your `PATH`.
- **Use the Docker image** (amd64, arm64, armv7, riscv64, ppc64le, s390x):
  `ghcr.io/naomiamethyst/structpeek`, tagged `:latest` and by version, or
  `:edge` for the main branch.
- **Build from source** with Go 1.24 or later:
  `go install github.com/NaomiAmethyst/structpeek/cmd/structpeek@latest`,
  or `make && sudo make install` from a checkout.

Then:

```sh
structpeek config.yaml                 # one file; the format comes from its name
structpeek records/*.json              # several files of one kind: a schema for them all
kubectl get pods -o json | structpeek  # standard input; the format is worked out
docker run --rm -i ghcr.io/naomiamethyst/structpeek < data.csv
docker run --rm -v "$PWD:/data:ro" ghcr.io/naomiamethyst/structpeek settings.toml
```

## Usage

```
structpeek [options] [file...]
```

With no file, or `-`, it reads standard input. Several files are taken as
examples of one kind of document, and the schema fits them all.

| Option | |
|---|---|
| `-f`, `--format` *format* | read input as `json`, `yaml`, `toml`, `ini`, `csv`, `tsv` or `xml` |
| `--csv-header` *auto\|yes\|no* | whether CSV/TSV files have a header row |
| `-o`, `--output` *file* | write the schema to a file |
| `-c`, `--compact` | write the schema on one line |
| `--enums` | list repeated values that look safe in enums, and tag kinds of object with them |
| `--ranges` | give numbers a range, rounded out to powers of ten |
| `--max-enum` *n* | with `--enums`, list at most *n* values in an enum (default 32) |
| `--cardinality` *fraction* | strings with fewer distinct values than this fraction of observations are low-cardinality (default 0.05) |
| `--similarity` *0–1* | how alike (by Jaccard index of their keys) kinds of object must be to merge (default 0.5) |
| `--max-properties` *n* | take objects with more distinct keys than this for maps (default 200) |

### For AI agents

Before reading a data file of unknown size or sensitivity, run
`structpeek FILE` and work from the schema. `$defs/lowCardinalityString`
means a field holds a few repeated values (like a status or a category);
`$defs/highEntropyString` means keys, tokens, hashes or opaque IDs.
Optional fields are the ones missing from `required`, and `anyOf` means
several kinds of record are mixed together.

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
kinds in an `anyOf`. With `--enums`, if a field such as `type` tells them
apart, each kind gets its value as a `const`.

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

- if they have few distinct values (fewer than 5% of how often they're
  seen), they're a reference to `$defs/lowCardinalityString`, or, with
  `--enums`, an `enum` if every value is safe to show;
- if they look randomly generated, they're a reference to
  `$defs/highEntropyString`;
- otherwise they're just strings.

**Numbers** are `integer` if they're all written as integers, else
`number`. With `--ranges`, they get a range rounded out to powers of ten:
it shows their scale without giving any of them away.

**Booleans** are `boolean`, and **nulls** make a place nullable.

**Arrays** of the same length whose positions hold different things are
**tuples** (`prefixItems`). Otherwise their elements are pooled: strings
widen into one string schema, numbers into one number schema, objects into
kinds as above. Arrays that are always the same length (seen at least
three times) get `minItems` and `maxItems`.

## What it writes

By default, nothing from the data's values: a schema holds the data's
keys, and what kinds of value they hold, and that's all.

Keys are written as they are, unless the object is taken for a map, so
objects whose keys are IDs, numbers, dates, addresses or tokens don't give
those away.

Two options write a little more, for when the structure alone isn't
enough:

- `--enums` lists **enum values**, and **tags** kinds of object with the
  field that tells them apart. These values must repeat (fewer distinct
  values than 5% of observations, so a place seen fewer than 21 times
  never has one; for tags, fewer kinds than 5% of the objects), be short,
  not look random or opaque, not be an address, ID or URL, not have six or
  more digits, and not be secret (see below).
- `--ranges` gives numbers the **power of ten** above and below them
  (none for secrets, or for numbers too big for a float).

A value is secret if it sits, at any depth, under a key that sounds secret
(`password`, `token`, `apiKey`, `credentials`, `session`, `pin` and many
more), in a map with any such key (its values are pooled), or in a
name/value list where any name sounds secret (`{"name": "DB_PASSWORD",
"value": …}`, or `<param name="api_key" value="…"/>`). Anything that looks
like a credential (private keys, JWTs, tokens with well-known prefixes,
URLs with passwords in) or a random string is never an enum value, tag or
key.

## Formats

The format comes from the file name (`.json`, `.jsonl`, `.ndjson`, `.yaml`,
`.yml`, `.toml`, `.ini`, `.cfg`, `.conf`, `.cnf`, `.properties`, `.csv`,
`.tsv`, `.xml`, `.svg`, `.rss`, `.atom`, `.plist`, …), or `--format`.
Anything else, including standard input, is sniffed: XML if it starts with
`<`, else the first of JSON, TOML, YAML (if it's more than a lone value),
INI, and CSV or TSV (if every row has as many fields) that reads it.

Each is read as JSON would see it:

- **JSON**: a stream of several values (JSON Lines) becomes an array. Raw
  control characters in strings, which JSON forbids but broken files have,
  are accepted. If the JSON comes after other text, such as a log saved
  with it, the error says where it starts.
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

## Limitations

- Keys are written as they are, unless an object looks like a map; a small
  object keyed by, say, usernames may not.
- With `--enums`, telling secrets from ordinary values is heuristic.
  Values that are sensitive but ordinary-looking (people's names, cities)
  can appear in an enum or tag if they repeat often enough, and so can a
  secret that nothing about its key, its neighbours or its shape gives
  away. Leave `--enums` off for data you can't vouch for.
- `$defs/highEntropyString` is a guess too: about one random token in a
  thousand passes for ordinary text, and is typed as a plain `string`.
- Whether a CSV file has a header is a guess; a file that's all text and
  has none may be taken to have one.
- The whole file is read into memory; structpeek gets through about 8 MB a
  second.

## Development

To build, `make`; to install, `sudo make install` (or `make install
PREFIX=~/.local`).

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
