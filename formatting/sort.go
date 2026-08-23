// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0
//
// The spec sorting and its position bookkeeping follow
// golang.org/x/tools/internal/imports/sortimports.go, itself a copy of go/ast/import.go, which
// carries:
//
//	Copyright 2013 The Go Authors. All rights reserved.
//	Use of this source code is governed by a BSD-style license.

package formatting

import (
	"go/ast"
	"go/token"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// sortImports orders the imports of every import block and removes the duplicates it safely can.
//
// The whole block is one run: a blank line the source left between two imports is not a boundary
// here, so "bytes" written in two groups is one import in the output. gofmt and goimports sort each
// blank-line-separated run on its own and leave that duplicate behind, which the compiler then
// rejects with "bytes redeclared in this block". [groupBreaks] and [spacer] put the blank lines
// back, from the prefixes [WithImportGroups] was given.
//
// It mutates the file and the token.File: a spec keeps the position of whichever spec used to sit
// where it lands, so the printer lays the block out on consecutive lines.
func sortImports(tokFile *token.File, file *ast.File, groups []string) {
	for i, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			break // imports come first, so the first other declaration ends the search
		}

		if len(gen.Specs) == 0 {
			file.Decls = slices.Delete(file.Decls, i, i+1)

			continue
		}

		if !gen.Lparen.IsValid() {
			continue // a single import needs no sorting
		}

		gen.Specs = sortSpecs(tokFile, file, gen.Specs, groups)
		closeGap(tokFile, gen)
	}
}

// closeGap removes the blank line a dedup may have left before the closing parenthesis.
func closeGap(tokFile *token.File, gen *ast.GenDecl) {
	if len(gen.Specs) == 0 {
		return
	}

	last := gen.Specs[len(gen.Specs)-1]
	lastLine := tokFile.PositionFor(last.Pos(), false).Line

	if rparen := tokFile.PositionFor(gen.Rparen, false).Line; rparen > lastLine+1 {
		tokFile.MergeLine(rparen - 1)
	}
}

