// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package locate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
)

// TestBuildConstraint covers the four shapes a derived //go:build expression can take.
//
// It swaps the package-level [Versions] registry, so it does not run in parallel.
func TestBuildConstraint(t *testing.T) {
	saved := Versions
	t.Cleanup(func() { Versions = saved })

	t.Run("a lone version carries no constraint", func(t *testing.T) {
		Versions = []Version{{UCD: "15.0.0", Dir: "v15"}}
		assert.Equal(t, "", BuildConstraint(0))
	})

	t.Run("two versions bound each other", func(t *testing.T) {
		Versions = []Version{
			{UCD: "15.0.0", Dir: "v15"},
			{UCD: "17.0.0", Dir: "v17", MinGo: "go1.27"},
		}
		assert.Equal(t, "!go1.27", BuildConstraint(0))
		assert.Equal(t, "go1.27", BuildConstraint(1))
	})

	t.Run("a middle version is bounded on both sides", func(t *testing.T) {
		Versions = []Version{
			{UCD: "15.0.0", Dir: "v15"},
			{UCD: "17.0.0", Dir: "v17", MinGo: "go1.27"},
			{UCD: "19.0.0", Dir: "v19", MinGo: "go1.29"},
		}
		assert.Equal(t, "!go1.27", BuildConstraint(0))
		assert.Equal(t, "go1.27 && !go1.29", BuildConstraint(1))
		assert.Equal(t, "go1.29", BuildConstraint(2))
	})
}

// TestShippedVersions checks the registry that ships: exactly one baseline, then strictly increasing Go bounds, so the
// derived constraints select exactly one flavor for any toolchain.
func TestShippedVersions(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, Versions)
	assert.Equal(t, "", Versions[0].MinGo, "the first version is the baseline and takes no lower bound")

	for i, v := range Versions {
		assert.NotEmptyf(t, v.UCD, "version %d has no UCD string", i)
		assert.NotEmptyf(t, v.Dir, "version %d has no data directory", i)

		if i > 0 {
			require.NotEmptyf(t, v.MinGo, "version %s must declare a Go baseline", v.UCD)
			assert.Greaterf(t, goMinor(v.MinGo), goMinor(Versions[i-1].MinGo),
				"version %s does not raise the Go baseline of %s", v.UCD, Versions[i-1].UCD,
			)
		}

		got, idx, ok := Lookup(v.UCD)
		require.Truef(t, ok, "Lookup(%q) missed", v.UCD)
		assert.Equal(t, i, idx)
		assert.Equal(t, v, got)
	}

	_, _, ok := Lookup("14.0.0")
	assert.False(t, ok, "an unregistered version must not resolve")
}

func TestGoMinor(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]int{
		"go1.26":       26,
		"go1.26.4":     26,
		"go1.27rc1":    27,
		"go1.27beta2":  27,
		"1.27":         27,
		"go1":          0,
		"devel":        0,
		"":             0,
		"go1.x":        0,
		"go1.28.0-foo": 28,
	} {
		assert.Equalf(t, want, goMinor(in), "goMinor(%q)", in)
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	t.Run("resolves the baseline against an overridden root", func(t *testing.T) {
		t.Parallel()

		dir, tag, suffix, err := Resolve(Versions[0].UCD, root)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, Versions[0].Dir), dir)
		assert.Equal(t, BuildConstraint(0), tag)
		assert.Equal(t, Versions[0].UCD, suffix)
	})

	t.Run("rejects an unknown version, listing the known ones", func(t *testing.T) {
		t.Parallel()

		_, _, _, err := Resolve("14.0.0", root)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown UCD version "14.0.0"`)
		assert.Contains(t, err.Error(), Versions[0].UCD)
	})

	t.Run("refuses a dataset the running toolchain is too old for", func(t *testing.T) {
		t.Parallel()

		for _, v := range Versions[1:] {
			_, _, _, err := Resolve(v.UCD, root)
			if goMinor(runtime.Version()) >= goMinor(v.MinGo) {
				require.NoErrorf(t, err, "%s is served by %s", v.UCD, runtime.Version())

				continue
			}

			require.Errorf(t, err, "%s targets %s and must be refused under %s", v.UCD, v.MinGo, runtime.Version())
			assert.Contains(t, err.Error(), v.MinGo)
		}
	})
}

// TestUCDRootHoldsTheExtracts checks the git-derived default root actually points at the shipped data: every version
// directory in [Versions] must be there with the three extracts the generators read.
func TestUCDRootHoldsTheExtracts(t *testing.T) {
	t.Parallel()

	root, err := UCDRoot()
	if err != nil {
		t.Skipf("no git checkout to resolve the UCD root from: %v", err)
	}

	for _, v := range Versions {
		for _, name := range []string{"DerivedName.txt", "DerivedNumericValues.txt", "emoji-data.txt"} {
			path := filepath.Join(root, v.Dir, name)
			_, err := os.Stat(path)
			assert.NoErrorf(t, err, "UCD %s: %s is missing from the resolved root", v.UCD, path)
		}
	}
}
