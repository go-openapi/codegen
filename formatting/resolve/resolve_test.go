// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/formatting"
	"github.com/go-openapi/codegen/formatting/resolve"
)

func TestNames(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("loads packages through go list")
	}

	t.Run("should read the name from the package, not from the path", func(t *testing.T) {
		t.Parallel()

		names, err := resolve.Names(t.Context(), []string{
			"net/http",
			"math/rand/v2",
			"golang.org/x/tools/go/ast/astutil",
		})

		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"net/http":                          "http",
			"math/rand/v2":                      "rand",
			"golang.org/x/tools/go/ast/astutil": "astutil",
		}, names)
	})

	t.Run("should answer where the path cannot", func(t *testing.T) {
		t.Parallel()

		const importPath = "golang.org/x/tools/go/packages"

		names, err := resolve.Names(t.Context(), []string{importPath})
		require.NoError(t, err)

		assert.Equal(t, "packages", names[importPath])
	})

	t.Run("should take an empty list without loading anything", func(t *testing.T) {
		t.Parallel()

		names, err := resolve.Names(t.Context(), nil)

		require.NoError(t, err)
		assert.Empty(t, names)
	})

	t.Run("should ask about a repeated path once", func(t *testing.T) {
		t.Parallel()

		names, err := resolve.Names(t.Context(), []string{"bytes", "bytes", "bytes"})

		require.NoError(t, err)
		assert.Len(t, names, 1)
	})

	t.Run("should name the paths it could not resolve, and keep the rest", func(t *testing.T) {
		t.Parallel()

		names, err := resolve.Names(t.Context(), []string{"bytes", "example.invalid/nope"})

		require.Error(t, err)
		assert.ErrorIs(t, err, resolve.ErrUnresolved)
		assert.ErrorIs(t, err, resolve.ErrResolve)
		assert.Contains(t, err.Error(), "example.invalid/nope")

		assert.Equal(t, "bytes", names["bytes"], "what resolved is still usable")
	})

	t.Run("should stop when the context is done", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := resolve.Names(ctx, []string{"bytes"})

		require.Error(t, err)
	})
}

// TestFeedsTheFormatter closes the loop the report opens.
func TestFeedsTheFormatter(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("loads packages through go list")
	}

	// astutil is bare and third-party, so the formatter cannot be sure of its name
	const src = `package p

import (
	"golang.org/x/tools/go/ast/astutil"
	"bytes"
)

var _ bytes.Buffer
`

	var first bytes.Buffer

	report, err := formatting.Format(&first, []byte(src))
	require.NoError(t, err)
	require.True(t, report.HasImportsInDoubt())
	require.Equal(t, []string{"golang.org/x/tools/go/ast/astutil"}, report.PathsInDoubt())

	names, err := resolve.Names(t.Context(), report.PathsInDoubt())
	require.NoError(t, err)

	var second bytes.Buffer

	settled, err := formatting.Format(&second, []byte(src), formatting.WithResolvedImports(names))
	require.NoError(t, err)

	assert.False(t, settled.HasImportsInDoubt(), "the map settled it:\n%s", settled)
	assert.NotContains(t, second.String(), "astutil", "and nothing used it, so it went")
}
