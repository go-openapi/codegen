// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"text/template"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
)

// dependentAssets declare a template that refers to another one, which is the shape every
// override has to work through.
func dependentAssets() fstest.MapFS {
	return fstest.MapFS{
		"root.gotmpl": {Data: []byte(`root=[{{template "leaf"}}]`)},
		"leaf.gotmpl": {Data: []byte(`DEFAULT`)},
	}
}

func render(t *testing.T, r *Repository, name string) string {
	t.Helper()

	tpl, err := r.Get(name)
	require.NoErrorf(t, err, "expected %q to be declared", name)

	var out strings.Builder
	require.NoError(t, tpl.Execute(&out, nil))

	return out.String()
}

// executeWith renders a template of a repository against data.
func executeWith(t *testing.T, r *Repository, name string, data any) (string, error) {
	t.Helper()

	tpl, err := r.Get(name)
	require.NoErrorf(t, err, "expected %q to be declared", name)

	var out strings.Builder
	err = tpl.Execute(&out, data)

	return out.String(), err
}

func TestNew(t *testing.T) {
	t.Run("should declare a template per asset", func(t *testing.T) {
		r, err := New(FromFS(fstest.MapFS{
			"model.gotmpl":                {Data: []byte("model")},
			"validation/primitive.gotmpl": {Data: []byte("primitive")},
			"server/parameter.gotmpl":     {Data: []byte("parameter")},
		}, ""))
		require.NoError(t, err)

		assert.Equal(t,
			[]string{"model", "serverParameter", "validationPrimitive"},
			slices.Collect(r.Names()),
		)
	})

	t.Run("should declare the inner templates of an asset", func(t *testing.T) {
		r, err := New(FromFS(fstest.MapFS{
			"schema.gotmpl": {Data: []byte(`{{define "schemaBody"}}body{{end}}{{define "schemaType"}}type{{end}}`)},
		}, ""))
		require.NoError(t, err)

		assert.Equal(t,
			[]string{"schema", "schemaSchemaBody", "schemaSchemaType"},
			slices.Collect(r.Names()),
		)
		assert.Equal(t, "body", render(t, r, "schemaSchemaBody"))
	})

	t.Run("should ignore an asset with an unsupported extension", func(t *testing.T) {
		r, err := New(FromFS(fstest.MapFS{
			"model.gotmpl": {Data: []byte("model")},
			"README.md":    {Data: []byte("not a template")},
		}, ""))
		require.NoError(t, err)

		assert.Equal(t, []string{"model"}, slices.Collect(r.Names()))
	})

	t.Run("should read every directory when the source skips none", func(t *testing.T) {
		assets := fstest.MapFS{
			"model.gotmpl":              {Data: []byte("model")},
			"contrib/mine/model.gotmpl": {Data: []byte("mine")},
			"nested/contrib/one.gotmpl": {Data: []byte("nested")},
			"kept/keeper.gotmpl":        {Data: []byte("kept")},
		}

		all, err := New(FromFS(assets, ""))
		require.NoError(t, err)
		assert.Equal(t,
			[]string{"contribMineModel", "keptKeeper", "model", "nestedContribOne"},
			slices.Collect(all.Names()),
		)
	})

	t.Run("should skip the directories a source declares as skipped", func(t *testing.T) {
		assets := fstest.MapFS{
			"model.gotmpl":              {Data: []byte("model")},
			"contrib/mine/model.gotmpl": {Data: []byte("mine")},
			"nested/contrib/one.gotmpl": {Data: []byte("nested")},
			"kept/keeper.gotmpl":        {Data: []byte("kept")},
		}

		r, err := New(FromFS(assets, "", SkipDirectories("contrib")))
		require.NoError(t, err)
		assert.Equal(t, []string{"keptKeeper", "model"}, slices.Collect(r.Names()))
	})

	t.Run("should skip for the source that says so, and no other", func(t *testing.T) {
		// what one source leaves out says nothing about a set someone else brings
		shipped := fstest.MapFS{"contrib/mine/model.gotmpl": {Data: []byte("shipped")}}
		brought := fstest.MapFS{"contrib/theirs/model.gotmpl": {Data: []byte("brought")}}

		r, err := New(
			FromFS(shipped, "", SkipDirectories("contrib")),
			FromFS(brought, ""),
		)
		require.NoError(t, err)

		assert.Equal(t, []string{"contribTheirsModel"}, slices.Collect(r.Names()))
	})

	t.Run("should build an empty repository from no source at all", func(t *testing.T) {
		r, err := New()
		require.NoError(t, err)

		assert.Empty(t, slices.Collect(r.Names()))
		assert.False(t, r.Has("anything"))
	})

	t.Run("should not declare the namespace itself", func(t *testing.T) {
		r, err := New(FromFS(dependentAssets(), ""))
		require.NoError(t, err)

		assert.False(t, r.Has(namespaceName))
		_, err = r.Get(namespaceName)
		assert.ErrorIs(t, err, ErrTemplateRepo)
	})

	t.Run("should build the same repository from the same sources", func(t *testing.T) {
		assets := fstest.MapFS{
			"a.gotmpl": {Data: []byte(`a=[{{template "b"}}]`)},
			"b.gotmpl": {Data: []byte(`b`)},
			"c.gotmpl": {Data: []byte(`c`)},
		}

		first, err := New(FromFS(assets, ""))
		require.NoError(t, err)
		second, err := New(FromFS(assets, ""))
		require.NoError(t, err)

		assert.Equal(t, slices.Collect(first.Names()), slices.Collect(second.Names()))
		assert.Equal(t, render(t, first, "a"), render(t, second, "a"))
	})
}

