// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
	"golang.org/x/mod/modfile"

	"github.com/go-openapi/codegen/genapp"
)

// tidyable lays down a module with one file that imports only the standard library, so tidying it
// resolves nothing and needs no network.
func tidyable(t *testing.T) (*genapp.GoGenApp, string) {
	t.Helper()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go mod tidy needs a toolchain on PATH")
	}

	dir := t.TempDir()
	app := newApp(t, genapp.WithOutputPath(dir))

	require.NoError(t, app.InitModule(
		genapp.WithModulePath("example.com/tidyme"),
		genapp.WithRequire("github.com/go-openapi/strfmt", "v0.24.0", false),
	))

	const source = "package tidyme\n\nimport \"strings\"\n\nfunc F() string { return strings.TrimSpace(\" x \") }\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tidyme.go"), []byte(source), 0o600))

	return app, dir
}

func TestTidyModule(t *testing.T) {
	t.Parallel()

	t.Run("should tidy a module that resolves offline", func(t *testing.T) {
		t.Parallel()

		app, dir := tidyable(t)

		require.NoError(t, app.TidyModule(t.Context()))

		parsed, err := modfile.Parse("go.mod", []byte(readMod(t, dir)), nil)
		require.NoError(t, err)
		assert.Empty(t, parsed.Require, "tidy drops the requirement nothing imports")
	})

	t.Run("should set the go directive when asked", func(t *testing.T) {
		t.Parallel()

		app, dir := tidyable(t)

		require.NoError(t, app.TidyModule(t.Context(), genapp.WithTidyGoVersion("1.24.0")))

		parsed, err := modfile.Parse("go.mod", []byte(readMod(t, dir)), nil)
		require.NoError(t, err)
		require.NotNil(t, parsed.Go)
		assert.Equal(t, "1.24.0", parsed.Go.Version)
	})

	t.Run("should copy what the command writes", func(t *testing.T) {
		t.Parallel()

		app, _ := tidyable(t)

		var watched bytes.Buffer
		require.NoError(t, app.TidyModule(t.Context(),
			genapp.WithTidyOutput(&watched),
			genapp.WithGoCommand("go"),
		))
	})

	t.Run("should report a module that is not there", func(t *testing.T) {
		t.Parallel()

		app := newApp(t, genapp.WithOutputPath(t.TempDir()))

		err := app.TidyModule(t.Context())

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "InitModule")
	})

	t.Run("should say a toolchain is needed when the command is not there", func(t *testing.T) {
		t.Parallel()

		app, _ := tidyable(t)

		err := app.TidyModule(t.Context(), genapp.WithGoCommand("no-such-go-command"))

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
		assert.ErrorIs(t, err, exec.ErrNotFound)
		assert.Contains(t, err.Error(), "toolchain is required")
	})

	t.Run("should stop when the context is done", func(t *testing.T) {
		t.Parallel()

		app, _ := tidyable(t)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := app.TidyModule(ctx)

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
	})

	t.Run("should refuse a version the go directive would reject", func(t *testing.T) {
		t.Parallel()

		app, _ := tidyable(t)

		err := app.TidyModule(t.Context(), genapp.WithTidyCompat("go1.24"))

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "-compat")
	})

	t.Run("should report what the command said when it fails", func(t *testing.T) {
		t.Parallel()

		app, dir := tidyable(t)
		unresolvable := "package tidyme\n\nimport _ \"example.invalid/nope/v9\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "unresolvable.go"), []byte(unresolvable), 0o600))

		// GOPROXY=off makes the failure immediate rather than a trip to the module proxy
		err := app.TidyModule(t.Context(),
			genapp.WithTidyEnv("GOPROXY=off"),
			genapp.WithTidyWaitDelay(time.Second),
		)

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "example.invalid/nope/v9", "the command's own words reach the caller")
	})
}
