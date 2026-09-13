// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/go-openapi/codegen/templates-repo/internal/cover"
	"github.com/go-openapi/codegen/templates-repo/reports"
)

// namespaceName is the name of the template holding the namespace shared by a repository.
//
// No asset can produce that name: [TemplateName] recases path segments, and none of them yields
// the angle brackets, so no template can take the place of the namespace.
const namespaceName = "<repository>"

// Repository is a set of compiled templates, resolved against one another and sealed.
//
// [New] builds one from the sources given as options and [Clone] derives one from another. There
// is no other constructor, and no method changes a repository once it is built.
//
// # Usage
//
// A repository reads its sources once, when it is built, and retains their content. Everything
// else follows from that:
//
//   - templates share a single namespace, so any of them may refer to any other by name
//   - [Clone] re-reads nothing and re-parses everything, so a template added by a clone is seen
//     by the templates that were already there
//   - the file system a repository was built from is not retained, and is never read again
//
// A clone therefore costs a full rebuild. Use it for the settings of a program, decided once, and
// not once per operation.
//
// # Concurrency
//
// A repository is immutable, holds no lock, and is safe for concurrent use. [Clone] only reads its
// source, so it is safe to clone a repository other goroutines are executing.
type Repository struct {
	namespace    *template.Template
	declarations map[string]declaration
	coverage     *cover.Profile
	names        []string
	assets       []asset
	overrides    []reports.Override
	resolutions  map[string]map[string]string
	layers       int
	settings     options
}

// declaration records where a template comes from.
type declaration struct {
	address   string
	assetPath string
	layer     int
}

// New builds a repository from the sources and settings given as options.
//
// Sources are read in the order they are declared, and a template declared by several of them
// comes from the last one. New returns an error for an unreadable source, a template that fails to
// parse, a template referring to one no source declares, and an override that would silently not
// replace what it overrides.
//
// Declaring no source at all builds an empty repository, and is not an error.
//
// Example:
//
//	repository, err := repo.New(
//		repo.FromFS(assets, ""),
//		repo.FromDir("./mytemplates", ""),
//		repo.WithFuncMap(funcs),
//	)
func New(opts ...Option) (*Repository, error) {
	settings, err := makeOptions(opts)
	if err != nil {
		return nil, err
	}

	assets, layers, err := settings.resolveSources(0)
	if err != nil {
		return nil, err
	}

	return build(assets, layers, settings)
}

// Clone builds a repository from the assets and settings of another one, with opts applied on top.
//
// Clone reads no source of source again. It carries over the content retained when source was
// built and appends whatever opts declares. Everything is parsed and resolved afresh, so a
// template opts overrides reaches every template referring to it, and the two repositories share
// nothing.
//
// source is left untouched, and other goroutines may execute it while Clone runs. A nil source
// returns an error.
//
// Example:
//
//	// the same templates, with one of them replaced
//	patched, err := repo.Clone(repository, repo.FromTemplate("model", myModel))
func Clone(source *Repository, opts ...Option) (*Repository, error) {
	if source == nil {
		return nil, fmt.Errorf("cannot clone a nil repository: %w", ErrTemplateRepo)
	}

	settings := source.settings.derive().apply(opts)
	if settings.err != nil {
		return nil, settings.err
	}

	added, layers, err := settings.resolveSources(source.layers)
	if err != nil {
		return nil, err
	}

	return build(slices.Concat(source.assets, added), layers, settings)
}

// Get returns the template registered under a name.
//
// A template is known by three strings, and this takes the third:
//
//	asset path   server/parameter.gotmpl   the file it was read from
//	address      server/parameter          where it was declared, never recased
//	name         serverParameter           what it answers to, and what Get takes
//
// Get returns an error when no source declares that name. The [Template] it returns resolves the
// templates it refers to in this repository.
func (r *Repository) Get(name string) (Template, error) {
	if _, declared := r.declarations[name]; !declared {
		return Template{}, fmt.Errorf("template %q is not declared in this repository: %w", name, ErrTemplateRepo)
	}

	return Template{tpl: r.namespace.Lookup(name)}, nil
}

