// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

import (
	"go/ast"
	"strconv"

	"github.com/go-openapi/codegen/formatting/internal/std"
)

// importKind separates the imports that bind a qualifier from the ones that do not.
type importKind int

const (
	kindNamed importKind = iota // an ordinary import, aliased or bare
	kindBlank                   // _ "embed": runs an init and binds nothing
	kindDot                     // . "strings": spills names into the file scope
	kindCgo                     // "C": carries a preamble, and nothing may touch it
)

// binding holds one import, the name it declares, and the evidence for that name.
//
// certain is set by the three sources that state the name outright: an alias written in the source,
// the generated standard library table, and the map a caller passes to [WithResolvedImports].
// Everything else fills candidates and leaves certain false, and [prune] may then keep the import but
// never delete it.
type binding struct {
	spec       *ast.ImportSpec
	path       string
	alias      string // as written, "" for a bare import
	kind       importKind
	name       string // the qualifier the import binds, when it is known
	proven     string // the name the package declares, from the table or the caller's map, "" otherwise
	certain    bool
	candidates []string // the guesses, when the name is not known
	pruned     bool     // set by prune when the import was deleted
}

// describeImports reads every import in the file and says what is known about the name it binds.
func describeImports(file *ast.File, resolved map[string]string) []binding {
	bindings := make([]binding, 0, len(file.Imports))

	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		bindings = append(bindings, describeImport(spec, importPath, resolved))
	}

	return bindings
}

func describeImport(spec *ast.ImportSpec, importPath string, resolved map[string]string) binding {
	described := binding{spec: spec, path: importPath, kind: kindNamed}

	if importPath == cgoImport {
		described.kind = kindCgo

		return described
	}

	// what the package declares, whether or not an alias renames it here
	described.proven = provenName(importPath, resolved)

	if spec.Name != nil {
		described.alias = spec.Name.Name

		switch described.alias {
		case "_":
			described.kind = kindBlank
		case ".":
			described.kind = kindDot
		default:
			// the alias names the package here, whatever the package calls itself
			described.name = described.alias
			described.certain = true
		}

		return described
	}

	if described.proven != "" {
		described.name = described.proven
		described.certain = true

		return described
	}

	described.candidates = importedPackageNames(importPath)

	return described
}

// provenName returns the name the package at importPath declares, or "" when nothing states it.
//
// The caller's map is asked first, so a name supplied through [WithResolvedImports] settles a path
// the generated table also holds. The table answers for the rest of the standard library.
func provenName(importPath string, resolved map[string]string) string {
	if name, ok := resolved[importPath]; ok {
		return name
	}

	if name, ok := std.Name(importPath); ok {
		return name
	}

	return ""
}

// isUsed reports whether the file writes the name this import binds.
//
// A certain binding answers exactly. A guess answers "yes" as soon as one candidate appears, which
// keeps the import; it never answers "no" with authority, so [prune] asks [inDoubt] before deleting.
func (b binding) isUsed(used map[string]bool) bool {
	if b.certain {
		return used[b.name]
	}

	for _, candidate := range b.candidates {
		if used[candidate] {
			return true
		}
	}

	return false
}

// prunable reports whether this import may be deleted when the file does not use it.
//
// Without forced pruning only a certain binding qualifies. Under it the caller promises every bare
// import declares the name [ImportedPackageName] gives, so a guess counts too — and the check still
// runs over every candidate, so "k8s.io/api/apps/v1" survives in a file writing v1.Pod.
func (b binding) prunable(forced bool) bool {
	if b.kind != kindNamed {
		return false // a blank, dot or cgo import binds no qualifier, so nothing marks it unused
	}

	return b.certain || forced
}

// effectiveName returns the qualifier this import binds in this file.
//
// A certain binding knows it outright. A guess settles on the one candidate the file writes: an
// import offering both v1 and apps in a file writing v1.Pod binds v1. Several candidates written at
// once leave the answer open, and so does none, and both return "".
func (b binding) effectiveName(used map[string]bool) string {
	if b.kind != kindNamed {
		return b.alias // "_" or "." as written, and "" for cgo
	}

	if b.certain {
		return b.name
	}

	settled := ""

	for _, candidate := range b.candidates {
		if !used[candidate] {
			continue
		}

		if settled != "" {
			return ""
		}

		settled = candidate
	}

	return settled
}

// status says what became of this import, before any collision is taken into account.
func (b binding) status(used map[string]bool) ImportStatus {
	switch b.kind {
	case kindCgo:
		return ImportCgo
	case kindBlank:
		return ImportBlank
	case kindDot:
		return ImportDot
	case kindNamed:
	}

	if b.pruned {
		return ImportPruned
	}

	if b.isUsed(used) {
		return ImportUsed
	}

	return ImportInDoubt
}
