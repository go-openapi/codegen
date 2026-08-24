// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"path/filepath"
	"strings"

	"github.com/go-openapi/codegen/formatting"
	repo "github.com/go-openapi/codegen/templates-repo"
)

type (
	// Option configures a [GoGenApp].
	Option func(options) options

	options struct {
		templates       *repo.Repository
		outputPath      string
		root            string
		formatOptions   []formatting.Option
		importsReporter func(string, *formatting.ImportsReport)
		skipFormat      bool
		skipFormatFunc  func(target string) bool
	}
)

// WithTemplates sets the repository to render from. [New] needs it.
//
// The repository is built by [github.com/go-openapi/codegen/templates-repo.New], which is where the
// sources, the funcmap and the scoping are decided:
//
//	templates, err := repo.New(
//		repo.FromFS(assets, ""),
//		repo.WithFuncMap(golang.FuncMap(mangling.MakeGoMangler())),
//	)
//	if err != nil {
//		return err
//	}
//
//	app, err := genapp.New(genapp.WithTemplates(templates))
func WithTemplates(templates *repo.Repository) Option {
	return func(o options) options {
		o.templates = templates

		return o
	}
}

// WithOutputPath sets where [GoGenApp.RenderFile] writes. Targets are relative to that directory.
func WithOutputPath(path string) Option {
	return func(o options) options {
		o.outputPath = path

		return o
	}
}

// WithRoot confines every file a [GoGenApp] writes to dir.
//
// The output path must sit at or below dir, and dir must exist. The caller declares the root, so
// WithRoot reports a missing one instead of creating it. Nothing outside dir is written, whether a
// target climbs out with "..", names an absolute path, or reaches a symbolic link pointing away.
// [os.Root] checks each link as it walks the path, where a prefix test on the name alone would miss
// a link halfway down.
//
//	app, err := genapp.New(
//		genapp.WithTemplates(templates),
//		genapp.WithOutputPath("./gen/models"),
//		genapp.WithRoot("./gen"),
//	)
//
// Use it when a spec supplies the target names, such as its operation and model names.
//
// Without it, writes still stay under the output path and the checks on a target still run.
// WithRoot widens the boundary past the output path, for a generator writing into several
// directories of one tree.
//
// It covers this package's writes and no more. [GoGenApp.TidyModule] runs the go command, which
// writes go.mod and go.sum itself, and no root reaches into another process. Reads run unconfined
// too: [GoGenApp.PackagePath] and [GoGenApp.EnclosingModule] walk up from the output path looking
// for a go.mod, and that go.mod usually sits above any root worth setting.
//
// [os.Root] confines path resolution and no more. It does not stop traversal of a bind mount, a
// /proc special file or a device file, so point WithRoot at a directory that holds only generated
// output.
func WithRoot(dir string) Option {
	return func(o options) options {
		o.root = dir

		return o
	}
}

// WithFormatOptions configures the formatter.
//
// Grouping, gofumpt and the rest are settled by
// [github.com/go-openapi/codegen/formatting], and this package re-exports none of it:
//
//	genapp.WithFormatOptions(
//		formatting.WithImportGroups("github.com/go-openapi", baseImport),
//	)
func WithFormatOptions(opts ...formatting.Option) Option {
	return func(o options) options {
		o.formatOptions = append(o.formatOptions, opts...)

		return o
	}
}

// WithImportsReporter calls report for every file rendered, with the name of the template that
// rendered it.
//
// [formatting.Format] keeps an import whose package it cannot name, rather than delete one the code
// may be using, and says so in the report. Use this to see those:
//
//	genapp.WithImportsReporter(func(template string, report *formatting.ImportsReport) {
//		if report.HasImportsInDoubt() {
//			log.Printf("%s: %v", template, report.PathsInDoubt())
//		}
//	})
//
// Resolve the paths it lists once, then pass the names through
// [formatting.WithResolvedImports] with [WithFormatOptions].
func WithImportsReporter(report func(template string, report *formatting.ImportsReport)) Option {
	return func(o options) options {
		o.importsReporter = report

		return o
	}
}

// WithSkipFormat writes every target as the template rendered it.
//
// A template that produces Go which does not parse makes [GoGenApp.RenderFile] fail with nothing
// written, and finding out why means reading the output. Turn this on and the file lands
// unformatted.
func WithSkipFormat(skipped bool) Option {
	return func(o options) options {
		o.skipFormat = skipped

		return o
	}
}

// WithSkipFormatFunc decides which targets [GoGenApp.RenderFile] formats.
//
// The default formats a target whose name ends in ".go" and copies anything else through.
func WithSkipFormatFunc(skip func(target string) bool) Option {
	return func(o options) options {
		o.skipFormatFunc = skip

		return o
	}
}

// skipsFormat reports whether a target is written as rendered.
func (o options) skipsFormat(target string) bool {
	return o.skipFormat || o.skipFormatFunc(target)
}

// applyWithDefaults folds the chain over the zero options, left to right, then settles the one
// default that is not a zero value.
func applyWithDefaults(opts []Option) options {
	var o options

	for _, apply := range opts {
		o = apply(o)
	}

	if o.skipFormatFunc == nil {
		o.skipFormatFunc = func(target string) bool {
			return !strings.EqualFold(filepath.Ext(target), ".go")
		}
	}

	return o
}