// Lookup returns the template declared at an address.
//
// An address is the path a template was declared at, slash-separated and never recased. Lookup
// takes one and [Repository.Get] takes a name. Use whichever you already hold.
//
// Leave the extension on or trim it: the asset path a template was read from addresses it too.
//
// Example:
//
//	tpl, err := repository.Lookup("server/parameter")
func (r *Repository) Lookup(address string) (Template, error) {
	clean, err := cleanAssetName(address)
	if err != nil {
		return Template{}, err
	}

	key := TemplateName(clean, r.settings.extensions...)
	if _, declared := r.declarations[key]; !declared {
		return Template{}, fmt.Errorf("no template is declared at %q: %w", address, ErrTemplateRepo)
	}

	return Template{tpl: r.namespace.Lookup(key)}, nil
}

// MustLookup returns the template declared at an address, and panics when there is none.
//
// Use it for an address hardcoded in the program, and [Repository.Lookup] for one a caller
// supplies.
func (r *Repository) MustLookup(address string) Template {
	tpl, err := r.Lookup(address)
	if err != nil {
		panic(err)
	}

	return tpl
}

// MustGet returns the template registered under a name, and panics when there is none.
//
// Use it for a name hardcoded in the program, and [Repository.Get] for one a caller supplies.
func (r *Repository) MustGet(name string) Template {
	tpl, err := r.Get(name)
	if err != nil {
		panic(err)
	}

	return tpl
}

// Has reports whether a name is declared in this repository.
func (r *Repository) Has(name string) bool {
	_, declared := r.declarations[name]

	return declared
}

// Names iterates over the names declared in this repository, in lexical order.
func (r *Repository) Names() iter.Seq[string] {
	return slices.Values(r.names)
}

// Addresses iterates over what this repository declares, address first, name second, ordered by
// name.
func (r *Repository) Addresses() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for _, name := range r.names {
			if !yield(r.declarations[name].address, name) {
				return
			}
		}
	}
}

// AddressOf returns the address a name is declared at, and whether it is declared at all.
//
// It reverses [Repository.NameOf].
func (r *Repository) AddressOf(name string) (string, bool) {
	declared, found := r.declarations[name]

	return declared.address, found
}

// Roots returns the names this repository is scoped to, in the order they were given to
// [WithRoots].
//
// A build without [WithRoots] keeps every template it read, and Roots then returns an empty slice.
// Roots reports the scope and nothing acts on it. To add a template to a repository you did not
// build, use [WithExtraRoots]: it widens a scope when there is one and changes nothing when there
// is not.
func (r *Repository) Roots() []string {
	return slices.Clone(r.settings.roots)
}

// AssetOf returns the path of the asset that declares a name, and whether it is declared at all.
//
// The path is the asset's, once mounted, and the name was derived from it.
func (r *Repository) AssetOf(name string) (string, bool) {
	declared, found := r.declarations[name]

	return declared.assetPath, found
}

// NameOf returns the name a template declared at an address answers to.
//
// NameOf recases and does not look anything up, so an address nothing declares still yields the
// name it would have. Call [Repository.Has] to find out whether that name is declared. The asset
// path addresses a template too, since the extension is trimmed either way:
// NameOf("server/parameter.gotmpl") and NameOf("server/parameter") both return serverParameter.
//
// It is a method because [WithExtensions] settles which extensions are trimmed. [TemplateName]
// answers the same question before a repository exists.
//
// It reverses [Repository.AddressOf] and it is idempotent on the names it produces, so you may
// hand one back to it: NameOf("serverParameter") returns serverParameter. That does not extend to
// a name no address produces, since an inner "define" is addressed under the asset holding it.
func (r *Repository) NameOf(address string) string {
	return r.settings.templateName(address)
}

