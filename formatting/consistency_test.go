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

func TestInconsistentImports(t *testing.T) {
	t.Parallel()

	// each fixture names the mismatch its message has to carry
	reported := map[string]string{
		"one-package-two-names":      `the package "bytes" is imported under 2 names, "b" and "bytes"`,
		"one-alias-two-packages":     `the name "x" is bound to 2 packages, "bytes" and "strings"`,
		"two-packages-same-base":     `the name "rand" is bound to 2 packages, "crypto/rand" and "math/rand"`,
		"alias-shadows-another-base": `the name "rand" is bound to 2 packages, "crypto/rand" and "math/rand"`,
		"base-collides-with-a-longer-path": `the name "core" is bound to 2 packages, ` +
			`"github.com/go-openapi/core" and "k8s.io/api/core/v1"`,
	}

	for fixture, toPin := range sourceSet(t, "inconsistent") {
		src := toPin

		t.Run("should reject "+caseName(fixture), func(t *testing.T) {
			t.Parallel()

			var out countingWriter
			_, err := formatting.Format(&out, []byte(src))

			require.Error(t, err)
			assert.ErrorIs(t, err, formatting.ErrInconsistentImports)
			assert.ErrorIs(t, err, formatting.ErrFormat)
			assert.Zero(t, out.writes, "nothing is printed")

			if message, pinned := reported[fixture]; pinned {
				assert.Contains(t, err.Error(), message)
			}
		})
	}

	t.Run("should name every mismatch at once", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		_, err := formatting.Format(&out, []byte(source(t, "inconsistent/several-mismatches")))

		require.Error(t, err)
		assert.Contains(t, err.Error(), `the package "bytes" is imported under 2 names`)
		assert.Contains(t, err.Error(), `the name "rand" is bound to 2 packages`)
		assert.Contains(t, err.Error(), `the name "x" is bound to 2 packages`)
	})
}

func TestConsistentImports(t *testing.T) {
	t.Parallel()

	for fixture, toPin := range sourceSet(t, "consistent") {
		src := toPin

		t.Run("should accept "+caseName(fixture), func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			_, err := formatting.Format(&out, []byte(src))
			require.NoError(t, err)
		})
	}

	t.Run("should check what pruning left, not what the source wrote", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "consistent/collision-pruned-away"))

		assert.NotContains(t, out, "rand", "both imports go, so the collision goes with them")
	})
}
