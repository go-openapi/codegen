// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package numbers

import (
	"math"
	"strconv"
)

// maxFullCardinal is the default magnitude up to which a number is spelled out in full.
//
// Above it, [hybridCardinal] keeps the lower groups as digits to bound the output length.
const maxFullCardinal = 1_000_000

var (
	cardinalOnes = []string{
		"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
		"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen",
		"seventeen", "eighteen", "nineteen",
	}
	cardinalTens = []string{
		"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety",
	}
	// cardinalScales[i] is the word for 1000^i (index 0 = units, unnamed).
	//
	// Covers the int64 range.
	cardinalScales = []string{"", "thousand", "million", "billion", "trillion", "quadrillion", "quintillion"}
)

// wordList streams space-separated words into a [strings.Builder]: the first word is written flush, each subsequent
// word is preceded by a single space.
//
// This replaces the old "collect []string then strings.Join" pattern (which allocated a slice per number and again per
// 3-digit group) with a single shared buffer.
type wordList struct {
	b       *buf
	written bool
}

func (w *wordList) add(s string) {
	if w.written {
		_ = w.b.WriteByte(' ')
	}
	_, _ = w.b.WriteString(s)
	w.written = true
}

func (w *wordList) addBytes(p []byte) {
	if w.written {
		_ = w.b.WriteByte(' ')
	}
	_, _ = w.b.Write(p)
	w.written = true
}

// cardinal renders an integer as english words: "one hundred and twenty three".
//
// Zero is "zero", negatives are prefixed with "minus".
//
// Options:
//
//   - stripOne elides a leading "one" before a scale ("one hundred" -> "hundred");
//   - stripAnd elides the "and".
//
// Numbers above [maxFullCardinal] use the compact [hybridCardinal] form.
func cardinal(n int64, o numberOptions) string {
	var b buf
	writeCardinal(&b, n, o)

	return unsafeStr(b.b)
}

// writeCardinal streams [cardinal] into b (no intermediate slices/strings).
func writeCardinal(b *buf, n int64, o numberOptions) {
	if n == 0 {
		_, _ = b.WriteString(cardinalOnes[0]) // "zero"

		return
	}

	if n < 0 {
		_, _ = b.WriteString("minus ")

		if n == math.MinInt64 {
			// -2^63 has no positive int64 counterpart, so negating it would leave n negative and spell nothing at all.
			// Spell its digits instead, as [writeSpellDecimal] does for an integer too large for int64.
			writeDigitWords(b, "9223372036854775808")

			return
		}

		n = -n
	}

	if n > maxFullCardinal {
		writeHybridCardinal(b, n, o)

		return
	}

	var buf [maxGroups]int64
	groups := groupsOf1000Into(buf[:0], n)

	w := wordList{b: b}
	for i, grp := range groups {
		if grp == 0 {
			continue
		}
		scale := len(groups) - 1 - i

		// British "and" before a units group that is a bare 1..99 (a hundreds part carries its own "and").
		if scale == 0 && grp < 100 && w.written && !o.stripAnd {
			w.add("and")
		}

		if scale > 0 && grp == 1 && o.stripOne {
			w.add(cardinalScales[scale]) // "thousand", not "one thousand"

			continue
		}

		writeThreeDigits(&w, int(grp), o)
		if scale > 0 {
			w.add(cardinalScales[scale])
		}
	}
}

// writeHybridCardinal renders n > maxFullCardinal compactly, streamed into b.
//
// The most-significant group is spelled out (so the result never starts with a digit and stays short), while lower
// groups are kept as digits with a plural scale word, e.g. 1234567 -> "one million 234 thousands and 567".
func writeHybridCardinal(b *buf, n int64, o numberOptions) {
	var buf [maxGroups]int64
	groups := groupsOf1000Into(buf[:0], n)

	var num [20]byte // scratch for base-10 digits, avoids strconv.FormatInt allocation
	const digitsBase = 10
	w := wordList{b: b}
	for i, grp := range groups {
		if grp == 0 {
			continue
		}
		scale := len(groups) - 1 - i

		switch {
		case i == 0: // top group: spelled out
			writeThreeDigits(&w, int(grp), o)
			if scale > 0 {
				w.add(cardinalScales[scale])
			}
		case scale == 0: // units group
			if w.written && !o.stripAnd {
				w.add("and")
			}
			w.addBytes(strconv.AppendInt(num[:0], grp, digitsBase))
		default: // middle group: digits + plural scale
			w.addBytes(strconv.AppendInt(num[:0], grp, digitsBase))
			w.add(cardinalScales[scale])
			_ = b.WriteByte('s') // "thousand" -> "thousands", no separator
		}
	}
}

// maxGroups is the number of 3-digit groups spanning the int64 range (19 digits -> 7 groups).
const maxGroups = 7

// groupsOf1000Into fills dst (backed by a caller stack array of cap >= maxGroups) with the 3-digit groups of a positive
// integer, most significant first, and returns the filled slice.
//
// No heap allocation.
func groupsOf1000Into(dst []int64, n int64) []int64 {
	const thousandBase = 1000
	start := len(dst)
	for m := n; m > 0; m /= thousandBase {
		dst = append(dst, m%thousandBase) // least significant first
	}
	for l, r := start, len(dst)-1; l < r; l, r = l+1, r-1 {
		dst[l], dst[r] = dst[r], dst[l] // reverse to most significant first
	}

	return dst
}

// writeThreeDigits renders 1..999 into w.
func writeThreeDigits(w *wordList, n int, o numberOptions) {
	const hundredBase = 100
	hundreds, rest := n/hundredBase, n%hundredBase

	if hundreds > 0 {
		if hundreds != 1 || !o.stripOne {
			w.add(cardinalOnes[hundreds])
		}
		w.add("hundred")
	}
	if rest > 0 {
		if hundreds > 0 && !o.stripAnd {
			w.add("and")
		}
		writeTwoDigits(w, rest)
	}
}

// writeTwoDigits renders 1..99 into w.
func writeTwoDigits(w *wordList, n int) {
	const (
		doubleDigitsBound = 20
		singleDigitsBound = 10
	)

	switch {
	case n < doubleDigitsBound:
		w.add(cardinalOnes[n])
	case n%singleDigitsBound == 0:
		w.add(cardinalTens[n/singleDigitsBound])
	default:
		w.add(cardinalTens[n/singleDigitsBound])
		w.add(cardinalOnes[n%singleDigitsBound])
	}
}
