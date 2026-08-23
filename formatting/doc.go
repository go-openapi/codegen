// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Package formatting formats generated Go source.
//
// [Format] parses the source, drops the imports nothing uses, sorts and groups the rest, and prints
// the result to an [io.Writer]:
//
//	err := formatting.Format(file, rendered,
//		formatting.WithImportGroups("github.com/go-openapi", baseImport),
//	)
//
// # Rendering many files
//
// A generator renders a template into a buffer and formats what it rendered. Reset one buffer and
// use it again rather than allocating one per template: Format takes a [bytes.Buffer] as its source,
// reads its bytes without copying them and leaves it untouched.
//
//	var rendered bytes.Buffer
//
//	for _, tpl := range templates {
//		rendered.Reset()
//
//		if err := tpl.Execute(&rendered, data); err != nil {
//			return err
//		}
//
//		if err := formatting.Format(out, &rendered, groups); err != nil {
//			return err
//		}
//	}
//
// That saves about a tenth of the memory a file costs to render and format. Passing rendered.Bytes()
// does the same thing; passing an [io.Reader] could not, which is why [Source] does not admit one.
//
// # Imports are never resolved
//
// Format removes an import no code uses. It never adds one. goimports resolves a missing import by
// searching the module cache and the build list, which makes the output depend on the machine that
// ran the generator: the same template and the same spec produce different files, and a maintainer
// cannot reproduce what a user reports. A template writes the imports its code needs, and code that
// names a package it did not import fails to compile, which is a better answer than a guess.
//
// # Duplicate imports
//
// The blank lines a template wrote inside its import block mean nothing to Format: it sorts the
// whole block at once and keeps one spec per path. A template that writes
//
//	import (
//		"bytes"
//
//		"bytes"
//		"context"
//	)
//
// gets "bytes" and "context" back, in one group.
//
// gofmt and goimports both answer differently. They sort each blank-line-separated run on its own
// and never move an import between runs, so both keep the second "bytes" and the file fails to
// compile with "bytes redeclared in this block". A template assembling its imports from several
// fragments hits this whenever two fragments contribute the same package.
//
// # Naming an import
//
// [ImportedPackageName] returns the identifier to qualify an import path with, version elements
// dropped, so a generator can name an import it is about to write. Format settles the other question
// — which name an existing import already binds — for itself.
//
// # Grouping
//
// Without [WithImportGroups] the output has two groups, the standard library and everything else.
// Each prefix passed to [WithImportGroups] adds a group between them, in the order given:
//
//	WithImportGroups("github.com/go-openapi", "example.com/petstore")
//
//	import (
//		"context"          // standard library
//
//		"github.com/go-openapi/runtime"
//
//		"example.com/petstore/models"
//
//		"github.com/google/uuid"   // everything else
//	)
//
// The prefixes travel with the call, so two goroutines may format with different grouping.
//
// A later gofmt or goimports keeps this layout. Both sort each blank-line-separated run of imports on
// its own and never move an import from one run into another, so they leave the groups where Format
// put them, and a "golangci-lint fmt" over generated code changes nothing. gci is the exception: it
// enforces one order over the whole block and regroups.
//
// # gofumpt
//
// [WithGoFumpt] applies the gofumpt rules before printing. gofumpt is an optional dependency and
// lives in its own module, so a build that does not ask for it does not pay for it. Enable it with a
// blank import:
//
//	import _ "github.com/go-openapi/codegen/formatting/enable/gofumpt"
//
// Without that import, [WithGoFumpt] makes [Format] return [ErrNoGoFumpt].
//
// # Fragments
//
// A source with no package clause is parsed as a declaration list, then as a statement list, the way
// [go/format.Source] does. A fragment cannot stream: Format prints it to a buffer, strips the
// wrapping and restores the surrounding white space before writing.
package formatting
