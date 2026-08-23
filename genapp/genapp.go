// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-openapi/swag/pools/shared"

	"github.com/go-openapi/codegen/formatting"
	repo "github.com/go-openapi/codegen/templates-repo"
)

// dirPerm and filePerm set the mode of a generated tree.
const (
	dirPerm  = 0o750
	filePerm = 0o640
)

// GoGenApp renders templates into formatted Go.
//
// It holds a templates repository and the formatting settings, and does the same three things for
// every file a generator produces: execute a template, format what it produced, write it.
type GoGenApp struct {
	options
}

// New builds a [GoGenApp] rendering from the repository [WithTemplates] gives it.
//
// It returns an error when no repository was given. Everything a repository is made of — its
// sources, its funcmap, the roots it is scoped to — is settled by
// [github.com/go-openapi/codegen/templates-repo.New], and a repository that would not build has
// already reported why by the time it reaches here.
func New(opts ...Option) (*GoGenApp, error) {
	o := optionsWithDefaults(opts)

	if o.templates == nil {
		return nil, fmt.Errorf("a templates repository is required, see WithTemplates: %w", ErrGenApp)
	}

	return &GoGenApp{options: o}, nil
}

// Templates returns the repository the app renders from, for a caller wanting to document, audit or
// derive it.
func (g *GoGenApp) Templates() *repo.Repository {
	return g.templates
}

// Render executes a template and writes the formatted result to w.
//
// The repository knows each template by a name derived from its asset path; see
// [github.com/go-openapi/codegen/templates-repo].
//
// Render formats unless [WithSkipFormat] is set, so it is the entry point for Go. Use
// [GoGenApp.RenderFile], which decides by target name, for a generator writing Go and other things
// side by side.
//
// A template rendering Go that does not parse leaves w untouched: the formatter reads the whole
// source before it writes anything.
func (g *GoGenApp) Render(w io.Writer, name string, data any) error {
	rendered := shared.BorrowBuffer()
	defer shared.RedeemBuffer(rendered)

	if err := g.execute(rendered, name, data); err != nil {
		return err
	}

	if g.skipFormat {
		if _, err := w.Write(rendered.Bytes()); err != nil {
			return fmt.Errorf("cannot write rendered %q: %w: %w", name, err, ErrGenApp)
		}

		return nil
	}

	return g.format(w, name, rendered)
}

// RenderFile executes a template and writes the result to target, under the output path.
//
// It creates the directories the target needs. A target ending in ".go" is formatted; anything else
// is written as rendered. See [WithSkipFormatFunc] and [WithSkipFormat].
//
// The file appears whole or not at all: RenderFile writes beside the target and renames over it, so
// a template that fails to render or to format leaves whatever was there untouched, and a write
// that fails halfway leaves no half-written target.
//
// When the formatter rejects what a template rendered, the unformatted output is kept beside the
// target, named for it with a ".unformatted" suffix, and the error names the file. A parse error
// reports a line and a column, and reading them means reading the source they came from; that
// source would otherwise be gone.
func (g *GoGenApp) RenderFile(target, name string, data any) error {
	rendered := shared.BorrowBuffer()
	defer shared.RedeemBuffer(rendered)

	if err := g.execute(rendered, name, data); err != nil {
		return err
	}

	path := filepath.Join(g.outputPath, filepath.FromSlash(target))
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("cannot create the directory for %q: %w: %w", target, err, ErrGenApp)
	}

	return g.writeFile(path, target, name, rendered)
}

// execute renders one template into the buffer it is given.
//
// The buffer comes from the shared pool rather than the [GoGenApp], which holds no state so that
// [GoGenApp.Render] and [GoGenApp.RenderFile] may run concurrently.
//
// [github.com/go-openapi/swag/pools/shared.RedeemBuffer] drops a buffer grown past 64 KiB instead
// of recycling it, so a generator that emits one enormous file does not park an enormous buffer in
// a pool the whole process shares. A file that size costs an allocation; the ones a generator
// usually writes run a couple of kilobytes and are recycled.
func (g *GoGenApp) execute(into *bytes.Buffer, name string, data any) error {
	tpl, err := g.templates.Get(name)
	if err != nil {
		return fmt.Errorf("no template %q: %w: %w", name, err, ErrGenApp)
	}

	if err := tpl.Execute(into, data); err != nil {
		return fmt.Errorf("cannot render template %q: %w: %w", name, err, ErrGenApp)
	}

	return nil
}

// format runs the formatter over what a template rendered.
// unformattedSuffix names the file a failed format leaves behind.
const unformattedSuffix = ".unformatted"

// dumpUnformatted keeps what a template rendered, so a parse error can be read against its source.
//
// The formatter may have printed part of a file before it failed, so the file is truncated and
// written again from the rendered bytes, then moved off the temporary name to path plus
// [unformattedSuffix]. The name is visible and predictable on purpose: the file sits beside the
// target it failed to become, and the next run over that target replaces it rather than adding
// another.
//
// The returned error carries the formatting failure and adds the path. It attaches no second
// [ErrGenApp]: the cause already carries one.
func dumpUnformatted(file *os.File, path string, rendered *bytes.Buffer, cause error) error {
	dumped := path + unformattedSuffix

	written := func() error {
		if err := file.Truncate(0); err != nil {
			return err
		}

		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}

		if _, err := file.Write(rendered.Bytes()); err != nil {
			return err
		}

		if err := file.Chmod(filePerm); err != nil {
			return err
		}

		if err := file.Close(); err != nil {
			return err
		}

		return os.Rename(file.Name(), dumped)
	}()

	if written != nil {
		return fmt.Errorf("could not keep the unformatted output at %q (%w): %w", dumped, written, cause)
	}

	return fmt.Errorf("the unformatted output is kept at %q: %w", dumped, cause)
}

// format writes the formatted render, and hands the imports report to the caller's sink.
//
// An import the formatter could not name is kept rather than pruned, so a report holding doubts is
// worth seeing: it lists the paths to feed [github.com/go-openapi/codegen/formatting/resolve]. See
// [WithImportsReporter].
func (g *GoGenApp) format(w io.Writer, name string, rendered *bytes.Buffer) error {
	report, err := formatting.Format(w, rendered, g.formatOptions...)
	if err != nil {
		return fmt.Errorf("template %q rendered Go that does not format: %w: %w", name, err, ErrGenApp)
	}

	if g.importsReporter != nil {
		g.importsReporter(name, report)
	}

	return nil
}

// writeFile writes the target through a temporary file in the same directory, then renames.
func (g *GoGenApp) writeFile(path, target, name string, rendered *bytes.Buffer) (err error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file for %q: %w: %w", target, err, ErrGenApp)
	}

	keep := false

	defer func() {
		if err == nil || keep {
			return
		}

		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()

	if g.skipsFormat(target) {
		if _, err = temporary.Write(rendered.Bytes()); err != nil {
			return fmt.Errorf("cannot write %q: %w: %w", target, err, ErrGenApp)
		}
	} else if formatErr := g.format(temporary, name, rendered); formatErr != nil {
		keep = true
		err = dumpUnformatted(temporary, path, rendered, formatErr)

		return err
	}

	if err = temporary.Chmod(filePerm); err != nil {
		return fmt.Errorf("cannot set the mode of %q: %w: %w", target, err, ErrGenApp)
	}

	if err = temporary.Close(); err != nil {
		return fmt.Errorf("cannot close %q: %w: %w", target, err, ErrGenApp)
	}

	if err = os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("cannot write %q: %w: %w", target, err, ErrGenApp)
	}

	return nil
}