// TestOverride covers the failure both earlier implementations had, in opposite ways: an
// override that reaches the templates depending on it.
func TestOverride(t *testing.T) {
	t.Run("should reach the templates depending on it", func(t *testing.T) {
		r, err := New(
			FromFS(dependentAssets(), ""),
			FromTemplate("leaf.gotmpl", []byte("OVERRIDDEN")),
		)
		require.NoError(t, err)

		assert.Equal(t, "OVERRIDDEN", render(t, r, "leaf"))
		assert.Equal(t, "root=[OVERRIDDEN]", render(t, r, "root"))
	})

	t.Run("should reach them when the override comes from a clone", func(t *testing.T) {
		r, err := New(FromFS(dependentAssets(), ""))
		require.NoError(t, err)
		require.Equal(t, "root=[DEFAULT]", render(t, r, "root"))

		clone, err := Clone(r, FromTemplate("leaf.gotmpl", []byte("OVERRIDDEN")))
		require.NoError(t, err)

		assert.Equal(t, "root=[OVERRIDDEN]", render(t, clone, "root"))
	})

	t.Run("should let the last source win", func(t *testing.T) {
		r, err := New(
			FromFS(fstest.MapFS{"leaf.gotmpl": {Data: []byte("FIRST")}}, ""),
			FromFS(fstest.MapFS{"leaf.gotmpl": {Data: []byte("SECOND")}}, ""),
			FromFS(fstest.MapFS{"leaf.gotmpl": {Data: []byte("THIRD")}}, ""),
		)
		require.NoError(t, err)

		assert.Equal(t, "THIRD", render(t, r, "leaf"))
	})

	t.Run("should override a define at the address it is declared at", func(t *testing.T) {
		// a define lives under the asset declaring it, so replacing one means declaring that asset
		r, err := New(
			FromFS(fstest.MapFS{
				"schema.gotmpl": {Data: []byte(`{{define "schemaBody"}}DEFAULT{{end}}`)},
				"user.gotmpl":   {Data: []byte(`user=[{{template "schemaBody"}}]`)},
			}, ""),
			FromTemplate("schema.gotmpl", []byte(`{{define "schemaBody"}}OVERRIDDEN{{end}}`)),
		)
		require.NoError(t, err)

		assert.Equal(t, "user=[OVERRIDDEN]", render(t, r, "user"))
	})

	t.Run("should refuse an override that would silently not replace", func(t *testing.T) {
		_, err := New(
			FromFS(fstest.MapFS{"leaf.gotmpl": {Data: []byte("DEFAULT")}}, ""),
			FromTemplate("leaf.gotmpl", []byte("   {{/* nothing at all */}}   ")),
		)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTemplateRepo)
		assert.ErrorContains(t, err, "would not replace")
	})

	t.Run("should accept an override that renders nothing on purpose", func(t *testing.T) {
		r, err := New(
			FromFS(fstest.MapFS{"leaf.gotmpl": {Data: []byte("DEFAULT")}}, ""),
			FromTemplate("leaf.gotmpl", []byte(`{{""}}`)),
		)
		require.NoError(t, err)

		assert.Empty(t, render(t, r, "leaf"))
	})
}

