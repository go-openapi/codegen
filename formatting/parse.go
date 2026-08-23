// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0
//
// The three-tier fragment parse, cutSpace and matchSpace follow
// golang.org/x/tools/internal/imports/imports.go, which carries:
//
//	Copyright 2013 The Go Authors. All rights reserved.
//	Use of this source code is governed by a BSD-style license.

package formatting

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// adjustFunc rewrites printed output to undo the wrapping a fragment needed to parse.
//
// It takes the original source and the printed bytes. A whole file needs none, and [Format] streams
// when it gets nil.
type adjustFunc func(orig, printed []byte) []byte

// The two parse modes.
//
// [prune] reads Ident.Obj to tell a package qualifier from a name a declaration shadows, and only
// [resolvedMode] fills Obj in. Building those scopes costs about a sixth of everything Format
// allocates, so [fastMode] runs first and [needsResolution] says whether the answer can differ.
//
// [go/ast.Object] is deprecated and points at [go/types] instead, which is no use here: the type
// checker needs an importer and imports that resolve, and this package formats generated files
// whose imports may name packages no module holds yet. The warning on Object is about composite
// literal keys, where T{K: 0} gives K a meaning only a type decides; prune asks about a selector
// base in expression position, which the parser settles on syntax alone. Replacing it means walking
// the scopes ourselves, not type checking.
const (
	fastMode     = parser.ParseComments | parser.AllErrors | parser.SkipObjectResolution
	resolvedMode = parser.ParseComments | parser.AllErrors
)

// parseFile parses src as a whole file, a declaration list or a statement list, in that order.
//
// It returns the file, an [adjustFunc] to run over the printed output, and an error. Only a source
// that fails to parse under all three readings returns an error, and it reports what the whole file
// attempt found, since that is the reading the caller meant.
//
// The parser is given no file name. Nothing here reads the file system, so a name would only label
// the positions a parse error reports, and the caller already has the path.
func parseFile(fset *token.FileSet, src []byte, mode parser.Mode) (*ast.File, adjustFunc, error) {
	file, err := parser.ParseFile(fset, "", src, mode)
	if err == nil {
		return file, nil, nil
	}

	if !strings.Contains(err.Error(), "expected 'package'") {
		return nil, nil, err
	}

	if file, adjust, ok := parseDeclList(fset, src, mode); ok {
		return file, adjust, nil
	}

	if file, adjust, ok := parseStmtList(fset, src, mode); ok {
		return file, adjust, nil
	}

	return nil, nil, err
}

// declPrefix opens a declaration list. The semicolon rather than a newline keeps every parse error
// on its original line.
const declPrefix = "package main;"

// parseDeclList reads src as a list of declarations by prefixing a package clause.
func parseDeclList(fset *token.FileSet, src []byte, mode parser.Mode) (*ast.File, adjustFunc, bool) {
	prefixed := append([]byte(declPrefix), src...)

	file, err := parser.ParseFile(fset, "", prefixed, mode)
	if err != nil {
		return nil, nil, false
	}

	// the printer turns the semicolon into a newline, so do it here and re-line the file, keeping
	// every position and line number below in step with what will be printed.
	prefixed[len(declPrefix)-1] = '\n'
	fset.File(file.Package).SetLinesForContent(prefixed)

	// a fragment declaring func main() is a package of its own, and keeping the clause is right.
	if declaresMain(file) {
		return file, nil, true
	}

	adjust := func(orig, printed []byte) []byte {
		return matchSpace(orig, printed[len(declPrefix):])
	}

	return file, adjust, true
}

// stmtPrefix and stmtSuffix wrap a statement list, an expression included, in a function body.
const (
	stmtPrefix = "package p; func _() {"
	stmtSuffix = "}"

	// printedStmtPrefix is stmtPrefix once the printer has laid it out.
	printedStmtPrefix = "package p\n\nfunc _() {"
	printedStmtSuffix = "}\n"
)

// parseStmtList reads src as a list of statements by wrapping it in a function.
func parseStmtList(fset *token.FileSet, src []byte, mode parser.Mode) (*ast.File, adjustFunc, bool) {
	wrapped := make([]byte, 0, len(stmtPrefix)+len(src)+len(stmtSuffix))
	wrapped = append(wrapped, stmtPrefix...)
	wrapped = append(wrapped, src...)
	wrapped = append(wrapped, stmtSuffix...)

	file, err := parser.ParseFile(fset, "", wrapped, mode)
	if err != nil {
		return nil, nil, false
	}

	adjust := func(orig, printed []byte) []byte {
		body := printed[len(printedStmtPrefix) : len(printed)-len(printedStmtSuffix)]
		// the printer indented the body one level; take that level back out
		body = bytes.ReplaceAll(body, []byte("\n\t"), []byte("\n"))

		return matchSpace(orig, body)
	}

	return file, adjust, true
}

// declaresMain reports whether file declares func main(), taking no arguments and returning nothing.
func declaresMain(file *ast.File) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "main" || fn.Recv != nil {
			continue
		}

		if len(fn.Type.Params.List) != 0 {
			continue
		}

		if fn.Type.Results != nil && len(fn.Type.Results.List) != 0 {
			continue
		}

		return true
	}

	return false
}

// matchSpace gives printed the white space that surrounded orig.
//
// Leading blank lines come back, the indentation of the first non-blank line of orig is applied to
// every non-blank line of printed, and the trailing space of orig replaces the trailing space of
// printed. A fragment rendered by a template sits inside a file, and matchSpace puts it back where
// it sat.
func matchSpace(orig, printed []byte) []byte {
	before, _, after := cutSpace(orig)
	lineStart := bytes.LastIndexByte(before, '\n')
	before, indent := before[:lineStart+1], before[lineStart+1:]

	_, printed, _ = cutSpace(printed)

	var out bytes.Buffer
	out.Write(before)

	for len(printed) > 0 {
		line := printed
		if end := bytes.IndexByte(line, '\n'); end >= 0 {
			line, printed = line[:end+1], line[end+1:]
		} else {
			printed = nil
		}

		if len(line) > 0 && line[0] != '\n' { // a blank line takes no indent
			out.Write(indent)
		}
		out.Write(line)
	}

	out.Write(after)

	return out.Bytes()
}

// cutSpace splits b into its leading space, its content and its trailing space.
func cutSpace(b []byte) (before, middle, after []byte) {
	start := 0
	for start < len(b) && isSpaceByte(b[start]) {
		start++
	}

	end := len(b)
	for end > 0 && isSpaceByte(b[end-1]) {
		end--
	}

	if start > end { // all space
		return nil, nil, b[end:]
	}

	return b[:start], b[start:end], b[end:]
}

// isSpaceByte reports whether c is one of the four bytes go/format counts as space.
//
// \r belongs here: a fragment written on Windows separates its lines with \r\n, and leaving \r out
// makes [cutSpace] read it as content, so [matchSpace] restores none of the space around the
// fragment. go/format/internal.go carries the same list.
func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
