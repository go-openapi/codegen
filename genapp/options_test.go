// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp_test

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/formatting"
	"github.com/go-openapi/codegen/genapp"
	repo "github.com/go-openapi/codegen/templates-repo"
)

func TestOptions(t *testing.T) {
	t.Parallel()

	t.Run("should render from a repository built by the caller", func(t *testing.T) {
		t.Parallel()

		templates, err := repo.New(repo.FromTemplate("mine", []byte("package {{ .Package }}\n")))
		require.NoError(t, err)

		app, err := genapp.New(genapp.WithTemplates(templates))
		require.NoError(t, err)

		var out bytes.Buffer
		require.NoError(t, app.Render(&out, "mine", pet))
		assert.Equal(t, "package models\n", out.String())
	})

	t.Run("should render with whatever funcmap the repository carries", func(t *testing.T) {
		t.Parallel()

		app, err := genapp.New(genapp.WithTemplates(newRepo(t, repo.WithFuncMap(template.FuncMap{
			"pascalize": func(string) string { return "Overridden" },
		}))))
		require.NoError(t, err)

		var out bytes.Buffer
		require.NoError(t, app.Render(&out, "model", pet))
		assert.Contains(t, out.String(), "type Overridden struct {", "the later funcmap wins")
	})

	t.Run("should pass options through to the formatter", func(t *testing.T) {
		t.Parallel()

		app, err := genapp.New(
			genapp.WithTemplates(newRepo(t)),
			genapp.WithFormatOptions(formatting.WithGoFumpt()),
		)
		require.NoError(t, err)

		var out bytes.Buffer
		err = app.Render(&out, "model", pet)

		require.Error(t, err, "the gofumpt enable module is not linked into this test")
		assert.ErrorIs(t, err, formatting.ErrNoGoFumpt)
	})

}

func TestExecuteError(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := newApp(t).Render(&out, "model", struct{ Package string }{Package: "models"})

	require.Error(t, err, "the template reaches a field the data does not carry")
	assert.ErrorIs(t, err, genapp.ErrGenApp)
	assert.Contains(t, err.Error(), "model")
}
