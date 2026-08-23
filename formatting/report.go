// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// ImportStatus says what became of one import.
type ImportStatus int

const (
	// ImportUsed marks an import the file writes as a qualifier. It stays.
	ImportUsed ImportStatus = iota

	// ImportPruned marks an import [Format] deleted, because its name is known and nothing used it.
	ImportPruned

	// ImportInDoubt marks a bare import whose package [Format] cannot name, so it cannot say whether
	// the file uses it. It stays. Resolve it with [WithResolvedImports], or accept the guess with
	// [WithForceImportsPruning].
	ImportInDoubt

	// ImportCollision marks an import whose guessed name is claimed by another import as well. It
	// stays, and neither of the two is pruned. A collision between names [Format] knows is an error
	// rather than a status: see [ErrInconsistentImports].
	ImportCollision

	// ImportBlank marks a _ import. It runs an init, binds no qualifier, and is never pruned.
	ImportBlank

	// ImportDot marks a . import. Its names land in the file scope and no qualifier appears, so
	// nothing about it can be checked. It is never pruned and always in doubt.
	ImportDot

	// ImportCgo marks the pseudo-package C, which carries a cgo preamble. Nothing may touch it.
	ImportCgo
)

func (s ImportStatus) String() string {
	switch s {
	case ImportUsed:
		return "used"
	case ImportPruned:
		return "pruned"
	case ImportInDoubt:
		return "in doubt"
	case ImportCollision:
		return "collision"
	case ImportBlank:
		return "blank"
	case ImportDot:
		return "dot"
	case ImportCgo:
		return "cgo"
	default:
		return "unknown"
	}
}

// ImportRecord is what [Format] made of one import.
type ImportRecord struct {
	// Path is the import path, unquoted.
	Path string

	// Alias is the name written before the path, empty for a bare import. "_" and "." appear here as
	// they were written.
	Alias string

	// Name is the qualifier the import binds, empty when [Format] could not tell. Under
	// [ImportCollision], the records that clash share a Name.
	Name string

	// Certain reports whether Name was stated rather than guessed: written as an alias, held in the
	// standard library table, or supplied through [WithResolvedImports].
	Certain bool

	Status ImportStatus
}

func (r ImportRecord) String() string {
	name := r.Name
	if name == "" {
		name = "?"
	}

	return fmt.Sprintf("%s (%s) %s", r.Path, name, r.Status)
}

// ImportsReport accounts for every import [Format] read.
//
// [Format] returns one whenever it parsed the source, including when it then failed, so a caller can
// see the imports behind an [ErrInconsistentImports].
//
// The report is the input to the resolving loop: format once, ask [ImportsReport.HasImportsInDoubt],
// resolve [ImportsReport.PathsInDoubt] with
// github.com/go-openapi/codegen/formatting/resolve, and format again with [WithResolvedImports] until
// nothing is left in doubt.
type ImportsReport struct {
	records []ImportRecord
}

// Imports returns every import, ordered by path.
func (r *ImportsReport) Imports() []ImportRecord {
	if r == nil {
		return nil
	}

	return slices.Clone(r.records)
}

// Used returns the imports the file writes as a qualifier.
func (r *ImportsReport) Used() []ImportRecord { return r.withStatus(ImportUsed) }

// Pruned returns the imports [Format] deleted.
func (r *ImportsReport) Pruned() []ImportRecord { return r.withStatus(ImportPruned) }

// InDoubt returns the imports [Format] kept because it could not name them.
//
// That covers [ImportInDoubt], [ImportCollision] and [ImportDot]: a name guessed and unmatched, a name
// guessed and claimed twice, and a dot import, which no qualifier ever reveals.
func (r *ImportsReport) InDoubt() []ImportRecord {
	if r == nil {
		return nil
	}

	var doubtful []ImportRecord

	for _, record := range r.records {
		switch record.Status {
		case ImportInDoubt, ImportCollision, ImportDot:
			doubtful = append(doubtful, record)
		case ImportUsed, ImportPruned, ImportBlank, ImportCgo:
		}
	}

	return doubtful
}

// PathsInDoubt returns the import paths of [ImportsReport.InDoubt], ready for a resolver.
func (r *ImportsReport) PathsInDoubt() []string {
	doubtful := r.InDoubt()

	paths := make([]string, 0, len(doubtful))
	for _, record := range doubtful {
		paths = append(paths, record.Path)
	}

	return paths
}

// HasImportsInDoubt reports whether any import was kept because [Format] could not name it.
//
// A false answer means every import was decided: pruning was exact, and the output has no import the
// file does not use.
func (r *ImportsReport) HasImportsInDoubt() bool { return len(r.InDoubt()) > 0 }

// String summarises the report, one import per line.
func (r *ImportsReport) String() string {
	if r == nil || len(r.records) == 0 {
		return "no imports"
	}

	lines := make([]string, 0, len(r.records))
	for _, record := range r.records {
		lines = append(lines, record.String())
	}

	return strings.Join(lines, "\n")
}

func (r *ImportsReport) withStatus(status ImportStatus) []ImportRecord {
	if r == nil {
		return nil
	}

	var matching []ImportRecord

	for _, record := range r.records {
		if record.Status == status {
			matching = append(matching, record)
		}
	}

	return matching
}

// newImportsReport turns the bindings into the report, once pruning and sorting are done.
//
// A binding whose spec left the tree without being pruned was an exact duplicate that [sortImports]
// collapsed, and the spec it collapsed into carries the same path and name, so dropping it here loses
// nothing.
func newImportsReport(bindings []binding, file *ast.File, used map[string]bool) *ImportsReport {
	surviving := survivingSpecs(file)
	report := &ImportsReport{records: make([]ImportRecord, 0, len(bindings))}

	for _, described := range bindings {
		if !described.pruned && !surviving[described.spec] {
			continue
		}

		report.records = append(report.records, ImportRecord{
			Path:    described.path,
			Alias:   described.alias,
			Name:    described.effectiveName(used),
			Certain: described.certain,
			Status:  described.status(used),
		})
	}

	slices.SortFunc(report.records, func(a, b ImportRecord) int { return strings.Compare(a.Path, b.Path) })

	return report
}

func survivingSpecs(file *ast.File) map[*ast.ImportSpec]bool {
	surviving := make(map[*ast.ImportSpec]bool, len(file.Imports))

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			break // imports come first, so the first other declaration ends the search
		}

		for _, spec := range gen.Specs {
			surviving[spec.(*ast.ImportSpec)] = true
		}
	}

	return surviving
}
