// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxTidyOutput bounds how much of a failing command's output goes into the error.
const maxTidyOutput = 4096

// TidyModule runs "go mod tidy" in the output path.
//
// This is the one thing in this package that needs a Go toolchain. Tidying resolves every import
// the generated code makes against the module graph and the checksum database, downloading what it
// must, and reproducing that here would mean reproducing the go command. So it is shelled out, and
// a generator that never calls it never needs go on the machine.
//
// The context bounds the run: cancelling it kills the command, and
// [WithTidyWaitDelay] bounds how long the command may hold the output pipes open after that.
//
// A go.work in a parent directory that does not list the generated module makes the go command
// refuse to work in it, so the command runs with GOWORK off unless [WithWorkspace] says otherwise.
// [WithTidyEnv] sets anything else the command needs, such as GOPROXY or GOPRIVATE.
//
// What the command wrote is reported when it fails; pass [WithTidyOutput] to watch it as it runs.
func (g *GoGenApp) TidyModule(ctx context.Context, opts ...TidyOption) error {
	o, err := tidyOptionsWithDefaults(opts)
	if err != nil {
		return err
	}

	if err := g.checkModulePresent(); err != nil {
		return err
	}

	var captured bytes.Buffer

	cmd := g.tidyCommand(ctx, o, &captured)

	if err := cmd.Run(); err != nil {
		return g.tidyError(o, err, captured.Bytes())
	}

	return nil
}

// checkModulePresent reports a missing go.mod, which tidy needs and does not create.
func (g *GoGenApp) checkModulePresent() error {
	path := filepath.Join(g.outputPath, goModFile)

	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no %q to tidy, see InitModule: %w: %w", path, err, ErrGenApp)
	}

	return nil
}

// tidyCommand assembles the command to run.
func (g *GoGenApp) tidyCommand(ctx context.Context, o tidyOptions, captured *bytes.Buffer) *exec.Cmd {
	args := []string{"mod", "tidy"}

	if o.goVersion != "" {
		args = append(args, "-go="+o.goVersion)
	}

	if o.compat != "" {
		args = append(args, "-compat="+o.compat)
	}

	cmd := exec.CommandContext(ctx, o.goCommand, args...) //nolint:gosec // the caller names the toolchain
	cmd.Dir = g.outputPath
	cmd.WaitDelay = o.waitDelay

	cmd.Env = os.Environ()
	if !o.workspace {
		cmd.Env = append(cmd.Env, "GOWORK=off")
	}

	cmd.Env = append(cmd.Env, o.env...)

	output := io.Writer(captured)
	if o.output != nil {
		output = io.MultiWriter(captured, o.output)
	}

	cmd.Stdout = output
	cmd.Stderr = output

	return cmd
}

// tidyError explains a command that did not run, or ran and failed.
func (g *GoGenApp) tidyError(o tidyOptions, cause error, output []byte) error {
	var missing *exec.Error
	if errors.As(cause, &missing) {
		return fmt.Errorf(
			"%q is needed to tidy %q and was not found, a Go toolchain is required for this step alone: %w: %w",
			o.goCommand, g.outputPath, cause, ErrGenApp,
		)
	}

	said := strings.TrimSpace(string(output))
	if len(said) > maxTidyOutput {
		said = said[:maxTidyOutput] + "..."
	}

	if said == "" {
		return fmt.Errorf("go mod tidy failed in %q: %w: %w", g.outputPath, cause, ErrGenApp)
	}

	return fmt.Errorf("go mod tidy failed in %q: %w: %s: %w", g.outputPath, cause, said, ErrGenApp)
}
