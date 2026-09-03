// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"text/template"
)

// Option configures a [Repository] built with [New] or derived with [Clone].
//
// An option that cannot be honoured reports an error from [New] or [Clone], rather than at the
// point where it is constructed: a repository built from settings the caller did not ask for is
// worse than one that fails to build.
//
// # Usage
//
// Options come in two kinds. Sources declare where templates are read from, and are applied in
// order: [FromFS], [FromDir] and [FromTemplate]. Settings shape how they are read, whatever the
// order: [WithFuncMap], [WithExtensions], [WithRoots], [WithExtraRoots] and [WithCoverage].
// How one source is read is settled where it is declared, with a [SourceOption].
type (
	Option func(options) options

	// options holds the settings of a repository, the sources it is yet to read, and the first
	// option that rejected its arguments.
	options struct {
		sources        []source
		funcs          template.FuncMap
		extensions     []string
		roots          []string
		coverPrefix    string
		err            error
		coverage       bool
		templateOption string
	}
)

// withError keeps the first failure and lets the rest of the chain run.
//
// An option reports a bad argument here rather than from its own constructor, so [New] and [Clone]
// are where a caller sees it - which is where every other reason a repository fails to build shows
// up too.
func (o options) withError(err error) options {
	if o.err == nil {
		o.err = err
	}

	return o
}

// makeOptions applies opts on top of the defaults.
//
// The defaults are built afresh on every call, so that no repository shares a map or a slice
// with another one.
func makeOptions(opts []Option) (options, error) {
	o := options{
		funcs:      make(template.FuncMap),
		extensions: []string{DefaultExtension},
	}

	o = o.apply(opts)
	if o.err != nil {
		return options{}, o.err
	}

	return o, nil
}

// apply folds a list of options over these settings, left to right.
func (o options) apply(opts []Option) options {
	for _, option := range opts {
		if option == nil {
			continue
		}

		o = option(o)
	}

	return o
}

// derive copies the settings of a repository for a [Clone], with no source left to read.
//
// The sources of the original have already been read into assets, which the clone inherits
// directly, so carrying them over would read them twice.
func (o options) derive() options {
	return options{
		funcs:       maps.Clone(o.funcs),
		extensions:  slices.Clone(o.extensions),
		roots:       slices.Clone(o.roots),
		coverPrefix: o.coverPrefix,
		coverage:    o.coverage,
	}
}

// WithFuncMap adds functions that templates may call.
//
// By default a repository binds no function, so templates only have the builtins of
// [text/template]. The map is copied, and repeated calls merge, the last definition of a name
// winning.
//
// Functions are bound when templates are parsed, which is why they cannot be changed afterwards.
// Adding a function to an existing repository is [Clone] with this option: the clone re-parses
// its templates, so the new function reaches all of them.
func WithFuncMap(funcs template.FuncMap) Option {
	return func(o options) options {
		maps.Copy(o.funcs, funcs)

		return o
	}
}

// WithExtensions sets the file extensions recognized as templates when reading a file system.
//
// The default is ".gotmpl" alone. An asset whose name ends with none of them is ignored by
// [FromFS] and [FromDir], while [FromTemplate] registers its content whatever its name.
//
// The extension is trimmed from the asset path before its name is derived, so
// "validation/primitive.gotmpl" is named validationPrimitive.
func WithExtensions(extensions ...string) Option {
	return func(o options) options {
		if len(extensions) == 0 {
			return o.withError(fmt.Errorf("at least one extension is required: %w", ErrTemplateRepo))
		}

		o.extensions = slices.Clone(extensions)

		return o
	}
}

