// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"fmt"
	"path"
	"slices"
	"text/template"

	"github.com/go-openapi/codegen/funcmaps"
)

// Rebase derives a repository holding the templates of another one, addressed under a base.
//
// Every address moves under base, so a repository built from server/parameter.gotmpl rebased under
// "v2" holds v2/server/parameter, answering to v2ServerParameter.
//
// What the templates refer to moves with them. A reference resolves outward from where it was
// written, and everything it could reach sits one level further in, so a set that resolved on its
// own resolves the same once rebased. Use this to assemble a repository out of parts written
// apart.
//
// source is left untouched.
//
// Example:
//
//	models, err := repo.Rebase(modelTemplates, "models")
func Rebase(source *Repository, base string) (*Repository, error) {
	if source == nil {
		return nil, fmt.Errorf("cannot rebase a nil repository: %w", ErrTemplateRepo)
	}

	under, err := cleanMountPoint(base)
	if err != nil {
		return nil, err
	}

	if under == "" {
		return nil, fmt.Errorf("rebasing needs a base to address the templates under: %w", ErrTemplateRepo)
	}

	moved := make([]asset, 0, len(source.assets))
	for _, item := range source.assets {
		item.path = path.Join(under, item.path)
		moved = append(moved, item)
	}

	return build(moved, source.layers, source.settings.derive())
}

// Merge derives a repository holding the templates of several, the last to declare an address
// winning.
//
// You merge in order to override, so a template declared twice is not an error.
// [Repository.Audit] reports which definition stands and which it replaced. Func maps merge the
// same way, through [github.com/go-openapi/codegen/funcmaps.Merge].
//
// Call [Rebase] first when the sets were written apart: it moves their addresses under a base of
// their own, so two of them never declare the same one.
//
// Example:
//
//	templates, err := repo.Merge(scaffolding,
//		must(repo.Rebase(modelTemplates, "models")),
//		must(repo.Rebase(serverTemplates, "server")),
//	)
func Merge(source *Repository, merged ...*Repository) (*Repository, error) {
	return compose(source, merged, "merge", func(assets []asset, _ map[string]struct{}) []asset {
		return assets
	}, funcmaps.Merge)
}

// Coalesce derives a repository holding the templates of several, the first to declare an address
// winning.
//
// It is [Merge] the other way round: a later repository declaring an address an earlier one
// already holds is dropped, and does not take its place. Func maps coalesce the same way, through
// [github.com/go-openapi/codegen/funcmaps.Coalesce], which also leaves the builtins alone.
//
// Use it when source holds the definitions that must win, and the later repositories only fill in
// the addresses it does not declare.
func Coalesce(source *Repository, coalesced ...*Repository) (*Repository, error) {
	return compose(source, coalesced, "coalesce", func(assets []asset, taken map[string]struct{}) []asset {
		kept := make([]asset, 0, len(assets))
		for _, item := range assets {
			if _, held := taken[item.path]; held {
				continue // the repository named first holds this address
			}

			kept = append(kept, item)
		}

		return kept
	}, funcmaps.Coalesce)
}

// compose is the body of [Merge] and [Coalesce].
//
// It appends the assets of each repository in others to those of source and rebuilds from the
// result, renumbering the layers as it goes so that two repositories never share one.
//
// keep filters each repository's assets against the addresses already taken. [Merge] passes all of
// them through, so the last declaration wins; [Coalesce] drops the ones already declared. combine
// folds the func maps the same way, and operation names the failing call in the error messages.
func compose(
	source *Repository,
	others []*Repository,
	operation string,
	keep func([]asset, map[string]struct{}) []asset,
	combine func(template.FuncMap, ...template.FuncMap) template.FuncMap,
) (*Repository, error) {
	if source == nil {
		return nil, fmt.Errorf("cannot %s a nil repository: %w", operation, ErrTemplateRepo)
	}

	settings := source.settings.derive()
	assets := slices.Clone(source.assets)
	layers := source.layers
	taken := addressesOf(assets)
	maps := make([]template.FuncMap, 0, len(others))

	for at, other := range others {
		if other == nil {
			return nil, fmt.Errorf("cannot %s a nil repository, at position %d: %w", operation, at+1, ErrTemplateRepo)
		}

		// layers say which source read an asset, and two repositories know nothing of each other's,
		// so those of each one carry on where the last left off
		moved := make([]asset, 0, len(other.assets))
		for _, item := range other.assets {
			item.layer += layers
			moved = append(moved, item)
		}

		for _, item := range keep(moved, taken) {
			assets = append(assets, item)
			taken[item.path] = struct{}{}
		}

		layers += other.layers
		maps = append(maps, other.settings.funcs)
	}

	settings.funcs = combine(settings.funcs, maps...)

	return build(assets, layers, settings)
}

// addressesOf lists the asset paths a set of assets holds.
func addressesOf(assets []asset) map[string]struct{} {
	held := make(map[string]struct{}, len(assets))
	for _, item := range assets {
		held[item.path] = struct{}{}
	}

	return held
}
