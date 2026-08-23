// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"io"

	"github.com/go-openapi/codegen/formatting/internal/rules"
)

// gofmtMode holds the printer mode bits gofmt uses.
//
// printer.UseSpaces and printer.TabIndent are exported. The third bit canonicalizes number literal
// prefixes and exponents — 0XFF prints as 0xFF — and go/printer defines it for go/format and gofmt
// alone, so it has no exported name. Reaching for [go/format.Node] instead is not an option: it
// treats every parenthesized import block as unsorted, re-parses the printed output and runs
// [go/ast.SortImports], which sorts by path and undoes the grouping. TestMatchesGofmt pins the bit
// by comparing our output against [go/format.Source].
const gofmtMode = printer.UseSpaces | printer.TabIndent | 1<<30

// gofmtTabWidth is the width a tab is taken to have. go/format and cmd/gofmt both fix it at 8 and
// carry a comment telling each other to stay in step.
//
// Under UseSpaces and TabIndent the value never reaches the output: indentation is written with
// tabs, alignment is padded with spaces measured from an indent the aligned lines share, and the
// width assumed for that indent cancels. Printing every fixture at 8 and at 4 gives the same bytes,
// which is why TestMatchesGofmt cannot pin this the way it pins the mode. It is 8 because gofmt
// says 8.
const gofmtTabWidth = 8

// Source lists the types [Format] accepts as source.
//
// Both terms give up their bytes without copying them, so those are the only two. An
// [io.Reader] is deliberately absent: Format reads the source more than once — the parser retries a
// fragment as a declaration list and then as a statement list, a file whose imports may be shadowed
// is parsed a second time with scopes, and a fragment's original text is needed again at the end to
// restore the space around it — so a reader would be drained into a buffer at the door and the
// signature would promise a streaming that cannot happen.
type Source interface {
	[]byte | *bytes.Buffer
}

// Format formats Go source and writes the result to w.
//
// It drops the imports nothing uses, sorts and groups the rest, and prints in gofmt style. It never
// adds an import: see the package documentation for why. The blank lines the source wrote inside its
// import block are ignored, so a path written in two groups is one import in the output.
//
// It returns [ErrInconsistentImports] when the imports left after pruning contradict each other —
// one package under two names, or one name bound to two packages.
//
// The [ImportsReport] accounts for every import: what was pruned, what stayed, and what stayed only
// because Format could not name the package. It comes back whenever the source parsed, an
// [ErrInconsistentImports] included, and is nil only when parsing failed. Ask
// [ImportsReport.HasImportsInDoubt] before trusting that pruning was exact.
//
// Passing a [bytes.Buffer] hands over its bytes and leaves it as it was: Format does not drain it,
// so a caller rendering one template after another resets it and writes the next.
//
// The source is parsed, pruned, sorted, grouped and checked before a byte is written, so a source
// that does not parse or whose imports contradict each other leaves w untouched. Once printing
// starts only w itself can fail, and a fragment is printed to a buffer and copied in one write.
func Format[T Source](w io.Writer, src T, opts ...Option) (*ImportsReport, error) {
	return format(w, sourceBytes(src), opts...)
}

// sourceBytes takes the bytes out of a [Source] without copying them.
//
// The union holds []byte rather than ~[]byte on purpose: a type switch on a named byte slice would
// match neither term, and a conversion cannot serve both terms at once.
func sourceBytes[T Source](src T) []byte {
	if buffer, ok := any(src).(*bytes.Buffer); ok {
		if buffer == nil {
			return nil
		}

		return buffer.Bytes()
	}

	return any(src).([]byte)
}

func format(w io.Writer, src []byte, opts ...Option) (*ImportsReport, error) {
	o := optionsWithDefaults(opts)

	var extraRules rules.Func
	if o.goFumpt {
		if extraRules = rules.Registered(); extraRules == nil {
			return nil, ErrNoGoFumpt
		}
	}

	fset, file, adjust, err := parse(src, o.resolved)
	if err != nil {
		return nil, fmt.Errorf("cannot parse source: %w: %w", err, ErrFormat)
	}

	bindings, used := prune(fset, file, o)
	mergeImports(file)
	sortImports(fset.File(file.FileStart), file, o.groups)

	report := newImportsReport(bindings, file, used)

	if err := checkImports(report, o.forcePruning); err != nil {
		return report, fmt.Errorf("%w: %w", err, ErrFormat)
	}

	breaks := groupBreaks(fset, file, o.groups)

	if extraRules != nil {
		extraRules(fset, file)
	}

	if adjust != nil {
		return report, printFragment(w, fset, file, src, breaks, adjust)
	}

	return report, printFile(w, fset, file, breaks)
}

// printFile prints a whole file straight to w, one line at a time.
// parse reads the source, once if it can and twice if it must.
//
// The first parse skips the parser's scope building, which costs about a sixth of everything Format
// allocates. [needsResolution] then says whether [prune] can tell a package qualifier from a
// shadowed name without those scopes; when it cannot, the source is parsed again with them. The two
// paths answer alike, so the second parse buys correctness in the rare file rather than in every
// file.
func parse(src []byte, resolved map[string]string) (*token.FileSet, *ast.File, adjustFunc, error) {
	fset := token.NewFileSet()

	file, adjust, err := parseFile(fset, src, fastMode)
	if err != nil {
		return nil, nil, nil, err
	}

	if !needsResolution(file, resolved) {
		return fset, file, adjust, nil
	}

	fset = token.NewFileSet()

	file, adjust, err = parseFile(fset, src, resolvedMode)
	if err != nil {
		return nil, nil, nil, err
	}

	return fset, file, adjust, nil
}

func printFile(w io.Writer, fset *token.FileSet, file *ast.File, breaks []string) error {
	spaced := newSpacer(w, breaks)

	if err := fprint(spaced, fset, file); err != nil {
		return err
	}

	if err := spaced.Flush(); err != nil {
		return fmt.Errorf("cannot write formatted source: %w: %w", err, ErrFormat)
	}

	return nil
}

// printFragment prints a fragment, then puts back the white space that surrounded it.
//
// A fragment was parsed by wrapping it, and the wrapping comes off the printed bytes rather than the
// tree, so only this path holds the whole output at once.
func printFragment(
	w io.Writer,
	fset *token.FileSet,
	file *ast.File,
	src []byte,
	breaks []string,
	adjust adjustFunc,
) error {
	var printed bytes.Buffer

	spaced := newSpacer(&printed, breaks)
	if err := fprint(spaced, fset, file); err != nil {
		return err
	}

	if err := spaced.Flush(); err != nil {
		return fmt.Errorf("cannot print fragment: %w: %w", err, ErrFormat)
	}

	if _, err := w.Write(adjust(src, printed.Bytes())); err != nil {
		return fmt.Errorf("cannot write formatted fragment: %w: %w", err, ErrFormat)
	}

	return nil
}

// fprint writes the tree to w in gofmt style.
func fprint(w io.Writer, fset *token.FileSet, file *ast.File) error {
	config := printer.Config{Mode: gofmtMode, Tabwidth: gofmtTabWidth}

	if err := config.Fprint(w, fset, file); err != nil {
		return fmt.Errorf("cannot print formatted source: %w: %w", err, ErrFormat)
	}

	return nil
}
