// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/genapp"
)

// module writes a go.mod declaring path in dir.
func writeModule(t *testing.T, dir, path string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module "+path+"\n\ngo 1.25.0\n"),
		0o600,
	))
}

func TestPackagePath(t *testing.T) {
	t.Parallel()

	t.Run("should name the module itself at its root", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeModule(t, root, "example.com/petstore")

		app := newApp(t, genapp.WithOutputPath(root))

		pkg, err := app.PackagePath()
		require.NoError(t, err)
		assert.Equal(t, "example.com/petstore", pkg)
	})

	t.Run("should follow the way down to the output path", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeModule(t, root, "example.com/petstore")

		out := filepath.Join(root, "gen", "models")
		require.NoError(t, os.MkdirAll(out, 0o750))

		pkg, err := newApp(t, genapp.WithOutputPath(out)).PackagePath()
		require.NoError(t, err)
		assert.Equal(t, "example.com/petstore/gen/models", pkg)
	})

	t.Run("should answer for a directory that does not exist yet", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeModule(t, root, "example.com/petstore")

		out := filepath.Join(root, "not", "created", "yet")

		pkg, err := newApp(t, genapp.WithOutputPath(out)).PackagePath()
		require.NoError(t, err)
		assert.Equal(t, "example.com/petstore/not/created/yet", pkg)

		_, statErr := os.Stat(out)
		assert.ErrorIs(t, statErr, os.ErrNotExist, "and creates nothing to find out")
	})

	t.Run("should take the nearest module when they nest", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeModule(t, root, "example.com/outer")

		inner := filepath.Join(root, "tools")
		writeModule(t, inner, "example.com/outer/tools")

		out := filepath.Join(inner, "gen")
		require.NoError(t, os.MkdirAll(out, 0o750))

		pkg, err := newApp(t, genapp.WithOutputPath(out)).PackagePath()
		require.NoError(t, err)
		assert.Equal(t, "example.com/outer/tools/gen", pkg)
	})

	t.Run("should report a path no module covers", func(t *testing.T) {
		t.Parallel()

		out := filepath.Join(t.TempDir(), "orphan")

		_, err := newApp(t, genapp.WithOutputPath(out)).PackagePath()

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrNoModule)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
	})

	t.Run("should report a go.mod that declares no module", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("go 1.25.0\n"), 0o600))

		_, err := newApp(t, genapp.WithOutputPath(root)).PackagePath()

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "declares no module")
	})

	t.Run("should answer what InitModule just declared", func(t *testing.T) {
		t.Parallel()

		out := filepath.Join(t.TempDir(), "generated")
		app := newApp(t, genapp.WithOutputPath(out))

		required, err := app.ModuleRequired()
		require.NoError(t, err)
		require.True(t, required, "nothing covers it yet")

		require.NoError(t, app.InitModule(genapp.WithModulePath("example.com/fresh/gen")))

		pkg, err := app.PackagePath()
		require.NoError(t, err)
		assert.Equal(t, "example.com/fresh/gen", pkg)
	})
}

func TestModuleRequired(t *testing.T) {
	t.Parallel()

	t.Run("should be false inside a module", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeModule(t, root, "example.com/petstore")

		required, err := newApp(t, genapp.WithOutputPath(filepath.Join(root, "gen"))).ModuleRequired()

		require.NoError(t, err)
		assert.False(t, required)
	})

	t.Run("should be true outside every module", func(t *testing.T) {
		t.Parallel()

		required, err := newApp(t, genapp.WithOutputPath(t.TempDir())).ModuleRequired()

		require.NoError(t, err)
		assert.True(t, required)
	})

	t.Run("should report a go.mod it cannot read rather than answer", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("go 1.25.0\n"), 0o600))

		_, err := newApp(t, genapp.WithOutputPath(root)).ModuleRequired()

		require.Error(t, err, "a broken go.mod is not the same as no go.mod")
		assert.ErrorIs(t, err, genapp.ErrGenApp)
	})
}

func TestEnclosingModule(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModule(t, root, "example.com/petstore")

	out := filepath.Join(root, "gen", "models")
	require.NoError(t, os.MkdirAll(out, 0o750))

	modulePath, moduleDir, err := newApp(t, genapp.WithOutputPath(out)).EnclosingModule()
	require.NoError(t, err)

	assert.Equal(t, "example.com/petstore", modulePath)

	resolved, err := filepath.EvalSymlinks(moduleDir)
	require.NoError(t, err)
	expected, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	assert.Equal(t, expected, resolved, "and says where the go.mod sits")
}

func TestPackagePathUnderGopath(t *testing.T) {
	t.Run("should name a package under GOPATH/src", func(t *testing.T) {
		gopath := t.TempDir()
		t.Setenv("GOPATH", gopath)

		out := filepath.Join(gopath, "src", "example.com", "legacy", "pkg")
		require.NoError(t, os.MkdirAll(out, 0o750))

		pkg, err := newApp(t, genapp.WithOutputPath(out)).PackagePath()

		require.NoError(t, err)
		assert.Equal(t, "example.com/legacy/pkg", pkg)
	})

	t.Run("should try every entry of GOPATH", func(t *testing.T) {
		first, second := t.TempDir(), t.TempDir()
		t.Setenv("GOPATH", first+string(os.PathListSeparator)+second)

		out := filepath.Join(second, "src", "example.com", "second", "pkg")
		require.NoError(t, os.MkdirAll(out, 0o750))

		pkg, err := newApp(t, genapp.WithOutputPath(out)).PackagePath()

		require.NoError(t, err)
		assert.Equal(t, "example.com/second/pkg", pkg)
	})

	t.Run("should let a module win where both apply", func(t *testing.T) {
		gopath := t.TempDir()
		t.Setenv("GOPATH", gopath)

		out := filepath.Join(gopath, "src", "example.com", "legacy", "pkg")
		writeModule(t, out, "example.com/modernised")

		pkg, err := newApp(t, genapp.WithOutputPath(out)).PackagePath()

		require.NoError(t, err)
		assert.Equal(t, "example.com/modernised", pkg, "the go.mod decides, as it does for the go command")
	})

	t.Run("should not name GOPATH/src itself", func(t *testing.T) {
		gopath := t.TempDir()
		t.Setenv("GOPATH", gopath)

		out := filepath.Join(gopath, "src")
		require.NoError(t, os.MkdirAll(out, 0o750))

		_, err := newApp(t, genapp.WithOutputPath(out)).PackagePath()

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrNoModule)
	})

	t.Run("should report a path under neither", func(t *testing.T) {
		t.Setenv("GOPATH", t.TempDir())

		_, err := newApp(t, genapp.WithOutputPath(t.TempDir())).PackagePath()

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrNoModule)
	})

	t.Run("should still say a module is required, since go defaults to module mode", func(t *testing.T) {
		gopath := t.TempDir()
		t.Setenv("GOPATH", gopath)

		out := filepath.Join(gopath, "src", "example.com", "legacy", "pkg")
		require.NoError(t, os.MkdirAll(out, 0o750))

		required, err := newApp(t, genapp.WithOutputPath(out)).ModuleRequired()

		require.NoError(t, err)
		assert.True(t, required, "the path has a name, and still needs a go.mod to build")
	})
}
