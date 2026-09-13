// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

// asset is a template file read from a source, held for as long as the repository lives.
//
// The path is the asset's, once mounted, slash-separated and cleaned. An asset holds its path and
// not its name, because [TemplateName] derives the name from the path and the extensions in force:
// a [Clone] changing [WithExtensions] renames the templates accordingly.
//
// The layer records which source read the asset. Layers are numbered in the order the sources are
// declared, and a [Clone] carries on where the repository it derives from left off, so two assets
// share a layer only when a single source read them both.
type asset struct {
	path  string
	data  []byte
	layer int
}

// source reads a set of assets, once, when the repository is built.
type source func(options) ([]asset, error)

// FromFS reads every supported asset of fsys, and mounts them at mountPoint.
//
// fsys is read from its root. Use [io/fs.Sub] to serve a subtree. An empty mountPoint, or ".",
// mounts the assets at the top of the template tree, which is the usual case.
//
// FromFS does not override. To make one set of files take precedence over another, stack them
// into a single [io/fs.FS] and pass the result.
//
// Example:
//
//	// the assets of an embed.FS, patched by an alternate set living elsewhere in the same tree
//	repo.New(repo.FromFS(fileutils.NewOverlayFS(
//		fileutils.MustSub(assets, "templates"),
//		fileutils.MustSub(assets, "templates/contrib/mine"),
//	), ""))
func FromFS(fsys fs.FS, mountPoint string, opts ...SourceOption) Option {
	return func(o options) options {
		mount, err := cleanMountPoint(mountPoint)
		if err != nil {
			return o.withError(err)
		}

		reading, err := makeSourceOptions(opts)
		if err != nil {
			return o.withError(err)
		}

		o.sources = append(slices.Clip(o.sources), func(settings options) ([]asset, error) {
			if fsys == nil {
				return nil, fmt.Errorf("cannot read templates from a nil fs.FS: %w", ErrTemplateRepo)
			}

			return readFS(fsys, reading.mount(mount), reading.skipDirectories, settings)
		})

		return o
	}
}

// Sources bundles several options into one, so that a package exports everything its templates
// need as a single option.
//
// A set of templates is rarely one source: the templates themselves, the ones that place each
// generated section, and whatever else a package ships. Export a single [Option] and the caller
// assembling it needs to know neither how many sources there are nor what order they go in.
//
// Example:
//
//	// the package publishes this, and nothing of what is inside it
//	func Sources(opts ...repo.SourceOption) repo.Option {
//		return repo.Sources(
//			repo.FromFS(templates, "", opts...),
//			repo.FromFS(filepaths, "paths", opts...),
//		)
//	}
func Sources(opts ...Option) Option {
	return func(o options) options {
		return o.apply(opts)
	}
}

// SourceOption configures how one source is read.
//
// A [SourceOption] describes the file system being walked, not the repository. [SkipDirectories]
// on one source leaves a directory of the same name fully readable in a template set someone else
// brings.
type SourceOption func(sourceOptions) sourceOptions

// sourceOptions holds the settings of a single source.
type sourceOptions struct {
	skipDirectories []string
	rebase          string
	err             error
}

// withError keeps the first failure and lets the rest of the chain run.
func (o sourceOptions) withError(err error) sourceOptions {
	if o.err == nil {
		o.err = err
	}

	return o
}

// Rebased mounts a source under a base, on top of wherever it already mounts.
//
// Use it to publish templates without knowing where they land. The package exports its sources,
// and whoever assembles them picks the mount point of each.
//
// Example:
//
//	// the package publishes this
//	func Sources(opts ...repo.SourceOption) repo.Option {
//		return repo.FromFS(templates, "", opts...)
//	}
//
//	// and whoever assembles decides where it lands
//	repo.New(
//		genmodels.Sources(repo.Rebased("models")),
//		genclient.Sources(repo.Rebased("client")),
//	)
func Rebased(base string) SourceOption {
	return func(o sourceOptions) sourceOptions {
		under, err := cleanMountPoint(base)
		if err != nil {
			return o.withError(err)
		}

		o.rebase = path.Join(o.rebase, under)

		return o
	}
}

