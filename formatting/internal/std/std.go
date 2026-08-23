// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Package std answers which name a standard library import declares.
//
// The table in packages.go is generated from "go list std" and holds the answer outright, so nothing
// here guesses. That is what lets the formatter prune an unused standard library import: the name is
// known, not inferred.
//
// A path the table does not hold is not treated as standard library, whatever it looks like. A
// package added by a newer Go release, and a local module such as "myapp/models" whose first path
// element holds no dot, are both unknown until the table is regenerated, and unknown means the
// formatter falls back to guessing.
package std

//go:generate go run gen.go

// Name returns the name the standard library package at importPath declares.
//
// The second result is false when the path is not in the table.
func Name(importPath string) (string, bool) {
	name, ok := names[importPath]

	return name, ok
}

// Len returns the number of packages in the table. Tests use it to notice an empty generation.
func Len() int { return len(names) }
