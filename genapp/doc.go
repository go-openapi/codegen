// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Package genapp renders templates into formatted Go files.
//
// A code generator holds a set of templates and, for each file it produces, executes one of them and
// formats the result. [GoGenApp] is that loop:
//
//	templates, err := repo.New(
//		repo.FromFS(assets, ""),
//		repo.WithFuncMap(golang.FuncMap(mangling.MakeGoMangler())),
//	)
//	if err != nil {
//		return err
//	}
//
//	app, err := genapp.New(
//		genapp.WithTemplates(templates),
//		genapp.WithOutputPath("./generated"),
//		genapp.WithFormatOptions(
//			formatting.WithImportGroups("github.com/go-openapi", baseImport),
//		),
//	)
//	if err != nil {
//		return err
//	}
//
//	if err := app.RenderFile("models/pet.go", "modelValidator", pet); err != nil {
//		return err
//	}
//
// [GoGenApp.Render] writes to an [io.Writer] and [GoGenApp.RenderFile] writes a file under the
// output path, creating the directories it needs.
//
// Where the templates come from, what funcmap they run with and which of them a run reaches are
// settled by [github.com/go-openapi/codegen/templates-repo], and this package re-exports none of
// it: build the repository, then hand it over with [WithTemplates].
//
// # Formatting
//
// Rendered Go goes through [github.com/go-openapi/codegen/formatting], which prunes the imports
// nothing uses, groups the rest and prints in gofmt style. It never resolves a missing import and
// never runs the go command, so a template that forgets an import produces a file that does not
// compile rather than one that differs from machine to machine.
//
// [GoGenApp.RenderFile] formats a target ending in ".go" and copies anything else through. Pass
// [WithSkipFormatFunc] to decide differently, or [WithSkipFormat] to write every target unformatted,
// which is worth doing when a template is misbehaving and the parse error hides the output.
//
// # Writing outside the output path
//
// A generator names its targets after the models and operations of a spec, so the spec decides what
// [GoGenApp.RenderFile] writes. RenderFile therefore refuses an absolute target, and one that climbs
// out of the output path with "..", whether or not the caller asks to be confined.
//
// Writes go through [os.Root], which checks each symbolic link as it walks the path. RenderFile
// removes a link standing at the target instead of following it, so the file it pointed at keeps its
// content, and refuses a link on the way to the target. Renaming replaces a name rather than the
// file behind it, so another hard link to the target keeps its own content. A directory, a device,
// a socket or a named pipe at the target is refused rather than overwritten.
//
// [WithRoot] widens the boundary from the output path to a directory above it, for a generator
// writing into several directories of one tree:
//
//	app, err := genapp.New(
//		genapp.WithTemplates(templates),
//		genapp.WithOutputPath("./gen/models"),
//		genapp.WithRoot("./gen"),
//	)
//
// Two things sit outside this. [GoGenApp.TidyModule] runs the go command, which writes go.mod and
// go.sum itself, and no root reaches into another process. Reads run unconfined too:
// [GoGenApp.PackagePath] and [GoGenApp.EnclosingModule] walk up from the output path looking for a
// go.mod, and that go.mod usually sits above any root worth setting.
//
// [os.Root] confines path resolution and no more. It does not stop traversal of a bind mount, a
// /proc special file or a device file.
//
// # Where the code lands
//
// A generator has to write the imports that reach the code it produces, and that means knowing the
// import path of the tree it is writing into. [GoGenApp.PackagePath] answers it by reading the
// go.mod above the output path:
//
//	module example.com/petstore  declared in /src/petstore/go.mod
//	output path                  /src/petstore/gen/models
//	PackagePath                  example.com/petstore/gen/models
//
// [GoGenApp.ModuleRequired] answers the other half: whether the output path sits outside every
// module, and so needs a go.mod of its own before anything there can be built.
//
// # Modules
//
// [GoGenApp.InitModule] writes a go.mod for the generated tree, as "go mod init" would, without
// running it:
//
//	err := app.InitModule(
//		genapp.WithModulePath("example.com/petstore/gen"),
//		genapp.WithRequire("github.com/go-openapi/strfmt", "v0.24.0", false),
//	)
//
// [GoGenApp.TidyModule] runs "go mod tidy", and is the one thing here that needs a Go toolchain:
//
//	err := app.TidyModule(ctx, genapp.WithTidyGoVersion("1.25.0"))
//
// Everything else runs the go command never and reads the environment never, so a generated tree
// can be laid down and formatted on a machine with no Go installed. Resolving the versions a module
// ends up with is the exception, because it means walking the module graph and the checksum
// database, and reproducing that would mean reproducing the go command.
//
// # Concurrency
//
// A [GoGenApp] holds no state between calls, so [GoGenApp.Render] and [GoGenApp.RenderFile] may run
// concurrently. Each render borrows its buffer from
// [github.com/go-openapi/swag/pools/shared] and gives it back before returning, so a generator
// writing a few hundred files recycles a handful of buffers rather than allocating one per file.
package genapp
