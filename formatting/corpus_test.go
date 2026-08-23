// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/formatting"
)

// corpusDir holds one package per fixture: an .input the formatter is given and a .go golden it has
// to produce. The goldens compile, which TestCorpusCompiles checks.
const corpusDir = "testdata/corpus"

var update = flag.Bool("update", false, "rewrite the corpus goldens")

// corpusGroups groups the imports of every fixture.
var corpusGroups = formatting.WithImportGroups("github.com/go-openapi")

func TestCorpus(t *testing.T) {
	t.Parallel()

	for _, input := range corpusInputs(t) {
		golden := strings.TrimSuffix(input, ".input") + ".go"

		t.Run("should format "+filepath.Base(filepath.Dir(input)), func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(input)
			require.NoError(t, err)

			var out bytes.Buffer
			_, err = formatting.Format(&out, src, corpusGroups)
			require.NoError(t, err)

			if *update {
				require.NoError(t, os.WriteFile(golden, out.Bytes(), 0o600))

				return
			}

			expected, err := os.ReadFile(golden)
			require.NoError(t, err, "run go test -update to write the goldens")
			assert.Equal(t, string(expected), out.String())
		})
	}
}

// TestCorpusCompiles builds the corpus module.
//
// A golden that is merely well formatted proves little: the failure worth catching is an import
// dropped although the code uses it, and only the compiler reports that.
func TestCorpusCompiles(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("builds the corpus module, which needs the module cache")
	}

	build := exec.CommandContext(t.Context(), "go", "build", "./...")
	build.Dir = corpusDir
	// the corpus is a module on purpose and is not in go.work, so the workspace has to stand aside
	build.Env = append(os.Environ(), "GOWORK=off")

	out, err := build.CombinedOutput()
	require.NoError(t, err, "the corpus goldens do not compile:\n%s", out)
}

func corpusInputs(t *testing.T) []string {
	t.Helper()

	inputs, err := filepath.Glob(filepath.Join(corpusDir, "*", "*.input"))
	require.NoError(t, err)
	require.NotEmpty(t, inputs, "the corpus is empty")

	return inputs
}
