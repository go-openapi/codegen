// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/go-openapi/codegen/mangling/ucd/internal/locate"
	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
)

// derivedNameFixture is a hand-picked slice of DerivedName.txt: two consecutive kept runes (so the emitter builds a
// multi-rune run), one kept rune per summarization rule, and one line per exclusion reason.
const derivedNameFixture = `# DerivedName-15.0.0.txt
# a comment line

0041          ; LATIN CAPITAL LETTER A
00E9          ; LATIN SMALL LETTER E WITH ACUTE
0301          ; COMBINING ACUTE ACCENT
0030          ; DIGIT ZERO
00BD          ; VULGAR FRACTION ONE HALF
0020          ; SPACE
03B1          ; GREEK SMALL LETTER ALPHA
03B2          ; GREEK SMALL LETTER BETA
0416          ; CYRILLIC CAPITAL LETTER ZHE
20AC          ; EURO SIGN
2713          ; CHECK MARK
2764          ; HEAVY BLACK HEART
4E00          ; CJK UNIFIED IDEOGRAPH-4E00
AC00          ; HANGUL SYLLABLE GA
1F600         ; GRINNING FACE
3400..4DBF    ; CJK UNIFIED IDEOGRAPH-*
`

// emojiDataFixture mirrors emoji-data.txt: the property name butts up against the '#' comment, as it does in the real
// extract, and the file mixes single codepoints with ranges.
const emojiDataFixture = `# emoji-data.txt
# All omitted code points have Extended_Pictographic=No

0023          ; Emoji                # E0.0   [1] (#) hash sign
2764          ; Extended_Pictographic# E0.6   [1] (❤) red heart
1F600..1F64F  ; Extended_Pictographic# E1.0  [80] (😀..🙏) grinning face..folded hands
`

func TestCollapse(t *testing.T) {
	t.Parallel()

	// One or two words are kept whole; 3+ words reduce to the longest word that is neither glue nor a qualifier.
	for in, want := range map[string]string{
		"alpha":             "alpha",
		"grinning face":     "grinning face",
		"heavy black heart": "heart",
		"place of sajdah":   "sajdah",
		"fehu feoh fe f":    "fehu",
		"black white heavy": "heavy", // all words skipped: fall back to the last one
	} {
		assert.Equalf(t, want, collapse(in), "collapse(%q)", in)
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()

	// Names a taxonomy rule matches: LETTER / SYLLABLE / CHARACTER / NUMBER, then SIGN|SYMBOL.
	for name, want := range map[string]string{
		"GREEK SMALL LETTER ALPHA":       "alpha",
		"CYRILLIC CAPITAL LETTER ZHE":    "zhe",
		"GREEK SMALL LETTER LAMDA":       "lambda", // wordOverrides fixes Unicode's spelling
		"KATAKANA LETTER SMALL A":        "small a",
		"HIRAGANA LETTER A WITH DAKUTEN": "a", // the "WITH …" diacritic tail is dropped
		"HANGUL SYLLABLE GA":             "ga",
		"THAI CHARACTER KO KAI":          "ko kai",
		"VULGAR FRACTION ONE HALF":       "one half",
		"GREEK BETA SYMBOL":              "greek beta",
		"EURO SIGN":                      "euro",
		"ROMAN NUMERAL ONE":              "one",
		"ARABIC PLACE OF SAJDAH":         "sajdah", // the script prefix is peeled, then 3 words collapse
	} {
		got, ok := summarize(name)
		assert.Truef(t, ok, "summarize(%q) should match a rule", name)
		assert.Equalf(t, want, got, "summarize(%q)", name)
	}

	// No rule matches: the whole name is kept as a failsafe, and the caller is told.
	got, ok := summarize("GRINNING FACE")
	assert.False(t, ok, "summarize should report an unmatched name")
	assert.Equal(t, "grinning face", got)
}

func TestStripScriptPrefix(t *testing.T) {
	t.Parallel()

	got, ok := stripScriptPrefix("ARABIC PLACE OF SAJDAH")
	assert.True(t, ok)
	assert.Equal(t, "PLACE OF SAJDAH", got)

	// The last word is never stripped, and a name that does not start with a script token is left alone.
	got, ok = stripScriptPrefix("GREEK")
	assert.False(t, ok)
	assert.Equal(t, "GREEK", got)

	got, ok = stripScriptPrefix("HEAVY BLACK HEART")
	assert.False(t, ok)
	assert.Equal(t, "HEAVY BLACK HEART", got)
}

// TestPictographicGate covers loading emoji-data.txt and the membership search over it.
//
// It writes the package-level pictRanges, so it does not run in parallel (see TestGenerateRunewords).
func TestPictographicGate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ucdEmojiFile)
	require.NoError(t, os.WriteFile(path, []byte(emojiDataFixture), 0o600))

	ranges, err := loadPictographic(path)
	require.NoError(t, err)
	require.Len(t, ranges, 2, "only Extended_Pictographic lines are loaded")
	assert.Equal(t, rrange{0x2764, 0x2764}, ranges[0])
	assert.Equal(t, rrange{0x1F600, 0x1F64F}, ranges[1])

	pictRanges = ranges
	t.Cleanup(func() { pictRanges = nil })

	for _, r := range []rune{'❤', '😀', '🙏', 0x1F64F} {
		assert.Truef(t, isPictographic(r), "%q should be pictographic", r)
	}
	for _, r := range []rune{'#', '✓', 0x2763, 0x2765, 0x1F5FF, 0x1F650} {
		assert.Falsef(t, isPictographic(r), "%q should not be pictographic", r)
	}

	_, err = loadPictographic(filepath.Join(dir, "absent.txt"))
	require.Error(t, err)
}