// mergeImports moves every import declaration into the first one.
//
// A cgo preamble is attached to the import that carries it, so a declaration importing "C" is left
// where it is.
func mergeImports(file *ast.File) {
	if len(file.Decls) <= 1 {
		return
	}

	var first *ast.GenDecl

	for i := 0; i < len(file.Decls); i++ {
		gen, ok := file.Decls[i].(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT || declaresCgo(gen) {
			continue
		}

		if first == nil {
			first = gen

			continue
		}

		first.Lparen = first.Pos() // more than one import, so the block needs parentheses

		for _, spec := range gen.Specs {
			updateBasicLitPos(spec.(*ast.ImportSpec).Path, first.Pos())
			first.Specs = append(first.Specs, spec)
		}

		file.Decls = slices.Delete(file.Decls, i, i+1)
		i--
	}
}

func declaresCgo(gen *ast.GenDecl) bool {
	for _, spec := range gen.Specs {
		if specPath(spec) == cgoImport {
			return true
		}
	}

	return false
}

// importGroup reports which group an import path belongs to.
//
// Group 0 is the standard library, group len(groups)+1 is everything the prefixes do not claim, and
// a prefix claims the group at its own index plus one. An import belongs to the first prefix it
// starts with, so a caller passing overlapping prefixes gets the more specific one by passing it
// first.
func importGroup(groups []string, importPath string) int {
	if !isThirdParty(importPath) {
		return 0
	}

	for i, prefix := range groups {
		if strings.HasPrefix(importPath, prefix) || strings.TrimSuffix(prefix, "/") == importPath {
			return i + 1
		}
	}

	return len(groups) + 1
}

// isThirdParty reports whether an import path names something outside the standard library.
//
// The standard library owns every path whose first element holds no dot, which is the same test
// gofmt and goimports apply.
func isThirdParty(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")

	return strings.Contains(first, ".")
}

// groupBreaks lists the import paths that open a group, past the first.
//
// The printer lays the sorted block out on consecutive lines, so these are the paths a blank line
// has to precede. [spacer] inserts them while the output is written.
func groupBreaks(fset *token.FileSet, file *ast.File, groups []string) []string {
	var breaks []string

	for _, block := range astutil.Imports(fset, file) {
		previous := -1

		for _, spec := range block {
			importPath := specPath(spec)
			group := importGroup(groups, importPath)

			if previous != -1 && group != previous {
				breaks = append(breaks, importPath)
			}

			previous = group
		}
	}

	return breaks
}

// sortSpecs sorts an import block and reassigns positions so it prints on consecutive lines.
func sortSpecs(tokFile *token.File, file *ast.File, specs []ast.Spec, groups []string) []ast.Spec {
	if len(specs) <= 1 {
		return specs // a lone import is sorted, and has nothing to collapse against
	}

	positions := make([]posSpan, len(specs))
	for i, spec := range specs {
		positions[i] = posSpan{Start: spec.Pos(), End: spec.End()}
	}

	comments := commentsInRun(tokFile, file, positions)
	attached := attachComments(comments, specs, positions)

	sort.Sort(byImportSpec{groups: groups, specs: specs})
	specs = dedup(tokFile, specs)

	replaceSpecPositions(specs, positions, attached)
	sort.Sort(byCommentPos(comments))
	closeRunGaps(tokFile, specs)

	return specs
}

type posSpan struct {
	Start token.Pos
	End   token.Pos
}

// commentsInRun returns the comment groups written inside the span the specs cover.
func commentsInRun(tokFile *token.File, file *ast.File, positions []posSpan) []*ast.CommentGroup {
	lastLine := tokFile.Line(positions[len(positions)-1].End)
	start, end := len(file.Comments), len(file.Comments)

	for i, group := range file.Comments {
		if group.Pos() < positions[0].Start {
			continue
		}

		if i < start {
			start = i
		}

		if tokFile.Line(group.End()) > lastLine {
			end = i

			break
		}
	}

	return file.Comments[start:end]
}

// attachComments assigns each comment group to the spec it follows.
func attachComments(
	comments []*ast.CommentGroup,
	specs []ast.Spec,
	positions []posSpan,
) map[*ast.ImportSpec][]*ast.CommentGroup {
	attached := make(map[*ast.ImportSpec][]*ast.CommentGroup, len(specs))
	current := 0

	for _, group := range comments {
		for current+1 < len(specs) && positions[current+1].Start <= group.Pos() {
			current++
		}

		spec := specs[current].(*ast.ImportSpec)
		attached[spec] = append(attached[spec], group)
	}

	return attached
}

// dedup drops a spec that repeats the one before it, now that sorting has made them adjacent.
func dedup(tokFile *token.File, specs []ast.Spec) []ast.Spec {
	deduped := specs[:0]

	for i, spec := range specs {
		if i == len(specs)-1 || !collapses(spec, specs[i+1]) {
			deduped = append(deduped, spec)

			continue
		}

		tokFile.MergeLine(tokFile.Line(spec.Pos()))
	}

	return deduped
}

// collapses reports whether previous may be dropped in favour of next, losing nothing.
func collapses(previous, next ast.Spec) bool {
	if specPath(next) != specPath(previous) || specName(next) != specName(previous) {
		return false
	}

	return previous.(*ast.ImportSpec).Comment == nil
}

// replaceSpecPositions gives the sorted specs the positions the block occupied before sorting.
func replaceSpecPositions(
	specs []ast.Spec,
	positions []posSpan,
	attached map[*ast.ImportSpec][]*ast.CommentGroup,
) {
	for i, s := range specs {
		spec := s.(*ast.ImportSpec)

		if spec.Name != nil {
			spec.Name.NamePos = positions[i].Start
		}

		updateBasicLitPos(spec.Path, positions[i].Start)
		spec.EndPos = positions[i].End

		next := positions[i].End
		for _, group := range attached[spec] {
			for _, comment := range group.List {
				comment.Slash = positions[i].End
				next = comment.End()
			}
		}

		if i < len(specs)-1 {
			positions[i+1].Start = next
			positions[i+1].End = next
		}
	}
}

// closeRunGaps merges away the blank lines, both the ones the source wrote between its own groups
// and the ones moving the comments opened.
func closeRunGaps(tokFile *token.File, specs []ast.Spec) {
	firstLine := tokFile.Line(specs[0].Pos())

	for _, spec := range specs[1:] {
		for line := tokFile.Line(spec.Pos()) - 1; line >= firstLine; line-- {
			// MergeLine panics outside the line range, and a comment can put a spec there.
			// golang/go#50329
			if line <= 0 || line >= tokFile.LineCount() {
				break
			}

			tokFile.MergeLine(line)
		}
	}
}

// updateBasicLitPos moves a literal, keeping its end in step with its start.
//
// ast.BasicLit.ValueEnd arrived in go1.26 and the module still builds on go1.25, so the field is
// reached by reflection. Assign it directly once the go directive moves.
func updateBasicLitPos(lit *ast.BasicLit, pos token.Pos) {
	length := lit.End() - lit.Pos()
	lit.ValuePos = pos

	if end := reflect.ValueOf(lit).Elem().FieldByName("ValueEnd"); end.IsValid() && end.Int() != 0 {
		end.SetInt(int64(pos + length))
	}
}

func specPath(spec ast.Spec) string {
	unquoted, err := strconv.Unquote(spec.(*ast.ImportSpec).Path.Value)
	if err != nil {
		return ""
	}

	return unquoted
}

func specName(spec ast.Spec) string {
	if name := spec.(*ast.ImportSpec).Name; name != nil {
		return name.Name
	}

	return ""
}

func specComment(spec ast.Spec) string {
	if comment := spec.(*ast.ImportSpec).Comment; comment != nil {
		return comment.Text()
	}

	return ""
}

type byImportSpec struct {
	groups []string
	specs  []ast.Spec
}

func (x byImportSpec) Len() int      { return len(x.specs) }
func (x byImportSpec) Swap(i, j int) { x.specs[i], x.specs[j] = x.specs[j], x.specs[i] }

func (x byImportSpec) Less(i, j int) bool {
	ipath, jpath := specPath(x.specs[i]), specPath(x.specs[j])

	igroup, jgroup := importGroup(x.groups, ipath), importGroup(x.groups, jpath)
	if igroup != jgroup {
		return igroup < jgroup
	}

	if ipath != jpath {
		return ipath < jpath
	}

	iname, jname := specName(x.specs[i]), specName(x.specs[j])
	if iname != jname {
		return iname < jname
	}

	return specComment(x.specs[i]) < specComment(x.specs[j])
}

type byCommentPos []*ast.CommentGroup

func (x byCommentPos) Len() int           { return len(x) }
func (x byCommentPos) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }
func (x byCommentPos) Less(i, j int) bool { return x[i].Pos() < x[j].Pos() }
