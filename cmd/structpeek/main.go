// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

// Command structpeek reads a data file (JSON, YAML, TOML, INI, CSV or XML) and
// writes a JSON Schema describing its structure, without any of the values
// that could be sensitive, so you (or an AI agent) can see what's in a
// file without seeing what's in it.
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
structure, without values that could be sensitive. With no file, or "-",
reads standard input. Several files are taken as examples of one kind of
document, and the schema fits them all.

Formats: json (and JSON Lines), yaml, toml, ini, csv, tsv, xml. Each
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
	formatName := fs.StringP("format", "f", "", "read input as `format`, whatever the file name says")
	header := fs.String("csv-header", "auto", "whether CSV/TSV files have a header row: auto, yes or no")
	output := fs.StringP("output", "o", "", "write the schema to `file` instead of standard output")
	compact := fs.BoolP("compact", "c", false, "write the schema on one line")
	fs.Float64Var(&opts.Cardinality, "cardinality", opts.Cardinality,
		"strings with fewer distinct values than this `fraction` of how often they're seen are low-cardinality")
	fs.IntVar(&opts.MaxEnum, "max-enum", opts.MaxEnum, "list at most `n` values in an enum (0: never list any, nor tag kinds of object)")
	noEnums := fs.Bool("no-enums", false,
		"never list values in an enum or tag: low-cardinality strings are always $defs/lowCardinalityString")
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

	if *noEnums {
		opts.MaxEnum = 0
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