// TestGenerateRunewords runs the generator over the fixtures and decodes the emitted tables the way runewords.Word
// does, so the whole pipeline — classify, summarize, intern, interval-encode — is checked end to end.
//
// It is not parallel: run() sets the package-level pictRanges.
func TestGenerateRunewords(t *testing.T) {
	dir := t.TempDir()
	ucdDir := filepath.Join(dir, "v15")
	require.NoError(t, os.MkdirAll(ucdDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(ucdDir, ucdFile), []byte(derivedNameFixture), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(ucdDir, ucdEmojiFile), []byte(emojiDataFixture), 0o600))
	t.Cleanup(func() { pictRanges = nil })

	out := filepath.Join(dir, "tables15.0.0.go")
	require.NoError(t, run("runewords", out, ucdDir, "go1.27"))

	src, err := os.ReadFile(out)
	require.NoError(t, err)

	file, err := parser.ParseFile(token.NewFileSet(), out, src, parser.ParseComments)
	require.NoError(t, err, "the generated tables must be valid Go")
	assert.Equal(t, "runewords", file.Name.Name)
	assert.Contains(t, string(src), "//go:build go1.27")
	assert.Contains(t, string(src), "Generated from v15/DerivedName.txt")

	tbl := readTables(t, file)

	t.Run("covered runes decode to their word", func(t *testing.T) {
		for r, want := range map[rune]string{
			'α': "alpha",
			'β': "beta",
			'Ж': "zhe",
			'€': "euro",
			'❤': "heart",
			'😀': "grinning face",
		} {
			got, ok := tbl.word(r)
			assert.Truef(t, ok, "U+%04X should be covered", r)
			assert.Equalf(t, want, got, "word(%q)", r)
		}
	})

	t.Run("excluded runes are absent", func(t *testing.T) {
		for name, r := range map[string]rune{
			"ascii":     'A',
			"latin":     'é',
			"combining": 0x0301,
			"digit":     '0',
			"numeral":   '½',
			"separator": ' ',
			"han":       '一',
			"hangul":    '가',
			"block":     '✓', // Dingbats, and not Extended_Pictographic
		} {
			_, ok := tbl.word(r)
			assert.Falsef(t, ok, "%s rune U+%04X should not be covered", name, r)
		}
	})

	t.Run("the encoding is well formed", func(t *testing.T) {
		require.Len(t, tbl.runFirstIndex, len(tbl.runStart)+1, "runFirstIndex must carry a trailing sentinel")
		assert.Equal(t, uint32(0), tbl.runFirstIndex[0])
		assert.Equal(t, uint32(len(tbl.wordID)), tbl.runFirstIndex[len(tbl.runFirstIndex)-1], "the sentinel is the rune count") //nolint:gosec // the fixture holds a handful of runes, so the count fits in uint32
		assert.Len(t, tbl.wordID, 6, "six runes survive classification")

		// α and β are consecutive, so they share one run; the other four runes stand alone.
		assert.Len(t, tbl.runStart, 5)
		assert.Equal(t, uint32('α'), tbl.runStart[0])
		assert.Equal(t, uint32(2), tbl.runFirstIndex[1], "the greek run holds two runes")

		for i := 1; i < len(tbl.runStart); i++ {
			assert.Lessf(t, tbl.runStart[i-1], tbl.runStart[i], "runStart not ascending at %d", i)
			assert.Lessf(t, tbl.runFirstIndex[i-1], tbl.runFirstIndex[i], "runFirstIndex not ascending at %d", i)
		}

		assert.Equal(t, uint32(0), tbl.offset(0))
		assert.Equal(t, uint32(len(tbl.blob)), tbl.offset(uint32(len(tbl.offLo)-1)), "the last offset closes the blob") //nolint:gosec // the fixture blob is a few dozen bytes long
	})

	t.Run("words are interned once", func(t *testing.T) {
		assert.Equal(t, "alphabetazheeuroheartgrinning face", tbl.blob)
	})
}

