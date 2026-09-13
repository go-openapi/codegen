// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Package repo compiles a set of Go text templates into an immutable repository.
//
// A code generator ships a default set of templates and lets its users override some of them.
// This package holds that set. It reads assets from an [io/fs.FS], a directory or a byte slice,
// parses them in a single pass so that every template can call every other,
// and returns them ready to execute from [Repository.Get].
//
// # Usage
//
// A repository is built once, from sources declared as options:
//
//	repository, err := repo.New(
//		repo.FromFS(assets, ""),
//		repo.WithFuncMap(funcs),
//	)
//	if err != nil {
//		return err
//	}
//
//	tpl, err := repository.Get("validationPrimitive")
//	if err != nil {
//		return err
//	}
//
//	err = tpl.Execute(w, data)
//
// A repository is immutable once [New] returns. To change the set, derive a new repository
// with [Clone]:
//
//	patched, err := repo.Clone(repository, repo.FromTemplate("validation/primitive", mine))
//
// [Clone] re-parses every asset, so a template that calls the overridden one calls the new
// definition. The two repositories share no state.
//
// Every error this package reports matches [ErrTemplateRepo] and wraps its cause,
// so a caller may also match a parse error or an [io/fs] error with [errors.Is] and [errors.As].
//
// # Naming
//
// A template is known by three strings: the asset path it was read from, the address it was
// declared at, and the name it answers to. The address is the asset path with the extension
// trimmed, slash-separated and otherwise untouched. The name is that address recased, "/" counting
// as a word boundary like any other.
//
//	validation/primitive.gotmpl  ->  address validation/primitive,  name validationPrimitive
//	server/parameter.gotmpl      ->  address server/parameter,      name serverParameter
//	model.gotmpl                 ->  address model,                 name model
//
// [Repository.Get] takes a name and [Repository.Lookup] takes an address. [Repository.NameOf] maps
// an address to a name, [Repository.AddressOf] maps it back, and [Repository.Addresses] iterates
// over both. [TemplateName] computes a name before any repository exists, which a caller needs
// when the templates it is about to declare are themselves chosen by name.
//
// A "define" statement declares a further template, addressed under the asset that holds it:
//
//	server/fred.gotmpl holding {{define "inner-macro"}}
//	  ->  address server/fred/inner-macro,  name serverFredInnerMacro
//
// Paths are slash-separated whatever the platform, so a caller may write "server\parameter" on
// Windows and reach the same template as everyone else.
//
// Two assets of the same source declaring one name is an error, since no source is read after the
// other. Across sources, the last declaration wins.
//
// # Overriding
//
// Sources are read in the order they are declared, and the last declaration of a name wins.
// There is no other precedence rule: a source cannot mark a template as final.
//
// Stacking whole sets of templates is a file system concern rather than a repository one.
// Merge the sets into one [io/fs.FS] with [github.com/go-openapi/swag/fileutils.NewOverlayFS],
// then pass the result to [FromFS].
//
// [SkipDirectories] attaches to one source, not to the build. Skipping "internal" in your own
// assets leaves an "internal" directory in a set someone else brings fully readable.
//
// It matches a directory by its own name, the last segment of its path, at any depth. It matches
// neither a path nor a template name: it decides what a source reads, before anything is named.
//
// [Repository.Audit] lists every name that more than one asset declared, with the definition that
// stands and the ones it replaced. Run it where a build can fail, so that a contrib set which
// shadows a template by accident is caught before it ships. It returns a
// [github.com/go-openapi/codegen/templates-repo/reports.Audit], which covers more than overrides.
//
// # Scoping
//
// A generator ships every template it may ever need, and one run uses a fraction of them.
// [WithRoots] keeps the named templates and everything they call, and prunes the rest:
//
//	client, err := repo.Clone(repository, repo.WithRoots("clientClient", "model"))
//
// A root is a name, the identity [Repository.Get] takes, and never the address a template was
// declared at. Scoping is the one place that accepts names alone: [Repository.Lookup] takes
// either, so a caller holding addresses converts them with [Repository.NameOf] first.
// Naming an address reports an error rather than building an empty repository.
//
// A pruned template is gone from the repository: [Repository.Names] does not list it,
// [Repository.Documentation] does not describe it, and [Repository.Coverage] does not count it.
// Every asset is still read and parsed, because a template only announces its name once parsed.
// The assets are retained whole, so a later [Clone] with [WithExtraRoots] widens the scope again.
//
// A scope only has to bind the functions its own templates call, so a pruned template may call a
// function no func map provides. Scope a repository and give it [WithFuncMap] of the parts it
// keeps, and the parts it drops cost it nothing.
//
// [Repository.Roots] returns the current scope, and is empty when the repository kept everything
// it read.
//
// # Assembling
//
// A repository may itself be a source. [FromRepository] reads what one holds and mounts it at a
// chosen point, so two sets written independently are assembled without either knowing about the
// other:
//
//	templates, err := repo.New(
//		repo.FromDir("./scaffolding", ""),
//		repo.FromRepository(modelTemplates, "models"),
//		repo.FromRepository(serverTemplates, "server"),
//	)
//
// Mounting moves every address under the mount point, and the references between templates move
// with them, so a set that resolved on its own resolves the same mounted. Two sets that each
// define a macro called "header" no longer collide, because their addresses now differ.
//
// A package that ships templates should therefore export sources rather than a repository.
// A scaffolding that calls into the parts it is assembled with cannot be built on its own,
// so those parts have to be sources of the same build:
//
//	// what the package exports, knowing nothing of where it lands
//	func Sources(opts ...repo.SourceOption) repo.Option {
//		return repo.Sources(
//			repo.FromFS(templates, "", opts...),
//			repo.FromFS(filepaths, "paths", opts...),
//		)
//	}
//
// One caveat is worth stating. A template that calls into another set names the address that set
// was mounted at, so a package whose templates do that has an expected mount point.
// Document it alongside the templates the package exports and the data they are executed on.
// A set that calls into nothing may be mounted anywhere.
//
// [Rebase], [Merge] and [Coalesce] do the same to repositories that are already built.
// [Rebase] moves every address under a base. [Merge] lets the last repository to declare an
// address win, and [Coalesce] lets the first. All three need each part to build on its own.
//
// # Documentation and audit
//
// Templates are part of the interface a generator exposes, so a repository can document them.
// [Repository.Documentation] reads the assets again, comments included, and reports:
//
//   - the comments documenting each template, attached as a Go author expects, a comment group
//     placed immediately before a "define" statement documenting that template
//   - the data paths each template reads, and which of them it reaches through the root
//   - the functions it calls, and the templates it calls, with the data passed to each
//
// [Repository.Dump] renders that model as markdown:
//
//	err = repository.Dump(w)
//
// The analysis runs on demand rather than when the repository is built, so a caller that only
// executes templates does not pay for it. The output is ordered throughout, so the same templates
// produce the same document every time, and that document can be committed and checked in CI.
//
// The types these methods return live in a package of their own,
// [github.com/go-openapi/codegen/templates-repo/reports]. Describing a repository takes ten types;
// executing its templates takes none of them, so a program that only renders imports neither the
// documentation model nor the audit report. That package also renders a document on its own,
// through reports.Dump, which suits a document built once and laid out several ways.
//
// # Coverage
//
// [WithCoverage] instruments a repository to count the lines of its templates that execute.
// It has to be set when the repository is built, because the templates that run must be the ones
// holding the counters. [Clone] carries the setting over, so a plain repository clones into an
// instrumented one:
//
//	counting, err := repo.Clone(repository, repo.WithCoverage("example.com/gen/templates"))
//
// The prefix is prepended to the path of every asset in the profile. go tool cover resolves the
// file a profile names by asking go list, so the paths have to read as an import path of a
// package that exists. That is why the prefix is required.
//
// [Repository.Coverage] returns a profile in the format go test writes, which go tool cover
// renders as html. A line that never ran appears at zero rather than being absent. A line holding
// nothing but a define, an end or an else is left out, so it greys out the way a Go declaration
// does.
//
// # Concurrency
//
// A repository is immutable and safe for concurrent use, as are the [Template] values it returns.
// [Clone] only reads the repository it derives from.
//
// The coverage counters are the one part that changes after a build. They are atomic, so a
// generator rendering templates in parallel needs no lock.
package repo
