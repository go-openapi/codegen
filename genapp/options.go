// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"path/filepath"
	"strings"

	"github.com/go-openapi/codegen/formatting"
	repo "github.com/go-openapi/codegen/templates-repo"
)

// Option configures a [GoGenApp].
type Option func(*options)

type options struct {
	templates       *repo.Repository
	outputPath      string
	formatOptions   []formatting.Option
	importsReporter func(string, *formatting.ImportsReport)
	skipFormat      bool
	skipFormatFunc  func(target string) bool
}

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
	return func(o *options) {
		o.templates = templates
	}
}

// WithOutputPath sets where [GoGenApp.RenderFile] writes. Targets are relative to that directory.
func WithOutputPath(path string) Option {
	return func(o *options) {
		o.outputPath = path
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
	return func(o *options) {
		o.formatOptions = append(o.formatOptions, opts...)
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
// Read the names of the paths it lists once, then pass them through
// [formatting.WithResolvedImports] with [WithFormatOptions].
func WithImportsReporter(report func(template string, report *formatting.ImportsReport)) Option {
	return func(o *options) {
		o.importsReporter = report
	}
}

// WithSkipFormat writes every target as the template rendered it.
//
// A template that produces Go which does not parse makes [GoGenApp.RenderFile] fail with nothing
// written, and finding out why means reading the output. Turn this on and the file lands
// unformatted.
func WithSkipFormat(skipped bool) Option {
	return func(o *options) {
		o.skipFormat = skipped
	}
}

// WithSkipFormatFunc decides which targets [GoGenApp.RenderFile] formats.
//
// The default formats a target whose name ends in ".go" and copies anything else through.
func WithSkipFormatFunc(skip func(target string) bool) Option {
	return func(o *options) {
		o.skipFormatFunc = skip
	}
}

// skipsFormat reports whether a target is written as rendered.
func (o options) skipsFormat(target string) bool {
	return o.skipFormat || o.skipFormatFunc(target)
}

func optionsWithDefaults(opts []Option) options {
	var o options

	for _, apply := range opts {
		apply(&o)
	}

	if o.skipFormatFunc == nil {
		o.skipFormatFunc = func(target string) bool {
			return !strings.EqualFold(filepath.Ext(target), ".go")
		}
	}

	return o
}
