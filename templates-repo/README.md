# templates-repo

The templates repository is a cache for collecting golang text templates.

It compiles a set of Go text templates into an immutable repository. It reads assets from an
`io/fs.FS`, a directory or a `[]byte`, parses them in a single pass so that every template can
call every other, and returns them ready to execute from `Get`.

A code generator ships a default set of templates and may allow its users to override some of them.
The repository holds that set, and takes overlay options to override from further sources.

## Features

* expose a namespace for the whole tree of templates, including `{{ define }}` macros
* safe for a concurrent use
* automatic check and dependencies resolution
* support for composition and overrides with overlays
* cache compiled templates from assets on a file system, possibly embedded

**Experimental features**

* self-check audit: unused or empty templates, likely errors
* generates documentation for your templates from data introspection and comments in source
* instruments templates for test coverage reporting

The types these three produce live in the [`reports`](reports/README.md) sub-package, so the main API stays
to what executing templates needs.

## Getting started

```cmd
go get github.com/go-openapi/codegen
```

```go
import repo "github.com/go-openapi/codegen/templates-repo"
```

Add the sub-package only to describe a repository, never to run one:

```go
import "github.com/go-openapi/codegen/templates-repo/reports"
```

## Building

A repository is built once, from sources declared as options, and is sealed from then on.

All dependencies and templates are resolved eagerly: any compilation or dependency error is known at build time.

```go
templates, err := repo.New(
    repo.FromFS(assets, ""),                                      // load from an io/fs.FS
    repo.FromDir("./mytemplates", ""),                            // load from local disk
    repo.FromTemplate("addon", []byte("{{ printf \"%#v\" . }}")), // load from []byte
    repo.WithFuncMap(funcs),
)

tpl, err := templates.Get("validationPrimitive")
err = tpl.Execute(w, data)
...
```

Sources are read in the order they are declared, so a template declared twice comes from the last
one. Use `Audit` to report on overridden templates.

Each source decides what it reads. `SkipDirectories` leaves a directory unread, along with
everything below it:

```go
repo.FromFS(assets, "", repo.SkipDirectories("contrib"))  // the alternate sets are stacked, not loaded wholesale
```

It matches a directory by its own name, the last segment of its path, at any depth: `contrib` at
the root and `server/legacy/contrib` alike. It matches neither a path nor a template name, since
it decides what a source reads before anything is named. Nothing is skipped by default, and what
one source skips says nothing about any other.

`New` reports an error when it cannot build the set of templates.
This may be because of an unreadable source, a template that does not parse, a reference that
reaches nothing, a root that no source declares, or an override that would be silently ignored.

> That last one needs a word. `text/template.Template.AddParseTree` keeps the existing definition
> when the new parse tree is empty, so an override holding nothing but white space and comments
> would leave the earlier definition in place.
> `New` rejects it rather than let the override pass unnoticed.
>
> To override a template with one that renders nothing, give it an action to run:
>
> ```gotmpl
> {{ "" }}
> ```

## Concurrency

A `Repository` is immutable and lock-free. It is safe for concurrent use, as are the returned templates.

`Clone` is the only way to derive a new `Repository` from one already built. It only reads the
repository it derives from, so cloning is safe while that repository is in use.

New sources may be added at cloning time. The clone rebuilds the entire set, and may error.

```go
patched, err := repo.Clone(repository, repo.FromTemplate("model.gotmpl", mine))
```

**The test coverage counters are the exception to immutability**: the templates are frozen, the
counters are not. They are atomic, so rendering in parallel needs no lock.

## A namespace for your templates

### Addresses and names

A template is known by three strings: the asset path it was read from, the address it was declared
at, and the name it answers to. Only the name executes it, and the address reaches the same
template through `Lookup`, so pick whichever your caller already holds.

```
asset path   server/parameter.gotmpl        the file it came from
address      server/parameter               where its author declared it
name         serverParameter                what it answers to
```

---

The **address** is the original path to the template source, where its author declared it.

