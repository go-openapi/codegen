// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

// Error is a string that implements error, so a sentinel below can be a constant.
type Error string

func (e Error) Error() string { return string(e) }

// ErrGenApp matches every error this package returns.
//
// It is attached where an error from elsewhere crosses into this package — from the templates
// repository, from text/template, from the formatter, from os — and a call that already wrapped one
// passes it back untouched. Attaching it twice would say "code generation error" twice in one
// message and add nothing the first said; TestErrorsWrapOnce counts it on every path.
const ErrGenApp Error = "code generation error"
