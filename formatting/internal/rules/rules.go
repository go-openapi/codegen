// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Package rules holds the formatting rules an enable module registers.
//
// A rule set that would cost a dependency does not ship with
// [github.com/go-openapi/codegen/formatting]. It lives in a module of its own under
// formatting/enable, which registers it here from an init, so a build that does not import that
// module does not require what it requires.
//
// The package is internal because registering is not something a caller does: a caller states the
// intent with a blank import, and the enable module calls [Register].
package rules

import (
	"go/ast"
	"go/token"
	"sync/atomic"
)

// Func applies a set of formatting rules to a parsed file, in place.
type Func func(*token.FileSet, *ast.File)

// registered is written once from an init and read on every format, so it is atomic.
var registered atomic.Pointer[Func]

// Register makes a rule set available. Registering twice replaces the previous set, and registering
// nil removes it.
func Register(rules Func) {
	if rules == nil {
		registered.Store(nil)

		return
	}

	registered.Store(&rules)
}

// Registered returns the rule set, or nil when no enable module was imported.
func Registered() Func {
	if rules := registered.Load(); rules != nil {
		return *rules
	}

	return nil
}
