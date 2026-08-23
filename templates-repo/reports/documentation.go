// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package reports

// Documentation is the structure of a repository, as far as a reader of its templates cares.
//
// It is grouped by asset rather than by template, so that it follows the tree its author edits,
// and it is ordered, so that a document generated from it twice is the same document twice.
type Documentation struct {
	// Assets holds one entry per asset that declares a template, ordered by path.
	Assets []Asset
}

// Asset is the documentation of a single template asset.
type Asset struct {
	// Path is the asset path, as mounted.
	Path string

	// Templates holds the templates the asset declares, the one named after the asset first,
	// then those declared by a "define" statement, ordered by name.
	Templates []Template
}

// Template is the documentation of a single template.
type Template struct {
	// Name is what [github.com/go-openapi/codegen/templates-repo.Repository.Get] answers to.
	Name string

	// Doc holds the comments documenting the template, one entry per comment.
	Doc []string

	// Reads lists the data paths the template may read, rooted at the data it is executed on.
	//
	// It closes over every branch, so it describes what the data must be able to answer, not a
	// list of what it must hold: a path guarded by a condition may never be reached.
	Reads []string

	// RootReads lists the paths read through "$", a reach past the current dot back to the data
	// the template was executed on.
	RootReads []string

	// Funcs lists the func map functions the template calls, sorted. Builtins are left out.
	Funcs []string

	// Dependencies lists the templates this one calls, with the data handed to each.
	Dependencies []Dependency

	// UsedBy lists the templates that refer to this one directly, sorted.
	UsedBy []string

	// Inner reports whether the template is declared by a "define" statement rather than by an
	// asset of its own.
	Inner bool

	// Empty reports whether the template holds nothing but white space and comments, so that
	// executing it renders nothing.
	//
	// An asset made of "define" statements alone declares such a template under its own name,
	// which is reachable like any other and renders nothing.
	Empty bool

	// Unresolved counts the data accesses the analysis could not place, because the value they
	// hang from comes out of a function call.
	Unresolved int

	// Dynamic reports whether the template invokes a function held by its data, with the builtin
	// "call", which makes its contract incomplete by construction.
	Dynamic bool

	// Transitive holds the data the template reads once the templates it calls are folded into it.
	Transitive Transitive
}

// Transitive holds the data paths a template reads once the templates it calls are folded
// into it.
//
// The paths a called template reads are rebased onto the data handed to it, so a template reading
// ".GoName" called with ".Properties[]" contributes ".Properties[].GoName" to its caller.
type Transitive struct {
	// Reads lists the data paths the template may read, itself or through the templates it calls.
	Reads []string

	// Funcs lists the func map functions reached the same way.
	Funcs []string

	// Reaches lists the templates it calls, directly or not, sorted.
	Reaches []string

	// Unresolved counts what could not be folded in.
	Unresolved int

	// Recursive reports whether the closure ran into a template that calls itself, directly or
	// not, and stopped there.
	Recursive bool
}

// Dependency is a template called by another one.
type Dependency struct {
	// Name is the template called.
	Name string

	// Data is the path handed to it, rooted like the paths of the calling template.
	//
	// It is "." when the caller hands over its own data, and empty when the analysis could not
	// place it.
	Data string

	// Folded is the number of paths the called template reads, itself and through its own calls.
	//
	// It locates the weight of a caller's own fold, so a reader can see which of its calls to
	// follow first.
	Folded int
}
