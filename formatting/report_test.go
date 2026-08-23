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

// reportOf formats and returns the report, failing the test if the source does not format.
func reportOf(t *testing.T, src string, opts ...formatting.Option) *formatting.ImportsReport {
	t.Helper()

	var out bytes.Buffer

	report, err := formatting.Format(&out, []byte(src), opts...)
	require.NoError(t, err)

	return report
}

// statusOf finds one import in the report.
func statusOf(t *testing.T, report *formatting.ImportsReport, importPath string) formatting.ImportRecord {
	t.Helper()

	for _, record := range report.Imports() {
		if record.Path == importPath {
			return record
		}
	}

	t.Fatalf("%s is not in the report:\n%s", importPath, report)

	return formatting.ImportRecord{}
}

func TestImportsReport(t *testing.T) {
	t.Parallel()

	t.Run("should account for every import that reached the output", func(t *testing.T) {
		t.Parallel()

		report := reportOf(t, source(t, "prune/blank-and-dot-unused"))

		assert.Equal(t, formatting.ImportUsed, statusOf(t, report, "bytes").Status)
		assert.Equal(t, formatting.ImportBlank, statusOf(t, report, "embed").Status)
		assert.Equal(t, formatting.ImportDot, statusOf(t, report, "strings").Status)
	})

	t.Run("should name what it pruned", func(t *testing.T) {
		t.Parallel()

		report := reportOf(t, source(t, "prune/unused"))

		pruned := report.Pruned()
		require.Len(t, pruned, 1)
		assert.Equal(t, "strings", pruned[0].Path)
		assert.True(t, pruned[0].Certain, "the standard library table named it")
	})

	t.Run("should report a bare third-party import it cannot decide", func(t *testing.T) {
		t.Parallel()

		report := reportOf(t, source(t, "prune/bare-third-party"))
		record := statusOf(t, report, "github.com/go-openapi/strfmt")

		assert.Equal(t, formatting.ImportInDoubt, record.Status)
		assert.False(t, record.Certain)
		assert.True(t, report.HasImportsInDoubt())
		assert.Equal(t, []string{"github.com/go-openapi/strfmt"}, report.PathsInDoubt())
	})

	t.Run("should settle that import once the caller names it", func(t *testing.T) {
		t.Parallel()

		report := reportOf(t, source(t, "prune/bare-third-party"),
			formatting.WithResolvedImports(map[string]string{"github.com/go-openapi/strfmt": "strfmt"}),
		)

		assert.False(t, report.HasImportsInDoubt())
		assert.Equal(t, formatting.ImportPruned, statusOf(t, report, "github.com/go-openapi/strfmt").Status)
	})

	t.Run("should hold a dot import in doubt, whatever the options say", func(t *testing.T) {
		t.Parallel()

		report := reportOf(t, source(t, "prune/blank-and-dot-unused"), formatting.WithForceImportsPruning())

		assert.True(t, report.HasImportsInDoubt(), "nothing reveals what a dot import declares")
		assert.Equal(t, []string{"strings"}, report.PathsInDoubt())
	})

	t.Run("should settle every import of a well-formed file", func(t *testing.T) {
		t.Parallel()

		report := reportOf(t, source(t, "prune/unused"))

		assert.False(t, report.HasImportsInDoubt())
		assert.Empty(t, report.InDoubt())
	})

	t.Run("should name the qualifier a guess settled on", func(t *testing.T) {
		t.Parallel()

		report := reportOf(t, source(t, "prune/version-directory"))
		record := statusOf(t, report, "k8s.io/api/apps/v1")

		assert.Equal(t, "v1", record.Name, "the file writes v1, not apps")
		assert.False(t, record.Certain)
		assert.Equal(t, formatting.ImportUsed, record.Status)
	})

	t.Run("should read as a summary", func(t *testing.T) {
		t.Parallel()

		report := reportOf(t, source(t, "prune/bare-third-party"))

		assert.Contains(t, report.String(), "github.com/go-openapi/strfmt (?) in doubt")
		assert.Contains(t, report.String(), "bytes (bytes) used")
	})

	t.Run("should survive being nil", func(t *testing.T) {
		t.Parallel()

		var absent *formatting.ImportsReport

		assert.False(t, absent.HasImportsInDoubt())
		assert.Empty(t, absent.Imports())
		assert.Equal(t, "no imports", absent.String())
	})

	t.Run("should come back even when the imports contradict each other", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		report, err := formatting.Format(&out, []byte(source(t, "inconsistent/one-alias-two-packages")))

		require.Error(t, err)
		assert.NotNil(t, report, "the caller can still see what the imports were")
		assert.NotEmpty(t, report.Imports())
	})
}

// TestGuessedCollision pins the difference between a clash we know about and one we inferred.
func TestGuessedCollision(t *testing.T) {
	t.Parallel()

	clash := source(t, "collision/guessed-names-clash")

	t.Run("should report a clash between guessed names rather than fail", func(t *testing.T) {
		t.Parallel()

		report := reportOf(t, clash)

		assert.Equal(t, formatting.ImportCollision, statusOf(t, report, "github.com/go-openapi/core").Status)
		assert.Equal(t, formatting.ImportCollision, statusOf(t, report, "k8s.io/api/core/v1").Status)
		assert.True(t, report.HasImportsInDoubt())
	})

	t.Run("should keep both, since either package may declare something else", func(t *testing.T) {
		t.Parallel()

		out := format2(t, clash)

		assert.Contains(t, out, `"github.com/go-openapi/core"`)
		assert.Contains(t, out, `"k8s.io/api/core/v1"`)
	})

	t.Run("should fail once the caller promises the guesses are right", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		_, err := formatting.Format(&out, []byte(clash), formatting.WithForceImportsPruning())

		require.Error(t, err)
		assert.ErrorIs(t, err, formatting.ErrInconsistentImports)
		assert.Contains(t, err.Error(), `the name "core" is bound to 2 packages`)
	})

	t.Run("should still fail on a clash between names it knows", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		_, err := formatting.Format(&out, []byte(source(t, "inconsistent/two-packages-same-base")))

		require.Error(t, err)
		assert.ErrorIs(t, err, formatting.ErrInconsistentImports, "crypto/rand and math/rand are both in the table")
	})
}
