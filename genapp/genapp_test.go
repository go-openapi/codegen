// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp_test

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/formatting"
	"github.com/go-openapi/codegen/funcmaps/golang"
	"github.com/go-openapi/codegen/genapp"
	"github.com/go-openapi/codegen/mangling"
	repo "github.com/go-openapi/codegen/templates-repo"
)

//go:embed testdata/templates
var templates embed.FS

type model struct {
	Package string
	Name    string
}

var pet = model{Package: "models", Name: "pet_owner"}

// newRepo builds the fixture repository the way a generator would.
func newRepo(t testing.TB, opts ...repo.Option) *repo.Repository {
	t.Helper()

	assets, err := fs.Sub(templates, "testdata/templates")
	require.NoError(t, err)

	templates, err := repo.New(append([]repo.Option{
		repo.FromFS(assets, ""),
		repo.WithFuncMap(golang.FuncMap(mangling.MakeGoMangler())),
	}, opts...)...)
	require.NoError(t, err)

	return templates
}

func newApp(t testing.TB, opts ...genapp.Option) *genapp.GoGenApp {
	t.Helper()

	app, err := genapp.New(append([]genapp.Option{
		genapp.WithTemplates(newRepo(t)),
	}, opts...)...)
	require.NoError(t, err)

	return app
}

func TestRender(t *testing.T) {
	t.Parallel()

	t.Run("should render and format a template", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, newApp(t).Render(&out, "model", pet))

		rendered := out.String()
		assert.Contains(t, rendered, "type PetOwner struct {", "the funcmap mangles the name")
		assert.Contains(t, rendered, "func (m *PetOwner) Validate(ctx context.Context) error {")
		assert.NotContains(t, rendered, `"strings"`, "the formatter prunes what the template did not use")
		assert.Contains(t, rendered, "\t\"context\"\n\t\"fmt\"\n\n\t\"github.com/go-openapi/strfmt\"\n",
			"and groups what is left")
	})

	t.Run("should group imports by the prefixes given", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		app := newApp(t, genapp.WithFormatOptions(formatting.WithImportGroups("github.com/go-openapi")))
		require.NoError(t, app.Render(&out, "model", pet))

		assert.Contains(t, out.String(), "\t\"context\"\n\t\"fmt\"\n\n\t\"github.com/go-openapi/strfmt\"\n")
	})

	t.Run("should reach a template under a directory by its name", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, newApp(t).Render(&out, "modelsNested", pet))

		assert.Contains(t, out.String(), "type PetOwnerNested struct{ Value int }")
	})

	t.Run("should report a template it does not hold", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		err := newApp(t).Render(&out, "noSuchTemplate", pet)

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "noSuchTemplate")
	})

	t.Run("should report Go that does not format", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		err := newApp(t).Render(&out, "broken", pet)

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "broken")
	})

	t.Run("should leave the writer untouched when the Go does not parse", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.Error(t, newApp(t).Render(&out, "broken", pet))

		assert.Zero(t, out.Len(), "the formatter reads the whole source before it writes")
	})

	t.Run("should write what the template rendered when format is skipped", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		app := newApp(t, genapp.WithSkipFormat(true))
		require.NoError(t, app.Render(&out, "broken", pet), "unformattable Go still lands")

		assert.Contains(t, out.String(), "func Broken( {")
	})
}

func TestRenderFile(t *testing.T) {
	t.Parallel()

	t.Run("should write a formatted file under the output path", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.RenderFile("models/pet.go", "model", pet))

		written, err := os.ReadFile(filepath.Join(dir, "models", "pet.go"))
		require.NoError(t, err)
		assert.Contains(t, string(written), "type PetOwner struct {")
		assert.NotContains(t, string(written), `"strings"`)
	})

	t.Run("should create the directories a target needs", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.RenderFile("a/b/c/pet.go", "model", pet))
		assert.FileExists(t, filepath.Join(dir, "a", "b", "c", "pet.go"))
	})

	t.Run("should copy a target that is not Go through unformatted", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.NoError(t, app.RenderFile("README.md", "readme", pet))

		written, err := os.ReadFile(filepath.Join(dir, "README.md"))
		require.NoError(t, err)
		assert.Equal(t, "# pet_owner\n\nGenerated, and not Go.\n", string(written))
	})

	t.Run("should keep the unformatted output when formatting fails", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		err := app.RenderFile("broken.go", "broken", pet)
		require.Error(t, err)

		dumped := dumpedPath(t, err)
		assert.Equal(t, filepath.Join(dir, "broken.go.unformatted"), dumped,
			"named for the target, beside it, and the same name on the next run")

		kept, readErr := os.ReadFile(dumped)
		require.NoError(t, readErr, "the error names a file that is there")
		assert.Contains(t, string(kept), "func Broken( {", "and it holds what the template rendered")

		_, statErr := os.Stat(filepath.Join(dir, "broken.go"))
		assert.ErrorIs(t, statErr, os.ErrNotExist, "the target itself was never written")
	})

	t.Run("should keep nothing when the template fails before the formatter", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t, genapp.WithOutputPath(dir))

		require.Error(t, app.RenderFile("pet.go", "noSuchTemplate", pet))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries, "nothing was rendered, so there is nothing to inspect")
	})

	t.Run("should leave an existing file untouched when the render fails", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		target := filepath.Join(dir, "broken.go")
		require.NoError(t, os.WriteFile(target, []byte("package kept\n"), 0o600))

		app := newApp(t, genapp.WithOutputPath(dir))
		require.Error(t, app.RenderFile("broken.go", "broken", pet))

		kept, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "package kept\n", string(kept))
	})

	t.Run("should honour a caller's own skip rule", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		app := newApp(t,
			genapp.WithOutputPath(dir),
			genapp.WithSkipFormatFunc(func(string) bool { return true }),
		)

		require.NoError(t, app.RenderFile("broken.go", "broken", pet), "nothing is formatted")
		assert.FileExists(t, filepath.Join(dir, "broken.go"))
	})
}

// dumpedPath pulls the kept file out of a formatting error.
//
// The message writes the path with %q, so what follows the marker is a Go quoted string, not the path
// itself. On Windows every separator arrives escaped - "D:\\a\\codegen" - so the quotes are cut with
// [strconv.QuotedPrefix] and the escapes undone with [strconv.Unquote] rather than by hand.
func dumpedPath(t *testing.T, err error) string {
	t.Helper()

	const marker = "the unformatted output is kept at "

	message := err.Error()
	start := strings.Index(message, marker)
	require.GreaterOrEqual(t, start, 0, "the error names the file it kept: %v", err)

	quoted, quoteErr := strconv.QuotedPrefix(message[start+len(marker):])
	require.NoError(t, quoteErr, "the path is quoted: %v", err)

	path, unquoteErr := strconv.Unquote(quoted)
	require.NoError(t, unquoteErr)

	return path
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("should refuse to build without a repository", func(t *testing.T) {
		t.Parallel()

		_, err := genapp.New(genapp.WithOutputPath(t.TempDir()))

		require.Error(t, err)
		assert.ErrorIs(t, err, genapp.ErrGenApp)
		assert.Contains(t, err.Error(), "WithTemplates")
	})

	t.Run("should expose the repository it built", func(t *testing.T) {
		t.Parallel()

		names := newApp(t).Templates().Names()
		require.NotNil(t, names)

		var found []string
		for name := range names {
			found = append(found, name)
		}

		assert.Contains(t, found, "model")
		assert.Contains(t, found, "modelsNested")
	})
}
