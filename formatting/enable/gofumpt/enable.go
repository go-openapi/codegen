// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package gofumpt

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
	"sync"

	"github.com/go-openapi/codegen/formatting/internal/rules"
	fumpt "mvdan.cc/gofumpt/format"
)

// settings holds what the registered pass applies. Configure writes it, Format reads it.
var (
	mx       sync.RWMutex
	settings fumpt.Options
)

func init() { //nolint:gochecknoinits // a blank import exists to run this
	rules.Register(apply)
}

// apply runs the gofumpt rules over a parsed file.
func apply(fset *token.FileSet, file *ast.File) {
	mx.RLock()
	current := settings
	mx.RUnlock()

	fumpt.File(fset, file, current)
}

type (
	// Option configures the gofumpt rules.
	Option func(options) options

	// options carries the gofumpt settings being assembled, and the first option that rejected its
	// arguments. [fumpt.Options] holds no pointer, so the copy the chain passes along is a value.
	options struct {
		fumpt fumpt.Options
		err   error
	}
)

// withError keeps the first failure and lets the rest of the chain run.
func (o options) withError(err error) options {
	if o.err == nil {
		o.err = err
	}

	return o
}

// applyWithDefaults folds the chain over the zero options, left to right.
//
// The zero value is gofumpt's own default: every extra rule off, and a language version of go1.
func applyWithDefaults(opts []Option) options {
	var o options

	for _, apply := range opts {
		o = apply(o)
	}

	return o
}

// Configure sets the rules for the whole program.
//
// Call it once, before formatting anything. It returns an error when an option is not one gofumpt
// knows, and leaves the previous settings in place.
func Configure(opts ...Option) error {
	next := applyWithDefaults(opts)
	if next.err != nil {
		return next.err
	}

	mx.Lock()
	settings = next.fumpt
	mx.Unlock()

	return nil
}

// WithLangVersion sets the Go version whose rules apply, as in "go1.25".
//
// gofumpt holds back the rules that need a language newer than the code targets. Empty means
// go1, which holds back all of them.
func WithLangVersion(version string) Option {
	return func(o options) options {
		o.fumpt.LangVersion = version

		return o
	}
}

// WithModulePath sets the module the formatted code belongs to, as in "example.com/petstore".
//
// gofumpt reads it to decide which import paths are outside the standard library when it puts the
// standard library imports first.
func WithModulePath(path string) Option {
	return func(o options) options {
		o.fumpt.ModulePath = path

		return o
	}
}

// WithExtraRules turns on rules gofumpt leaves off, named as gofumpt names them on its command line:
// "group_params", "clothe_returns", "balance_calls". Passing "true" turns all of them on.
//
// The rules named replace whatever an earlier WithExtraRules asked for, which is how gofumpt's own
// -extra flag behaves. Name every rule in one call:
//
//	gofumpt.WithExtraRules("group_params", "clothe_returns")
func WithExtraRules(rules ...string) Option {
	return func(o options) options {
		// Extra.Set clears itself before reading a list, so one call per rule would keep only the
		// last. It takes the comma-separated form gofumpt's -extra flag takes.
		named := strings.Join(rules, ",")

		if err := o.fumpt.Extra.Set(named); err != nil {
			return o.withError(fmt.Errorf("unknown gofumpt rule in %q: %w", named, err))
		}

		return o
	}
}
