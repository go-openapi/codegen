// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"maps"
	"slices"

	"github.com/go-openapi/codegen/templates-repo/reports"
)

// Audit lists the overridden, unused, empty and dynamic templates of a repository.
//
// It reads the assets again, as [Repository.Documentation] does, so nothing is computed until you
// call it. Run it where a build may fail: it finds a contrib set that replaced a template by
// accident, and a macro no other template calls any more.
//
// Example:
//
//	report, err := repository.Audit()
//	if err != nil {
//		return err
//	}
//
//	for _, override := range report.Overridden {
//		log.Printf("%s comes from %s, replacing %v", override.Name, override.Standing, override.Replaced)
//	}
func (r *Repository) Audit() (reports.Audit, error) {
	documentation, err := r.Documentation()
	if err != nil {
		return reports.Audit{}, err
	}

	report := reports.Audit{Overridden: slices.Clone(r.overrides)}
	roots := r.Roots()
	called := make(map[string]struct{}, len(r.settings.funcs))

	for _, asset := range documentation.Assets {
		for _, tpl := range asset.Templates {
			for _, function := range tpl.Funcs {
				called[function] = struct{}{}
			}

			if tpl.Empty {
				report.Empty = append(report.Empty, tpl.Name)
			}

			if tpl.Dynamic {
				report.Dynamic = append(report.Dynamic, tpl.Name)
			}

			if len(tpl.UsedBy) == 0 && !slices.Contains(roots, tpl.Name) {
				report.Unused = append(report.Unused, tpl.Name)
			}
		}
	}

	for _, function := range slices.Sorted(maps.Keys(r.settings.funcs)) {
		if _, reached := called[function]; !reached {
			report.UnusedFuncs = append(report.UnusedFuncs, function)
		}
	}

	slices.Sort(report.Unused)
	slices.Sort(report.Empty)
	slices.Sort(report.Dynamic)

	return report, nil
}
