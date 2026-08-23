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
// It is not a name any asset may produce, since a template name is made of recased path
// segments, so no asset can take the place of the namespace.
const namespaceName = "<repository>"

// Repository is a set of compiled templates, resolved against one another and sealed.
//
// A repository is built by [New] from the sources given as options, and derived by [Clone].
// It has no other constructor, and nothing alters it once built.
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
// A clone therefore costs a full rebuild. This is meant for the settings of a program, decided
// once, and not for a per-operation derivation.
//
// # Concurrency
//
// A repository is immutable, holds no lock, and is safe for concurrent use. [Clone] only reads
// its source, so cloning a repository other goroutines are using is safe as well.
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
// comes from the last one. Reporting an error rather than a repository covers an unreadable
// source, a template that fails to parse, a template that refers to one no source declares, and
// an override that would silently not replace what it overrides.
//
// Building a repository with no source at all yields an empty one, which is not an error.
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
// The sources of source are not read again: their content, retained when source was built, is
// carried over and the sources declared by opts are appended to it. Everything is parsed and
// resolved afresh, so a template that opts overrides is picked up by every template referring to
// it, and the two repositories share nothing.
//
// source is left untouched, and may be used by other goroutines while [Clone] runs. A nil source
// reports an error.
//
// Example:
//
//	// the same templates, with one of them replaced
//	patched, err := repo.Clone(repository, repo.FromTemplate("model", myModel))
func Clone(source *Repository, opts ...Option) (*Repository, error) {
	if source == nil {
		return nil, fmt.Errorf("cannot clone a nil repository: %w", ErrTemplateRepo)
	}

	settings := source.settings.derive()
	if err := settings.apply(opts); err != nil {
		return nil, err
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
// It reports an error when no source declares that name. The returned [Template] resolves the
// templates it refers to in this repository.
func (r *Repository) Get(name string) (Template, error) {
	if _, declared := r.declarations[name]; !declared {
		return Template{}, fmt.Errorf("template %q is not declared in this repository: %w", name, ErrTemplateRepo)
	}

	return Template{tpl: r.namespace.Lookup(name)}, nil
}

// Lookup returns the template declared at an address.
//
// An address is the path a template was declared at, slash-separated and never recased. This method
// takes one, [Repository.Get] takes a name. Use whichever a caller already holds.
//
// The extension may be left on, so the asset path a template was read from addresses it too.
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
// Use it for an address hardcoded in the program, and [Repository.Lookup] for one coming from
// the outside.
func (r *Repository) MustLookup(address string) Template {
	tpl, err := r.Lookup(address)
	if err != nil {
		panic(err)
	}

	return tpl
}

// MustGet returns the template registered under a name, and panics when there is none.
//
// Use it for a name hardcoded in the program, and [Repository.Get] for one coming from the
// outside.
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
// It is empty when the repository holds every template it read, which is a repository built
// without [WithRoots]. This reports a scope rather than deciding anything with it: a caller adding
// a template to a repository it did not build wants [WithExtraRoots], which does the right thing
// whether there is a scope to widen or not.
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
// It recases rather than looks up, so an address nothing declares still yields the name it would
// have. Ask [Repository.Has] whether that name is declared. The asset path addresses a template
// too, the extension being trimmed either way: NameOf("server/parameter.gotmpl") and
// NameOf("server/parameter") are both serverParameter.
//
// Which extensions are trimmed is a setting of the repository, which is why this is a method.
// [TemplateName] answers the same question before a repository exists.
//
// It reverses [Repository.AddressOf], and it is idempotent on the names it produces, so a name may
// be handed back to it: NameOf("serverParameter") is serverParameter. A name an address never
// produced is not covered by that, an inner "define" being addressed under the asset that holds it.
func (r *Repository) NameOf(address string) string {
	return r.settings.templateName(address)
}

// build compiles a set of assets into a sealed repository.
//
// Assets are read in order, so that a template declared twice keeps its last definition. They are
// all parsed before any of them is registered, because which templates the repository keeps is
// decided on the call graph they form, and only the templates it keeps are instrumented.
func build(assets []asset, layers int, settings options) (*Repository, error) {
	parsed, err := parseAssets(assets, settings)
	if err != nil {
		return nil, err
	}

	space, err := newAddressSpace(parsed.declared)
	if err != nil {
		return nil, err
	}

	// what an author wrote is relative to where they wrote it, and a namespace is flat: every
	// reference is settled here, once, and never looked at again while a template runs
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

	namespace := template.New(namespaceName).Funcs(settings.funcs)

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

// keyedDeclarations indexes what a repository declares by the name it answers to.
func keyedDeclarations(byPath map[string]*declared) map[string]*declared {
	byKey := make(map[string]*declared, len(byPath))
	for _, item := range byPath {
		byKey[item.key] = item
	}

	return byKey
}

// reportUnresolved rejects a repository holding a template that refers to one it cannot address.
//
// Only the templates it keeps are checked, so a set that is incomplete for the runs this one is
// not scoped to builds all the same. That is the point of scoping.
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

// parsedAssets holds the result of parsing every asset of a repository.
type parsedAssets struct {
	// assets holds the parsed assets, in the order they were read.
	assets []parsedAsset

	// declarations records, per name, the asset the definition that stands comes from.
	declarations map[string]declaration

	// declared holds, per address, the definition that stands there.
	declared map[string]*declared

	// declaring records every asset that declared an address, in the order they were read.
	declaring map[string][]string
}

// overridesOf reports the names a later source redeclared, among those a repository retains.
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
// under it. Each is parsed on its own, so that what it declares is known, and checked, before any
// of it is registered.
func parseAssets(assets []asset, settings options) (parsedAssets, error) {
	parsed := parsedAssets{
		assets:       make([]parsedAsset, 0, len(assets)),
		declarations: make(map[string]declaration, len(assets)),
		declared:     make(map[string]*declared, len(assets)),
		declaring:    make(map[string][]string, len(assets)),
	}

	for _, item := range assets {
		owner := settings.trimmedPath(item.path)

		tpl, err := template.New(owner).Funcs(settings.funcs).Parse(string(item.data))
		if err != nil {
			return parsedAssets{},
				fmt.Errorf("could not parse template %q from asset %q: %w: %w", owner, item.path, err, ErrTemplateRepo)
		}

		declaredHere := make(map[string]*declared, len(tpl.Templates()))
		for _, found := range tpl.Templates() {
			if found.Tree == nil {
				continue
			}

			bare := found.Name()
			address := addressOf(owner, bare)
			if bare == owner {
				bare = ""
			}

			if err := checkCollision(parsed.declarations, address, item); err != nil {
				return parsedAssets{}, err
			}

			if err := checkOverride(parsed.declared, address, found.Tree, item.path); err != nil {
				return parsedAssets{}, err
			}

			declaredHere[address] = &declared{
				address:   address,
				key:       TemplateName(address),
				owner:     owner,
				bare:      bare,
				assetPath: item.path,
				layer:     item.layer,
				tree:      found.Tree,
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

// register adds the retained templates of one asset to the namespace.
//
// An asset all of whose templates were pruned away contributes nothing, counters included: a
// template a repository does not hold is not a template its coverage has an opinion on.
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

	// the trees that run hold the counters, and the emptiness of a template is judged before they do
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
// Names are flat, so a template declared twice keeps one definition and loses the other. Which
// one that is depends on the order the assets are read in, which a caller stacking sources
// chooses, and which a caller pointing at a directory does not.
//
// A redeclaration is therefore taken as intended when it crosses sources, and as a mistake when
// it happens within one: an override is something a caller asks for by declaring a further
// source, never something a directory listing decides.
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
// empty, so a tree holding nothing but white space and comments does not replace one that holds
// something. To override a template with one that renders nothing, give it an action to run,
// such as an empty string.
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
// The templates of a repository are frozen, their counters are not. The counters record what a
// run reaches, and are the one part of a repository that changes.
func (r *Repository) Coverage() *cover.Profile {
	return r.coverage
}
