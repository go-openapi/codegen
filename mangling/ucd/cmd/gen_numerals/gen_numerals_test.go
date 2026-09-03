// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-openapi/codegen/mangling/ucd/internal/locate"
	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
)

// numericValuesFixture is a hand-picked slice of DerivedNumericValues.txt: one kept line per category, the categories
// the generator drops, a range line, and two malformed lines.
const numericValuesFixture = `# DerivedNumericValues-15.0.0.txt
# a comment line

0030          ; 0.0 ; ; 0 # Nd       DIGIT ZERO
4E00          ; 1.0 ; ; 1 # Lo       CJK UNIFIED IDEOGRAPH-4E00
00BD          ; 0.5 ; ; 1/2 # No       VULGAR FRACTION ONE HALF
2461          ; 2.0 ; ; 2 # No       CIRCLED DIGIT TWO
2160          ; 1.0 ; ; 1 # Nl       ROMAN NUMERAL ONE
11FC9..11FCA  ; 0.0625 ; ; 1/16 # No   [2] TAMIL FRACTION ONE SIXTEENTH-1..TAMIL FRACTION ONE SIXTEENTH-2
0BF0          ; ? ; ; ? # No       BROKEN VALUE
0BF1          ; nan ; ; ? # No       NO NUMERIC VALUE
0BF2          ; inf ; ; ? # No       INFINITE VALUE
ZZZZ          ; 3.0 ; ; 3 # No       BROKEN CODEPOINT
2460 no comment here
`

func TestParse(t *testing.T) {
	t.Parallel()

	fields, cat, ok := parse("00BD          ; 0.5 ; ; 1/2 # No       VULGAR FRACTION ONE HALF")
	require.True(t, ok)
	assert.Equal(t, "No", cat)
	require.Len(t, fields, 4)
	assert.Equal(t, "00BD", strings.TrimSpace(fields[0]))
	assert.Equal(t, "0.5", strings.TrimSpace(fields[1]))

	// Lines the parser rejects: no trailing comment, too few fields, empty comment.
	for _, line := range []string{
		"00BD          ; 0.5 ; ; 1/2",
		"00BD ; 0.5 # No",
		"00BD          ; 0.5 ; ; 1/2 #",
	} {
		_, _, ok := parse(line)
		assert.Falsef(t, ok, "parse(%q) should reject", line)
	}
}

func TestCodeRange(t *testing.T) {
	t.Parallel()

	lo, hi, err := codeRange("1F100")
	require.NoError(t, err)
	assert.Equal(t, rune(0x1F100), lo)
	assert.Equal(t, rune(0x1F100), hi, "a single codepoint is its own upper bound")

	lo, hi, err = codeRange("11FC9..11FCA")
	require.NoError(t, err)
	assert.Equal(t, rune(0x11FC9), lo)
	assert.Equal(t, rune(0x11FCA), hi)

	for _, s := range []string{"ZZZZ", "11FC9..ZZZZ", ""} {
		_, _, err := codeRange(s)
		assert.Errorf(t, err, "codeRange(%q) should fail", s)
	}
}

