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
	"testing"

	"github.com/go-openapi/codegen/mangling/ucd/internal/locate"
	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
)

// derivedNameFixture is a hand-picked slice of DerivedName.txt: one line per rule deriveFold applies, plus lines the
// generator must skip.
const derivedNameFixture = `# DerivedName-15.0.0.txt
# a comment line

0041          ; LATIN CAPITAL LETTER A
00E9          ; LATIN SMALL LETTER E WITH ACUTE
00C9          ; LATIN CAPITAL LETTER E WITH ACUTE
00E6          ; LATIN SMALL LETTER AE
01FC          ; LATIN CAPITAL LETTER AE WITH ACUTE
01C5          ; LATIN CAPITAL LETTER D WITH SMALL LETTER Z WITH CARON
0254          ; LATIN SMALL LETTER OPEN O
0294          ; LATIN LETTER GLOTTAL STOP
0416          ; CYRILLIC CAPITAL LETTER ZHE
FB00          ; LATIN SMALL LIGATURE FF
3400..4DBF    ; CJK UNIFIED IDEOGRAPH-*
`

func TestDeriveFold(t *testing.T) {
	t.Parallel()

	folded := map[string]string{
		"LATIN SMALL LETTER E WITH ACUTE":                       "e",
		"LATIN CAPITAL LETTER E WITH ACUTE":                     "E",
		"LATIN SMALL LETTER O WITH HORN":                        "o",
		"LATIN SMALL LETTER AE":                                 "ae",
		"LATIN CAPITAL LETTER AE WITH ACUTE":                    "AE",
		"LATIN SMALL LETTER DZ":                                 "dz",
		"LATIN SMALL LIGATURE FF":                               "ff",
		"LATIN CAPITAL LIGATURE OE":                             "OE",
		"LATIN CAPITAL LETTER D WITH SMALL LETTER Z WITH CARON": "D",
	}
	for name, want := range folded {
		got, ok := deriveFold(name)
		assert.Truef(t, ok, "deriveFold(%q) should fold", name)
		assert.Equalf(t, want, got, "deriveFold(%q)", name)
	}

	// Distinct letters with no ASCII base, and non-Latin names.
	for _, name := range []string{
		"LATIN SMALL LETTER OPEN O",
		"LATIN SMALL LETTER SCHWA",
		"LATIN SMALL LETTER ESH",
		"LATIN LETTER GLOTTAL STOP", // uncased: IPA/phonetic, not a cased letter
		"CYRILLIC CAPITAL LETTER ZHE",
		"GREEK SMALL LETTER ALPHA",
		"SNOWMAN",
	} {
		got, ok := deriveFold(name)
		assert.Falsef(t, ok, "deriveFold(%q) should not fold", name)
		assert.Emptyf(t, got, "deriveFold(%q)", name)
	}
}

// TestGenerateAsciifold runs the generator over the fixture and reads the emitted table back.
func TestGenerateAsciifold(t *testing.T) {
	t.Parallel()

	out := generateAsciifold(t, derivedNameFixture, "!go1.27")
	src, err := os.ReadFile(out)
	require.NoError(t, err)

	file, err := parser.ParseFile(token.NewFileSet(), out, src, parser.ParseComments)
	require.NoError(t, err, "the generated table must be valid Go")
	assert.Equal(t, "mangling", file.Name.Name)
	assert.Contains(t, string(src), "//go:build !go1.27")
	assert.Contains(t, string(src), "DO NOT EDIT")
	assert.Contains(t, string(src), "Generated from v15/DerivedName.txt", "the source path is recorded relative to the ucd root")

	folds := asciiFoldTable(t, file)

	t.Run("derives a fold per name rule", func(t *testing.T) {
		for r, want := range map[rune]string{
			'é': "e",
			'É': "E",
			'æ': "ae",
			'Ǽ': "AE",
			'ﬀ': "ff",
		} {
			assert.Equalf(t, want, folds[r], "asciiFold[%q]", r)
		}
	})

	t.Run("skips runes with no ASCII base", func(t *testing.T) {
		for _, r := range []rune{
			'A', // ASCII, handled directly
			'ɔ', // OPEN O: a distinct letter
			'ʔ', // uncased LATIN LETTER
			'Ж', // not Latin
		} {
			_, ok := folds[r]
			assert.Falsef(t, ok, "asciiFold should not hold %q", r)
		}
	})

	t.Run("a seed overrides the derived fold", func(t *testing.T) {
		// U+01C5 derives to "D" from its name (see TestDeriveFold), but the seeds table renders the titlecase
		// digraph as "Dz".
		assert.Equal(t, "Dz", folds['ǅ'])
	})

	t.Run("emits every seed", func(t *testing.T) {
		for r, want := range seeds {
			assert.Equalf(t, want, folds[r], "seed %q missing from the table", r)
		}
	})
}

