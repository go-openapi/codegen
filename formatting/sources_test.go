// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"embed"
	"path"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/require"
)

// sources holds the Go source every test formats.
//
// A fixture is a file rather than a string in a test, so that it reads as the Go it is, an editor
// indents it, and adding a case to a table means adding a file. The .input extension keeps gofmt
// away from sources that are deliberately misformatted.
//
//go:embed testdata/sources
var sources embed.FS

const sourceRoot = "testdata/sources"

// source returns one fixture, named without its extension: source(t, "prune/unused").
func source(t *testing.T, name string) string {
	t.Helper()

	content, err := sources.ReadFile(path.Join(sourceRoot, name+".input"))
	require.NoError(t, err, "no such fixture, add %s.input under %s", name, sourceRoot)

	return string(content)
}

// sourceSet returns every fixture in a directory, keyed by file name without its extension.
//
// A table built from it grows by dropping a file in, which is the point.
func sourceSet(t *testing.T, dir string) map[string]string {
	t.Helper()

	entries, err := sources.ReadDir(path.Join(sourceRoot, dir))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "no fixtures under %s", path.Join(sourceRoot, dir))

	set := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".input") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".input")
		set[name] = source(t, path.Join(dir, name))
	}

	require.NotEmpty(t, set, "no .input fixtures under %s", path.Join(sourceRoot, dir))

	return set
}

// caseName turns a fixture file name into the sentence a subtest reads as.
func caseName(fixture string) string {
	return strings.ReplaceAll(fixture, "-", " ")
}