An asset is addressed at its own path, extension trimmed. Separators are normalized to `/`, so a
caller may write `server\parameter` on Windows and get the same address as everyone else.
A `define` statement is addressed under it like so:

```
server/parameter.gotmpl                     ->  server/parameter
{{ define "bind-primitive" }} within it     ->  server/parameter/bind-primitive
```

`Lookup` and `MustLookup` retrieve a template by address:

```go
tpl, err := templates.Lookup("server/parameter")
```

---

The **name** is the identity `Get` takes. It camel-cases the address, `/` counting as a word
boundary like any other:

```
server/parameter                ->  serverParameter
server/parameter/bindPrimitive  ->  serverParameterBindPrimitive
```

```go
tpl, err := templates.Get("serverParameter")
```

Four methods translate between the three:

| | |
|---|---|
| `NameOf("server/parameter")` | the name an address answers to |
| `AddressOf("serverParameter")` | the address behind a name |
| `AssetOf("serverParameter")` | the file a name was read from |
| `Addresses()` | every address and name the repository holds |

`TemplateName` computes a name without a repository, for a caller that has to choose its sources
by name before there is a repository to ask.

The godoc carries a runnable example for each of these, over a two-template repository. Start
there if the three words have not settled yet.

## Resolving relative references

`{{ template "x" }}` means something relative to where an author wrote it. Two sets of templates
can therefore each have a `body` macro, and each reaches its own.

Take this tree:

```
server/fred.gotmpl                   declares  {{ define "inner-macro" }}
server/claude.gotmpl
server/operations/operation.gotmpl
client/swagger.gotmpl
```

A reference is looked up outward from the template holding it. Starting at `server/claude.gotmpl`,
that means: templates under `server/claude` itself, then under `server/`, then under the root. At
each step two things can match, and the first match wins:

* a template **addressed** under that step, named by recasing its address relative to that step
* a **define** declared by an asset sitting directly in that step, named as its author wrote it

So `server/claude.gotmpl` reaches four things four ways:

```gotmpl
{{ template "inner-macro" }}          {{/* a define of a neighbour, by its own name */}}
{{ template "fred" }}                 {{/* server/fred, relative to server/         */}}
{{ template "operationsOperation" }}  {{/* server/operations/operation, relative    */}}
{{ template "serverFredInnerMacro" }} {{/* anything at all, by its name             */}}
```

From `client/swagger.gotmpl`, only the last one works. `client/` encloses none of those templates,
so nothing there is in reach except by name. A define never travels beyond the directory holding
it, so one set cannot capture another set's macro.

The repository reports an error for two situations rather than guessing:

* one name matching both a template addressed under a level and a define of that level
* two assets of a directory declaring the same bare name

The build resolves every reference once, writing the name it addresses into the parse tree, so
nothing is resolved again while a template runs.

### Scoping a run

A generator ships every template it may ever need, and a single run might need only a part of them.

Scope the repository so that a run carries only the templates it executes.

```go
client, err := repo.Clone(repository, repo.WithRoots("clientClient", "model")) // resolves dependencies from these roots
```

The repository then holds the roots and whatever they reach, and nothing else.

A root is a **name**, the identity `Get` takes, and never the address a template was declared at.
Scoping is the one place that accepts names alone: `Lookup` takes either, so convert with `NameOf`
if your caller holds addresses.

```go
scoped, err := repo.Clone(repository, repo.WithRoots(repository.NameOf("client/client")))
```

> A root that no source declares is an error:
> a filter naming a template that does not exist would build a repository that quietly generates nothing.
> Naming an address instead of a name reports that same error, and says so.

#### A scope only binds the functions it calls

Every asset is still read and parsed, so a source that does not parse is an error whether the roots
prune it away or not. The funcmap is the exception. A repository scoped to a few roots only has to
bind what its own templates call, so you can assemble a set from several parts, name the roots of
the parts you want, and pass `WithFuncMap` of those parts alone.

