// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package std_test

import (
	"os/exec"
	"runtime"
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

// TestTableMatchesToolchain checks the generated table against the toolchain running the test.
//
// The table is not the same everywhere. A newer Go release adds packages, and the standard library
// differs by platform: runtime/cgo is absent on windows, syscall/js exists only on js/wasm. So the
// hard check covers correctness alone: a path both sides hold must declare the same name. A path only
// "go list std" holds means the table is behind, which leaves the formatter guessing rather than
// wrong, and is reported rather than failed.
//
// On [std.GeneratedFor], the release the table was read from, the two must agree exactly.
//
// Regenerate with "go generate ./internal/std".
func TestTableMatchesToolchain(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("shells out to go list")
	}

	out, err := exec.CommandContext(t.Context(), "go", "list", "-f", "{{.ImportPath}} {{.Name}}", "std").Output()
	require.NoError(t, err)

	var absent []string

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		importPath, name, ok := strings.Cut(line, " ")
		if !ok || name == "main" || strings.Contains(importPath, "internal/") || strings.Contains(importPath, "vendor/") {
			continue
		}

		got, found := std.Name(importPath)
		if !found {
			absent = append(absent, importPath)

			continue
		}

		assert.Equal(t, name, got, "%s: the table names it wrongly", importPath)
	}

	if len(absent) == 0 {
		return
	}

	if runningRelease() == std.GeneratedFor {
		assert.Empty(t, absent, "the table was read from %s and is missing packages it holds", std.GeneratedFor)

		return
	}

	t.Logf("%d packages of %s are absent from the table, read from %s: %v",
		len(absent), runningRelease(), std.GeneratedFor, absent)
}

// runningRelease names the Go release running the test, as in "go1.27".
func runningRelease() string {
	parts := strings.SplitN(runtime.Version(), ".", 3)
	if len(parts) < 2 {
		return runtime.Version()
	}

	return parts[0] + "." + parts[1]
}
