// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"testing"
	"testing/fstest"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
)

func TestWithMissingKey(t *testing.T) {
	missingKeyAssets := fstest.MapFS{
		"page.gotmpl": {Data: []byte(`{{ .Missing }}`)},
	}

	t.Run("should report a missing key at execution", func(t *testing.T) {
		r, err := New(FromFS(missingKeyAssets, ""), WithMissingKey(MissingKeyBehaviorError))
		require.NoError(t, err)

		_, err = executeWith(t, r, "page", map[string]string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `map has no entry for key "Missing"`)
	})

	t.Run("should render the zero value when asked to", func(t *testing.T) {
		r, err := New(FromFS(missingKeyAssets, ""), WithMissingKey(MissingKeyBehaviorZero))
		require.NoError(t, err)

		out, err := executeWith(t, r, "page", map[string]string{})
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("should leave execution alone by default", func(t *testing.T) {
		r, err := New(FromFS(missingKeyAssets, ""))
		require.NoError(t, err)

		out, err := executeWith(t, r, "page", map[string]string{})
		require.NoError(t, err)
		assert.Equal(t, "<no value>", out)
	})

	t.Run("should carry the setting over a clone", func(t *testing.T) {
		base, err := New(FromFS(missingKeyAssets, ""), WithMissingKey(MissingKeyBehaviorError))
		require.NoError(t, err)

		clone, err := Clone(base, FromTemplate("other", []byte(`other`)))
		require.NoError(t, err)

		_, err = executeWith(t, clone, "page", map[string]string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `map has no entry for key "Missing"`)
	})
}
