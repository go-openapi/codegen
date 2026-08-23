// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"bytes"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/formatting"
)

func TestPrune(t *testing.T) {
	t.Parallel()

	t.Run("should drop an import nothing uses", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/unused"))

		assert.NotContains(t, out, `"strings"`)
		assert.Contains(t, out, `"bytes"`)
	})

	t.Run("should keep a blank and a dot import", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/blank-and-dot"))

		assert.Contains(t, out, `_ "embed"`)
		assert.Contains(t, out, `. "strings"`)
	})

	t.Run("should trust an alias rather than guess", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/aliased"))

		assert.Contains(t, out, `buf "bytes"`)
		assert.NotContains(t, out, `unused "strings"`)
	})

	t.Run("should keep an import whose package it cannot name", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/unnameable"))

		assert.Contains(t, out, `"example.com/2fa"`)
	})

	t.Run("should keep the cgo preamble", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/cgo"))

		assert.Contains(t, out, `import "C"`)
	})

	t.Run("should keep an import a shadowed name does not hide", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/shadowed-partly"))

		assert.Contains(t, out, `"bytes"`)
	})

	t.Run("should drop an import every use of which is shadowed", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/shadowed-wholly"))

		assert.NotContains(t, out, `"bytes"`)
	})

	t.Run("should never add an import", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/missing"))

		assert.NotContains(t, out, `"fmt"`, "a missing import is the template's business, not ours")
	})
}

// TestPruneConfidence pins which imports may be deleted, and on what evidence.
//
// An import is deleted only when its name is known: written as an alias, held by the generated
// standard library table, or supplied through [formatting.WithResolvedImports]. A bare third-party
// import is a guess, and a guess keeps an import rather than delete it.
func TestPruneConfidence(t *testing.T) {
	t.Parallel()

	strfmtResolved := formatting.WithResolvedImports(map[string]string{
		"github.com/go-openapi/strfmt": "strfmt",
	})

	t.Run("should delete an unused import whose name is certain", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/unused"))
		assert.NotContains(t, out, `"strings"`, "the standard library table names it")

		out = format2(t, source(t, "prune/aliased"))
		assert.NotContains(t, out, `unused "strings"`, "the alias names it")
	})

	t.Run("should keep an unused bare third-party import", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/bare-third-party"))

		assert.Contains(t, out, `"github.com/go-openapi/strfmt"`,
			"strfmt is a guess, and the package may declare something else")
	})

	t.Run("should delete it once the caller names it", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/bare-third-party"), strfmtResolved)

		assert.NotContains(t, out, "strfmt")
	})

	t.Run("should delete it once the caller promises the naming convention", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/bare-third-party"), formatting.WithForceImportsPruning())

		assert.NotContains(t, out, "strfmt")
	})

	t.Run("should keep an import whose package no rule can name", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/unnameable-third-party"))

		assert.Contains(t, out, `"github.com/json-iterator/go"`, "the path never says jsoniter")
	})

	t.Run("should delete that one under a promise it breaks", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/unnameable-third-party"), formatting.WithForceImportsPruning())

		assert.NotContains(t, out, "json-iterator",
			"jsoniter does not follow the convention, so the promise was wrong and the build will say so")
	})

	t.Run("should keep a version directory the file uses under its own name", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/version-directory"), formatting.WithForceImportsPruning())

		assert.Contains(t, out, `"k8s.io/api/apps/v1"`,
			"forced pruning checks every candidate, so v1 counts as much as apps")
	})

	t.Run("should never delete a blank or a dot import", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/blank-and-dot-unused"), formatting.WithForceImportsPruning())

		assert.Contains(t, out, `_ "embed"`, "a blank import runs an init and binds no qualifier")
		assert.Contains(t, out, `. "strings"`, "a dot import spills its names, so nothing can be checked")
	})
}

// TestPromiseWithExceptions pins that the two options compose.
//
// [formatting.WithForceImportsPruning] promises the naming convention holds, and
// [formatting.WithResolvedImports] states the names where it does not. A caller with one awkward
// dependency does not have to choose between them.
func TestPromiseWithExceptions(t *testing.T) {
	t.Parallel()

	const (
		jsoniterPath = "github.com/json-iterator/go"
		swagPath     = "github.com/go-openapi/swag"
	)

	src := source(t, "prune/promise-with-exceptions")
	hints := formatting.WithResolvedImports(map[string]string{jsoniterPath: "jsoniter"})
	force := formatting.WithForceImportsPruning()

	t.Run("should prune a used import when only the promise is given", func(t *testing.T) {
		t.Parallel()

		out := format2(t, src, force)

		assert.NotContains(t, out, jsoniterPath, "jsoniter breaks the convention, so the promise was wrong")
	})

	t.Run("should keep it once the map names it, and still prune the rest", func(t *testing.T) {
		t.Parallel()

		out := format2(t, src, force, hints)

		assert.Contains(t, out, jsoniterPath, "the map states what the promise could not cover")
		assert.NotContains(t, out, swagPath, "the promise still settles everything the map leaves out")
	})

	t.Run("should not care which option comes first", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, format2(t, src, force, hints), format2(t, src, hints, force))
	})

	t.Run("should leave nothing in doubt", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		report, err := formatting.Format(&out, []byte(src), force, hints)
		require.NoError(t, err)

		assert.False(t, report.HasImportsInDoubt(), "every import was decided:\n%s", report)
	})
}
