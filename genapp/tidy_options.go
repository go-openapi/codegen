// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"fmt"
	"io"
	"time"

	"golang.org/x/mod/modfile"
)

// TidyOption configures [GoGenApp.TidyModule].
type TidyOption func(*tidyOptions)

type tidyOptions struct {
	goCommand string
	goVersion string
	compat    string
	output    io.Writer
	env       []string
	workspace bool
	waitDelay time.Duration
}

// WithGoCommand names the go binary to run. It defaults to "go", found on PATH.
//
// Pass an absolute path to run a toolchain the PATH does not point at.
func WithGoCommand(command string) TidyOption {
	return func(o *tidyOptions) {
		if command != "" {
			o.goCommand = command
		}
	}
}

// WithTidyGoVersion passes -go to the command, as in "1.25.0", which sets the go directive while
// tidying.
func WithTidyGoVersion(version string) TidyOption {
	return func(o *tidyOptions) {
		o.goVersion = version
	}
}

// WithTidyCompat passes -compat to the command, as in "1.24", which keeps the checksums an older
// go needs to load the module.
func WithTidyCompat(version string) TidyOption {
	return func(o *tidyOptions) {
		o.compat = version
	}
}

// WithTidyOutput copies what the command writes to w, as it writes it.
//
// The output is kept either way and reported when the command fails. Pass a writer to watch a tidy
// that takes a while, since it downloads what the module requires.
func WithTidyOutput(w io.Writer) TidyOption {
	return func(o *tidyOptions) {
		o.output = w
	}
}

// WithTidyEnv sets environment variables for the command, as "GOPROXY=off" or "GOPRIVATE=example.com".
//
// They are added to the environment this process runs in, so a later setting replaces an earlier
// one. Tidying reaches the module proxy and the checksum database, and a generated module often
// wants different settings for those than the generator itself.
func WithTidyEnv(vars ...string) TidyOption {
	return func(o *tidyOptions) {
		o.env = append(o.env, vars...)
	}
}

// WithWorkspace lets the go workspace apply to the command.
//
// A generated module is usually a module of its own, and a go.work in a parent directory that does
// not list it makes the go command refuse to work in it, so [GoGenApp.TidyModule] runs with GOWORK
// off. Turn this on for a module the surrounding workspace is meant to cover.
func WithWorkspace(enabled bool) TidyOption {
	return func(o *tidyOptions) {
		o.workspace = enabled
	}
}

// WithTidyWaitDelay bounds how long the command may hold the output pipes open after its context is
// done, before it is killed. It defaults to five seconds.
func WithTidyWaitDelay(delay time.Duration) TidyOption {
	return func(o *tidyOptions) {
		o.waitDelay = delay
	}
}

func tidyOptionsWithDefaults(opts []TidyOption) (tidyOptions, error) {
	const defaultWaitDelay = 5 * time.Second

	o := tidyOptions{goCommand: "go", waitDelay: defaultWaitDelay}

	for _, apply := range opts {
		apply(&o)
	}

	for _, version := range [...]struct{ flag, value string }{
		{flag: "-go", value: o.goVersion},
		{flag: "-compat", value: o.compat},
	} {
		if version.value == "" {
			continue
		}

		if !modfile.GoVersionRE.MatchString(version.value) {
			return o, fmt.Errorf(
				"%s=%q is not a go version, want something like 1.25.0: %w", version.flag, version.value, ErrGenApp,
			)
		}
	}

	return o, nil
}
