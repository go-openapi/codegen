// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
)

// TestNeedsResolution states which files pay for the parser's scopes.
func TestNeedsResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		resolved bool
	}{
		{
			name: "should trust the cheap parse when no name is both qualified and declared",
			src: `package p

import "bytes"

func F() {
	var buf bytes.Buffer
	_ = buf
}
`,
		},
		{
			name: "should trust the cheap parse for an aliased import",
			src: `package p

import buf "bytes"

var _ buf.Buffer
`,
		},
		{
			name: "should ask for scopes when a local goes by the name of a qualifier",
			src: `package p

import "bytes"

func F() {
	bytes := "shadow"
	_ = bytes
	var _ bytes.Buffer
}
`,
			resolved: true,
		},
		{
			name: "should ask for scopes when a package level name matches a qualifier",
			src: `package p

import "bytes"

var bytes = 1

var _ = bytes.Buffer
`,
			resolved: true,
		},
		{
			name: "should not count the field of a selector",
			src: `package p

import "bytes"

type T struct{ Buffer int }

var _ bytes.Buffer
`,
		},
	}

	for _, toPin := range tests {
		test := toPin
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			file, _, err := parseFile(token.NewFileSet(), []byte(test.src), fastMode)
			require.NoError(t, err)

			assert.Equal(t, test.resolved, needsResolution(file, nil))
		})
	}
}

// TestParsePathsAgree checks the claim [needsResolution] makes.
//
// The claim is not that the two parses always agree — on testdata/sources/prune/shadowed-wholly they
// must not, since only the resolved one sees that every use of bytes is a local and the import is
// dead. The claim is that they agree on every file needsResolution passes through, which is what
// makes skipping the scopes safe. So this formats each fixture both ways and compares only where the
// cheap parse would have been trusted.
func TestParsePathsAgree(t *testing.T) {
	t.Parallel()

	fixtures, err := filepath.Glob(filepath.Join("testdata", "sources", "*", "*.input"))
	require.NoError(t, err)

	more, err := filepath.Glob(filepath.Join("testdata", "sources", "*.input"))
	require.NoError(t, err)
	fixtures = append(fixtures, more...)

	corpus, err := filepath.Glob(filepath.Join("testdata", "corpus", "*", "*.input"))
	require.NoError(t, err)
	fixtures = append(fixtures, corpus...)

	require.NotEmpty(t, fixtures)

	var slowPath atomic.Int64
	t.Cleanup(func() {
		assert.Positive(t, slowPath.Load(), "no fixture exercises the resolved parse, so it is untested")
	})

	for _, toPin := range fixtures {
		fixture := toPin
		t.Run(filepath.ToSlash(fixture), func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(fixture)
			require.NoError(t, err)

			file, _, err := parseFile(token.NewFileSet(), src, fastMode)
			if err != nil {
				t.Skip("fixture does not parse; the error paths are covered elsewhere")
			}

			if needsResolution(file, nil) {
				slowPath.Add(1)

				return // Format parses this one again, so the paths are free to differ
			}

			fast, fastErr := formatWith(src, fastMode)
			require.NoError(t, fastErr)

			resolved, resolvedErr := formatWith(src, resolvedMode)
			require.NoError(t, resolvedErr)

			assert.Equal(t, resolved, fast, "the cheap parse must format as the resolved one does")
		})
	}
}

// formatWith runs the pipeline with the parse mode pinned, bypassing the choice Format makes.
func formatWith(src []byte, mode parser.Mode) (string, error) {
	fset := token.NewFileSet()

	file, adjust, err := parseFile(fset, src, mode)
	if err != nil {
		return "", err
	}

	prune(fset, file, options{forcePruning: true})
	mergeImports(file)
	sortImports(fset.File(file.FileStart), file, nil)
	breaks := groupBreaks(fset, file, nil)

	var out bytes.Buffer
	if adjust != nil {
		var printed bytes.Buffer
		spaced := newSpacer(&printed, breaks)
		if err := fprint(spaced, fset, file); err != nil {
			return "", err
		}
		if err := spaced.Flush(); err != nil {
			return "", err
		}
		out.Write(adjust(src, printed.Bytes()))

		return out.String(), nil
	}

	spaced := newSpacer(&out, breaks)
	if err := fprint(spaced, fset, file); err != nil {
		return "", err
	}
	if err := spaced.Flush(); err != nil {
		return "", err
	}

	return out.String(), nil
}
