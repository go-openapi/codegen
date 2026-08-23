// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"bytes"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
	"golang.org/x/tools/imports"

	"github.com/go-openapi/codegen/formatting"
)

// TestAgainstReference compares Format with goimports on sources where the two cannot disagree.
//
// This package was written against x/tools/internal/imports. Format parts company with it in three
// places, none of them exercised here: Format never adds an import, [formatting.WithImportGroups]
// opens groups goimports has no way to express, and Format sorts and dedups the whole import block
// where goimports sorts each blank-line-separated run on its own. No fixture below writes a blank
// line inside its import block, so the third difference cannot show. Any other difference is a bug
// in one of them.
func TestAgainstReference(t *testing.T) {
	t.Parallel()

	for fixture, toPin := range sourceSet(t, "reference") {
		src := toPin

		t.Run("should agree with goimports on "+caseName(fixture), func(t *testing.T) {
			t.Parallel()

			expected, err := imports.Process("p.go", []byte(src), &imports.Options{
				Comments:  true,
				TabIndent: true,
				TabWidth:  8,
			})
			require.NoError(t, err)

			var out bytes.Buffer
			require.NoError(t, formatting.Format(&out, []byte(src)))

			assert.Equal(t, string(expected), out.String())
		})
	}
}

// TestNeverAdds states the one difference on purpose.
func TestNeverAdds(t *testing.T) {
	t.Parallel()

	src := source(t, "prune/missing")

	resolved, err := imports.Process("p.go", []byte(src), &imports.Options{Comments: true, TabIndent: true, TabWidth: 8})
	require.NoError(t, err)
	require.Contains(t, string(resolved), `"fmt"`, "goimports resolves the missing import")

	var out bytes.Buffer
	require.NoError(t, formatting.Format(&out, []byte(src)))
	assert.NotContains(t, out.String(), `"fmt"`, "Format leaves it to the compiler to complain")
}

func BenchmarkFormat(b *testing.B) {
	src, err := sources.ReadFile(sourceRoot + "/reference/third-party-apart-from-std.input")
	if err != nil {
		b.Fatal(err)
	}

	groups := formatting.WithImportGroups("github.com/go-openapi")

	b.Run("formatting.Format", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var out bytes.Buffer
			if err := formatting.Format(&out, src, groups); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("imports.Process", func(b *testing.B) {
		b.ReportAllocs()
		opts := &imports.Options{Comments: true, TabIndent: true, TabWidth: 8}

		for b.Loop() {
			if _, err := imports.Process("p.go", src, opts); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("imports.Process format only", func(b *testing.B) {
		b.ReportAllocs()
		opts := &imports.Options{Comments: true, TabIndent: true, TabWidth: 8, FormatOnly: true}

		for b.Loop() {
			if _, err := imports.Process("p.go", src, opts); err != nil {
				b.Fatal(err)
			}
		}
	})
}
