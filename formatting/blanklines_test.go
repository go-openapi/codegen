// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"testing"

	"github.com/go-openapi/testify/v2/assert"

	"github.com/go-openapi/codegen/formatting"
)

// TestBlankLinesAreNotGroups pins that the source's own blank lines change nothing.
//
// gofmt and goimports sort each blank-line-separated run of imports on its own, so a template
// writing "bytes" in two groups gets both back and the file does not compile. Format sorts the
// whole block, then writes the blank lines [formatting.WithImportGroups] asks for.
func TestBlankLinesAreNotGroups(t *testing.T) {
	t.Parallel()

	t.Run("should keep one import when two groups repeat a path", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "duplicate-across-groups"),
			formatting.WithImportGroups("github.com/go-openapi"),
		)

		assert.Equal(t, [][]string{
			{"bytes", "context"},
			{"github.com/go-openapi/strfmt"},
		}, importBlocks(t, out))
	})

	t.Run("should sort across the blank lines the source wrote", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "unsorted-across-groups"))

		assert.Equal(t, [][]string{{"bytes", "context", "errors", "strings"}}, importBlocks(t, out))
	})
}
