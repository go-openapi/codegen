// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Package gofumpt enables [github.com/go-openapi/codegen/formatting.WithGoFumpt].
//
// # Usage
//
// Blank-import this package, then pass the option:
//
//	import (
//		"github.com/go-openapi/codegen/formatting"
//
//		_ "github.com/go-openapi/codegen/formatting/enable/gofumpt"
//	)
//
//	err := formatting.Format(w, src, formatting.WithGoFumpt())
//
// It lives in a module of its own so that a build which does not want gofumpt does not require
// mvdan.cc/gofumpt. Without the blank import, formatting.WithGoFumpt makes Format return
// formatting.ErrNoGoFumpt.
//
// # Settings
//
// [Configure] sets what the rules do, for the whole program. Call it before formatting anything:
//
//	gofumpt.Configure(
//		gofumpt.WithLangVersion("go1.25"),
//		gofumpt.WithModulePath("example.com/petstore"),
//		gofumpt.WithExtraRules("group_params", "clothe_returns"),
//	)
//
// The extra rules are named by string rather than by field. gofumpt documents its Extra struct as a
// set that may gain and lose members, and points API users at the string form for that reason.
//
// # gofumpt and generated code
//
// The gofumpt command leaves a file carrying a "// Code generated ... DO NOT EDIT." line alone,
// unless it was named on the command line. That gate is in the command, not in the library this
// package calls, so enabling gofumpt here applies the rules to generated files. That is the point of
// the option, and it is a deliberate departure from what the tool does on its own.
package gofumpt