// TestGenerateAsciifoldWithoutBuildTag covers the lone-version case: no //go:build line is emitted.
func TestGenerateAsciifoldWithoutBuildTag(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(generateAsciifold(t, derivedNameFixture, ""))
	require.NoError(t, err)
	assert.NotContains(t, string(src), "//go:build")
}

func TestGenerateAsciifoldMissingSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := run("mangling", filepath.Join(dir, "out.go"), filepath.Join(dir, "v15"), "")
	require.Error(t, err, "a missing extract must be reported, not silently produce an empty table")
}

// generateAsciifold writes the fixture as a versioned UCD directory, runs the generator over it and returns the path
// of the emitted table.
func generateAsciifold(t *testing.T, fixture, buildTag string) string {
	t.Helper()

	dir := t.TempDir()
	ucdDir := filepath.Join(dir, "v15")
	require.NoError(t, os.MkdirAll(ucdDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(ucdDir, ucdFile), []byte(fixture), 0o600))

	out := filepath.Join(dir, "asciifold_table15.0.0.go")
	require.NoError(t, run("mangling", out, ucdDir, buildTag))

	return out
}

// asciiFoldTable reads the generated `var asciiFold = map[rune]string{…}` back out of the emitted source.
func asciiFoldTable(t *testing.T, file *ast.File) map[rune]string {
	t.Helper()

	out := map[rune]string{}
	for _, kv := range mapEntries(t, file, "asciiFold") {
		key, ok := kv.Key.(*ast.BasicLit)
		require.True(t, ok, "map key is not a literal")
		r, err := strconv.ParseInt(key.Value, 0, 32)
		require.NoErrorf(t, err, "map key %q", key.Value)

		val, ok := kv.Value.(*ast.BasicLit)
		require.True(t, ok, "map value is not a literal")
		s, err := strconv.Unquote(val.Value)
		require.NoErrorf(t, err, "map value %q", val.Value)

		out[rune(r)] = s
	}
	require.NotEmpty(t, out, "no %s table in the generated source", "asciiFold")

	return out
}

// mapEntries returns the key/value elements of the map literal assigned to the package-level variable varName.
func mapEntries(t *testing.T, file *ast.File, varName string) []*ast.KeyValueExpr {
	t.Helper()

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != varName || len(value.Values) != 1 {
				continue
			}
			lit, ok := value.Values[0].(*ast.CompositeLit)
			require.Truef(t, ok, "%s is not a composite literal", varName)

			out := make([]*ast.KeyValueExpr, 0, len(lit.Elts))
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				require.Truef(t, ok, "%s holds a non key/value element", varName)
				out = append(out, kv)
			}

			return out
		}
	}
	require.FailNowf(t, "variable not found", "no package-level var %s in the generated source", varName)

	return nil
}

// TestResolveArgs covers the [package [outbase [version [ucd-root]]]] command line.
//
// It swaps os.Args, so it does not run in parallel.
func TestResolveArgs(t *testing.T) {
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })

	t.Run("defaults to the mangling package and the baseline version", func(t *testing.T) {
		if _, err := locate.UCDRoot(); err != nil {
			t.Skipf("no git checkout to resolve the default UCD root from: %v", err)
		}

		os.Args = []string{"gen_asciifold"}
		pkg, outFile, ucdDir, buildTag := resolveArgs()

		baseline := locate.Versions[0]
		assert.Equal(t, "mangling", pkg)
		assert.Equal(t, "asciifold_table"+baseline.UCD+".go", outFile)
		assert.Equal(t, baseline.Dir, filepath.Base(ucdDir))
		assert.Equal(t, locate.BuildConstraint(0), buildTag)
	})

	t.Run("takes every argument", func(t *testing.T) {
		root := t.TempDir()
		baseline := locate.Versions[0]

		os.Args = []string{"gen_asciifold", "otherpkg", "othertable", baseline.UCD, root}
		pkg, outFile, ucdDir, buildTag := resolveArgs()

		assert.Equal(t, "otherpkg", pkg)
		assert.Equal(t, "othertable"+baseline.UCD+".go", outFile, "the version is appended to the output base name")
		assert.Equal(t, filepath.Join(root, baseline.Dir), ucdDir)
		assert.Equal(t, locate.BuildConstraint(0), buildTag)
	})
}