func TestGenerateRunewordsMissingSource(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { pictRanges = nil })

	ucdDir := filepath.Join(dir, "v15")
	require.NoError(t, os.MkdirAll(ucdDir, 0o750))

	err := run("runewords", filepath.Join(dir, "out.go"), ucdDir, "")
	require.Error(t, err, "a missing emoji-data.txt must be reported")

	require.NoError(t, os.WriteFile(filepath.Join(ucdDir, ucdEmojiFile), []byte(emojiDataFixture), 0o600))
	err = run("runewords", filepath.Join(dir, "out.go"), ucdDir, "")
	require.Error(t, err, "a missing DerivedName.txt must be reported")
}

// tables holds the generated arrays, read back from the emitted source.
type tables struct {
	blob          string
	offLo         []uint32
	offHi         []uint32
	runStart      []uint32
	runFirstIndex []uint32
	wordID        []uint32
}

// offset reconstructs an 18-bit blob offset from the uint16 low array and the 2-bit high sidecar, as
// runewords.offset18 does.
func (tb tables) offset(id uint32) uint32 {
	hi := (tb.offHi[id>>2] >> (2 * (id & 3))) & 0x3

	return hi<<16 | tb.offLo[id]
}

// word looks a rune up through the interval encoding, mirroring runewords.Word.
func (tb tables) word(r rune) (string, bool) {
	u := uint32(r) //nolint:gosec // false positive: rune aliases to int32, so it is okay to consider the result unsigned

	i := sort.Search(len(tb.runStart), func(i int) bool { return tb.runStart[i] > u })
	if i == 0 {
		return "", false
	}
	i--

	pos := tb.runFirstIndex[i] + (u - tb.runStart[i])
	if pos >= tb.runFirstIndex[i+1] {
		return "", false
	}

	id := tb.wordID[pos]

	return tb.blob[tb.offset(id):tb.offset(id+1)], true
}

func readTables(t *testing.T, file *ast.File) tables {
	t.Helper()

	return tables{
		blob:          stringConst(t, file, "wordBlob"),
		offLo:         uintSlice(t, file, "wordOffLo"),
		offHi:         uintSlice(t, file, "wordOffHi"),
		runStart:      uintSlice(t, file, "runStart"),
		runFirstIndex: uintSlice(t, file, "runFirstIndex"),
		wordID:        uintSlice(t, file, "nameWordID"),
	}
}

// stringConst returns the value of the generated string constant named name.
func stringConst(t *testing.T, file *ast.File, name string) string {
	t.Helper()

	lit, ok := declValue(file, token.CONST, name).(*ast.BasicLit)
	require.Truef(t, ok, "const %s is not a literal", name)
	s, err := strconv.Unquote(lit.Value)
	require.NoErrorf(t, err, "const %s", name)

	return s
}

// uintSlice returns the elements of the generated integer slice named name.
func uintSlice(t *testing.T, file *ast.File, name string) []uint32 {
	t.Helper()

	lit, ok := declValue(file, token.VAR, name).(*ast.CompositeLit)
	require.Truef(t, ok, "var %s is not a composite literal", name)

	out := make([]uint32, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		e, ok := elt.(*ast.BasicLit)
		require.Truef(t, ok, "var %s holds a non-literal element", name)
		v, err := strconv.ParseUint(e.Value, 0, 32)
		require.NoErrorf(t, err, "var %s element %q", name, e.Value)
		out = append(out, uint32(v))
	}
	require.NotEmptyf(t, out, "var %s is empty", name)

	return out
}

// declValue returns the expression assigned to the package-level const or var named name.
func declValue(file *ast.File, tok token.Token, name string) ast.Expr {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != tok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != name || len(value.Values) != 1 {
				continue
			}

			return value.Values[0]
		}
	}

	return nil
}

// TestResolveArgs covers the [package [outbase [version [ucd-root]]]] command line.
//
// It swaps os.Args, so it does not run in parallel.
func TestResolveArgs(t *testing.T) {
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })

	t.Run("defaults to the runewords package and the baseline version", func(t *testing.T) {
		if _, err := locate.UCDRoot(); err != nil {
			t.Skipf("no git checkout to resolve the default UCD root from: %v", err)
		}

		os.Args = []string{"gen_runewords"}
		pkg, outFile, ucdDir, buildTag := resolveArgs()

		baseline := locate.Versions[0]
		assert.Equal(t, "runewords", pkg)
		assert.Equal(t, "tables"+baseline.UCD+".go", outFile)
		assert.Equal(t, baseline.Dir, filepath.Base(ucdDir))
		assert.Equal(t, locate.BuildConstraint(0), buildTag)
	})

	t.Run("takes every argument", func(t *testing.T) {
		root := t.TempDir()
		baseline := locate.Versions[0]

		os.Args = []string{"gen_runewords", "otherpkg", "othertable", baseline.UCD, root}
		pkg, outFile, ucdDir, buildTag := resolveArgs()

		assert.Equal(t, "otherpkg", pkg)
		assert.Equal(t, "othertable"+baseline.UCD+".go", outFile, "the version is appended to the output base name")
		assert.Equal(t, filepath.Join(root, baseline.Dir), ucdDir)
		assert.Equal(t, locate.BuildConstraint(0), buildTag)
	})
}
