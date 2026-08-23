// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// ErrNoModule is returned when nothing above the output path declares a module, and the path is
// under no GOPATH either.
//
// Generated code lands where the caller says, and that may be outside any module: a fresh directory
// in /tmp, a tree beside a repository rather than inside it. [GoGenApp.ModuleRequired] asks the same
// question without treating the answer as a failure.
const ErrNoModule Error = "no module and no GOPATH covers the output path"

// EnclosingModule finds the module the output path belongs to.
//
// It returns the module path as go.mod declares it, and the directory that go.mod sits in. The
// search walks up from the output path and stops at the first go.mod, so a module nested inside
// another wins, which is how the go command reads the same tree.
//
// It reads go.mod files and nothing else: no go command, no environment, no GOPATH. A directory
// that does not exist yet is not an obstacle, since only its name takes part in the answer.
//
// It returns [ErrNoModule] when the walk reaches the root of the file system without finding one.
func (g *GoGenApp) EnclosingModule() (modulePath, moduleDir string, err error) {
	start, err := filepath.Abs(g.outputPath)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve the output path %q: %w: %w", g.outputPath, err, ErrGenApp)
	}

	for dir := start; ; {
		declared, found, err := readModulePath(filepath.Join(dir, goModFile))
		if err != nil {
			return "", "", err
		}

		if found {
			return declared, dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("%w above %q: %w", ErrNoModule, start, ErrGenApp)
		}

		dir = parent
	}
}

// PackagePath returns the import path of the output path.
//
// It is the module path of the enclosing module followed by the way down to the output path, so a
// generator can write the import statements that reach the code it is about to produce:
//
//	module example.com/petstore  declared in /src/petstore/go.mod
//	output path                  /src/petstore/gen/models
//	PackagePath                  example.com/petstore/gen/models
//
// A caller generating into a tree that has no module yet calls [GoGenApp.InitModule] first, and
// PackagePath then returns what that declared.
//
// Failing a module, a path under GOPATH/src answers too: the way down from src is the import path
// such a tree has when GO111MODULE is off. Modules are looked for first, since they win wherever
// both apply.
//
// It returns [ErrNoModule] when neither names the path. See [GoGenApp.ModuleRequired].
func (g *GoGenApp) PackagePath() (string, error) {
	start, err := filepath.Abs(g.outputPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the output path %q: %w: %w", g.outputPath, err, ErrGenApp)
	}

	modulePath, moduleDir, err := g.EnclosingModule()
	if err != nil {
		if !errors.Is(err, ErrNoModule) {
			return "", err
		}

		within, found := gopathPackage(start)
		if !found {
			return "", err
		}

		return checkedImportPath(within)
	}

	within, err := filepath.Rel(moduleDir, start)
	if err != nil {
		return "", fmt.Errorf("%q is not under %q: %w: %w", start, moduleDir, err, ErrGenApp)
	}

	importPath := modulePath
	if within != "." {
		importPath = path.Join(modulePath, filepath.ToSlash(within))
	}

	return checkedImportPath(importPath)
}

// checkedImportPath reports a path the go command would not import.
func checkedImportPath(importPath string) (string, error) {
	if err := module.CheckImportPath(importPath); err != nil {
		return "", fmt.Errorf(
			"%q does not name a package, the output path has a directory go would not import: %w: %w",
			importPath, err, ErrGenApp,
		)
	}

	return importPath, nil
}

// ModuleRequired reports whether the output path needs a go.mod of its own.
//
// It is true when nothing above the output path declares a module, which is when generated code
// there could not be built until [GoGenApp.InitModule] gives it one.
//
// It is false when a module already covers the path. That module may be the one the generator is
// running from, so a caller generating into its own repository gets false and needs no go.mod.
//
// GOPATH does not enter into it, though [GoGenApp.PackagePath] falls back to it. A tree under
// GOPATH/src builds only with GO111MODULE off, and go has defaulted the other way since 1.16, so
// such a tree does need a go.mod for the go a caller will be running.
func (g *GoGenApp) ModuleRequired() (bool, error) {
	_, _, err := g.EnclosingModule()

	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, ErrNoModule):
		return true, nil
	default:
		return false, err
	}
}

// readModulePath reads the module path a go.mod declares.
//
// It reports found as false when there is no file there, and an error when the file is unreadable
// or declares no module, since a go.mod without a module line stops the go command too.
func readModulePath(path string) (modulePath string, found bool, err error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("cannot read %q: %w: %w", path, err, ErrGenApp)
	}

	declared := modfile.ModulePath(content)
	if strings.TrimSpace(declared) == "" {
		return "", false, fmt.Errorf("%q declares no module: %w", path, ErrGenApp)
	}

	return declared, true, nil
}
