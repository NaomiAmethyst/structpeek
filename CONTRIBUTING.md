# Contributing

Bug reports, ideas and pull requests are welcome.

- **Bugs**: say what you ran, on what input (made up, or with values
  changed: the shape is what matters), and what you expected. For a value
  leaking into a schema, see [SECURITY.md](SECURITY.md) instead.
- **Changes**: run `make check` before sending, and add a test. Inference
  rules live in `internal/infer` (the package comment says how they fit
  together); readers for each format live in `internal/decode`.
- **Schemas changing**: if a change alters the schemas for `testdata`,
  run `go test ./cmd/structpeek -update` and include the golden files, so
  the diff shows the effect.
- **Leaning**: when in doubt, withhold a value, and prefer a schema that
  takes in the data's structure over one that fits the sample exactly.

By contributing, you agree your work is licensed under the GPL, version 3
or later, like the rest of structpeek.
