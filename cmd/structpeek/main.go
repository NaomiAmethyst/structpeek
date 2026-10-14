// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

// Command structpeek reads a data or config file (JSON, YAML, TOML, INI,
// .env, CSV, XML, HCL, plist and more) and writes a JSON Schema describing
// its structure, without its values, so you (or an AI agent) can see
// what's in a file without seeing what's in it.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/pflag"

	"github.com/NaomiAmethyst/structpeek/internal/decode"
	"github.com/NaomiAmethyst/structpeek/internal/infer"
)

var version = ""

const usage = `Usage: structpeek [options] [file...]

Reads data files and writes a JSON Schema (2020-12) describing their
structure. Unless --enums or --ranges says otherwise, nothing from the
values is written: only their structure and what kinds of value they are.
With no file, or "-", reads standard input. Several files are taken as
examples of one kind of document, and the schema fits them all.

Formats: json (and JSON Lines, JSONC, JSON5), yaml, toml, ini, env,
properties, csv, tsv, xml, hcl, plist, netrc, ssh-config, pgpass. Each
file's format comes from its name, or else from what it looks like.

Options:
`

func main() {
	// The whole document stays in memory, so collecting garbage often
	// buys nothing.
	debug.SetGCPercent(400)
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "structpeek:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	opts := infer.DefaultOptions()
	fs := pflag.NewFlagSet("structpeek", pflag.ContinueOnError)
	fs.SortFlags = false
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
	}
	formatName := fs.StringP("format", "f", "", "read input as `format` (see above), whatever the file name says")
	header := fs.String("csv-header", "auto", "whether CSV/TSV files have a header row: auto, yes or no")
	output := fs.StringP("output", "o", "", "write the schema to `file` instead of standard output")
	compact := fs.BoolP("compact", "c", false, "write the schema on one line")
	fs.BoolVar(&opts.Enums, "enums", false,
		"list repeated values that look safe in enums, and tag kinds of object with them")
	fs.BoolVar(&opts.Ranges, "ranges", false, "give numbers a range, rounded out to powers of ten")
	fs.IntVar(&opts.MaxEnum, "max-enum", opts.MaxEnum, "with --enums, list at most `n` values in an enum")
	fs.Float64Var(&opts.Cardinality, "cardinality", opts.Cardinality,
		"strings with fewer distinct values than this `fraction` of how often they're seen are low-cardinality")
	fs.Float64Var(&opts.Similarity, "similarity", opts.Similarity,
		"merge kinds of object whose keys are at least this `alike` (0-1, by Jaccard index)")
	fs.IntVar(&opts.MaxProperties, "max-properties", opts.MaxProperties,
		"take objects with more than `n` distinct keys to be maps")
	showVersion := fs.BoolP("version", "V", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Fprintln(stdout, "structpeek", buildVersion())
		return nil
	}

	format, err := decode.ParseFormat(*formatName)
	if err != nil {
		return err
	}
	var dopts decode.Options
	switch h := decode.Header(strings.ToLower(*header)); h {
	case decode.HeaderAuto, decode.HeaderYes, decode.HeaderNo:
		dopts.CSVHeader = h
	default:
		return fmt.Errorf("--csv-header must be auto, yes or no, not %q", *header)
	}
	if opts.Cardinality < 0 || opts.Cardinality > 1 || opts.Similarity < 0 || opts.Similarity > 1 {
		return errors.New("--cardinality and --similarity must be between 0 and 1")
	}

	files := fs.Args()
	if len(files) == 0 {
		files = []string{"-"}
	}
	docs := make([]any, 0, len(files))
	for _, name := range files {
		doc, err := read(name, format, dopts, stdin)
		if err != nil {
			return err
		}
		docs = append(docs, doc)
	}

	indent := "  "
	if *compact {
		indent = ""
	}
	out, err := infer.Marshal(infer.Infer(docs, opts), indent)
	if err != nil {
		return err
	}
	if *output != "" {
		return os.WriteFile(*output, out, 0o644)
	}
	_, err = stdout.Write(out)
	return err
}

func read(name string, format decode.Format, o decode.Options, stdin io.Reader) (any, error) {
	var data []byte
	var err error
	if name == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(name)
		if format == decode.Auto {
			format = decode.ForPath(name)
		}
	}
	if err != nil {
		return nil, err
	}
	doc, err := decode.Decode(data, format, o)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return doc, nil
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}
