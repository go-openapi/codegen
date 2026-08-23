// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package std_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/formatting/internal/std"
)

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should hold the standard library", func(t *testing.T) {
		t.Parallel()

		assert.Greater(t, std.Len(), 150, "the generated table looks empty")
	})

	t.Run("should name a package the path does not name", func(t *testing.T) {
		t.Parallel()

		name, ok := std.Name("math/rand/v2")

		require.True(t, ok)
		assert.Equal(t, "rand", name, "the version element names no package")
	})

	t.Run("should not answer for a path outside the standard library", func(t *testing.T) {
		t.Parallel()

		for _, importPath := range []string{
			"github.com/go-openapi/strfmt",
			"myapp/models",  // a local module: no dot, and not standard library either
			"internal/abi",  // nothing outside the standard library may import it
			"math/rand/v99", // no such package
		} {
			_, ok := std.Name(importPath)
			assert.False(t, ok, importPath)
		}
	})
}

// TestTableIsCurrent fails when a Go release has moved the standard library under us.
//
// Regenerate with "go generate ./internal/std".
func TestTableIsCurrent(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("shells out to go list")
	}

	out, err := exec.CommandContext(t.Context(), "go", "list", "-f", "{{.ImportPath}} {{.Name}}", "std").Output()
	require.NoError(t, err)

	var checked int

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		importPath, name, ok := strings.Cut(line, " ")
		if !ok || name == "main" || strings.Contains(importPath, "internal/") || strings.Contains(importPath, "vendor/") {
			continue
		}

		checked++

		got, found := std.Name(importPath)
		if assert.True(t, found, "%s is missing from the table", importPath) {
			assert.Equal(t, name, got, "%s", importPath)
		}
	}

	assert.Equal(t, checked, std.Len(), "the table holds packages go list std does not")
}
