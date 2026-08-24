// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp_test

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/genapp"
)

// skipWithoutSymlinks skips a test on a runner that cannot create one.
//
// Windows grants the privilege to an administrator or to a machine in developer mode, and the CI
// runner is neither, so the symlink cases are skipped there rather than failing on a setup step.
func skipWithoutSymlinks(t *testing.T, dir string) {
	t.Helper()

	if runtime.GOOS != "windows" {
		return
	}

	probe := filepath.Join(dir, ".symlink-probe")
	if err := os.Symlink(filepath.Join(dir, ".absent"), probe); err != nil {
		t.Skip("this runner cannot create symbolic links")
	}

	require.NoError(t, os.Remove(probe))
}

// assertAbsent reports a path that a confined write must not have created.
func assertAbsent(t *testing.T, path string, msgAndArgs ...any) {
	t.Helper()

	_, err := os.Lstat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, msgAndArgs...)
}

// outside builds a directory beside the output path, holding a file a test tries to reach.
func outside(t *testing.T) (dir, secret string) {
	t.Helper()

	dir = t.TempDir()
	secret = filepath.Join(dir, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("SECRET"), 0o600))

	return dir, secret
}

func TestTargetIsConfined(t *testing.T) {
	t.Parallel()

	t.Run("should refuse a target that climbs out of the output path", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(filepath.Join(dir, "out")))

		err := app.RenderFile("../../escaped.go", "model", pet)

		require.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "climbs out of the output path")
		assertAbsent(t, filepath.Join(dir, "escaped.go"))
	})

	t.Run("should refuse an absolute target", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(filepath.Join(dir, "out")))

		absolute := filepath.Join(dir, "absolute.go")

		err := app.RenderFile(absolute, "model", pet)

		require.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "is an absolute path")
		assertAbsent(t, absolute)
	})

	t.Run("should refuse a rooted target on every platform", func(t *testing.T) {
		t.Parallel()

		app := newApp(t, genapp.WithOutputPath(t.TempDir()))

		err := app.RenderFile("/etc/passwd", "model", pet)

		require.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "is an absolute path",
			`filepath.IsAbs reads "/etc/passwd" as relative on Windows, so path.IsAbs decides too`)
	})

	t.Run("should refuse a target naming no file", func(t *testing.T) {
		t.Parallel()

		app := newApp(t, genapp.WithOutputPath(t.TempDir()))

		for _, target := range []string{"", "   ", ".", "a/.."} {
			err := app.RenderFile(target, "model", pet)
			require.ErrorIs(t, err, genapp.ErrGenApp, "target %q", target)
		}
	})

	t.Run("should keep a target that stays under the output path", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.RenderFile("models/./nested/../pet.go", "model", pet))
		assert.FileExists(t, filepath.Join(dir, "models", "pet.go"))
	})
}

func TestSymlinkIsNotFollowed(t *testing.T) {
	t.Parallel()

	t.Run("should scratch a symbolic link standing at the target", func(t *testing.T) {
		t.Parallel()

		dir, secret := outside(t)
		skipWithoutSymlinks(t, dir)

		out := filepath.Join(dir, "out")
		require.NoError(t, os.MkdirAll(out, 0o750))
		require.NoError(t, os.Symlink(secret, filepath.Join(out, "pet.go")))

		app := newApp(t, genapp.WithOutputPath(out))
		require.NoError(t, app.RenderFile("pet.go", "model", pet))

		kept, err := os.ReadFile(secret)
		require.NoError(t, err)
		assert.Equal(t, "SECRET", string(kept), "the link was replaced, not written through")

		info, err := os.Lstat(filepath.Join(out, "pet.go"))
		require.NoError(t, err)
		assert.True(t, info.Mode().IsRegular(), "a regular file stands where the link did")
	})

	t.Run("should refuse a symbolic link on the way to the target", func(t *testing.T) {
		t.Parallel()

		dir, _ := outside(t)
		skipWithoutSymlinks(t, dir)

		out := filepath.Join(dir, "out")
		require.NoError(t, os.MkdirAll(out, 0o750))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "elsewhere"), 0o750))
		require.NoError(t, os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(out, "models")))

		app := newApp(t, genapp.WithOutputPath(out))

		err := app.RenderFile("models/pet.go", "model", pet)

		require.ErrorIs(t, err, genapp.ErrGenApp)
		assertAbsent(t, filepath.Join(dir, "elsewhere", "pet.go"),
			"os.MkdirAll would have walked through the link and written there")
	})

	t.Run("should break a hard link rather than write through it", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		out := filepath.Join(dir, "out")
		require.NoError(t, os.MkdirAll(out, 0o750))

		target := filepath.Join(out, "pet.go")
		require.NoError(t, os.WriteFile(target, []byte("SHARED"), 0o600))

		other := filepath.Join(out, "other.go")
		if err := os.Link(target, other); err != nil {
			t.Skip("this file system does not support hard links")
		}

		app := newApp(t, genapp.WithOutputPath(out))
		require.NoError(t, app.RenderFile("pet.go", "model", pet))

		kept, err := os.ReadFile(other)
		require.NoError(t, err)
		assert.Equal(t, "SHARED", string(kept), "the rename replaced a name, not the file behind it")

		written, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Contains(t, string(written), "package models")
	})

	t.Run("should refuse to overwrite a socket", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			t.Skip("unix domain sockets in the file system are not the same thing here")
		}

		dir := t.TempDir()

		var config net.ListenConfig

		listener, err := config.Listen(t.Context(), "unix", filepath.Join(dir, "pet.go"))
		require.NoError(t, err)
		defer func() { _ = listener.Close() }()

		app := newApp(t, genapp.WithOutputPath(dir))

		err = app.RenderFile("pet.go", "model", pet)

		require.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "socket")
	})

	t.Run("should refuse to overwrite something that is not a regular file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "pet.go"), 0o750))

		app := newApp(t, genapp.WithOutputPath(dir))

		err := app.RenderFile("pet.go", "model", pet)

		require.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "directory")
	})
}