// build compiles a set of assets into a sealed repository.
//
// Assets are read in order, so a template declared twice keeps its last definition. Every asset is
// parsed before any of them is registered, because [retainedNames] walks the call graph they form
// to decide what the repository keeps, and [register] instruments only what is kept.
//
// A scoped repository parses twice. Only a parsed template yields the call graph the roots are
// followed over, so the first pass skips the check that every function a template calls is bound,
// and [checkFunctions] runs it again over the assets the roots keep. An unscoped repository keeps
// every template it reads, so it parses once, with the check.
func build(assets []asset, layers int, settings options) (*Repository, error) {
	scoped := len(settings.roots) > 0

	parsed, err := parseAssets(assets, settings, scoped)
	if err != nil {
		return nil, err
	}

	space, err := newAddressSpace(parsed.declared)
	if err != nil {
		return nil, err
	}

	// an author writes a reference relative to where the template sits, and a namespace is flat.
	// rewrite resolves every reference once, here, and no template resolves anything while it runs
	unresolved, resolutions, err := space.rewrite()
	if err != nil {
		return nil, err
	}

	byKey := keyedDeclarations(parsed.declared)

	retained, err := retainedNames(byKey, settings.roots)
	if err != nil {
		return nil, err
	}

	if err := reportUnresolved(unresolved, retained); err != nil {
		return nil, err
	}

	if scoped {
		if err := checkFunctions(parsed, retained, settings); err != nil {
			return nil, err
		}
	}

	namespace := template.New(namespaceName).Funcs(settings.funcs)
	if settings.templateOption != MissingKeyBehaviorNone {
		namespace = namespace.Option(string(settings.templateOption))
	}

	var profile *cover.Profile
	if settings.coverage {
		profile = cover.NewProfile(settings.coverPrefix)
	}

	for _, item := range parsed.assets {
		if err := register(namespace, item, retained, profile); err != nil {
			return nil, err
		}
	}

	declarations := parsed.declarationsOf(retained)
	names := slices.Sorted(maps.Keys(declarations))

	return &Repository{
		namespace:    namespace,
		declarations: declarations,
		coverage:     profile,
		names:        names,
		assets:       assets,
		overrides:    parsed.overridesOf(names),
		resolutions:  resolutions,
		layers:       layers,
		settings:     settings,
	}, nil
}

// keyedDeclarations indexes the declarations by the name each template answers to.
func keyedDeclarations(byPath map[string]*declared) map[string]*declared {
	byKey := make(map[string]*declared, len(byPath))
	for _, item := range byPath {
		byKey[item.key] = item
	}

	return byKey
}