// mount joins the mount point a source declared with the base the caller asked for.
func (o sourceOptions) mount(declared string) string {
	return path.Join(o.rebase, declared)
}

// makeSourceOptions applies opts on top of the defaults, which skip nothing.
func makeSourceOptions(opts []SourceOption) (sourceOptions, error) {
	var o sourceOptions

	for _, option := range opts {
		if option == nil {
			continue
		}

		o = option(o)
	}

	if o.err != nil {
		return sourceOptions{}, o.err
	}

	return o, nil
}

// SkipDirectories walks past the directories named, wherever they are in the tree read.
//
// Directories are matched on their name, at any depth. Nothing is skipped by default. Put it on
// the source that ships the alternate sets, then declare the set you want as a further source.
//
// Example:
//
//	// the assets shipped, leaving the alternate sets to be stacked explicitly
//	repo.FromFS(assets, "", repo.SkipDirectories("contrib"))
func SkipDirectories(names ...string) SourceOption {
	return func(o sourceOptions) sourceOptions {
		o.skipDirectories = append(o.skipDirectories, names...)

		return o
	}
}

// FromDir reads every supported asset of a local directory, and mounts them at mountPoint.
//
// dir is a path in the os file system, and the assets are named relative to it. FromDir is the
// shorthand for [FromFS] over an [os.DirFS], and it reports an error when dir is not a readable
// directory.
//
// The directory is read once, when the repository is built. Editing a template on disk
// afterwards has no effect until a repository is built again.
func FromDir(dir, mountPoint string, opts ...SourceOption) Option {
	return func(o options) options {
		mount, err := cleanMountPoint(mountPoint)
		if err != nil {
			return o.withError(err)
		}

		reading, err := makeSourceOptions(opts)
		if err != nil {
			return o.withError(err)
		}

		o.sources = append(slices.Clip(o.sources), func(settings options) ([]asset, error) {
			info, err := os.Stat(dir)
			if err != nil {
				return nil, fmt.Errorf("cannot read templates from %q: %w: %w", dir, err, ErrTemplateRepo)
			}

			if !info.IsDir() {
				return nil, fmt.Errorf("%q is not a directory: %w", dir, ErrTemplateRepo)
			}

			return readFS(os.DirFS(dir), reading.mount(mount), reading.skipDirectories, settings)
		})

		return o
	}
}

// FromRepository reads the templates another repository holds, and mounts them at mountPoint.
//
// Use it to build a set out of parts in one pass. [New] only builds a repository when every
// template it refers to is there, so a scaffolding calling into the parts it assembles cannot
// build on its own. Declare those parts as sources of the same build, and each one is written
// apart and resolved together.
//
// The templates are read as they were declared, and mounting them moves their addresses the way
// [Rebase] does. What they refer to moves with them, so a set that resolved on its own resolves
// the same mounted.
//
// FromRepository reads no source of source again. It carries over the content source retained when
// it was built. source is left untouched.
//
// Example:
//
//	// a scaffolding of one's own, with the sets it calls into
//	repo.New(
//		repo.FromDir("./scaffolding", ""),
//		repo.FromRepository(modelTemplates, "models"),
//		repo.FromRepository(serverTemplates, "server"),
//	)
func FromRepository(source *Repository, mountPoint string, opts ...SourceOption) Option {
	return func(o options) options {
		mount, err := cleanMountPoint(mountPoint)
		if err != nil {
			return o.withError(err)
		}

		reading, err := makeSourceOptions(opts)
		if err != nil {
			return o.withError(err)
		}

		mount = reading.mount(mount)

		o.sources = append(slices.Clip(o.sources), func(options) ([]asset, error) {
			if source == nil {
				return nil, fmt.Errorf("cannot read templates from a nil repository: %w", ErrTemplateRepo)
			}

			read := make([]asset, 0, len(source.assets))
			for _, item := range source.assets {
				item.path = path.Join(mount, item.path)
				read = append(read, item)
			}

			return read, nil
		})

		return o
	}
}

