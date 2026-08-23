// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/formatting"
)

func TestGrouping(t *testing.T) {
	t.Parallel()

	grouped := source(t, "grouped")

	t.Run("should put the standard library first and everything else after it", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, [][]string{
			{"context"},
			{"example.com/petstore/models", "github.com/go-openapi/strfmt", "github.com/google/uuid"},
		}, importBlocks(t, format2(t, grouped)))
	})

	t.Run("should open one group per prefix, in the order given", func(t *testing.T) {
		t.Parallel()

		out := format2(t, grouped,
			formatting.WithImportGroups("github.com/go-openapi", "example.com/petstore"),
		)

		assert.Equal(t, [][]string{
			{"context"},
			{"github.com/go-openapi/strfmt"},
			{"example.com/petstore/models"},
			{"github.com/google/uuid"},
		}, importBlocks(t, out))
	})

	t.Run("should claim an import for the first prefix that matches", func(t *testing.T) {
		t.Parallel()

		out := format2(t, grouped,
			formatting.WithImportGroups("github.com", "github.com/go-openapi"),
		)

		assert.Equal(t, [][]string{
			{"context"},
			{"github.com/go-openapi/strfmt", "github.com/google/uuid"},
			{"example.com/petstore/models"},
		}, importBlocks(t, out))
	})

	t.Run("should ignore an empty prefix", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t,
			importBlocks(t, format2(t, grouped)),
			importBlocks(t, format2(t, grouped, formatting.WithImportGroups(""))),
		)
	})

	t.Run("should merge separate import declarations into one", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "separate-decls"))

		assert.Equal(t, [][]string{{"bytes", "context"}}, importBlocks(t, out))
		assert.Equal(t, 1, strings.Count(out, "import"))
	})
}

func TestFragment(t *testing.T) {
	t.Parallel()

	t.Run("should format a declaration list, keeping no package clause", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "fragment-decls"))

		assert.NotContains(t, out, "package")
		assert.Contains(t, out, "func F() int {")
	})

	t.Run("should format a statement list", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "fragment-stmts"))

		assert.NotContains(t, out, "package")
		assert.NotContains(t, out, "func _()")
		assert.Contains(t, out, "x := 1")
	})

	t.Run("should keep the package clause a main function earns", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "fragment-main"))

		assert.Contains(t, out, "package main")
	})

	t.Run("should restore the space that surrounded the fragment", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "fragment-spaced"))

		assert.True(t, strings.HasPrefix(out, "\n\n\t"), "leading blank lines and indent come back")
		assert.True(t, strings.HasSuffix(out, "\n\n"), "trailing space comes back")
	})
}

func TestFormatErrors(t *testing.T) {
	t.Parallel()

	t.Run("should report a source it cannot parse", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		_, err := formatting.Format(&out, []byte(source(t, "broken-decl")))

		require.Error(t, err)
		assert.ErrorIs(t, err, formatting.ErrFormat)
		assert.Contains(t, err.Error(), "3:9", "the parser's position survives; naming the file is the caller's job")
	})

	t.Run("should report the whole-file error, not the fragment one", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		_, err := formatting.Format(&out, []byte(source(t, "broken-expr")))

		require.Error(t, err)
		assert.NotContains(t, err.Error(), "expected 'package'")
	})

	t.Run("should refuse gofumpt when the enable module is absent", func(t *testing.T) {
		t.Parallel()

		_, err := formatting.Format(failingWriter{}, []byte(source(t, "empty-package")), formatting.WithGoFumpt())

		require.Error(t, err)
		assert.ErrorIs(t, err, formatting.ErrNoGoFumpt)
		assert.Contains(t, err.Error(), "enable/gofumpt")
	})

	t.Run("should write nothing when the source does not parse", func(t *testing.T) {
		t.Parallel()

		for _, fixture := range []string{"broken-decl", "broken-expr"} {
			var out countingWriter
			_, err := formatting.Format(&out, []byte(source(t, fixture)))
			require.Error(t, err)

			assert.Zero(t, out.writes, "%s: the printer never started", fixture)
			assert.Zero(t, out.Len(), "%s: and w is untouched", fixture)
		}
	})

	t.Run("should report a writer that fails", func(t *testing.T) {
		t.Parallel()

		_, err := formatting.Format(failingWriter{}, []byte(source(t, "grouped")))

		require.Error(t, err)
		assert.ErrorIs(t, err, errWriter)
	})
}

