// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

// Error is a string that implements error, so a sentinel below can be a constant.
type Error string

func (e Error) Error() string { return string(e) }

const (
	// ErrFormat matches every error [Format] returns.
	ErrFormat Error = "formatting error"

	// ErrInconsistentImports is returned when the imports left after pruning contradict one another:
	// one package imported under two names, or one name bound to two packages. The message names
	// every mismatch [Format] found, and nothing is printed.
	ErrInconsistentImports Error = "inconsistent imports"

	// ErrNoGoFumpt is returned when [WithGoFumpt] is passed but the gofumpt rules were never
	// registered. Blank-import github.com/go-openapi/codegen/formatting/enable/gofumpt.
	ErrNoGoFumpt Error = "gofumpt requested but not enabled: blank-import " +
		`_ "github.com/go-openapi/codegen/formatting/enable/gofumpt"`
)