// WithRoots keeps the templates named, and the templates they reach, and prunes the rest.
//
// A generator ships every template it may ever need, and a single run needs a part of them: the
// templates a client needs are not those a server needs. Naming the roots of a run keeps the
// repository to what that run executes, and lets a template set that is incomplete for the other
// runs build all the same.
//
// Roots are names, the identity [Repository.Get] takes, never the address a template was declared
// at. [Repository.NameOf] converts one to the other for a caller holding addresses.
//
// A root no source declares is an error: a filter naming a template that does not exist builds a
// repository that generates nothing, which is worse than a build that fails.
//
// Everything is still read and parsed, since a template only names itself once parsed, so a source
// that does not parse is an error whether it is pruned away or not. Pruning decides only which
// templates the repository holds, and therefore what [Repository.Names] lists, what its
// documentation covers, and what its coverage counts.
//
// This sets the scope rather than adding to it: a [Clone] naming roots of its own is scoped to
// those alone, whatever the repository it derives from was scoped to. [WithExtraRoots] is the one
// that widens a scope. A repository with no root at all keeps every template it reads, which is
// the default.
//
// Example:
//
//	// the templates a client generation executes, and nothing else
//	client, err := repo.Clone(repository, repo.WithRoots("clientClient", "clientParameter", "model"))
func WithRoots(names ...string) Option {
	return func(o options) options {
		scope, err := scopeOf(names)
		if err != nil {
			return o.withError(err)
		}

		o.roots = scope

		return o
	}
}

// WithExtraRoots widens the scope of a repository with roots of its own.
//
// It adds to whatever [WithRoots] settled, and changes nothing on a repository that keeps every
// template it reads, since that scope already holds them. It takes names, as [WithRoots] does.
//
// Use it to add a template to a repository the caller did not build. [WithRoots] would be wrong
// either way: on an unscoped repository it prunes everything else away, and on a scoped one it
// discards the scope already set.
//
// Example:
//
//	// one more template, reachable whether or not the repository is scoped
//	mine, err := repo.Clone(repository, repo.FromTemplate("mine", body), repo.WithExtraRoots("mine"))
func WithExtraRoots(names ...string) Option {
	return func(o options) options {
		scope, err := scopeOf(names)
		if err != nil {
			return o.withError(err)
		}

		if len(o.roots) == 0 {
			return o // every template is kept already, these among them
		}

		widened := slices.Clone(o.roots)

		for _, name := range scope {
			if !slices.Contains(widened, name) {
				widened = append(widened, name)
			}
		}

		o.roots = widened

		return o
	}
}

// scopeOf checks the roots a caller names, and drops the repetitions.
func scopeOf(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("at least one root template is required: %w", ErrTemplateRepo)
	}

	scope := make([]string, 0, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("a root template cannot be unnamed: %w", ErrTemplateRepo)
		}

		if !slices.Contains(scope, name) {
			scope = append(scope, name)
		}
	}

	return scope, nil
}

// WithCoverage counts the lines of the templates that run.
//
// Counting is decided here rather than later: the templates that execute have to be the ones
// holding the counters, so a repository either is instrumented or is not. [Clone] carries the
// setting over, and a clone of a plain repository asking for it yields an instrumented twin.
//
// prefix is prepended to the path of every asset in the profile [Repository.Coverage] writes.
// go tool cover resolves the file a profile names by asking go list, so the paths have to read as
// an import path of a package that exists, so prefix is required.
//
// Example:
//
//	repo.WithCoverage("github.com/go-swagger/go-swagger/generator/templates")
func WithCoverage(prefix string) Option {
	return func(o options) options {
		if strings.TrimSpace(prefix) == "" {
			return o.withError(
				fmt.Errorf("coverage needs the import path the templates live under: %w", ErrTemplateRepo),
			)
		}

		o.coverage = true
		o.coverPrefix = strings.TrimSuffix(prefix, "/") + "/"

		return o
	}
}

// MissingKeyBehavior exposes the options provided by [template.Template].
type MissingKeyBehavior string

const (
	MissingKeyBehaviorDefault MissingKeyBehavior = "missingkey=default"
	MissingKeyBehaviorZero    MissingKeyBehavior = "missingkey=zero"
	MissingKeyBehaviorError   MissingKeyBehavior = "missingkey=error"
)

// WithMissingKey instructs templates to be built with the options provided by [template.Template]:
//
//   - [MissingKeyBehaviorDefault]: execution continues on missing keys on maps, the value is set to "<no value>".
//   - [MissingKeyBehaviorZero]: execution continues with the value set to the zero value of the element type.
//   - [MissingKeyBehaviorError]: execution stops with an error
func WithMissingKey(onMissingKey MissingKeyBehavior) Option {
	return func(o options) options {
		o.templateOption = string(onMissingKey)

		return o
	}
}
