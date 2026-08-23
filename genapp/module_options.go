// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// ModOption configures the go.mod [GoGenApp.InitModule] writes.
type ModOption func(*modOptions)

type modOptions struct {
	modulePath string
	goVersion  string
	toolchain  string
	requires   []requirement
	replace    bool
}

type requirement struct {
	path     string
	version  string
	indirect bool
}

// WithModulePath names the module, as the argument to "go mod init" does.
//
// The path is cleaned and slash-separated, and it is checked the way the go command checks it, so a
// path no module could have is reported here rather than by the first build.
func WithModulePath(pth string) ModOption {
	return func(o *modOptions) {
		o.modulePath = path.Clean(filepath.ToSlash(pth))
	}
}

// WithGoVersion sets the go directive, as in "1.25.0".
//
// It defaults to the version of Go this program was built with, which is what "go mod init" writes.
func WithGoVersion(version string) ModOption {
	return func(o *modOptions) {
		o.goVersion = version
	}
}

// WithToolchain sets the toolchain directive, as in "go1.25.0", or "default" to pin the module to
// whatever toolchain is installed.
//
// The two directives are spelled differently — "go 1.25.0" carries no prefix, "toolchain go1.25.0"
// does — so a bare version is accepted here and written in the form the directive takes.
//
// There is no default: "go mod init" writes no toolchain line, and the go command adds one when it
// needs a toolchain newer than the one installed. Set it to say which toolchain a generated module
// is meant to build with, whatever is on the machine that generated it.
func WithToolchain(name string) ModOption {
	return func(o *modOptions) {
		if name != "" && name != toolchainDefault && !strings.HasPrefix(name, "go") {
			name = "go" + name
		}

		o.toolchain = name
	}
}

// WithRequire adds a require directive, as in ("github.com/go-openapi/strfmt", "v0.24.0").
//
// A generated module knows what its templates import, and saying so here means "go mod tidy" has
// versions to start from rather than resolving every import from scratch. Mark a requirement
// indirect when nothing the module itself holds imports it.
func WithRequire(pth, version string, indirect bool) ModOption {
	return func(o *modOptions) {
		o.requires = append(o.requires, requirement{path: pth, version: version, indirect: indirect})
	}
}

// WithReplaceExisting overwrites a go.mod that is already there.
//
// Without it [GoGenApp.InitModule] leaves an existing file alone and reports [fs.ErrExist], as
// "go mod init" does.
func WithReplaceExisting(replace bool) ModOption {
	return func(o *modOptions) {
		o.replace = replace
	}
}

// toolchainDefault pins a module to the installed toolchain, whatever it is.
const toolchainDefault = "default"

// buildVersion is the language version of the toolchain that built this program.
//
// [runtime.Version] reports things like "go1.25.0", and a development build reports something the go
// directive would reject, so what it returns is matched rather than trusted.
var buildVersion = regexp.MustCompile(`^go(\d+\.\d+(\.\d+)?)`)

// defaultGoVersion returns the go directive to write when a caller names none.
func defaultGoVersion() string {
	const fallback = "1.24"

	if matched := buildVersion.FindStringSubmatch(runtime.Version()); matched != nil {
		return matched[1]
	}

	return fallback
}

func modOptionsWithDefaults(opts []ModOption) (modOptions, error) {
	o := modOptions{goVersion: defaultGoVersion()}

	for _, apply := range opts {
		apply(&o)
	}

	if o.modulePath == "" || o.modulePath == "." {
		return o, fmt.Errorf("a module path is required, see WithModulePath: %w", ErrGenApp)
	}

	if err := module.CheckPath(o.modulePath); err != nil {
		return o, fmt.Errorf("%q is not a module path: %w: %w", o.modulePath, err, ErrGenApp)
	}

	if o.toolchain != "" && !modfile.ToolchainRE.MatchString(o.toolchain) {
		return o, fmt.Errorf(
			"%q is not a toolchain name, want something like go1.25.0 or %q: %w",
			o.toolchain, toolchainDefault, ErrGenApp,
		)
	}

	if !modfile.GoVersionRE.MatchString(o.goVersion) {
		return o, fmt.Errorf("%q is not a go version, want something like 1.25.0: %w", o.goVersion, ErrGenApp)
	}

	// modfile takes a requirement without looking at it, and a version it would not have written
	// reaches the go command as a broken go.mod rather than as an error here.
	for _, required := range o.requires {
		if err := module.Check(required.path, required.version); err != nil {
			return o, fmt.Errorf(
				"cannot require %q at %q: %w: %w", required.path, required.version, err, ErrGenApp,
			)
		}
	}

	return o, nil
}