// FromTemplate registers a single template held in memory, at the address given.
//
// The address locates the template exactly as written, so it may hold directories, and it is never
// mangled. To override a template declared elsewhere, name the address it was declared at. The
// name it answers to is derived from the address like any other.
//
// [FromFS] and [FromDir] only read an asset whose name carries a recognized extension. FromTemplate
// registers content whatever the name.
//
// Declare a template no file holds this way, such as one a configuration provides. For several of
// them, build an in-memory [io/fs.FS] and pass it to [FromFS], so every override goes through one
// mechanism.
//
// The content is retained, not copied.
func FromTemplate(name string, content []byte, opts ...SourceOption) Option {
	return func(o options) options {
		clean, err := cleanAssetName(name)
		if err != nil {
			return o.withError(err)
		}

		reading, err := makeSourceOptions(opts)
		if err != nil {
			return o.withError(err)
		}

		clean = reading.mount(clean)

		o.sources = append(slices.Clip(o.sources), func(options) ([]asset, error) {
			return []asset{{path: clean, data: content}}, nil
		})

		return o
	}
}

// resolveSources reads every source declared, in the order it was declared.
//
// Layers are numbered from baseLayer on, so [checkCollision] never mistakes an asset a [Clone]
// added for one its origin already held. resolveSources returns the next free layer along with
// the assets.
func (o options) resolveSources(baseLayer int) ([]asset, int, error) {
	var assets []asset

	layer := baseLayer
	for _, read := range o.sources {
		found, err := read(o)
		if err != nil {
			return nil, 0, err
		}

		for _, item := range found {
			item.layer = layer
			assets = append(assets, item)
		}

		layer++
	}

	return assets, layer, nil
}

// readFS walks a file system and reads the assets it holds that are recognized as templates.
func readFS(fsys fs.FS, mount string, skipped []string, settings options) ([]asset, error) {
	var assets []asset

	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if name != "." && slices.Contains(skipped, path.Base(name)) {
				return fs.SkipDir
			}

			return nil
		}

		if !settings.hasSupportedExtension(name) {
			return nil
		}

		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}

		assets = append(assets, asset{path: path.Join(mount, name), data: data})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not read templates: %w: %w", err, ErrTemplateRepo)
	}

	return assets, nil
}

// slashed reads a caller's path the way an address is written, whatever separator they typed.
//
// An [io/fs.FS] always returns slash-separated names, so only a path a caller typed can carry a
// backslash. Reading it as a separator gives a repository the same addresses on every platform, so
// a template refers to another one the same way everywhere.
func slashed(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

// cleanMountPoint validates the place a source is mounted at in the template tree.
//
// An empty mount point, or ".", mounts at the top. Separators are not translated. A mount point is
// a slash-separated path, so the same declaration yields the same names on every platform.
func cleanMountPoint(mountPoint string) (string, error) {
	trimmed := strings.Trim(slashed(mountPoint), "/")
	if trimmed == "" || trimmed == "." {
		return "", nil
	}

	clean := path.Clean(trimmed)
	if !fs.ValidPath(clean) {
		return "", fmt.Errorf("invalid mount point %q: %w", mountPoint, ErrTemplateRepo)
	}

	return clean, nil
}

// cleanAssetName validates the name a template is registered under by [FromTemplate].
func cleanAssetName(name string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(slashed(name), "/"))
	if clean == "." || !fs.ValidPath(clean) {
		return "", fmt.Errorf("invalid template name %q: %w", name, ErrTemplateRepo)
	}

	return clean, nil
}