func TestWithRoot(t *testing.T) {
	t.Parallel()

	t.Run("should write under a root wider than the output path", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		app := newApp(t,
			genapp.WithRoot(root),
			genapp.WithOutputPath(filepath.Join(root, "gen", "models")),
		)

		require.NoError(t, app.RenderFile("pet.go", "model", pet))
		assert.FileExists(t, filepath.Join(root, "gen", "models", "pet.go"))
	})

	t.Run("should refuse an output path outside the root", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t,
			genapp.WithRoot(filepath.Join(dir, "root")),
			genapp.WithOutputPath(filepath.Join(dir, "elsewhere")),
		)

		err := app.RenderFile("pet.go", "model", pet)

		require.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "outside the root")
	})

	t.Run("should refuse a root that does not exist", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "absent")
		app := newApp(t,
			genapp.WithRoot(root),
			genapp.WithOutputPath(filepath.Join(root, "gen")),
		)

		err := app.RenderFile("pet.go", "model", pet)

		require.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "WithRoot", "the message points at the option that set it")
		assertAbsent(t, root, "a mistyped root is reported, not created")
	})

	t.Run("should refuse a symbolic link that leaves the root", func(t *testing.T) {
		t.Parallel()

		dir, _ := outside(t)
		skipWithoutSymlinks(t, dir)

		root := filepath.Join(dir, "root")
		require.NoError(t, os.MkdirAll(filepath.Join(root, "gen"), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "elsewhere"), 0o750))
		require.NoError(t, os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(root, "gen", "models")))

		app := newApp(t,
			genapp.WithRoot(root),
			genapp.WithOutputPath(filepath.Join(root, "gen", "models")),
		)

		err := app.RenderFile("pet.go", "model", pet)

		require.ErrorIs(t, err, genapp.ErrGenApp)
		assertAbsent(t, filepath.Join(dir, "elsewhere", "pet.go"))
	})
}

func TestInitModuleIsConfined(t *testing.T) {
	t.Parallel()

	t.Run("should report a live symbolic link at go.mod as existing", func(t *testing.T) {
		t.Parallel()

		dir, secret := outside(t)
		skipWithoutSymlinks(t, dir)

		out := filepath.Join(dir, "out")
		require.NoError(t, os.MkdirAll(out, 0o750))
		require.NoError(t, os.Symlink(secret, filepath.Join(out, "go.mod")))

		app := newApp(t, genapp.WithOutputPath(out))

		err := app.InitModule(genapp.WithModulePath("example.com/gen"))

		require.ErrorIs(t, err, os.ErrExist)
		assert.Contains(t, err.Error(), "WithReplaceExisting")

		kept, readErr := os.ReadFile(secret)
		require.NoError(t, readErr)
		assert.Equal(t, "SECRET", string(kept))
	})

	t.Run("should report a dangling symbolic link at go.mod as existing", func(t *testing.T) {
		t.Parallel()

		dir, _ := outside(t)
		skipWithoutSymlinks(t, dir)

		out := filepath.Join(dir, "out")
		require.NoError(t, os.MkdirAll(out, 0o750))
		require.NoError(t, os.Symlink(filepath.Join(dir, "absent"), filepath.Join(out, "go.mod")))

		app := newApp(t, genapp.WithOutputPath(out))

		err := app.InitModule(genapp.WithModulePath("example.com/gen"))

		require.ErrorIs(t, err, os.ErrExist,
			"os.Stat follows the link and calls it absent, os.Root.Lstat reads the link itself")
	})

	t.Run("should scratch the link when asked to replace", func(t *testing.T) {
		t.Parallel()

		dir, secret := outside(t)
		skipWithoutSymlinks(t, dir)

		out := filepath.Join(dir, "out")
		require.NoError(t, os.MkdirAll(out, 0o750))
		require.NoError(t, os.Symlink(secret, filepath.Join(out, "go.mod")))

		app := newApp(t, genapp.WithOutputPath(out))

		require.NoError(t, app.InitModule(
			genapp.WithModulePath("example.com/gen"),
			genapp.WithReplaceExisting(true),
		))

		kept, err := os.ReadFile(secret)
		require.NoError(t, err)
		assert.Equal(t, "SECRET", string(kept), "the link was removed, not written through")

		info, err := os.Lstat(filepath.Join(out, "go.mod"))
		require.NoError(t, err)
		assert.True(t, info.Mode().IsRegular())

		written, err := os.ReadFile(filepath.Join(out, "go.mod"))
		require.NoError(t, err)
		assert.Contains(t, string(written), "module example.com/gen")
	})
}
