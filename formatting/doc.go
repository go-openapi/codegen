// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Package formatting formats generated Go source.
//
// [Format] parses the source, drops the imports nothing uses, sorts and groups the rest, rejects the
// ones that contradict each other, and prints the result to an [io.Writer]:
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
// # What pruning can know
//
// The identifier an import binds is the imported package's own package clause, and the path only says
// where to find it. "github.com/json-iterator/go" declares jsoniter,
// "github.com/prometheus/client_model/go" declares io_prometheus_client, and reading either name
// means loading the package. So Format deletes an import only when it knows the name, from one of
// three places:
//
//   - an alias, which states the name in the source;
//   - the standard library, held in a generated table built from "go list std";
//   - [WithResolvedImports], where the caller states the name.
//
// A bare third-party import is a guess. A guess keeps an import when one of the names it could
// declare appears as a qualifier, and never deletes one, so this survives:
//
//	import "github.com/go-openapi/strfmt"   // nothing writes strfmt., and the import stays
//
// Two ways to get it deleted. [WithForceImportsPruning] promises every bare import declares the name
// [ImportedPackageName] gives. [WithResolvedImports] states the awkward names instead of promising
// there are none, and a map built once serves every build.
//
// A third way costs no option at all: write the alias even where it repeats the package name.
//
//	import strfmt "github.com/go-openapi/strfmt"   // the binding is certain, so the import is pruned
//
// gofmt leaves that alone, and only revive's redundant-import-alias rule reports it, which is off by
// default. A generator holding either the promise or the map needs it only for the packages that
// break the convention, where the alias is not redundant and writing it is ordinary Go.
//
// [WithSimplifiedImportAliases] takes such an alias back out once the name is proven, so a template
// may write every alias for safety and hand the reader ordinary Go. It drops nothing on a guess, and
// nothing whose alias the path cannot replace.
//
// A blank import runs an init and binds no qualifier, so it is never pruned. A dot import spills its
// names into the file scope and no qualifier ever appears, so nothing about it can be checked: use
// goimports on a file that relies on them.
//
// # What Format does not check
//
// Format reads one file's syntax and nothing else. An import of another module's internal package
// formats like any other, a //go:build line is copied through untouched, and config_linux.go is
// formatted the same as any other file. The go compiler enforces those rules, and Format does not
// duplicate them.
//
// # The imports report
//
// Format returns an [ImportsReport] beside its error, holding one [ImportRecord] per import: the
// path, the name it binds, whether that name was stated or guessed, and what became of it.
//
//	report, err := formatting.Format(out, rendered)
//	if err != nil {
//		return err
//	}
//
//	if report.HasImportsInDoubt() {
//		log.Printf("could not name: %v", report.PathsInDoubt())
//	}
//
// A report with nothing in doubt means every import was decided, so the output holds no import the
// file does not use. Anything in doubt is a path to resolve:
// github.com/go-openapi/codegen/formatting/resolve reads the names from the packages themselves and
// returns the map [WithResolvedImports] takes. Run it once, commit the map, and every build agrees.
//
// A clash between names Format knows is an error. A clash between guessed names may not be real —
// either package may declare something the path does not show — so those imports come back as
// [ImportCollision] and neither is pruned.
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
// # Inconsistent imports
//
// Two imports left after pruning may still contradict each other, and Format returns
// [ErrInconsistentImports] rather than print a file the caller has to debug:
//
//   - one package under two names, as "bytes" beside b "bytes". The go compiler accepts it; the code
//     reads as though b and bytes were different packages.
//   - one name bound to two packages, as "crypto/rand" beside "math/rand" in a file writing
//     rand.Read. The go compiler rejects it.
//
// A name is compared as the file writes it: an alias is the name it declares, and a bare import binds
// whichever of its guessed names the file writes as a qualifier. So "crypto/rand" beside "math/rand"
// passes when nothing writes rand. — pruning takes both — and "github.com/go-openapi/core" beside
// "k8s.io/api/core/v1" passes when the file writes both core. and v1., because then the two bind
// different names.
//
// One error names every mismatch, so a template with three bad imports is fixed in one pass. _ and .
// bind no qualifier and are left alone, and so is an import whose package Format cannot name.
//
// # Naming an import
//
// [ImportedPackageName] returns the identifier to qualify an import path with, version elements
// dropped, so a generator can name an import it is about to write. Format settles the other question
// — which name an existing import already binds — for itself, and reports what it could not settle.
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