// TestCloneIsolation covers the other half of the mutation problem: what a clone changes must
// not reach the repository it was derived from, nor a sibling clone.
func TestCloneIsolation(t *testing.T) {
	origin, err := New(FromFS(dependentAssets(), ""))
	require.NoError(t, err)

	first, err := Clone(origin, FromTemplate("leaf.gotmpl", []byte("FROM-FIRST")))
	require.NoError(t, err)

	second, err := Clone(origin, FromTemplate("leaf.gotmpl", []byte("FROM-SECOND")))
	require.NoError(t, err)

	t.Run("should isolate a clone from its origin", func(t *testing.T) {
		assert.Equal(t, "root=[DEFAULT]", render(t, origin, "root"))
	})

	t.Run("should isolate sibling clones from one another", func(t *testing.T) {
		assert.Equal(t, "root=[FROM-FIRST]", render(t, first, "root"))
		assert.Equal(t, "root=[FROM-SECOND]", render(t, second, "root"))
	})

	t.Run("should isolate a clone of a clone", func(t *testing.T) {
		third, err := Clone(first, FromTemplate("leaf.gotmpl", []byte("FROM-THIRD")))
		require.NoError(t, err)

		assert.Equal(t, "root=[FROM-THIRD]", render(t, third, "root"))
		assert.Equal(t, "root=[FROM-FIRST]", render(t, first, "root"))
		assert.Equal(t, "root=[DEFAULT]", render(t, origin, "root"))
	})

	t.Run("should carry the settings of the origin over", func(t *testing.T) {
		origin, err := New(
			FromFS(fstest.MapFS{"a.tmpl": {Data: []byte("a")}}, ""),
			WithExtensions(".tmpl"),
		)
		require.NoError(t, err)

		clone, err := Clone(origin, FromFS(fstest.MapFS{"b.tmpl": {Data: []byte("b")}}, ""))
		require.NoError(t, err)

		assert.Equal(t, []string{"a", "b"}, slices.Collect(clone.Names()))
	})

	t.Run("should report a nil origin", func(t *testing.T) {
		_, err := Clone(nil)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTemplateRepo)
	})
}

func TestFuncMap(t *testing.T) {
	shout := template.FuncMap{"shout": strings.ToUpper}

	t.Run("should bind functions to every template", func(t *testing.T) {
		r, err := New(
			FromFS(fstest.MapFS{"a.gotmpl": {Data: []byte(`{{ shout "hi" }}`)}}, ""),
			WithFuncMap(shout),
		)
		require.NoError(t, err)

		assert.Equal(t, "HI", render(t, r, "a"))
	})

	t.Run("should report a template calling an unknown function", func(t *testing.T) {
		_, err := New(FromFS(fstest.MapFS{"a.gotmpl": {Data: []byte(`{{ shout "hi" }}`)}}, ""))

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTemplateRepo)
	})

	t.Run("should let a clone add a function that reaches the templates already there", func(t *testing.T) {
		origin, err := New(
			FromFS(fstest.MapFS{"a.gotmpl": {Data: []byte(`{{ shout "hi" }}`)}}, ""),
			WithFuncMap(shout),
		)
		require.NoError(t, err)

		clone, err := Clone(origin, WithFuncMap(template.FuncMap{"shout": strings.ToLower}))
		require.NoError(t, err)

		assert.Equal(t, "hi", render(t, clone, "a"), "the clone re-parses, so the new function applies")
		assert.Equal(t, "HI", render(t, origin, "a"), "the origin keeps the function it was built with")
	})

	t.Run("should not share a function map between repositories", func(t *testing.T) {
		funcs := template.FuncMap{"shout": strings.ToUpper}

		first, err := New(FromFS(fstest.MapFS{"a.gotmpl": {Data: []byte(`{{ shout "hi" }}`)}}, ""), WithFuncMap(funcs))
		require.NoError(t, err)

		// a caller mutating the map it passed must not reach the repository it built with it
		funcs["shout"] = strings.ToLower
		assert.Equal(t, "HI", render(t, first, "a"))

		second, err := New(FromFS(fstest.MapFS{"b.gotmpl": {Data: []byte(`{{ shout "hi" }}`)}}, ""))
		require.Error(t, err, "the defaults of the first repository must not have leaked into the second")
		assert.Nil(t, second)
	})
}

