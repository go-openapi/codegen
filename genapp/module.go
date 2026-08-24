// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// goModFile is "go.mod". The go command reads a module definition from a file of that name.
const goModFile = "go.mod"

// InitModule writes a go.mod in the output path, as "go mod init" would.
//
// It writes the module path, the go directive, the toolchain directive when [WithToolchain] asks
// for one, and whatever [WithRequire] declared, formatted the way the go command formats a go.mod. It runs no command and reads no environment, so a generator can
// lay down a buildable module on a machine with no Go toolchain installed, and the file it produces
// does not depend on the one that is installed.
//
// What it does not do is resolve anything. "go mod init" fills nothing in either; the versions a
// module ends up with come from "go mod tidy", which needs the toolchain and the network. Declare
// what the templates import with [WithRequire] and tidy has somewhere to start.
//
// A go.mod already in the output path is left alone and reported as [fs.ErrExist], unless
// [WithReplaceExisting] says otherwise.
func (g *GoGenApp) InitModule(opts ...ModOption) error {
	o, err := applyModWithDefaults(opts)
	if err != nil {
		return err
	}

	content, err := buildModFile(o)
	if err != nil {
		return err
	}

	root, down, err := g.openRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	written := destination{
		root:      root,
		rel:       filepath.Join(down, goModFile),
		target:    filepath.Join(g.outputPath, goModFile),
		displayed: filepath.Join(g.outputPath, goModFile),
	}

	if err := checkModuleAbsent(written, o); err != nil {
		return err
	}

	if down != "." {
		if err := root.MkdirAll(down, dirPerm); err != nil {
			return fmt.Errorf("cannot create the module directory %q: %w: %w", g.outputPath, err, ErrGenApp)
		}
	}

	return writeModFile(written, content)
}

// writeModFile writes a go.mod through a temporary file, then renames.
//
// It writes through a temporary file and renames, as [GoGenApp.RenderFile] does, so a go.mod
// appears whole or not at all, and a symbolic link standing at the path is removed rather than
// written through.
func writeModFile(d destination, content []byte) (err error) {
	temporary, temporaryRel, err := createTemp(d.root, d.dir(), goModFile)
	if err != nil {
		return fmt.Errorf("cannot create a temporary file for %q: %w: %w", d.target, err, ErrGenApp)
	}

	defer func() {
		if err == nil {
			return
		}

		_ = temporary.Close()
		_ = d.root.Remove(temporaryRel)
	}()

	if _, err = temporary.Write(content); err != nil {
		return fmt.Errorf("cannot write %q: %w: %w", d.target, err, ErrGenApp)
	}

	if err = temporary.Close(); err != nil {
		return fmt.Errorf("cannot close %q: %w: %w", d.target, err, ErrGenApp)
	}

	return commit(d, temporaryRel)
}

// checkModuleAbsent reports an existing go.mod, unless the caller asked to replace it.
//
// It reads the entry with [os.Root.Lstat] rather than Stat, so a symbolic link at the path counts
// as something already there. Stat follows the link and calls a dangling one absent, and InitModule
// would then write over a link the caller never put there.
func checkModuleAbsent(d destination, o modOptions) error {
	if o.replace {
		return nil
	}

	switch _, err := d.root.Lstat(d.rel); {
	case err == nil:
		return fmt.Errorf("%q exists, see WithReplaceExisting: %w: %w", d.target, fs.ErrExist, ErrGenApp)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("cannot read %q: %w: %w", d.target, err, ErrGenApp)
	}
}

// buildModFile renders the go.mod content.
func buildModFile(o modOptions) ([]byte, error) {
	file := new(modfile.File)

	if err := file.AddModuleStmt(o.modulePath); err != nil {
		return nil, fmt.Errorf("cannot declare module %q: %w: %w", o.modulePath, err, ErrGenApp)
	}

	if err := file.AddGoStmt(o.goVersion); err != nil {
		return nil, fmt.Errorf("cannot declare go %q: %w: %w", o.goVersion, err, ErrGenApp)
	}

	if o.toolchain != "" {
		if err := file.AddToolchainStmt(o.toolchain); err != nil {
			return nil, fmt.Errorf("cannot declare toolchain %q: %w: %w", o.toolchain, err, ErrGenApp)
		}
	}

	for _, required := range o.requires {
		if err := file.AddRequire(required.path, required.version); err != nil {
			return nil, fmt.Errorf(
				"cannot require %q %q: %w: %w", required.path, required.version, err, ErrGenApp,
			)
		}
	}

	markIndirect(file, o.requires)

	file.Cleanup()

	return modfile.Format(file.Syntax), nil
}

// markIndirect puts the "// indirect" comment on the requirements the caller declared as such.
//
// modfile carries the flag on the parsed rule rather than taking it when a requirement is added, so
// the requirements are matched back by path.
func markIndirect(file *modfile.File, declared []requirement) {
	indirect := make(map[string]bool, len(declared))

	for _, required := range declared {
		if required.indirect {
			indirect[required.path] = true
		}
	}

	for _, required := range file.Require {
		if indirect[required.Mod.Path] {
			required.Indirect = true
			file.SetRequire(file.Require)

			break
		}
	}
}
