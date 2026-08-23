// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

import (
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
)

// ImportedPackageName returns the single identifier a generator should qualify importPath with.
//
// A version element is never the answer, so "k8s.io/api/apps/v1" and "k8s.io/api/core/v1" give apps
// and core instead of v1 twice. That is the point: two imports of the same API version collide under
// the name they declare and not under this one.
//
// The package at that path really does declare v1, so write the name as an alias whenever the path
// carries a version element:
//
//	apps "k8s.io/api/apps/v1"
//
// It returns "" when no part of the path is a legal Go identifier, as for "example.com/2fa".
//
// Use [importedPackageNames] to ask the other question, which name an existing import is already
// bound to. The two disagree exactly where an alias is needed.
func ImportedPackageName(importPath string) string {
	names := elementNames(unversioned(importPath))
	if len(names) == 0 {
		return ""
	}

	return names[0]
}

// importedPackageNames lists every name the package at importPath could already declare, guessed from
// the path.
//
// [prune] keeps an import when the file writes any of these, and [checkImports] reads which name an
// import binds by matching them against the qualifiers the file writes. Both need every candidate,
// because one guess does not cover a version element: "k8s.io/api/apps/v1" declares v1, while
// "github.com/go-openapi/testify/v2" declares testify, and nothing in either path separates the two.
//
// A hyphen is the other awkward case, because no Go package name holds one. "example.com/my-pkg"
// comes back as pkg, mypkg, my, in that order, which covers 89% of the hyphenated packages in a
// module cache. The rest are past guessing: "github.com/go-critic/go-critic" declares gorules.
//
// An empty result means no candidate is a legal Go identifier, as for "example.com/2fa". It means
// "unknown", not "declares nothing", and [binding] then marks the import in doubt.
//
// This stays unexported: a caller naming an import wants [ImportedPackageName], and a caller asking
// what became of one wants [ImportsReport]. A list of guesses answers neither question on its own.
//
// golang.org/x/tools/internal/imports answers with ImportPathToAssumedName, which returns one name
// and cuts the element at the first character an identifier may not hold, so it reads
// "example.com/my-pkg" as my. That package is internal, so no caller can import it.
func importedPackageNames(importPath string) []string {
	base := path.Base(importPath)

	names := elementNames(base)
	if !isMajorVersion(base) {
		return names
	}

	// a version element is a module major version in "github.com/go-openapi/testify/v2" and a real
	// directory in "k8s.io/api/apps/v1", and only the module boundary tells them apart. Offer both.
	for _, name := range elementNames(unversioned(importPath)) {
		names = appendNew(names, name)
	}

	return names
}

// unversioned returns the last element of importPath that does not read as a version.
//
// "go.mongodb.org/mongo-driver/internal/aws/signer/v4" is a directory named v4 and a module path
// ending in a major version at the same time, and the path does not say which. Both name signer.
func unversioned(importPath string) string {
	for {
		base := path.Base(importPath)
		if !isMajorVersion(base) {
			return base
		}

		dir := path.Dir(importPath)
		if dir == "." || dir == "/" || dir == importPath {
			return base
		}

		importPath = dir
	}
}

// elementNames lists the identifiers one path element could name, likeliest first.
func elementNames(element string) []string {
	var names []string

	add := func(candidate string) {
		// token.IsIdentifier accepts "_", which the compiler rejects with "invalid package name _",
		// and which qualifies nothing anyway
		if candidate != "_" && token.IsIdentifier(candidate) {
			names = appendNew(names, candidate)
		}
	}

	if token.IsIdentifier(element) {
		add(element)

		return names
	}

	// "gopkg.in/yaml.v3" declares yaml
	if dot := strings.IndexByte(element, '.'); dot >= 0 {
		add(element[:dot])
	}

	addHyphenated(add, element)

	return names
}

// addHyphenated offers the names a hyphenated path element could declare, likeliest first.
//
// A Go package name holds no hyphen, so "example.com/my-pkg" declares something else and the path
// does not say what. Measured over the 35 hyphenated package directories in a module cache, the last
// segment is right 74% of the time, dropping the hyphens 9%, and the first segment 6%; the go- and
// -go affixes account for another 46% and 6% between them. Offering all five raises the hit rate from
// 46% to 89%, with the right name first in 80% of them.
//
// The rest are past guessing: "go-critic" declares gorules and "universal-translator" declares ut.
func addHyphenated(add func(string), element string) {
	// "github.com/jessevdk/go-flags" declares flags, "github.com/googleapis/gax-go" declares gax
	add(strings.TrimPrefix(element, "go-"))
	add(strings.TrimSuffix(element, "-go"))

	segments := strings.Split(element, "-")

	add(segments[len(segments)-1])            // "example.com/my-pkg" declares pkg
	add(strings.ReplaceAll(element, "-", "")) // and mypkg is the next best guess
	add(segments[0])                          // "github.com/dgrijalva/jwt-go" declares jwt
}

// isMajorVersion reports whether a path element reads as a module major version, as in v2 or v3.
func isMajorVersion(element string) bool {
	if len(element) < 2 || element[0] != 'v' {
		return false
	}

	_, err := strconv.Atoi(element[1:])

	return err == nil
}

// appendNew adds value unless values already holds it.
func appendNew(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}

	return append(values, value)
}
