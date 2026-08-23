// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"testing"

	"github.com/go-openapi/testify/v2/assert"

	"github.com/go-openapi/codegen/formatting"
)

// TestSimplifiedImportAliases pins which aliases may be taken back out.
//
// An alias states the name an import binds, which is the cheapest way for a template to get exact
// pruning. Dropping it needs proof that the package declares that same name, and that the path says
// so too, or the bare import left behind would bind something else.
func TestSimplifiedImportAliases(t *testing.T) {
	t.Parallel()

	src := source(t, "redundant-aliases")

	simplify := formatting.WithSimplifiedImportAliases()
	hints := formatting.WithResolvedImports(map[string]string{
		"github.com/go-openapi/strfmt": "strfmt",
		"github.com/json-iterator/go":  "jsoniter",
	})

	t.Run("should leave every alias alone without the option", func(t *testing.T) {
		t.Parallel()

		out := format2(t, src, hints)

		assert.Contains(t, out, `fmt "fmt"`)
		assert.Contains(t, out, `strfmt "github.com/go-openapi/strfmt"`)
	})

	t.Run("should drop an alias the standard library table proves redundant", func(t *testing.T) {
		t.Parallel()

		out := format2(t, src, simplify)

		assert.Contains(t, out, "\t\"fmt\"\n", "the table names fmt, and so does the path")
		assert.NotContains(t, out, `fmt "fmt"`)
	})

	t.Run("should keep a third-party alias until the caller proves the name", func(t *testing.T) {
		t.Parallel()

		assert.Contains(t, format2(t, src, simplify), `strfmt "github.com/go-openapi/strfmt"`,
			"strfmt is only a guess, and a guess is not evidence")

		assert.Contains(t, format2(t, src, simplify, hints), "\t\"github.com/go-openapi/strfmt\"\n",
			"the map states it, and the path agrees")
	})

	t.Run("should keep an alias the path cannot replace", func(t *testing.T) {
		t.Parallel()

		out := format2(t, src, simplify, hints)

		assert.Contains(t, out, `jsoniter "github.com/json-iterator/go"`,
			"the name is proven, but nothing in the path says jsoniter")
	})

	t.Run("should keep an alias that renames a package", func(t *testing.T) {
		t.Parallel()

		out := format2(t, src, simplify, hints)

		assert.Contains(t, out, `sql "database/sql/driver"`, "the package is driver, so sql is not redundant")
	})

	t.Run("should not touch a blank or a dot import", func(t *testing.T) {
		t.Parallel()

		out := format2(t, src, simplify, hints)

		assert.Contains(t, out, `_ "embed"`)
		assert.Contains(t, out, `. "strings"`)
	})

	t.Run("should not change its own output on a second pass", func(t *testing.T) {
		t.Parallel()

		once := format2(t, src, simplify, hints)
		twice := format2(t, once, simplify, hints)

		assert.Equal(t, once, twice)
	})

	t.Run("should leave the output nameable by the path alone", func(t *testing.T) {
		t.Parallel()

		// what a later run without the map makes of the simplified output
		simplified := format2(t, src, simplify, hints)
		report := reportOf(t, simplified)

		for _, record := range report.Imports() {
			if record.Path == "github.com/go-openapi/strfmt" {
				assert.Equal(t, "strfmt", formatting.ImportedPackageName(record.Path),
					"dropping the alias lost nothing the path does not say")
			}
		}
	})
}
