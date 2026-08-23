// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

import "maps"

type (
	// Option configures [Format].
	Option func(options) options

	options struct {
		groups   []string
		resolved map[string]string

		goFumpt         bool
		forcePruning    bool
		simplifyAliases bool
	}
)

// WithImportGroups adds one import group per prefix, between the standard library and the rest.
//
// An import belongs to the first prefix it starts with, so pass the more specific prefix first.
// Without this option the output has two groups: the standard library, then everything else.
func WithImportGroups(prefixes ...string) Option {
	return func(o options) options {
		for _, prefix := range prefixes {
			if prefix == "" {
				continue
			}

			o.groups = append(o.groups, prefix)
		}

		return o
	}
}

// WithGoFumpt applies the gofumpt rules before printing.
//
// Blank-import github.com/go-openapi/codegen/formatting/enable/gofumpt to make the rules available.
// Without it [Format] returns [ErrNoGoFumpt] rather than printing without them.
func WithGoFumpt() Option {
	return func(o options) options {
		o.goFumpt = true

		return o
	}
}

// WithForceImportsPruning prunes an unused import even when its name was only guessed.
//
// Passing it is a promise: every import in the source either carries an alias, or declares the name
// [ImportedPackageName] gives for its path — the last path element, with a /v2 or later suffix
// dropped and the last segment taken from a hyphenated element. Idiomatic packages keep that promise.
// "github.com/json-iterator/go" declares jsoniter and breaks it, and such an import is then pruned
// although the file uses it.
//
// Without this option the formatter keeps a bare third-party import it cannot name, and reports it as
// in doubt.
//
// Pass [WithResolvedImports] alongside to cover the imports the promise does not. A name given there
// is used instead of the guess, so one awkward dependency does not cost the promise:
//
//	formatting.Format(out, src,
//		formatting.WithForceImportsPruning(),
//		formatting.WithResolvedImports(map[string]string{
//			"github.com/json-iterator/go": "jsoniter",
//		}),
//	)
func WithForceImportsPruning() Option {
	return func(o options) options {
		o.forcePruning = true

		return o
	}
}

// WithResolvedImports states the name each import path declares, for the paths no rule can guess.
//
// "github.com/json-iterator/go" declares jsoniter and "github.com/prometheus/client_model/go"
// declares io_prometheus_client; nothing in either path says so. A name given here is treated as
// certain, so the import is pruned when unused and never reported as in doubt.
//
// A path appears in at most one place, and the first of these wins: an alias written in the source,
// this map, then the generated standard library table, then the guesses.
//
// It combines with [WithForceImportsPruning], which settles every path the map leaves out.
//
// The map is read, not kept: pass the same map to as many concurrent calls as you like. Build it with
// github.com/go-openapi/codegen/formatting/resolve, which answers from the packages themselves rather
// than from the machine, so one map serves every build.
func WithResolvedImports(names map[string]string) Option {
	return func(o options) options {
		if len(names) == 0 {
			return o
		}

		if o.resolved == nil {
			o.resolved = make(map[string]string, len(names))
		}

		maps.Copy(o.resolved, names)

		return o
	}
}

// WithSimplifiedImportAliases drops an alias that repeats the name its package declares.
//
//	import fmt "fmt"                              ->  import "fmt"
//	import strfmt "github.com/go-openapi/strfmt"  ->  import "github.com/go-openapi/strfmt"
//
// A template that writes the alias even where Go would leave it out gets exact pruning without
// promising anything, because an alias states the name. This takes those aliases back out once the
// name is proven, so the output reads as ordinary Go. The second line above needs
// [WithResolvedImports] to name that package; the first is proven by the standard library table.
//
// An alias survives when dropping it would lose something. jsoniter "github.com/json-iterator/go"
// keeps its alias even with the name proven, because the path does not say jsoniter and the bare
// import would leave nothing that does. So does an alias that renames a package, as sql "database/sql
// /driver", and so do _ and . imports.
//
// Nothing is dropped on a guess. Without evidence from the table or the map, every alias stays.
func WithSimplifiedImportAliases() Option {
	return func(o options) options {
		o.simplifyAliases = true

		return o
	}
}

// applyWithDefaults folds the chain over the zero options, left to right.
//
// The zero value is the default throughout: no group past the standard library and the rest, no
// gofumpt, no forced pruning, and no name the caller supplied.
func applyWithDefaults(opts []Option) options {
	var o options

	for _, apply := range opts {
		o = apply(o)
	}

	return o
}
