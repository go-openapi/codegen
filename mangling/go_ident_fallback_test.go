// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package mangling

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/go-openapi/testify/v2/assert"
)

// TestGoIdentFallback covers the contract that Go identifier producers never emit an empty identifier.
//
// Input that reduces to nothing (empty, all-separators, or all-elided runes) yields a fallback word, cased per target.
//
// Package/Module are exempt (they carry a dir prefix), and the base Mangler is not.
func TestGoIdentFallback(t *testing.T) {
	t.Parallel()

	// each of these reduces to nothing after segmentation / ASCII folding
	// (note: "---" is no longer here — "--" now verbalizes as "decrement")
	emptyish := []string{"", "___", "   ", "日本" /* CJK, folded away */, "_", "."}

	t.Run("default fallback, cased per target", func(t *testing.T) {
		t.Parallel()
		g := MakeGoMangler()
		for _, in := range emptyish {
			if in == "." {
				continue // "." → "Dot" (a symbol with a name), not empty — covered elsewhere
			}
			assert.EqualTf(t, "Empty", g.IdentExported(in), "IdentExported(%q)", in)
			assert.EqualTf(t, "empty", g.IdentUnexported(in), "IdentUnexported(%q)", in)
			assert.EqualTf(t, "Empty", g.ConstName(in), "ConstName(%q)", in)
			assert.EqualTf(t, "empty", g.File(in), "File(%q)", in)
		}
	})

	t.Run("File keeps dir and extension around the fallback stem", func(t *testing.T) {
		t.Parallel()
		g := MakeGoMangler()
		assert.EqualT(t, "sub/empty.go", g.File("sub/___.go"))
		assert.EqualT(t, "a/b/empty", g.File("a/b/___"))
	})

	t.Run("Package and Module stay empty-allowed (dir-only)", func(t *testing.T) {
		t.Parallel()
		g := MakeGoMangler()
		short, pkg := g.Package("___")
		assert.EqualT(t, "", short)
		assert.EqualT(t, "", pkg)
		assert.EqualT(t, "sub/", g.Module("sub/___"))
	})

	t.Run("configured fallback is itself sanitized and cased", func(t *testing.T) {
		t.Parallel()
		g := MakeGoMangler(WithGoIdentFallback("unknown value"))
		assert.EqualT(t, "UnknownValue", g.IdentExported("___"))
		assert.EqualT(t, "unknownValue", g.IdentUnexported("___"))
		assert.EqualT(t, "unknown_value", g.File("___"))
	})

	t.Run("a fallback that itself reduces to nothing guards to the built-in default", func(t *testing.T) {
		t.Parallel()
		g := MakeGoMangler(WithGoIdentFallback("___"))
		assert.EqualT(t, "Empty", g.IdentExported("___"))
		assert.EqualT(t, "empty", g.IdentUnexported(""))
	})

	t.Run("non-empty input is unaffected", func(t *testing.T) {
		t.Parallel()
		g := MakeGoMangler()
		assert.EqualT(t, "HelloWorld", g.IdentExported("hello world"))
		assert.EqualT(t, "AtHashDollar", g.IdentExported("@#$")) // symbols name themselves, not empty
	})

	t.Run("the base Mangler has no such contract (may return empty)", func(t *testing.T) {
		t.Parallel()
		m := MakeMangler(WithASCIIFolding(true))
		assert.EqualT(t, "", m.Camelize("___"))
		assert.EqualT(t, "", m.Pascalize("日本")) // elided CJK runes
	})
}

// TestGoIdentExtremeNumbers covers the sibling contract: whatever the magnitude of a number in the input, a Go
// identifier producer verbalizes it and never leaves a leading digit behind.
//
// The numbers engine spells an integer too large for int64 digit by digit, and -2^63 the same way.
func TestGoIdentExtremeNumbers(t *testing.T) {
	t.Parallel()

	g := MakeGoMangler()
	for _, in := range []string{
		"99999999999999999999.5",              // integer part beyond int64
		"-9223372036854775808 items",          // -2^63, no positive int64 counterpart
		"1" + strings.Repeat("0", 400),        // beyond float64 range: ParseFloat overflows to +Inf
		"1" + strings.Repeat("0", 400) + ".5", // same, with a fractional part
	} {
		got := g.IdentExported(in)
		assert.NotEmptyf(t, got, "IdentExported(%q)", in)
		first, _ := utf8.DecodeRuneInString(got)
		assert.Truef(t, unicode.IsLetter(first), "IdentExported(%q) starts with %q: %.40q", in, first, got)
	}
}