```go
// server.gotmpl calls serverFunc, which nothing here binds - the roots prune it away
client, err := repo.New(
    repo.FromFS(everything, ""),
    repo.WithFuncMap(clientFuncs),
    repo.WithRoots("client"),
)
```

The check runs on whole assets. A `define` the roots prune away still owes its functions when the
repository keeps another template of the same file.

#### Widening a scope

`WithRoots` sets the scope. `WithExtraRoots` widens it. Both take names.

> `WithExtraRoots` changes nothing on a repository that already keeps everything,
> so a caller adding a template writes the same call either way.

```go
client, _ := repo.Clone(repository, repo.WithRoots("clientClient"))

// "mine" is reachable from no root, so the scope has to admit it
mine, err := repo.Clone(client,
    repo.FromTemplate("mine.gotmpl", body),
    repo.WithExtraRoots("mine"),
)
```

Against a repository that keeps everything, the second call changes nothing, since `mine` is
already there. Write it the same way either way, without checking which kind of repository you
hold.

`Roots` returns the current scope, and is empty when the repository kept everything it read.

## Composition

A package shipping templates publishes **sources**, not a repository.

> A repository can be built only when everything it refers to is there.

<!-- internal code
// what the package exports, knowing nothing of where it lands
func Sources(opts ...repo.SourceOption) repo.Option {
    return repo.Sources(
        repo.FromFS(templates, "", opts...),
        repo.FromFS(filepaths, "paths", opts...),
    )
}
-->

```go
// assembling two repos exposed by other packages
templates, err := repo.New(
    genmodels.Sources(repo.Rebased("models")),
    genclient.Sources(repo.Rebased("client")),
    repo.FromDir("./mine", ""),
)
```

`Rebased` mounts a source under a base, on top of wherever it already mounts, so the package that
ships it chooses none of that. Mounting moves every address under the mount point, and the
references between templates move with them: a set resolves the same wherever it lands, and two
sets that each declare a `body` macro no longer collide.

One caveat. A template that calls into another set names the address that set was mounted at, so a
package whose templates do that has an expected mount point. Document it alongside the root
templates the package exports and the data they are executed on.

`Rebase`, `Merge` and `Coalesce` do the same to repositories that are already built. `Merge` lets
the last repository to declare an address win, `Coalesce` the first, and each combines the func
maps the same way.

## Experimental features

These describe a repository rather than run it, and their types live in the
[`reports`](reports/README.md) sub-package:

```go
import (
    repo "github.com/go-openapi/codegen/templates-repo"
    "github.com/go-openapi/codegen/templates-repo/reports"
)
```

Ten types describe a repository; executing its templates needs none of them. A program that only
renders imports `repo` alone.

### Self-healthcheck

`Audit` reads the assets again and returns a `reports.Audit`, listing what compiles and runs but
still deserves a look:

* templates that more than one asset declared, and which definition stands
* templates that no other template calls
* templates that render nothing
* templates that call a function carried by their data
* funcmap entries that no template calls

None of it is an error. `New` rejects what it cannot resolve, so everything the audit reports
already compiles and runs.

#### Overrides and shadowed templates

Stacking sources is how a set replaces what it needs to, so an override is intended far more often
than not and is never an error. It is still worth seeing:

```go
report, err := repository.Audit()
if err != nil {
    return err
}

for _, override := range report.Overridden {
    log.Printf("%s comes from %s, replacing %v",
        override.Name, override.Standing, override.Replaced)
}
```

Nothing else reveals a set that replaced a template by accident.

#### Unused templates and functions

`Unused` lists the templates that no other template calls. `UnusedFuncs` lists the func map
entries that no template calls.

Neither is a verdict. Nothing calls a generator's entry points either, so a repository that keeps
every template it read cannot distinguish an entry point from a template that outlived its
callers.

Scope the repository with `WithRoots` and `Unused` comes back empty, since everything left is a
root or is reached from one. To find dead templates, audit the unscoped repository and subtract
the entry points you know about; what remains is worth a look.

`UnusedFuncs` reads the same way. A generator that gives its templates a general-purpose library
will find most of it unused, which is expected rather than a defect.