// TestGenerateNumerals runs the generator over the fixture and reads the emitted table back.
func TestGenerateNumerals(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ucdDir := filepath.Join(dir, "v15")
	require.NoError(t, os.MkdirAll(ucdDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(ucdDir, ucdFile), []byte(numericValuesFixture), 0o600))

	out := filepath.Join(dir, "numerals15.0.0.go")
	require.NoError(t, run("numbers", out, ucdDir, "!go1.27"))

	src, err := os.ReadFile(out)
	require.NoError(t, err)

	file, err := parser.ParseFile(token.NewFileSet(), out, src, parser.ParseComments)
	require.NoError(t, err, "the generated table must be valid Go")
	assert.Equal(t, "numbers", file.Name.Name)
	assert.Contains(t, string(src), "//go:build !go1.27")
	assert.Contains(t, string(src), "Generated from v15/DerivedNumericValues.txt")

	nums := numeralTable(t, file)

	t.Run("keeps No and Nl", func(t *testing.T) {
		assert.Equal(t, 0.5, nums['½'])
		assert.Equal(t, 2.0, nums['②'])
		assert.Equal(t, 1.0, nums['Ⅰ'])
	})

	t.Run("drops Nd digits and Lo ideographs", func(t *testing.T) {
		_, ok := nums['0']
		assert.False(t, ok, "Nd digits are handled by the digit offset")
		_, ok = nums['一']
		assert.False(t, ok, "Lo (CJK) numbers are elided")
	})

	t.Run("expands a range into one entry per rune", func(t *testing.T) {
		assert.Equal(t, 0.0625, nums[0x11FC9])
		assert.Equal(t, 0.0625, nums[0x11FCA])
	})

	t.Run("skips malformed lines", func(t *testing.T) {
		_, ok := nums[0x0BF0]
		assert.False(t, ok, "an unparseable value is skipped")
	})

	// ParseFloat accepts "nan" and "inf", but Go has no literal for either: FormatFloat would emit the identifiers
	// NaN and +Inf, and the table would not compile.
	t.Run("skips NaN and infinite values", func(t *testing.T) {
		for _, r := range []rune{0x0BF1, 0x0BF2} {
			_, ok := nums[r]
			assert.Falsef(t, ok, "U+%04X carries a non-finite value and must be dropped", r)
		}
		assert.NotContains(t, string(src), "NaN")
		assert.NotContains(t, string(src), "Inf")
		assert.Len(t, nums, 5, "only the well-formed, finite No/Nl lines are kept")
	})
}

func TestGenerateNumeralsMissingSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := run("numbers", filepath.Join(dir, "out.go"), filepath.Join(dir, "v15"), "")
	require.Error(t, err, "a missing extract must be reported, not silently produce an empty table")
}

// numeralTable reads the generated `var runeNumericValue = map[rune]float64{…}` back out of the emitted source.
func numeralTable(t *testing.T, file *ast.File) map[rune]float64 {
	t.Helper()

	out := map[rune]float64{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "runeNumericValue" {
				continue
			}
			lit, ok := value.Values[0].(*ast.CompositeLit)
			require.True(t, ok, "runeNumericValue is not a composite literal")

			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				require.True(t, ok, "runeNumericValue holds a non key/value element")

				key, ok := kv.Key.(*ast.BasicLit)
				require.True(t, ok, "map key is not a literal")
				r, err := strconv.ParseInt(key.Value, 0, 32)
				require.NoErrorf(t, err, "map key %q", key.Value)

				val, ok := kv.Value.(*ast.BasicLit)
				require.True(t, ok, "map value is not a literal")
				v, err := strconv.ParseFloat(val.Value, 64)
				require.NoErrorf(t, err, "map value %q", val.Value)

				out[rune(r)] = v
			}
		}
	}
	require.NotEmpty(t, out, "no runeNumericValue table in the generated source")

	return out
}

// TestResolveArgs covers the [package [outbase [version [ucd-root]]]] command line.
//
// It swaps os.Args, so it does not run in parallel.
func TestResolveArgs(t *testing.T) {
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })

	t.Run("defaults to the numbers package and the baseline version", func(t *testing.T) {
		if _, err := locate.UCDRoot(); err != nil {
			t.Skipf("no git checkout to resolve the default UCD root from: %v", err)
		}

		os.Args = []string{"gen_numerals"}
		pkg, outFile, ucdDir, buildTag := resolveArgs()

		baseline := locate.Versions[0]
		assert.Equal(t, "numbers", pkg)
		assert.Equal(t, "numerals"+baseline.UCD+".go", outFile)
		assert.Equal(t, baseline.Dir, filepath.Base(ucdDir))
		assert.Equal(t, locate.BuildConstraint(0), buildTag)
	})

	t.Run("takes every argument", func(t *testing.T) {
		root := t.TempDir()
		baseline := locate.Versions[0]

		os.Args = []string{"gen_numerals", "otherpkg", "othertable", baseline.UCD, root}
		pkg, outFile, ucdDir, buildTag := resolveArgs()

		assert.Equal(t, "otherpkg", pkg)
		assert.Equal(t, "othertable"+baseline.UCD+".go", outFile, "the version is appended to the output base name")
		assert.Equal(t, filepath.Join(root, baseline.Dir), ucdDir)
		assert.Equal(t, locate.BuildConstraint(0), buildTag)
	})
}
