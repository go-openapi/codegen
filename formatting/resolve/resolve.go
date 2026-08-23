// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package resolve

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Error is a string that implements error, so a sentinel below can be a constant.
type Error string

func (e Error) Error() string { return string(e) }

const (
	// ErrResolve matches every error [Names] returns.
	ErrResolve Error = "cannot resolve import names"

	// ErrUnresolved is returned when a path did not come back with a name. The map still holds every
	// path that did, so a caller may use what resolved and act on the rest.
	ErrUnresolved Error = "some import paths did not resolve"
)

// Option configures [Names].
type Option func(*options)

type options struct {
	dir        string
	env        []string
	buildFlags []string
}

// WithDir loads the packages as if from dir.
//
// "go list" runs there, so dir has to sit in a module requiring the paths being asked about. Without
// it the current working directory is used, which is right only when the process already runs inside
// that module.
func WithDir(dir string) Option {
	return func(o *options) {
		o.dir = dir
	}
}

// WithEnv replaces the environment "go list" runs with, in the form os.Environ returns.
//
// Use it to pin GOFLAGS, GOPATH or GOMODCACHE. An empty slice leaves the process environment alone.
func WithEnv(env []string) Option {
	return func(o *options) {
		o.env = slices.Clone(env)
	}
}

// WithBuildFlags passes flags to "go list", as in -tags or -mod=mod.
func WithBuildFlags(flags ...string) Option {
	return func(o *options) {
		o.buildFlags = append(o.buildFlags, flags...)
	}
}

// Names returns the name each import path declares, ready for
// [github.com/go-openapi/codegen/formatting.WithResolvedImports].
//
// It loads the packages, so it needs the go toolchain and a module requiring them: see [WithDir].
// An empty or nil paths leaves it returning an empty map and no error, so a caller may hand it
// [github.com/go-openapi/codegen/formatting.ImportsReport.PathsInDoubt] without checking first.
//
// A path that does not resolve is left out of the map, and the error wraps [ErrUnresolved] and names
// every one of them. The map still holds what did resolve, so use it and report the rest:
//
//	names, err := resolve.Names(ctx, paths)
//	if err != nil && !errors.Is(err, resolve.ErrUnresolved) {
//		return err
//	}
//
// Duplicate paths are asked once. The map has one entry per distinct path.
func Names(ctx context.Context, paths []string, opts ...Option) (map[string]string, error) {
	wanted := distinct(paths)
	if len(wanted) == 0 {
		return map[string]string{}, nil
	}

	var o options
	for _, apply := range opts {
		apply(&o)
	}

	loaded, err := packages.Load(&packages.Config{
		Context:    ctx,
		Mode:       packages.NeedName,
		Dir:        o.dir,
		Env:        o.env,
		BuildFlags: o.buildFlags,
	}, wanted...)
	if err != nil {
		return nil, fmt.Errorf("cannot load %d import paths: %w: %w", len(wanted), err, ErrResolve)
	}

	names := make(map[string]string, len(loaded))

	for _, pkg := range loaded {
		if pkg.Name == "" || pkg.PkgPath == "" || len(pkg.Errors) > 0 {
			continue
		}

		names[pkg.PkgPath] = pkg.Name
	}

	if missing := missingFrom(wanted, names); len(missing) > 0 {
		return names, fmt.Errorf("%s: %w: %w", strings.Join(missing, ", "), ErrUnresolved, ErrResolve)
	}

	return names, nil
}

// distinct returns the paths worth asking about, in the order they were given, without repeats.
func distinct(paths []string) []string {
	wanted := make([]string, 0, len(paths))

	for _, importPath := range paths {
		if importPath == "" || slices.Contains(wanted, importPath) {
			continue
		}

		wanted = append(wanted, importPath)
	}

	return wanted
}

// missingFrom lists the paths that came back without a name.
func missingFrom(wanted []string, names map[string]string) []string {
	var missing []string

	for _, importPath := range wanted {
		if _, ok := names[importPath]; !ok {
			missing = append(missing, importPath)
		}
	}

	return missing
}
