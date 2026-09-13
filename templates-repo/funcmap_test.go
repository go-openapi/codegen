// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"testing"
	"testing/fstest"
	"text/template"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
)

// partedAssets declare two generation targets, each calling a function of its own.
func partedAssets() fstest.MapFS {
	return fstest.MapFS{
		"client.gotmpl": {Data: []byte(`client=[{{ clientFunc }}]`)},
		"server.gotmpl": {Data: []byte(`server=[{{ serverFunc }}]`)},
	}
}

// clientFuncs bind what the client templates call, and nothing else.
func clientFuncs() template.FuncMap {
	return template.FuncMap{"clientFunc": func() string { return "call" }}
}

func TestScopedFuncMap(t *testing.T) {
	t.Run("should build with a func map covering the roots alone", func(t *testing.T) {
		r, err := New(
			FromFS(partedAssets(), ""),
			WithFuncMap(clientFuncs()),
			WithRoots("client"),
		)
		require.NoError(t, err)

		assert.Equal(t, "client=[call]", render(t, r, "client"))
		assert.False(t, r.Has("server"))
	})

	t.Run("should report the function an unscoped repository does not bind", func(t *testing.T) {
		_, err := New(
			FromFS(partedAssets(), ""),
			WithFuncMap(clientFuncs()),
		)
		require.ErrorIs(t, err, ErrTemplateRepo)
		assert.Contains(t, err.Error(), `function "serverFunc" not defined`)
	})

	t.Run("should report the function a retained template calls", func(t *testing.T) {
		_, err := New(
			FromFS(partedAssets(), ""),
			WithFuncMap(clientFuncs()),
			WithRoots("server"),
		)
		require.ErrorIs(t, err, ErrTemplateRepo)
		assert.Contains(t, err.Error(), `function "serverFunc" not defined`)
		assert.Contains(t, err.Error(), "server.gotmpl")
	})

	t.Run("should report the function a template a root reaches calls", func(t *testing.T) {
		_, err := New(
			FromFS(fstest.MapFS{
				"client.gotmpl":      {Data: []byte(`client=[{{template "clientCall"}}]`)},
				"client/call.gotmpl": {Data: []byte(`{{ deepFunc }}`)},
			}, ""),
			WithRoots("client"),
		)
		require.ErrorIs(t, err, ErrTemplateRepo)
		assert.Contains(t, err.Error(), `function "deepFunc" not defined`)
		assert.Contains(t, err.Error(), "client/call.gotmpl")
	})

	t.Run("should owe the functions of a whole asset it keeps a template of", func(t *testing.T) {
		// the check runs on the asset, so a "define" pruned away still owes its functions when
		// another template of the same asset is kept
		_, err := New(
			FromFS(fstest.MapFS{
				"schema.gotmpl": {Data: []byte(
					`{{define "kept"}}kept{{end}}{{define "dropped"}}{{ droppedFunc }}{{end}}`,
				)},
			}, ""),
			WithRoots("schemaKept"),
		)
		require.ErrorIs(t, err, ErrTemplateRepo)
		assert.Contains(t, err.Error(), `function "droppedFunc" not defined`)
	})

	t.Run("should carry the func map of a scope over a clone", func(t *testing.T) {
		base, err := New(
			FromFS(partedAssets(), ""),
			WithFuncMap(clientFuncs()),
			WithRoots("client"),
		)
		require.NoError(t, err)

		clone, err := Clone(base, FromTemplate("extra", []byte(`extra=[{{ clientFunc }}]`)), WithExtraRoots("extra"))
		require.NoError(t, err)

		assert.Equal(t, "extra=[call]", render(t, clone, "extra"))
		assert.False(t, clone.Has("server"))
	})
}

func TestScopedParseErrors(t *testing.T) {
	// the permissive pass drops the check on functions, and drops nothing else: a pruned asset
	// still has to parse
	for _, tc := range []struct {
		name    string
		content string
		reports string
	}{
		{"syntax error", `{{ if .X }}no end`, "unexpected EOF"},
		{"undefined variable", `{{ $z }}`, `undefined variable "$z"`},
		{"duplicate define", `{{define "x"}}a{{end}}{{define "x"}}b{{end}}`, `multiple definition of template "x"`},
	} {
		t.Run("should report "+tc.name+" in a pruned asset", func(t *testing.T) {
			_, err := New(
				FromFS(fstest.MapFS{
					"kept.gotmpl":   {Data: []byte(`kept`)},
					"pruned.gotmpl": {Data: []byte(tc.content)},
				}, ""),
				WithRoots("kept"),
			)
			require.ErrorIs(t, err, ErrTemplateRepo)
			assert.Contains(t, err.Error(), tc.reports)
		})
	}

	t.Run("should keep break and continue as keywords in a scoped repository", func(t *testing.T) {
		r, err := New(
			FromFS(fstest.MapFS{
				"loop.gotmpl": {Data: []byte(
					`{{range .}}{{if eq . "b"}}{{continue}}{{end}}{{if eq . "c"}}{{break}}{{end}}{{.}}{{end}}`,
				)},
			}, ""),
			WithRoots("loop"),
		)
		require.NoError(t, err)

		out, err := executeWith(t, r, "loop", []string{"a", "b", "c", "d"})
		require.NoError(t, err)
		assert.Equal(t, "a", out)
	})
}
