// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
	"golang.org/x/mod/modfile"

	"github.com/go-openapi/codegen/genapp"
)

func TestInitModule(t *testing.T) {
	t.Parallel()

	t.Run("should write a go.mod the go command would have written", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.InitModule(
			genapp.WithModulePath("example.com/petstore/gen"),
			genapp.WithGoVersion("1.25.0"),
		))

		assert.Equal(t, "module example.com/petstore/gen\n\ngo 1.25.0\n", readMod(t, dir))
	})

	t.Run("should default the go directive to the toolchain that built this", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.InitModule(genapp.WithModulePath("example.com/petstore/gen")))

		parsed := parseMod(t, dir)
		require.NotNil(t, parsed.Go)
		assert.Contains(t, runtime.Version(), parsed.Go.Version)
	})

	t.Run("should declare the requirements it was given", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.InitModule(
			genapp.WithModulePath("example.com/petstore/gen"),
			genapp.WithGoVersion("1.25.0"),
			genapp.WithRequire("github.com/go-openapi/strfmt", "v0.24.0", false),
			genapp.WithRequire("github.com/go-openapi/errors", "v0.22.8", true),
		))

		parsed := parseMod(t, dir)
		require.Len(t, parsed.Require, 2)

		byPath := map[string]*modfile.Require{}
		for _, required := range parsed.Require {
			byPath[required.Mod.Path] = required
		}

		require.Contains(t, byPath, "github.com/go-openapi/strfmt")
		assert.Equal(t, "v0.24.0", byPath["github.com/go-openapi/strfmt"].Mod.Version)
		assert.False(t, byPath["github.com/go-openapi/strfmt"].Indirect)
		assert.True(t, byPath["github.com/go-openapi/errors"].Indirect, "and says which are indirect")
	})

	t.Run("should write a toolchain directive when asked for one", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.InitModule(
			genapp.WithModulePath("example.com/petstore/gen"),
			genapp.WithGoVersion("1.25.0"),
			genapp.WithToolchain("go1.26.0"),
		))

		assert.Equal(t,
			"module example.com/petstore/gen\n\ngo 1.25.0\n\ntoolchain go1.26.0\n",
			readMod(t, dir))
	})

	t.Run("should take a toolchain spelled as a bare version", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.InitModule(
			genapp.WithModulePath("example.com/petstore/gen"),
			genapp.WithGoVersion("1.25.0"),
			genapp.WithToolchain("1.26.0"),
		))

		parsed := parseMod(t, dir)
		require.NotNil(t, parsed.Toolchain)
		assert.Equal(t, "go1.26.0", parsed.Toolchain.Name, "written in the form the directive takes")
	})

	t.Run("should take default as a toolchain", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.InitModule(
			genapp.WithModulePath("example.com/petstore/gen"),
			genapp.WithToolchain("default"),
		))

		parsed := parseMod(t, dir)
		require.NotNil(t, parsed.Toolchain)
		assert.Equal(t, "default", parsed.Toolchain.Name)
	})

	t.Run("should write no toolchain directive by default", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.InitModule(genapp.WithModulePath("example.com/petstore/gen")))

		assert.Nil(t, parseMod(t, dir).Toolchain, "as go mod init writes none")
	})

	t.Run("should create the module directory", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "a", "b")
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.InitModule(genapp.WithModulePath("example.com/deep")))
		assert.FileExists(t, filepath.Join(dir, "go.mod"))
	})

	t.Run("should leave an existing go.mod alone", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))
		existing := "module example.com/already/there\n\ngo 1.24.0\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(existing), 0o600))

		err := app.InitModule(genapp.WithModulePath("example.com/petstore/gen"))

		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrExist)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Equal(t, existing, readMod(t, dir), "and does not touch it")
	})

	t.Run("should replace an existing go.mod when told to", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module gone\n"), 0o600))

		require.NoError(t, app.InitModule(
			genapp.WithModulePath("example.com/petstore/gen"),
			genapp.WithGoVersion("1.25.0"),
			genapp.WithReplaceExisting(true),
		))

		assert.Equal(t, "module example.com/petstore/gen\n\ngo 1.25.0\n", readMod(t, dir))
	})
}

func TestInitModuleErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []genapp.ModOption
		says string
	}{
		{
			name: "no module path",
			opts: nil,
			says: "WithModulePath",
		},
		{
			name: "a path no module could have",
			opts: []genapp.ModOption{genapp.WithModulePath("not a module path!")},
			says: "not a module path",
		},
		{
			name: "a version no go directive could have",
			opts: []genapp.ModOption{
				genapp.WithModulePath("example.com/petstore/gen"),
				genapp.WithGoVersion("go1.25"),
			},
			says: "go version",
		},
		{
			name: "a toolchain name the directive would reject",
			opts: []genapp.ModOption{
				genapp.WithModulePath("example.com/petstore/gen"),
				genapp.WithToolchain("tip"),
			},
			says: "toolchain name",
		},
		{
			name: "a requirement with no version",
			opts: []genapp.ModOption{
				genapp.WithModulePath("example.com/petstore/gen"),
				genapp.WithRequire("github.com/go-openapi/strfmt", "not-a-version", false),
			},
			says: "cannot require",
		},
	}

	for _, toPin := range tests {
		test := toPin

		t.Run("should report "+test.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			err := newApp(t, genapp.WithOutputPath(dir)).InitModule(test.opts...)

			require.Error(t, err)
			assert.ErrorIs(t, err, genapp.ErrGenApp)
			assert.Contains(t, err.Error(), test.says)

			_, statErr := os.Stat(filepath.Join(dir, "go.mod"))
			assert.ErrorIs(t, statErr, os.ErrNotExist, "nothing is written when the options are wrong")
		})
	}
}

func readMod(t *testing.T, dir string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)

	return string(content)
}

func parseMod(t *testing.T, dir string) *modfile.File {
	t.Helper()

	parsed, err := modfile.Parse("go.mod", []byte(readMod(t, dir)), nil)
	require.NoError(t, err)

	return parsed
}
