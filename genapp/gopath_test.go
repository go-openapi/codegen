// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
)

// TestDescendantOf covers the comparison a prefix test gets wrong.
func TestDescendantOf(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)

	tests := []struct {
		name     string
		parent   string
		target   string
		within   string
		below    bool
		platform string
	}{
		{
			name:   "should find the way down",
			parent: filepath.Join(sep, "gopath", "src"),
			target: filepath.Join(sep, "gopath", "src", "example.com", "legacy", "pkg"),
			within: filepath.Join("example.com", "legacy", "pkg"),
			below:  true,
		},
		{
			name:   "should refuse the parent itself, which names no package",
			parent: filepath.Join(sep, "gopath", "src"),
			target: filepath.Join(sep, "gopath", "src"),
		},
		{
			name:   "should refuse a sibling",
			parent: filepath.Join(sep, "gopath", "src"),
			target: filepath.Join(sep, "gopath", "pkg", "mod"),
		},
		{
			name:   "should refuse a name that merely starts the same",
			parent: filepath.Join(sep, "gopath", "src"),
			target: filepath.Join(sep, "gopath", "srcery", "pkg"),
		},
		{
			name:   "should refuse a path above",
			parent: filepath.Join(sep, "gopath", "src"),
			target: filepath.Join(sep, "gopath"),
		},
		{
			name:     "should refuse a target on another volume",
			parent:   `C:\gopath\src`,
			target:   `D:\code\pkg`,
			platform: "windows",
		},
	}

	for _, toPin := range tests {
		test := toPin

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.platform != "" && test.platform != runtime.GOOS {
				t.Skipf("%s only", test.platform)
			}

			within, below := descendantOf(test.parent, test.target)

			assert.Equal(t, test.below, below)
			assert.Equal(t, test.within, within)
		})
	}
}

// TestDescendantOfCrossVolume states what a prefix test would get wrong on Windows.
//
// filepath.Rel reports an error when no relative path exists, which is a target belonging to
// another GOPATH entry rather than a failure. go-swagger's resolver reached the same conclusion by
// a different route and reported it as "target must reside inside a location within $GOPATH/src".
func TestDescendantOfCrossVolume(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		// the same shape, on a platform where Rel can express it: nothing relates these
		within, below := descendantOf(filepath.Join("relative", "src"), filepath.Join("/absolute", "pkg"))

		assert.False(t, below)
		assert.Empty(t, within)

		return
	}

	within, below := descendantOf(`C:\gopath\src`, `D:\code\pkg`)
	assert.False(t, below)
	assert.Empty(t, within)
}
