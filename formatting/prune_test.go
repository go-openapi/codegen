// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"testing"

	"github.com/go-openapi/testify/v2/assert"
)

func TestPrune(t *testing.T) {
	t.Parallel()

	t.Run("should drop an import nothing uses", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/unused"))

		assert.NotContains(t, out, `"strings"`)
		assert.Contains(t, out, `"bytes"`)
	})

	t.Run("should keep a blank and a dot import", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/blank-and-dot"))

		assert.Contains(t, out, `_ "embed"`)
		assert.Contains(t, out, `. "strings"`)
	})

	t.Run("should trust an alias rather than guess", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/aliased"))

		assert.Contains(t, out, `buf "bytes"`)
		assert.NotContains(t, out, `unused "strings"`)
	})

	t.Run("should keep an import whose package it cannot name", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/unnameable"))

		assert.Contains(t, out, `"example.com/2fa"`)
	})

	t.Run("should keep the cgo preamble", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/cgo"))

		assert.Contains(t, out, `import "C"`)
	})

	t.Run("should keep an import a shadowed name does not hide", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/shadowed-partly"))

		assert.Contains(t, out, `"bytes"`)
	})

	t.Run("should drop an import every use of which is shadowed", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/shadowed-wholly"))

		assert.NotContains(t, out, `"bytes"`)
	})

	t.Run("should never add an import", func(t *testing.T) {
		t.Parallel()

		out := format2(t, source(t, "prune/missing"))

		assert.NotContains(t, out, `"fmt"`, "a missing import is the template's business, not ours")
	})
}
