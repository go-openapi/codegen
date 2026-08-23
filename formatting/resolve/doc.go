// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Package resolve reads the name each imported package declares.
//
// [github.com/go-openapi/codegen/formatting.Format] never loads a package, so it cannot name one
// whose package clause does not follow its import path: "github.com/json-iterator/go" declares
// jsoniter and "github.com/prometheus/client_model/go" declares io_prometheus_client. It keeps such
// an import rather than delete one the code may be using, and lists it in the report.
//
// [Names] loads those packages and answers outright. Feed it what the report could not settle, and
// pass the map back:
//
//	report, err := formatting.Format(out, rendered)
//	if err != nil {
//		return err
//	}
//
//	if report.HasImportsInDoubt() {
//		names, err := resolve.Names(ctx, report.PathsInDoubt(), resolve.WithDir(moduleDir))
//		if err != nil {
//			return err
//		}
//		// format again with formatting.WithResolvedImports(names)
//	}
//
// # Resolve once, not on every run
//
// The answer depends on the dependencies and on nothing else — not on the machine, the module cache
// or the build list. So run this once, commit the map, and every generator run anywhere agrees. That
// is the whole reason [github.com/go-openapi/codegen/formatting] does not resolve imports itself:
// a generator that searched the build list would produce different files on different machines.
//
// Rerun it when a dependency is added, or when one renames its package. Nothing detects that for you.
//
// # What it does not check
//
// [Names] answers for any path go list can find, an internal package of another module included.
// Nothing here asks whether the importing file may legally use it: go build rejects such an import
// and names the file and the line.
//
// # What it costs
//
// [Names] runs "go list" through golang.org/x/tools/go/packages, so it needs the go toolchain and a
// module that requires the paths being asked about. It is far slower than formatting, which is why it
// belongs in a separate step rather than inside [github.com/go-openapi/codegen/formatting.Format].
package resolve