#### Spot dynamic calls

The `call` builtin invokes a function carried by the data, and only at execution time. A
repository resolves everything else before a run starts, so neither the audit nor the
documentation can report what these calls reach.

`Dynamic` lists the templates that use `call`, which at least bounds the blind spot.

A function that no funcmap provides never reaches the audit: templates are parsed against the
funcmap, so calling a function nothing provides fails the build. In a repository scoped with
`WithRoots`, that check covers the assets the roots keep, so a pruned template may call a function
nothing provides.

### Self-documentation

Templates are part of the interface a generator exposes, so a repository can document them: the
comments on each template, the data paths it reads, the functions it calls, and the templates it
calls with the data passed to each.

```go
err = repository.Dump(w)                             // markdown, the common way to ask
documentation, err := repository.Documentation()     // or the reports.Documentation behind it
```

`reports.Dump` renders a documentation on its own, which suits a document built once and laid out
several ways:

```go
err = reports.Dump(w, documentation, reports.WithTemplate(myLayout))
```

The analysis runs on demand rather than when the repository is built, so a caller that only
executes templates does not pay for it. The output is ordered throughout, so the same templates
produce the same document every time, and that document can be committed and checked in CI.

It reports the data as a closure over every branch: what the data must be able to answer, not a
list of what it must hold.

### Test coverage

```go
counting, err := repo.Clone(repository, repo.WithCoverage("example.com/gen/templates"))
...
err = counting.Coverage().Flush(profile)
```

`go tool cover -html` renders the result. A line that never ran appears in the profile at zero,
and a line holding nothing but a `define`, an `end` or an `else` is left out, so it greys out the
way a Go declaration does. Instrumentation has to be set when the repository is built, because the
templates that execute must be the ones holding the counters.

Two branches on one line share one counter: `{{if .A}}x{{else}}y{{end}}` reports the line covered
when either ran. Telling them apart needs column positions, which the template parser does not
report.

## Design notes

A record of the decisions that were not obvious.

### Why a repository retains its sources rather than its compiled state

`Clone` re-parses everything and copies no compiled object. An override therefore reaches the
templates that already referred to it, which earlier designs got wrong in both directions: one
mutated a shared cache and contaminated every holder, the other isolated so thoroughly that the
override never took effect.

The price is a full parse per derivation. Derive for the settings of a program, decided once, not
per operation.

### Why references are rewritten into the parse trees

Authors write names relative to where they are. `text/template` executes against one flat
namespace. The build reconciles the two by resolving each reference once and writing the resolved
name into the node.

The alternative, a namespace per template with its dependencies grafted in, costs a copy per
dependency. Rewriting costs one pass: about 0.1 ms over the go-swagger set, against about 22 ms to
parse it.

This works only because the scope is lexical. A reference resolves by where its template was
declared, never by who invoked it. Dynamic scope would force namespaces back.

### Why a doubly answered reference is refused

One name can match both a template addressed under a scope and a define of that scope. Picking
either silently sends the reference somewhere the author did not mean.

We tried precedence first, addressed-before-define. It sent a call to the wrong template and the
test suite hung on the recursion that followed. The repository now reports the ambiguity and
leaves the author to rename one of the two.

### Why skipping directories belongs to the source

Which directories to skip describes the file system being walked, not the repository. As a
repository-wide setting it also skipped the same directory name in template sets brought by users,
and an override placed there did nothing at all, silently.

### Why the analysis re-parses the assets

Execution trees drop comments, and their references have already been rewritten to names. Neither
the docstrings nor the names an author typed survive there. Assets are retained anyway, so the
analysis reads them again and maps what it finds back onto the addresses the repository holds.

### Why composition rebuilds

Re-addressing compiled trees would run about six times faster than parsing again. `Rebase` and
`Merge` still rebuild from the retained assets, because relative resolution then costs nothing
extra: a set that resolved on its own resolves identically once rebased, and no reference needs
recomputing.

Take the faster path only if assembling ever lands on a hot path.
