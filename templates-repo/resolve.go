// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"text/template/parse"
)

// declared holds one template of the repository, together with the address declaring it.
type declared struct {
	// address is the path the template was declared at, never mangled.
	//
	// An asset declares a template at its own path, extension trimmed, and one per "define"
	// statement at that path followed by the name the statement gives:
	//
	//	server/fred.gotmpl                     ->  server/fred
	//	{{ define "inner-macro" }} within it   ->  server/fred/inner-macro
	address string

	// key is the name [Repository.Get] takes, mangled from the path.
	key string

	// owner is the path of the asset template that declares this one, which is the path itself
	// for a template named after its asset.
	owner string

	// bare is the name a "define" statement gave, empty for a template named after its asset.
	bare string

	// assetPath is the path of the asset it was read from, extension and all.
	assetPath string

	layer int
	tree  *parse.Tree
}

// addressOf returns the address an asset declares one of its templates at.
//
// An asset declares a template at its own address, and one per "define" statement under it.
func addressOf(owner, declaredName string) string {
	if declaredName == owner {
		return owner
	}

	return owner + "/" + declaredName
}

// isDefine reports whether a "define" statement declared this template.
func (d declared) isDefine() bool { return d.bare != "" }

// addressSpace resolves what a template refers to, from where it was declared.
//
// A reference is mangled to a key, then looked for outward from the template holding it: its own
// children, its directory, each enclosing directory, the root. At every level a reference matches
// either a template addressed under that level, by the key of the path relative to it, or a
// "define" of an asset sitting directly in it, by its bare name. The first match wins, so the
// nearest scope shadows the ones outside it.
type addressSpace struct {
	// byPath holds every template the repository declares.
	byPath map[string]*declared

	// relative maps, per scope, the key of a path relative to that scope onto the key of the
	// template it addresses.
	relative map[string]map[string]string

	// siblings maps, per directory, the bare name of a "define" onto the template it declares.
	// Only the assets sitting directly in that directory contribute to it.
	siblings map[string]map[string]*declared
}

// newAddressSpace indexes what a set of declarations addresses, and reports what it cannot tell
// apart.
func newAddressSpace(declarations map[string]*declared) (addressSpace, error) {
	space := addressSpace{
		byPath:   declarations,
		relative: make(map[string]map[string]string, len(declarations)),
		siblings: make(map[string]map[string]*declared, len(declarations)),
	}

	var ambiguous []string

	for _, address := range sortedPaths(declarations) {
		item := declarations[address]

		for _, scope := range ancestorsOf(address) {
			relative := TemplateName(strings.TrimPrefix(strings.TrimPrefix(address, scope), "/"))

			if taken, found := space.relative[scope][relative]; found && taken != item.key {
				ambiguous = append(ambiguous, fmt.Sprintf(
					"%q and %q are both %q under %s", taken, item.key, relative, scopeName(scope)))

				continue
			}

			if space.relative[scope] == nil {
				space.relative[scope] = make(map[string]string)
			}
			space.relative[scope][relative] = item.key
		}

		if !item.isDefine() {
			continue
		}

		directory := path.Dir(item.owner)
		if directory == "." {
			directory = ""
		}

		bare := TemplateName(item.bare)
		if taken, found := space.siblings[directory][bare]; found && taken.key != item.key {
			ambiguous = append(ambiguous, fmt.Sprintf(
				"%q and %q both declare %q in %s", taken.owner, item.owner, item.bare,
				scopeName(directory)))

			continue
		}

		if space.siblings[directory] == nil {
			space.siblings[directory] = make(map[string]*declared)
		}
		space.siblings[directory][bare] = item
	}

	if len(ambiguous) > 0 {
		return addressSpace{}, fmt.Errorf(
			"templates that cannot be told apart: %s: %w", strings.Join(ambiguous, ", "), ErrTemplateRepo)
	}

	return space, nil
}

// resolve names the template a reference held by another one addresses.
//
// A scope may answer a reference in two ways at once. That is an ambiguous reference rather than
// a precedence to settle, so it is reported instead of resolved either way.
func (a addressSpace) resolve(from, reference string) (string, bool, error) {
	wanted := TemplateName(reference)

	for _, scope := range scopesOf(from) {
		addressed, byAddress := a.relative[scope][wanted]
		sibling, byName := a.siblings[scope][wanted]

		switch {
		case byAddress && byName && addressed != sibling.key:
			return "", false, fmt.Errorf(
				"%q refers to %q, which is both the template addressed %q and the %q declared by %q, in %s: %w",
				from, reference, addressed, sibling.bare, sibling.owner, scopeName(scope), ErrTemplateRepo)

		case byAddress:
			return addressed, true, nil

		case byName:
			return sibling.key, true, nil
		}
	}

	return "", false, nil
}

// rewrite resolves every reference a template holds, and writes the key it addresses in its place.
//
// The names an author writes are relative to where they wrote them, and the namespace a template
// executes in is flat. Resolving every reference here reconciles the two, and nothing is resolved
// again while a template runs.
func (a addressSpace) rewrite() (map[string][]string, map[string]map[string]string, error) {
	unresolved := make(map[string][]string)
	resolutions := make(map[string]map[string]string)

	for _, address := range sortedPaths(a.byPath) {
		item := a.byPath[address]
		if item.tree == nil {
			continue
		}

		for _, node := range referencesIn(item.tree.Root) {
			key, found, err := a.resolve(address, node.Name)
			if err != nil {
				return nil, nil, err
			}

			if !found {
				unresolved[item.key] = append(unresolved[item.key], node.Name)

				continue
			}

			if resolutions[item.key] == nil {
				resolutions[item.key] = make(map[string]string)
			}
			resolutions[item.key][node.Name] = key

			node.Name = key
		}
	}

	return unresolved, resolutions, nil
}

// referencesIn collects the nodes invoking another template, which are the ones to resolve.
func referencesIn(node parse.Node) []*parse.TemplateNode {
	var found []*parse.TemplateNode
	collectReferences(node, &found)

	return found
}

// collectReferences walks a parse tree and gathers every template invocation it holds.
func collectReferences(node parse.Node, found *[]*parse.TemplateNode) {
	switch typed := node.(type) {
	case *parse.ListNode:
		if typed == nil {
			return
		}

		for _, child := range typed.Nodes {
			collectReferences(child, found)
		}

	case *parse.IfNode:
		collectReferences(typed.List, found)
		collectReferences(typed.ElseList, found)

	case *parse.RangeNode:
		collectReferences(typed.List, found)
		collectReferences(typed.ElseList, found)

	case *parse.WithNode:
		collectReferences(typed.List, found)
		collectReferences(typed.ElseList, found)

	case *parse.TemplateNode:
		*found = append(*found, typed)
	}
}

// scopesOf lists where a template looks for what it refers to, nearest first.
func scopesOf(address string) []string {
	scopes := []string{address}

	for {
		cut := strings.LastIndex(address, "/")
		if cut < 0 {
			break
		}

		address = address[:cut]
		scopes = append(scopes, address)
	}

	if address != "" {
		scopes = append(scopes, "")
	}

	return scopes
}

// ancestorsOf lists the scopes a template is addressable under, nearest first.
func ancestorsOf(address string) []string {
	return scopesOf(address)[1:]
}

// scopeName renders a scope for a diagnostic.
func scopeName(scope string) string {
	if scope == "" {
		return "the root"
	}

	return scope
}

// sortedPaths lists the addresses of a set of declarations, so that what is built from them does
// not depend on the order a map hands them over.
func sortedPaths(declarations map[string]*declared) []string {
	return slices.Sorted(maps.Keys(declarations))
}
