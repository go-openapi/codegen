// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

import (
	"go/ast"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
)

// cgoImport names the pseudo-package C, which carries a cgo preamble. Nothing may touch it.
const cgoImport = "C"

// prune deletes the imports the file does not use and can be shown not to use.
//
// It never adds one, and it deletes only an import whose name it knows: an alias states the name, the
// generated table states it for the standard library, and [WithResolvedImports] lets the caller state
// it for anything else. A bare third-party import is a guess, and a guess keeps an import rather than
// delete it — see [binding.inDoubt]. [WithForceImportsPruning] turns the guesses into decisions.
//
// It returns the bindings, so the caller can report on what was pruned and what was left in doubt.
func prune(fset *token.FileSet, file *ast.File, o options) ([]binding, map[string]bool) {
	used := usedQualifiers(file)
	bindings := describeImports(file, o.resolved)

	type deletion struct{ name, path string }
	var unused []deletion

	for i := range bindings {
		described := &bindings[i]

		if !described.prunable(o.forcePruning) || described.isUsed(used) {
			continue
		}

		unused = append(unused, deletion{name: described.alias, path: described.path})
		described.pruned = true
	}

	for _, spec := range unused {
		astutil.DeleteNamedImport(fset, file, spec.name, spec.path)
	}

	return bindings, used
}

// usedQualifiers collects every identifier the file uses to qualify a selector.
//
// An identifier the parser resolved to a declaration carries a non-nil Obj, which separates the
// package fmt in fmt.Println from a local variable named fmt. Since one shadowed use does not hide
// another, a package used anywhere in the file lands in the set.
//
// A file parsed in [fastMode] carries no Obj at all, so every selector base lands in the set. That
// is the same answer whenever no declaration in the file shares a name with a selector's qualifier.
// [needsResolution] checks that before the cheap parse is trusted.
func usedQualifiers(file *ast.File) map[string]bool {
	used := make(map[string]bool)

	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if ident, ok := selector.X.(*ast.Ident); ok && ident.Obj == nil {
			used[ident.Name] = true
		}

		return true
	})

	return used
}

// needsResolution reports whether telling a package qualifier from a shadowed name needs the
// parser's scopes.
//
// Only the names an import could declare are worth asking about, and there are a handful of those.
// It needs the scopes when the file writes one of them somewhere that is not a selector base — as a
// parameter, a range variable, the left of a :=, any declaration at all — because only scopes then
// say whether bytes.Buffer means the package or the local. When no imported name is written that
// way, an unresolved parse already separates them: a selector base the file never declares is a
// package, or a name a sibling file declares, and the parser resolves neither.
//
// Asking about every selector base instead would be useless. A method receiver is a selector base:
// m.ID is the same shape as fmt.Println, so m would collide with itself and no file would ever take
// the cheap parse.
func needsResolution(file *ast.File, resolved map[string]string) bool {
	names := importedNames(file, resolved)
	if len(names) == 0 {
		return false
	}

	shadowed := false

	var walk func(ast.Node) bool
	walk = func(node ast.Node) bool {
		if shadowed {
			return false
		}

		switch typed := node.(type) {
		case *ast.ImportSpec:
			return false // the alias declares the name; nothing here shadows it

		case *ast.SelectorExpr:
			if _, ok := typed.X.(*ast.Ident); ok {
				return false // a qualifier or a value with a field, and neither declares a name
			}

			ast.Inspect(typed.X, walk) // m.Count.Value: walk the left, skip the field

			return false

		case *ast.Ident:
			if _, imported := names[typed.Name]; imported {
				shadowed = true
			}
		}

		return true
	}

	ast.Inspect(file, walk)

	return shadowed
}

// importedNames returns every name the file's imports could declare.
//
// An alias declares its name outright, and [WithResolvedImports] states the names a caller supplied.
// Without either the package name is guessed, and [importedPackageNames] returns every guess, so a
// name that could belong to an import is in the set.
func importedNames(file *ast.File, resolved map[string]string) map[string]struct{} {
	names := make(map[string]struct{}, len(file.Imports))

	for _, spec := range file.Imports {
		if spec.Name != nil {
			if spec.Name.Name != "_" && spec.Name.Name != "." {
				names[spec.Name.Name] = struct{}{}
			}

			continue
		}

		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		if name, ok := resolved[importPath]; ok {
			names[name] = struct{}{}

			continue
		}

		for _, name := range importedPackageNames(importPath) {
			names[name] = struct{}{}
		}
	}

	return names
}