func TestIdempotent(t *testing.T) {
	t.Parallel()

	for fixture, toPin := range sourceSet(t, "idempotent") {
		src := toPin
		t.Run("should not change "+caseName(fixture)+" on a second pass", func(t *testing.T) {
			t.Parallel()

			once := format2(t, src, formatting.WithImportGroups("github.com/go-openapi"))
			twice := format2(t, once, formatting.WithImportGroups("github.com/go-openapi"))

			assert.Equal(t, once, twice)
		})
	}
}

// TestMatchesGofmt pins how Format prints.
//
// Format cannot call [go/format.Node], which re-sorts a grouped import block by path and undoes the
// grouping, so it prints with a [go/printer.Config] carrying the mode bits gofmt uses. One of those
// bits has no exported name. This test fails the day it stops meaning what it means.
func TestMatchesGofmt(t *testing.T) {
	t.Parallel()

	for fixture, toPin := range sourceSet(t, "gofmt") {
		src := toPin
		t.Run("should print "+caseName(fixture)+" the way gofmt does", func(t *testing.T) {
			t.Parallel()

			gofmted, err := format.Source([]byte(src))
			require.NoError(t, err)

			// every source here lands in one group, where our layout and gofmt's coincide
			assert.Equal(t, string(gofmted), format2(t, src))
		})
	}
}

// format2 formats src and fails the test if it cannot.
func format2(t *testing.T, src string, opts ...formatting.Option) string {
	t.Helper()

	var out bytes.Buffer
	_, err := formatting.Format(&out, []byte(src), opts...)
	require.NoError(t, err)

	return out.String()
}

// importBlocks lists the import paths of formatted source, one slice per blank-line-separated group.
func importBlocks(t *testing.T, src string) [][]string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	require.NoError(t, err)

	var blocks [][]string
	current := make([]string, 0, len(file.Imports))
	previousLine := 0

	for _, spec := range file.Imports {
		line := fset.Position(spec.Pos()).Line
		if previousLine != 0 && line > previousLine+1 {
			blocks = append(blocks, current)
			current = nil
		}

		current = append(current, importPathOf(spec))
		previousLine = line
	}

	if len(current) > 0 {
		blocks = append(blocks, current)
	}

	return blocks
}

func importPathOf(spec *ast.ImportSpec) string {
	return strings.Trim(spec.Path.Value, `"`)
}

// countingWriter records how many times the printer wrote to it.
type countingWriter struct {
	bytes.Buffer

	writes int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.writes++

	return c.Buffer.Write(p)
}

var errWriter = errors.New("writer refused")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriter }

func TestSource(t *testing.T) {
	t.Parallel()

	src := source(t, "grouped")

	t.Run("should format a byte slice", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		_, err := formatting.Format(&out, []byte(src))
		require.NoError(t, err)
		assert.Contains(t, out.String(), "package p")
	})

	t.Run("should format a buffer and leave it as it was", func(t *testing.T) {
		t.Parallel()

		var rendered bytes.Buffer
		rendered.WriteString(src)

		var out bytes.Buffer
		_, err := formatting.Format(&out, &rendered)
		require.NoError(t, err)

		assert.Equal(t, src, rendered.String(), "Format reads the buffer without draining it")
		assert.Equal(t, format2(t, src), out.String(), "and formats it the same as the bytes")
	})

	t.Run("should read the buffer without copying it", func(t *testing.T) {
		t.Parallel()

		var rendered bytes.Buffer
		rendered.WriteString(src)

		var fromBuffer, fromBytes bytes.Buffer
		_, err := formatting.Format(&fromBuffer, &rendered)
		require.NoError(t, err)

		_, err = formatting.Format(&fromBytes, rendered.Bytes())
		require.NoError(t, err)

		assert.Equal(t, fromBytes.String(), fromBuffer.String())
	})

	t.Run("should take a nil buffer for an empty source", func(t *testing.T) {
		t.Parallel()

		var nothing *bytes.Buffer

		var out bytes.Buffer
		_, err := formatting.Format(&out, nothing)

		require.Error(t, err, "an empty source has no package clause")
		assert.ErrorIs(t, err, formatting.ErrFormat)
	})

	t.Run("should serve a buffer reset between templates", func(t *testing.T) {
		t.Parallel()

		var rendered bytes.Buffer

		for _, fixture := range []string{"grouped", "separate-decls"} {
			rendered.Reset()
			rendered.WriteString(source(t, fixture))

			var out bytes.Buffer
			_, err := formatting.Format(&out, &rendered)
			require.NoError(t, err)
			assert.Equal(t, format2(t, source(t, fixture)), out.String())
		}
	})
}
