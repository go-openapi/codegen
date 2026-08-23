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
// The path is the asset's, once mounted, slash-separated and cleaned. The name of the template is
// derived from it, so the path is retained rather than the name: a [Clone] that changes the
// recognized extensions renames the templates accordingly.
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
// fsys is read from its root, so a caller serving a subtree re-roots it beforehand with
// [io/fs.Sub]. An empty mountPoint, or ".", mounts the assets at the top of the template tree,
// which is the usual case.
//
// Overriding is not this option's business: a caller that wants one set of files to take
// precedence over another stacks them into a single [io/fs.FS] first, and passes the result.
//
// Example:
//
//	// the assets of an embed.FS, patched by an alternate set living elsewhere in the same tree
//	repo.New(repo.FromFS(fileutils.NewOverlayFS(
//		fileutils.MustSub(assets, "templates"),
//		fileutils.MustSub(assets, "templates/contrib/mine"),
//	), ""))
func FromFS(fsys fs.FS, mountPoint string, opts ...SourceOption) Option {
	return func(o *options) error {
		mount, err := cleanMountPoint(mountPoint)
		if err != nil {
			return err
		}

		reading, err := makeSourceOptions(opts)
		if err != nil {
			return err
		}

		o.sources = append(o.sources, func(settings options) ([]asset, error) {
			if fsys == nil {
				return nil, fmt.Errorf("cannot read templates from a nil fs.FS: %w", ErrTemplateRepo)
			}

			return readFS(fsys, reading.mount(mount), reading.skipDirectories, settings)
		})

		return nil
	}
}

// Sources bundles several options into one, so that a package exports everything its templates
// need as a single option.
//
// A set of templates is rarely one source: the templates themselves, the ones that place each
// generated section, and whatever else a package ships. The caller assembling them does not have
// to know how many there are, nor in what order they go.
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
	return func(o *options) error {
		return o.apply(opts)
	}
}

// SourceOption configures how one source is read.
//
// Which directories to skip describes the file system being walked, not the repository. Skipping a
// directory of the assets one source ships leaves a directory of the same name fully readable in
// a template set someone else brings.
type SourceOption func(*sourceOptions) error

// sourceOptions holds the settings of a single source.
type sourceOptions struct {
	skipDirectories []string
	rebase          string
}

// Rebased mounts a source under a base, on top of wherever it already mounts.
//
// Use it to publish templates without knowing where they land: the package exports sources, and
// the caller assembling them chooses the mount point of each.
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
	return func(o *sourceOptions) error {
		under, err := cleanMountPoint(base)
		if err != nil {
			return err
		}

		o.rebase = path.Join(o.rebase, under)

		return nil
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

		if err := option(&o); err != nil {
			return sourceOptions{}, err
		}
	}

	return o, nil
}

// SkipDirectories walks past the directories named, wherever they are in the tree read.
//
// Directories are matched on their name, at any depth. Nothing is skipped by default. Use this
// to stack a set of alternate templates without reading all of it, on the source that holds
// them.
//
// Example:
//
//	// the assets shipped, leaving the alternate sets to be stacked explicitly
//	repo.FromFS(assets, "", repo.SkipDirectories("contrib"))
func SkipDirectories(names ...string) SourceOption {
	return func(o *sourceOptions) error {
		o.skipDirectories = append(o.skipDirectories, names...)

		return nil
	}
}

// FromDir reads every supported asset of a local directory, and mounts them at mountPoint.
//
// dir is a path in the os file system, and the assets are named relative to it. It is the
// shorthand for [FromFS] over an [os.DirFS], and it reports an error when dir is not a readable
// directory.
//
// The directory is read once, when the repository is built. Editing a template on disk
// afterwards has no effect until a repository is built again.
func FromDir(dir, mountPoint string, opts ...SourceOption) Option {
	return func(o *options) error {
		mount, err := cleanMountPoint(mountPoint)
		if err != nil {
			return err
		}

		reading, err := makeSourceOptions(opts)
		if err != nil {
			return err
		}

		o.sources = append(o.sources, func(settings options) ([]asset, error) {
			info, err := os.Stat(dir)
			if err != nil {
				return nil, fmt.Errorf("cannot read templates from %q: %w: %w", dir, err, ErrTemplateRepo)
			}

			if !info.IsDir() {
				return nil, fmt.Errorf("%q is not a directory: %w", dir, ErrTemplateRepo)
			}

			return readFS(os.DirFS(dir), reading.mount(mount), reading.skipDirectories, settings)
		})

		return nil
	}
}

// FromRepository reads the templates another repository holds, and mounts them at mountPoint.
//
// This is how a set assembled out of parts is built in one pass. A repository is only built when
// everything it refers to is there, so a scaffolding that calls into the parts it is assembled
// with cannot stand on its own. Declare the parts as sources of the same build, and each may be
// written apart and resolved together.
//
// The templates of the repository are read as they were declared, and mounting them somewhere
// moves their addresses the way [Rebase] does. What they refer to moves with them, so a set that
// resolved on its own resolves the same mounted.
//
// Nothing of the repository is read again: it retained the content of its own sources, and that is
// what is carried over. It is left untouched.
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
	return func(o *options) error {
		mount, err := cleanMountPoint(mountPoint)
		if err != nil {
			return err
		}

		reading, err := makeSourceOptions(opts)
		if err != nil {
			return err
		}

		mount = reading.mount(mount)

		o.sources = append(o.sources, func(options) ([]asset, error) {
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

		return nil
	}
}

// FromTemplate registers a single template held in memory, at the address given.
//
// The address locates the template, exactly as written, so it may hold directories and it
// is never mangled: overriding a template declared elsewhere means naming the address it was
// declared at. The key it answers to is derived from the address like any other.
//
// Unlike an asset read from a file system, it is registered whatever its extension.
//
// This is the way to declare a template that no file holds, such as one a configuration
// provides. A caller holding several of them is better served by an in-memory [io/fs.FS] passed
// to [FromFS], which keeps every override going through the same mechanism.
//
// The content is retained, not copied.
func FromTemplate(name string, content []byte, opts ...SourceOption) Option {
	return func(o *options) error {
		clean, err := cleanAssetName(name)
		if err != nil {
			return err
		}

		reading, err := makeSourceOptions(opts)
		if err != nil {
			return err
		}

		clean = reading.mount(clean)

		o.sources = append(o.sources, func(options) ([]asset, error) {
			return []asset{{path: clean, data: content}}, nil
		})

		return nil
	}
}

// resolveSources reads every source declared, in the order it was declared.
//
// Layers are numbered from baseLayer on, so that the assets a [Clone] adds are never mistaken for
// the ones its origin already held. The number of the next free layer is returned along with the
// assets.
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
// An [io/fs.FS] always hands over slash-separated names, so only a path written by a caller can
// carry a backslash. Reading it as a separator keeps a repository holding the same addresses on
// every platform, so a template refers to another one the same way everywhere.
func slashed(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

// cleanMountPoint validates the place a source is mounted at in the template tree.
//
// An empty mount point, or ".", mounts at the top. Separators are not translated: a mount point
// is a slash-separated path, so that the same declaration yields the same names on every
// platform.
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
