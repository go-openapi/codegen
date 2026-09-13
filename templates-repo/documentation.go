// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/go-openapi/codegen/templates-repo/internal/document"
	"github.com/go-openapi/codegen/templates-repo/reports"
)

// Documentation returns the structure of the repository and the documentation of its templates.
//
// A parse tree carries no comment, and records no data path the template reads. The repository
// retains its sources, so Documentation parses them again to recover both. Nothing is computed
// until you call it.
//
// The result is freshly built and shared with nobody. Hold it, walk it, or render it in whatever
// format.
func (r *Repository) Documentation() (reports.Documentation, error) {
	analysed, err := r.analyse()
	if err != nil {
		return reports.Documentation{}, err
	}

	usedBy := r.reverseDependencies(analysed)

	contracts := make(map[string]document.Contract, len(analysed))
	for name, found := range analysed {
		contracts[name] = found.contract
	}
	closed := document.Closure(contracts)

	byAsset := make(map[string][]reports.Template, len(r.declarations))
	for _, name := range r.names {
		declared := r.declarations[name]
		contract := analysed[name].contract

		byAsset[declared.assetPath] = append(byAsset[declared.assetPath], reports.Template{
			Name:         name,
			Doc:          analysed[name].doc,
			Reads:        contract.Reads,
			RootReads:    contract.RootReads,
			Funcs:        contract.Funcs,
			Dependencies: dependenciesOfContract(contract, closed),
			UsedBy:       usedBy[name],
			Inner:        name != r.settings.templateName(declared.assetPath),
			Empty:        contract.Empty,
			Unresolved:   contract.Unresolved,
			Dynamic:      contract.Dynamic,
			Transitive:   transitiveOf(closed[name]),
		})
	}

	documentation := reports.Documentation{Assets: make([]reports.Asset, 0, len(byAsset))}
	for _, path := range slices.Sorted(maps.Keys(byAsset)) {
		templates := byAsset[path]

		// the template named after the asset comes first, the "define" statements follow by name
		slices.SortFunc(templates, func(a, b reports.Template) int {
			if a.Inner != b.Inner {
				if a.Inner {
					return 1
				}

				return -1
			}

			return strings.Compare(a.Name, b.Name)
		})

		documentation.Assets = append(documentation.Assets, reports.Asset{Path: path, Templates: templates})
	}

	return documentation, nil
}

// analysed holds the result of re-reading an asset, for one template name.
type analysed struct {
	doc      []string
	contract document.Contract
}

// analyse reads the assets again and keeps, per name, what the asset that declares it reported.
//
// An asset a later one overrides is analysed too, and the later findings then replace its own, so
// the documentation describes the definitions the repository holds.
func (r *Repository) analyse() (map[string]analysed, error) {
	found := make(map[string]analysed, len(r.names))

	for _, item := range r.assets {
		owner := r.settings.trimmedPath(item.path)

		analysis, err := document.Analyze(item.path, owner, item.data, r.settings.funcs)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", err, ErrTemplateRepo)
		}

		// the analysis reads the source again, so it sees what an author declared rather than the
		// address it landed at: each is placed back where the repository holds it
		for declaredName, contract := range analysis.Contracts {
			key := TemplateName(addressOf(owner, declaredName))
			if r.declarations[key].assetPath != item.path {
				continue // another asset declares this address
			}

			found[key] = analysed{
				doc:      analysis.Docstrings[declaredName],
				contract: r.resolvedContract(key, contract),
			}
		}
	}

	return found, nil
}

// resolvedContract names the templates a contract calls the way the repository holds them.
//
// An author writes a reference relative to where the template sits, and [build] resolved it to a
// name once. This analysis reads the source again, so it sees the reference as written and has to
// resolve it the same way.
func (r *Repository) resolvedContract(name string, contract document.Contract) document.Contract {
	resolved := r.resolutions[name]
	if len(resolved) == 0 {
		return contract
	}

	calls := make([]document.Call, 0, len(contract.Calls))
	for _, call := range contract.Calls {
		if key, found := resolved[call.Name]; found {
			call.Name = key
		}

		calls = append(calls, call)
	}

	contract.Calls = calls

	return contract
}

// transitiveOf maps a folded contract onto the exported model.
func transitiveOf(folded document.Transitive) reports.Transitive {
	return reports.Transitive{
		Reads:      folded.Reads,
		Funcs:      folded.Funcs,
		Reaches:    folded.Reaches,
		Unresolved: folded.Unresolved,
		Recursive:  folded.Recursive,
	}
}

// dependenciesOfContract turns the calls a template makes into its dependencies.
func dependenciesOfContract(contract document.Contract, closed map[string]document.Transitive) []reports.Dependency {
	dependencies := make([]reports.Dependency, 0, len(contract.Calls))
	for _, call := range contract.Calls {
		dependencies = append(dependencies, reports.Dependency{
			Name:   call.Name,
			Data:   call.Data,
			Folded: len(closed[call.Name].Reads),
		})
	}

	return dependencies
}

// reverseDependencies inverts the call graph, so that every template lists its callers.
func (r *Repository) reverseDependencies(analysed map[string]analysed) map[string][]string {
	usedBy := make(map[string][]string, len(r.names))

	for _, name := range r.names {
		for _, call := range analysed[name].contract.Calls {
			if !slices.Contains(usedBy[call.Name], name) {
				usedBy[call.Name] = append(usedBy[call.Name], name)
			}
		}
	}

	for dependency := range usedBy {
		slices.Sort(usedBy[dependency])
	}

	return usedBy
}

// Dump writes the documentation of the repository, as markdown by default.
//
// It calls [Repository.Documentation] and passes the result to [reports.Dump]. Call reports.Dump
// yourself to render one document in several formats.
func (r *Repository) Dump(w io.Writer, opts ...reports.DumpOption) error {
	documentation, err := r.Documentation()
	if err != nil {
		return err
	}

	if err := reports.Dump(w, documentation, opts...); err != nil {
		// a caller of this method matches the error of this package, whichever one reports it
		return fmt.Errorf("%w: %w", err, ErrTemplateRepo)
	}

	return nil
}