func TestDependencies(t *testing.T) {
	t.Run("should report a template referring to an undeclared one", func(t *testing.T) {
		_, err := New(FromFS(fstest.MapFS{
			"root.gotmpl": {Data: []byte(`{{template "nowhere"}}`)},
		}, ""))

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTemplateRepo)
		assert.ErrorContains(t, err, `"root" refers to "nowhere", which it cannot reach`)
	})

	t.Run("should report every missing dependency at once", func(t *testing.T) {
		_, err := New(FromFS(fstest.MapFS{
			"a.gotmpl": {Data: []byte(`{{template "missingOne"}}`)},
			"b.gotmpl": {Data: []byte(`{{template "missingTwo"}}`)},
		}, ""))

		require.Error(t, err)
		assert.ErrorContains(t, err, "missingOne")
		assert.ErrorContains(t, err, "missingTwo")
	})

	t.Run("should find a dependency nested in a control structure", func(t *testing.T) {
		_, err := New(FromFS(fstest.MapFS{
			"root.gotmpl": {Data: []byte(
				`{{if .}}{{range .}}{{with .}}{{template "deep"}}{{end}}{{end}}{{else}}{{template "otherwise"}}{{end}}`,
			)},
		}, ""))

		require.Error(t, err)
		assert.ErrorContains(t, err, "deep")
		assert.ErrorContains(t, err, "otherwise")
	})

	t.Run("should accept a dependency declared by another asset", func(t *testing.T) {
		r, err := New(FromFS(dependentAssets(), ""))
		require.NoError(t, err)

		assert.Equal(t, "root=[DEFAULT]", render(t, r, "root"))
	})
}

func TestGet(t *testing.T) {
	r, err := New(FromFS(dependentAssets(), ""))
	require.NoError(t, err)

	t.Run("should report an undeclared name", func(t *testing.T) {
		_, err := r.Get("nowhere")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTemplateRepo)
	})

	t.Run("should report the asset a template comes from", func(t *testing.T) {
		assetPath, declared := r.AssetOf("leaf")
		assert.True(t, declared)
		assert.Equal(t, "leaf.gotmpl", assetPath)

		_, declared = r.AssetOf("nowhere")
		assert.False(t, declared)
	})

	t.Run("should panic on MustGet of an undeclared name", func(t *testing.T) {
		assert.Panics(t, func() { _ = r.MustGet("nowhere") })
		assert.NotPanics(t, func() { _ = r.MustGet("leaf") })
	})

	t.Run("should report the name of the template it returns", func(t *testing.T) {
		tpl, err := r.Get("leaf")
		require.NoError(t, err)
		assert.Equal(t, "leaf", tpl.Name())
	})

	t.Run("should report the zero template as unusable", func(t *testing.T) {
		var zero Template

		assert.Empty(t, zero.Name())
		assert.ErrorIs(t, zero.Execute(&strings.Builder{}, nil), ErrTemplateRepo)
	})

	t.Run("should report an execution failure", func(t *testing.T) {
		failing, err := New(FromFS(fstest.MapFS{"a.gotmpl": {Data: []byte(`{{ .Missing }}`)}}, ""))
		require.NoError(t, err)

		tpl, err := failing.Get("a")
		require.NoError(t, err)

		err = tpl.Execute(&strings.Builder{}, struct{ Present string }{})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTemplateRepo)
		assert.ErrorContains(t, err, `executing template "a"`)
	})
}