// reportUnresolved rejects a repository holding a template that refers to one it cannot address.
//
// Only the templates it keeps are checked, so a set that is incomplete for the runs this scope
// leaves out builds all the same.
func reportUnresolved(unresolved map[string][]string, retained map[string]struct{}) error {
	var missing []string

	for _, name := range slices.Sorted(maps.Keys(unresolved)) {
		if _, keep := retained[name]; !keep {
			continue
		}

		for _, reference := range unresolved[name] {
			missing = append(missing, fmt.Sprintf("%q refers to %q, which it cannot reach", name, reference))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("unresolved template references: %s: %w", strings.Join(missing, ", "), ErrTemplateRepo)
	}

	return nil
}

// parsedAsset is an asset with the templates it declares, before any of them is registered.
type parsedAsset struct {
	item     asset
	declared map[string]*declared
}

// contributes reports whether a repository keeps any of the templates this asset declares.
func (p parsedAsset) contributes(retained map[string]struct{}) bool {
	for _, item := range p.declared {
		if _, keep := retained[item.key]; keep {
			return true
		}
	}

	return false
}

// parsedAssets holds the result of parsing every asset of a repository.
type parsedAssets struct {
	// assets holds the parsed assets, in the order they were read.
	assets []parsedAsset

	// declarations records, per name, the asset the standing definition comes from.
	declarations map[string]declaration

	// declared holds, per address, the definition that stands there.
	declared map[string]*declared

	// declaring records every asset that declared an address, in the order they were read.
	declaring map[string][]string
}

// overridesOf lists the retained names that a later source redeclared.
func (p parsedAssets) overridesOf(names []string) []reports.Override {
	var overrides []reports.Override

	for _, name := range names {
		declaring := p.declaring[p.declarations[name].address]
		if len(declaring) < 2 { //nolint:mnd // one asset declaring a name overrides nothing
			continue
		}

		last := len(declaring) - 1
		overrides = append(overrides, reports.Override{
			Name:     name,
			Standing: declaring[last],
			Replaced: slices.Clone(declaring[:last]),
		})
	}

	return overrides
}

// declarationsOf keeps the declarations of the names a repository retains.
func (p parsedAssets) declarationsOf(retained map[string]struct{}) map[string]declaration {
	declarations := make(map[string]declaration, len(retained))
	for name := range retained {
		declarations[name] = p.declarations[name]
	}

	return declarations
}

// parseAssets parses every asset, and records which definition stands at each address.
//
// An asset declares a template at its own path, plus one per inner "define" statement, addressed
// under it. Each asset is parsed on its own, so [checkCollision] and [checkOverride] see what it
// declares before anything is registered.
func parseAssets(assets []asset, settings options, permissive bool) (parsedAssets, error) {
	parsed := parsedAssets{
		assets:       make([]parsedAsset, 0, len(assets)),
		declarations: make(map[string]declaration, len(assets)),
		declared:     make(map[string]*declared, len(assets)),
		declaring:    make(map[string][]string, len(assets)),
	}

	for _, item := range assets {
		owner := settings.trimmedPath(item.path)

		trees, err := parseAsset(owner, item.data, settings, permissive)
		if err != nil {
			return parsedAssets{},
				fmt.Errorf("could not parse template %q from asset %q: %w: %w", owner, item.path, err, ErrTemplateRepo)
		}

		declaredHere := make(map[string]*declared, len(trees))
		for name, tree := range trees {
			bare := name
			address := addressOf(owner, bare)
			if bare == owner {
				bare = ""
			}

			if err := checkCollision(parsed.declarations, address, item); err != nil {
				return parsedAssets{}, err
			}

			if err := checkOverride(parsed.declared, address, tree, item.path); err != nil {
				return parsedAssets{}, err
			}

			declaredHere[address] = &declared{
				address:   address,
				key:       TemplateName(address),
				owner:     owner,
				bare:      bare,
				assetPath: item.path,
				layer:     item.layer,
				tree:      tree,
			}
		}

		for address, found := range declaredHere {
			parsed.declared[address] = found
			parsed.declarations[found.key] = declaration{
				address: address, assetPath: item.path, layer: item.layer,
			}
			parsed.declaring[address] = append(parsed.declaring[address], item.path)
		}

		parsed.assets = append(parsed.assets, parsedAsset{item: item, declared: declaredHere})
	}

	return parsed, nil
}

// parseAsset parses one asset and returns the templates it declares, keyed by the name each was
// declared under: the path of the asset for the template it holds, and the name a "define"
// statement gives for each of those.
//
// permissive parses under [text/template/parse.SkipFuncCheck], which drops the check that every
// function a template calls is bound and drops nothing else: a syntax error, an undefined variable
// and a name defined twice are still reported. The parser builds the same nodes either way, so
// [build] registers these trees, however parseAsset was called.
//
// Pass the func map even when the check is skipped. The parser reads it a second time, to decide
// whether "break" and "continue" lex as keywords, and [text/template/parse.SkipFuncCheck] leaves
// that read alone.
func parseAsset(owner string, data []byte, settings options, permissive bool) (map[string]*parse.Tree, error) {
	if !permissive {
		tpl, err := template.New(owner).Funcs(settings.funcs).Parse(string(data))
		if err != nil {
			return nil, err
		}

		trees := make(map[string]*parse.Tree, len(tpl.Templates()))
		for _, found := range tpl.Templates() {
			if found.Tree == nil {
				continue
			}

			trees[found.Name()] = found.Tree
		}

		return trees, nil
	}

	trees := make(map[string]*parse.Tree)
	tree := parse.New(owner)
	tree.Mode = parse.SkipFuncCheck

	if _, err := tree.Parse(string(data), "", "", trees, map[string]any(settings.funcs)); err != nil {
		return nil, err
	}

	return trees, nil
}

// checkFunctions reports a function that a template the roots keep calls and no func map binds.
//
// [parseAsset] skipped that check, so that a repository scoped by [WithRoots] does not have to
// bind the functions of the templates it prunes away. Parsing the surviving assets again leaves
// the check, and its message, to text/template. The trees are dropped, since [build] registers
// what [parseAsset] already built.
//
// An asset is checked whole, so a pruned "define" still owes its functions when the repository
// keeps another template of the same asset.
func checkFunctions(parsed parsedAssets, retained map[string]struct{}, settings options) error {
	for _, item := range parsed.assets {
		if !item.contributes(retained) {
			continue
		}

		owner := settings.trimmedPath(item.item.path)
		if _, err := template.New(owner).Funcs(settings.funcs).Parse(string(item.item.data)); err != nil {
			return fmt.Errorf(
				"asset %q holds a template this repository keeps, and calls a function no func map binds: %w: %w",
				item.item.path, err, ErrTemplateRepo)
		}
	}

	return nil
}

// register adds the retained templates of one asset to the namespace.
//
// An asset whose templates were all pruned away contributes nothing, counters included, so
// [Repository.Coverage] never counts a line of a template the repository does not hold.
func register(
	namespace *template.Template,
	parsed parsedAsset,
	retained map[string]struct{},
	profile *cover.Profile,
) error {
	trees := make(map[string]*parse.Tree, len(parsed.declared))
	for _, item := range parsed.declared {
		if _, keep := retained[item.key]; keep {
			trees[item.key] = item.tree
		}
	}

	if len(trees) == 0 {
		return nil
	}

	// the counters go into the trees that run, and [checkOverride] tested emptiness before that
	if profile != nil {
		instrumented := profile.Instrument(parsed.item.path, parsed.item.data, trees)
		trees = instrumented.Trees
		namespace.Funcs(instrumented.Bind())
	}

	for name, tree := range trees {
		if _, err := namespace.AddParseTree(name, tree); err != nil {
			return fmt.Errorf("could not register template %q from asset %q: %w: %w",
				name, parsed.item.path, err, ErrTemplateRepo)
		}
	}

	return nil
}

// checkCollision reports two assets of a single source declaring the same template.
//
// Names are flat, so a template declared twice keeps one definition and loses the other. The order
// the assets are read in settles which. A caller stacking sources chooses that order; a caller
// pointing [FromDir] at a directory does not.
//
// So a redeclaration crossing sources is taken as intended, and one within a single source is
// reported. You ask for an override by declaring a further source, and a directory listing never
// decides one for you.
func checkCollision(declarations map[string]declaration, address string, item asset) error {
	previous, found := declarations[TemplateName(address)]
	if !found || previous.layer != item.layer {
		return nil
	}

	return fmt.Errorf(
		"template %q is declared by assets %q and %q, which come from the same source: "+
			"rename one of them, or declare the overriding one as a further source: %w",
		address, previous.assetPath, item.path, ErrTemplateRepo,
	)
}

// checkOverride rejects an override that would be silently ignored.
//
// [text/template.Template.AddParseTree] keeps the older definition when the new parse tree is
// empty, so a tree holding nothing but white space and comments silently fails to replace one that
// holds something. To override a template with one that renders nothing, give it an action to run,
// such as {{ "" }}.
func checkOverride(overriding map[string]*declared, address string, tree *parse.Tree, assetPath string) error {
	overridden := overriding[address]
	if overridden != nil && overridden.tree != nil &&
		parse.IsEmptyTree(tree.Root) && !parse.IsEmptyTree(overridden.tree.Root) {
		return fmt.Errorf(
			"template %q declared by asset %q is empty and would not replace the definition it overrides: %w",
			address, assetPath, ErrTemplateRepo,
		)
	}

	return nil
}

// Coverage returns the counters of the templates, or nil when the repository was not built with
// [WithCoverage].
//
// The templates of a repository never change; their counters do. Each counter records how many
// times a run reached one line of one template.
func (r *Repository) Coverage() *cover.Profile {
	return r.coverage
}
