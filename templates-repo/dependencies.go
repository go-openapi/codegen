// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"text/template/parse"
)

// retainedNames selects the templates a repository keeps, from the roots the caller named.
//
// An empty roots keeps every template in byKey. Otherwise retainedNames keeps the roots and
// whatever they reach: a template referring to another one keeps it, all the way down, and a loop
// stops at the templates already kept.
//
// A root no source declares is an error. Nothing else would report it, since a scope naming a
// template that does not exist builds a repository that generates nothing.
func retainedNames(byKey map[string]*declared, roots []string) (map[string]struct{}, error) {
	retained := make(map[string]struct{}, len(byKey))

	if len(roots) == 0 {
		for name := range byKey {
			retained[name] = struct{}{}
		}

		return retained, nil
	}

	var undeclared []string
	for _, root := range roots {
		if _, found := byKey[root]; !found {
			undeclared = append(undeclared, strconv.Quote(root))
		}
	}

	if len(undeclared) > 0 {
		slices.Sort(undeclared)

		return nil, fmt.Errorf(
			"no source declares the root template %s: a root is a name, the identity Get takes, "+
				"not the address it was declared at: %w",
			strings.Join(undeclared, ", "), ErrTemplateRepo)
	}

	pending := slices.Clone(roots)
	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]

		item, found := byKey[name]
		if !found {
			continue // a reference that resolved to nothing, reported once the scope is known
		}

		if _, kept := retained[name]; kept {
			continue
		}

		retained[name] = struct{}{}
		if item.tree != nil {
			pending = append(pending, dependenciesOf(item.tree.Root)...)
		}
	}

	return retained, nil
}

// dependenciesOf collects the names a template refers to, sorted and deduplicated.
//
// It walks only the nodes that may hold a template invocation. An action holds an expression and
// never an invocation, so it has no child to visit.
func dependenciesOf(node parse.Node) []string {
	found := make(map[string]struct{})
	collectDependencies(node, found)

	dependencies := make([]string, 0, len(found))
	for name := range found {
		if name == "" {
			continue
		}

		dependencies = append(dependencies, name)
	}

	slices.Sort(dependencies)

	return dependencies
}

// collectDependencies walks a parse tree and records every template invocation it holds.
func collectDependencies(node parse.Node, found map[string]struct{}) {
	switch typed := node.(type) {
	case *parse.ListNode:
		if typed == nil {
			return
		}

		for _, child := range typed.Nodes {
			collectDependencies(child, found)
		}

	case *parse.IfNode:
		collectDependencies(typed.List, found)
		collectDependencies(typed.ElseList, found)

	case *parse.RangeNode:
		collectDependencies(typed.List, found)
		collectDependencies(typed.ElseList, found)

	case *parse.WithNode:
		collectDependencies(typed.List, found)
		collectDependencies(typed.ElseList, found)

	case *parse.TemplateNode:
		found[typed.Name] = struct{}{}
	}
}
