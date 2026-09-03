// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package numbers

import (
	"math"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
)

// TestNumberWordsOverflow covers the writeSpellDecimal fallback: an integer too large for int64 is spelled digit by
// digit (so it is still verbalized and never leaves a leading digit).
func TestNumberWordsOverflow(t *testing.T) {
	t.Parallel()

	m := MakeNumberMangler()

	got := m.NumberWords("10000000000000000000") // 10^19, > int64 max
	assert.EqualT(t, "one"+strings.Repeat(" zero", 19), got)

	neg := m.NumberWords("-10000000000000000000")
	assert.Falsef(t, strings.ContainsAny(neg, "0123456789"), "overflow number left raw digits: %q", neg)
	assert.EqualT(t, "minus one"+strings.Repeat(" zero", 19), neg)
}

// TestNonFiniteValues covers the wording of NaN and the two infinities, which have no cardinal form.
//
// The generated numeral table holds finite values only (see the gen_numerals command), so this guards every other way
// a value reaches the verbalizer.
func TestNonFiniteValues(t *testing.T) {
	t.Parallel()

	var o numberOptions

	assert.EqualT(t, "not a number", numberWords(math.NaN(), o))
	assert.EqualT(t, "infinity", numberWords(math.Inf(1), o))
	assert.EqualT(t, "minus infinity", numberWords(math.Inf(-1), o))

	// The streaming form agrees with the string form.
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		var b buf
		writeNumberValue(&b, x, o)
		assert.EqualTf(t, numberWords(x, o), string(b.b), "writeNumberValue(%v)", x)
	}
}

// TestNumberWordsDecimalOverflow covers a decimal whose integer part is too large for int64.
//
// strconv.ParseInt clamps such an integer to int64 max, so the digits are spelled one by one instead — as
// [TestNumberWordsOverflow] does for a plain integer.
func TestNumberWordsDecimalOverflow(t *testing.T) {
	t.Parallel()

	m := MakeNumberMangler()

	got := m.NumberWords("99999999999999999999.5") // 20 nines, > int64 max
	assert.EqualT(t, strings.Repeat("nine ", 20)+"dot five", got)

	// Beyond float64 range: strconv.ParseFloat returns +Inf with strconv.ErrRange.
	huge := m.NumberWords("1" + strings.Repeat("0", 400) + ".5")
	assert.Falsef(t, strings.ContainsAny(huge, "0123456789"), "overflowing decimal left raw digits: %q", huge)
	assert.Truef(t, strings.HasSuffix(huge, " dot five"), "overflowing decimal lost its fractional part: %q", huge)
}

// TestCardinalMinInt64 covers -2^63, the one negative int64 with no positive counterpart: negating it leaves it
// negative, so writeCardinal spells its digits instead of emitting "minus " and nothing else.
func TestCardinalMinInt64(t *testing.T) {
	t.Parallel()

	//nolint:dupword // the repeated words are the digit-by-digit spelling of 9223372036854775808
	const want = "minus nine two two three three seven two zero three six eight five four seven seven five eight zero eight"

	assert.EqualT(t, want, cardinal(math.MinInt64, numberOptions{}))
	assert.EqualT(t, want, MakeNumberMangler().NumberWords("-9223372036854775808"))
}

// TestWithNumberDetectPrecision covers the tolerance knob: a tighter precision stops a loose decimal from matching a
// simple fraction.
func TestWithNumberDetectPrecision(t *testing.T) {
	t.Parallel()

	assert.EqualT(t, "one third", MakeNumberMangler().NumberWords("0.333")) // default precision 3: matches 1/3
	//nolint:dupword // "three three three" is the correct digit-by-digit spelling of 0.333
	assert.EqualT(t, "zero dot three three three",
		MakeNumberMangler(WithNumberDetectPrecision(6)).NumberWords("0.333")) // precision 6: no longer 1/3
}

func TestNewNumberMangler(t *testing.T) {
	t.Parallel()

	require := NewNumberMangler()
	assert.NotNil(t, require)
	assert.EqualT(t, MakeNumberMangler().NumberWords("123"), require.NumberWords("123"))
}
