// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NaomiAmethyst/structpeek/internal/decode"
	"github.com/NaomiAmethyst/structpeek/internal/schematest"
)

var update = flag.Bool("update", false, "rewrite the golden schemas in testdata/golden")

// TestGolden runs structpeek on each file in testdata, checks the schema
// is valid and fits the file, and compares it with the one in
// testdata/golden.
func TestGolden(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/*.*")
	if len(files) == 0 {
		t.Fatal("no test data")
	}
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := run([]string{file}, nil, &out); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(file)
			doc, err := decode.Decode(data, decode.ForPath(file), decode.Options{CSVHeader: decode.HeaderAuto})
			if err != nil {
				t.Fatal(err)
			}
			schematest.Validate(t, out.Bytes(), doc)

			golden := filepath.Join("../../testdata/golden", name+".schema.json")
			if *update {
				os.MkdirAll(filepath.Dir(golden), 0o755)
				if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run go test ./cmd/structpeek -update)", err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Errorf("schema for %s differs from %s:\n%s", name, golden, out.Bytes())
			}
		})
	}
}

func TestSeveralFilesAndStdin(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.yaml")
	os.WriteFile(a, []byte(`{"name": "x", "port": 1}`), 0o644)
	os.WriteFile(b, []byte("name: y\n"), 0o644)
	var out bytes.Buffer
	if err := run([]string{"-c", a, b}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"required":["name"]`) {
		t.Errorf("port should be optional: %s", out.String())
	}

	out.Reset()
	if err := run([]string{"-f", "csv", "-c"}, strings.NewReader("a,b\n1,x\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"type":"array","items":{"type":"object"`) {
		t.Errorf("CSV from stdin: %s", out.String())
	}
}

func TestBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{"-f", "xml"}, {"--csv-header", "maybe"}, {"--cardinality", "2"}, {"/does/not/exist.json"},
	} {
		if err := run(args, strings.NewReader("{}"), &bytes.Buffer{}); err == nil {
			t.Errorf("run(%q) succeeded", args)
		}
	}
}
