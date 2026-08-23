// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package gofumpt

import (
	"fmt"
	"go/ast"
	"go/token"
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

// Option configures the gofumpt rules.
type Option func(*fumpt.Options) error

// Configure sets the rules for the whole program.
//
// Call it once, before formatting anything. It returns an error when an option is not one gofumpt
// knows, and leaves the previous settings in place.
func Configure(opts ...Option) error {
	next := fumpt.Options{}

	for _, apply := range opts {
		if err := apply(&next); err != nil {
			return err
		}
	}

	mx.Lock()
	settings = next
	mx.Unlock()

	return nil
}

// WithLangVersion sets the Go version whose rules apply, as in "go1.25".
//
// gofumpt holds back the rules that need a language newer than the code targets. Empty means
// go1, which holds back all of them.
func WithLangVersion(version string) Option {
	return func(o *fumpt.Options) error {
		o.LangVersion = version

		return nil
	}
}

// WithModulePath sets the module the formatted code belongs to, as in "example.com/petstore".
//
// gofumpt reads it to decide which import paths are outside the standard library when it puts the
// standard library imports first.
func WithModulePath(path string) Option {
	return func(o *fumpt.Options) error {
		o.ModulePath = path

		return nil
	}
}

// WithExtraRules turns on rules gofumpt leaves off, named as gofumpt names them on its command line:
// "group_params", "clothe_returns", "balance_calls". Passing "true" turns all of them on.
func WithExtraRules(rules ...string) Option {
	return func(o *fumpt.Options) error {
		for _, rule := range rules {
			if err := o.Extra.Set(rule); err != nil {
				return fmt.Errorf("unknown gofumpt rule %q: %w", rule, err)
			}
		}

		return nil
	}
}
