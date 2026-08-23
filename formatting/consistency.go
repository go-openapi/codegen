// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// checkImports reports the imports a generator wrote inconsistently, and marks the doubtful ones.
//
// Two shapes are wrong, and both come from a template importing a package a second time under another
// name:
//
//   - one package under two names, as "bytes" beside b "bytes". This compiles, and the code it comes
//     from reads as though b and bytes were different packages.
//   - one name bound to two packages, as rand "math/rand" beside "crypto/rand" in a file writing
//     rand.Read. The compiler rejects it with "rand redeclared in this block".
//
// What follows from a clash depends on the evidence. Names [Format] knows - an alias, the standard
// library table, [WithResolvedImports], or any name at all under [WithForceImportsPruning] - make it
// an error wrapping [ErrInconsistentImports], and nothing is printed. A clash between guessed names
// may not be real, since either package may declare something no rule reading the path produces, so
// those records are marked [ImportCollision] and the caller decides.
//
// One error names every mismatch, separated by "; ", so a template with three bad imports is fixed in
// one pass.
func checkImports(report *ImportsReport, forced bool) error {
	namesOf := make(map[string][]string) // import path -> the names bound to it
	pathsOf := make(map[string][]string) // name -> the import paths bound to it
	guessed := make(map[string]bool)     // names that came from a guess

	for _, record := range report.records {
		if record.Name == "" || !bindsQualifier(record.Status) {
			continue
		}

		namesOf[record.Path] = appendNew(namesOf[record.Path], record.Name)
		pathsOf[record.Name] = appendNew(pathsOf[record.Name], record.Path)

		if !record.Certain {
			guessed[record.Name] = true
		}
	}

	var mismatches []string
	clashing := make(map[string]bool) // names whose clash is only a guess

	for _, importPath := range slices.Sorted(maps.Keys(namesOf)) {
		names := namesOf[importPath]
		if len(names) <= 1 {
			continue
		}

		slices.Sort(names)

		if certainClash(names, guessed, forced) {
			mismatches = append(mismatches, fmt.Sprintf(
				"the package %q is imported under %d names, %s",
				importPath, len(names), quoteAll(names),
			))

			continue
		}

		for _, name := range names {
			clashing[name] = true
		}
	}

	for _, name := range slices.Sorted(maps.Keys(pathsOf)) {
		paths := pathsOf[name]
		if len(paths) <= 1 {
			continue
		}

		slices.Sort(paths)

		if !guessed[name] || forced {
			mismatches = append(mismatches, fmt.Sprintf(
				"the name %q is bound to %d packages, %s",
				name, len(paths), quoteAll(paths),
			))

			continue
		}

		clashing[name] = true
	}

	report.markCollisions(clashing)

	if len(mismatches) == 0 {
		return nil
	}

	return fmt.Errorf("%s: %w", strings.Join(mismatches, "; "), ErrInconsistentImports)
}

// certainClash reports whether every name in a clash was stated rather than guessed.
func certainClash(names []string, guessed map[string]bool, forced bool) bool {
	if forced {
		return true // the caller promised the guesses are right, so a clash between them is real
	}

	for _, name := range names {
		if guessed[name] {
			return false
		}
	}

	return true
}

// bindsQualifier reports whether an import of this status declares a name the file could write.
func bindsQualifier(status ImportStatus) bool {
	switch status {
	case ImportUsed, ImportInDoubt, ImportCollision:
		return true
	case ImportPruned, ImportBlank, ImportDot, ImportCgo:
		return false
	default:
		return false
	}
}

// markCollisions moves every record holding one of the clashing names to [ImportCollision].
func (r *ImportsReport) markCollisions(clashing map[string]bool) {
	if len(clashing) == 0 {
		return
	}

	for i := range r.records {
		record := &r.records[i]

		if clashing[record.Name] && bindsQualifier(record.Status) {
			record.Status = ImportCollision
		}
	}
}

func quoteAll(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}

	return strings.Join(quoted, " and ")
}