// TestConcurrency exercises the contract the type advertises: a sealed repository is read
// concurrently, and cloning one reads it without disturbing it. Run under -race.
func TestConcurrency(t *testing.T) {
	origin, err := New(FromFS(dependentAssets(), ""))
	require.NoError(t, err)

	const readers, cloners = 16, 8

	var wg sync.WaitGroup
	wg.Add(readers + cloners)

	for range readers {
		go func() {
			defer wg.Done()

			for range 50 {
				tpl, err := origin.Get("root")
				assert.NoError(t, err)

				var out strings.Builder
				assert.NoError(t, tpl.Execute(&out, nil))
				assert.Equal(t, "root=[DEFAULT]", out.String())
			}
		}()
	}

	for i := range cloners {
		go func() {
			defer wg.Done()

			for range 10 {
				clone, err := Clone(origin, FromTemplate("leaf.gotmpl", []byte(strings.Repeat("X", i+1))))
				assert.NoError(t, err)

				var out strings.Builder
				tpl, err := clone.Get("root")
				assert.NoError(t, err)
				assert.NoError(t, tpl.Execute(&out, nil))
				assert.Equal(t, "root=["+strings.Repeat("X", i+1)+"]", out.String())
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, "root=[DEFAULT]", render(t, origin, "root"))
}

// TestScoping documents how a reference is resolved: mangled to a key, then looked for outward
// from the template holding it - its own children, its directory, each enclosing directory, the
// root. The nearest match wins, and a "define" reaches no further than the directory it sits in.
func TestScoping(t *testing.T) {
	assets := fstest.MapFS{
		"server/fred.gotmpl":                 {Data: []byte(`{{define "inner-macro"}}MACRO{{end}}fred=[{{template "inner-macro" .}}]`)},
		"server/operations/operation.gotmpl": {Data: []byte(`{{define "deep"}}DEEP{{end}}op`)},
		"server/claude.gotmpl":               {Data: []byte(`claude`)},
		"client/swagger.gotmpl":              {Data: []byte(`swagger`)},
	}

	r, err := New(FromFS(assets, ""))
	require.NoError(t, err)

	t.Run("should address a define under the asset declaring it", func(t *testing.T) {
		assert.Equal(t, []string{
			"clientSwagger", "serverClaude", "serverFred", "serverFredInnerMacro",
			"serverOperationsOperation", "serverOperationsOperationDeep",
		}, slices.Collect(r.Names()))
	})

	t.Run("should map an address onto the name it answers to", func(t *testing.T) {
		name := r.NameOf("server/fred/inner-macro")
		assert.Equal(t, "serverFredInnerMacro", name)
		assert.True(t, r.Has(name))

		address, found := r.AddressOf("serverFredInnerMacro")
		assert.True(t, found)
		assert.Equal(t, "server/fred/inner-macro", address)

		assert.False(t, r.Has(r.NameOf("inner-macro")), "a define is not addressed at the root")
	})

	t.Run("should let a template reach its own define by the name it gave it", func(t *testing.T) {
		assert.Equal(t, "fred=[MACRO]", render(t, r, "serverFred"))
	})

	for _, reference := range []string{
		"serverFred",                    // by key, from the root
		"serverFredInnerMacro",          // a define, by key
		"serverOperationsOperationDeep", // a define of a deeper directory, by key
		"fred",                          // relative to the caller's own directory
		"fredInnerMacro",                // a define of a sibling, relative
		"operationsOperation",           // relative, through a directory
		"inner-macro",                   // the bare name of a sibling's define
	} {
		t.Run("should resolve "+reference+" from a sibling", func(t *testing.T) {
			reaching, err := Clone(r, FromTemplate("server/reaching.gotmpl",
				[]byte(`[{{template "`+reference+`" .}}]`)))
			require.NoErrorf(t, err, "expected %q to resolve from server/", reference)
			assert.NotEmpty(t, render(t, reaching, "serverReaching"))
		})
	}

	for _, reference := range []string{
		"fred",           // a sibling of server/, not of client/
		"fredInnerMacro", // likewise
		"inner-macro",    // a define reaches no further than its own directory
		"operation",      // no deep search: server/operation does not exist
	} {
		t.Run("should refuse "+reference+" from another directory", func(t *testing.T) {
			_, err := Clone(r, FromTemplate("client/reaching.gotmpl",
				[]byte(`[{{template "`+reference+`" .}}]`)))
			require.Errorf(t, err, "expected %q not to resolve from client/", reference)
			assert.ErrorIs(t, err, ErrTemplateRepo)
		})
	}

	t.Run("should let the nearest scope shadow the ones outside it", func(t *testing.T) {
		shadowing, err := Clone(r,
			FromTemplate("swagger.gotmpl", []byte(`ROOT`)),
			FromTemplate("client/reaching.gotmpl", []byte(`[{{template "swagger" .}}]`)),
		)
		require.NoError(t, err)

		// client/swagger stands nearer than the swagger at the root
		assert.Equal(t, "[swagger]", render(t, shadowing, "clientReaching"))
	})
}

// TestCollision covers what a repository cannot tell apart, and what it takes as an override.
// Overriding is declaring the same address again; two addresses answering to one reference is not.
func TestCollision(t *testing.T) {
	t.Run("should refuse two assets of a directory declaring the same define", func(t *testing.T) {
		_, err := New(FromFS(fstest.MapFS{
			"one.gotmpl": {Data: []byte(`{{define "shared"}}FROM-ONE{{end}}`)},
			"two.gotmpl": {Data: []byte(`{{define "shared"}}FROM-TWO{{end}}`)},
		}, ""))

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTemplateRepo)
		assert.ErrorContains(t, err, `both declare "shared"`)
	})

	t.Run("should refuse it across sources too, an address being no layer's to take", func(t *testing.T) {
		_, err := New(
			FromFS(fstest.MapFS{"one.gotmpl": {Data: []byte(`{{define "shared"}}FROM-ONE{{end}}`)}}, ""),
			FromFS(fstest.MapFS{"two.gotmpl": {Data: []byte(`{{define "shared"}}FROM-TWO{{end}}`)}}, ""),
		)

		require.Error(t, err)
		assert.ErrorContains(t, err, `both declare "shared"`)
	})

	t.Run("should let a define of another directory carry the same name", func(t *testing.T) {
		r, err := New(FromFS(fstest.MapFS{
			"one.gotmpl":      {Data: []byte(`{{define "shared"}}FROM-ONE{{end}}one=[{{template "shared"}}]`)},
			"deep/two.gotmpl": {Data: []byte(`{{define "shared"}}FROM-TWO{{end}}two=[{{template "shared"}}]`)},
		}, ""))
		require.NoError(t, err)

		assert.Equal(t, "one=[FROM-ONE]", render(t, r, "one"))
		assert.Equal(t, "two=[FROM-TWO]", render(t, r, "deepTwo"))
	})

	t.Run("should report two asset paths that yield the same key", func(t *testing.T) {
		_, err := New(FromFS(fstest.MapFS{
			"some-name.gotmpl": {Data: []byte("kebab")},
			"some_name.gotmpl": {Data: []byte("snake")},
		}, ""))

		require.Error(t, err)
		assert.ErrorContains(t, err, `template "some_name" is declared by assets`)
	})

	t.Run("should let a caller's own define outrank an asset of the same name", func(t *testing.T) {
		r, err := New(FromFS(fstest.MapFS{
			"model.gotmpl": {Data: []byte("ASSET")},
			"other.gotmpl": {Data: []byte(`{{define "model"}}DEFINE{{end}}other=[{{template "model"}}]`)},
		}, ""))
		require.NoError(t, err)

		// the nearest scope wins, and a template's own define is as near as it gets
		assert.Equal(t, "other=[DEFINE]", render(t, r, "other"))
	})

	t.Run("should refuse a reference an asset and a define both answer", func(t *testing.T) {
		// from a third template the two stand at the same distance, and nothing says which is meant
		_, err := New(FromFS(fstest.MapFS{
			"model.gotmpl": {Data: []byte("ASSET")},
			"other.gotmpl": {Data: []byte(`{{define "model"}}DEFINE{{end}}other`)},
			"third.gotmpl": {Data: []byte(`third=[{{template "model"}}]`)},
		}, ""))

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTemplateRepo)
		assert.ErrorContains(t, err, "which is both the template addressed")
	})

	t.Run("should take a further source declaring the same address as an override", func(t *testing.T) {
		r, err := New(
			FromFS(fstest.MapFS{"one.gotmpl": {Data: []byte(`{{define "shared"}}FROM-ONE{{end}}`)}}, ""),
			FromTemplate("one.gotmpl", []byte(`{{define "shared"}}FROM-TWO{{end}}`)),
		)
		require.NoError(t, err)

		assert.Equal(t, "FROM-TWO", render(t, r, "oneShared"))
	})

	t.Run("should take a clone declaring the same address as an override", func(t *testing.T) {
		origin, err := New(FromFS(fstest.MapFS{"one.gotmpl": {Data: []byte(`{{define "shared"}}ORIGIN{{end}}`)}}, ""))
		require.NoError(t, err)

		clone, err := Clone(origin, FromTemplate("one.gotmpl", []byte(`{{define "shared"}}CLONE{{end}}`)))
		require.NoError(t, err)

		assert.Equal(t, "CLONE", render(t, clone, "oneShared"))
		assert.Equal(t, "ORIGIN", render(t, origin, "oneShared"))
	})
}
