// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
)

// tempAttempts caps how many names createTemp tries before it fails.
const tempAttempts = 1000

// destination addresses one file three ways, because a write needs each of them.
type destination struct {
	root *os.Root

	// rel addresses the file from the root. Every [os.Root] method takes this form.
	rel string

	// target is the caller's argument to [GoGenApp.RenderFile]. Error messages quote it.
	target string

	// displayed prefixes target with the output path, so a reader can open it. Only the messages
	// naming a file left on disk need it: a relative target alone does not locate that file.
	displayed string
}

// sibling names a file beside this one, suffixed in every form at once.
func (d destination) sibling(suffix string) destination {
	d.rel += suffix
	d.target += suffix
	d.displayed += suffix

	return d
}

// dir returns the directory holding the file, addressed from the root.
func (d destination) dir() string { return filepath.Dir(d.rel) }

// openRoot opens the directory that confines every write, and returns the way down to the output
// path from there.
//
// Without [WithRoot] the root is the output path itself, so one code path serves both cases.
// openRoot creates that directory first, as [GoGenApp.RenderFile] has always created its output
// directory.
//
// With [WithRoot] the root has to exist. The caller declares the root, so openRoot reports a
// missing one instead of building a tree under a mistyped path.
//
// openRoot opens the root per call, and the caller closes it. A [GoGenApp] holds no state between
// calls, which is what lets [GoGenApp.Render] and [GoGenApp.RenderFile] run concurrently. An open
// root would add a lifetime to the type, and a Close method with it. The cost is one openat per
// file.
func (g *GoGenApp) openRoot() (*os.Root, string, error) {
	output := g.outputPath
	if output == "" {
		output = "."
	}

	if g.root == "" {
		if err := os.MkdirAll(output, dirPerm); err != nil {
			return nil, "", fmt.Errorf("cannot create the output path %q: %w: %w", output, err, ErrGenApp)
		}

		root, err := os.OpenRoot(output)
		if err != nil {
			return nil, "", fmt.Errorf("cannot open the output path %q: %w: %w", output, err, ErrGenApp)
		}

		return root, ".", nil
	}

	down, err := within(g.root, output)
	if err != nil {
		return nil, "", err
	}

	root, err := os.OpenRoot(g.root)
	if err != nil {
		return nil, "", fmt.Errorf("cannot open the root %q, see WithRoot: %w: %w", g.root, err, ErrGenApp)
	}

	return root, down, nil
}

// createTemp opens a temporary file beside the target, under an unused name.
//
// [os.Root] has no CreateTemp, so this opens a random name with O_EXCL and tries another when that
// name is taken. O_EXCL fails on a name that already exists, so the suffix only has to avoid
// collisions rather than be unguessable.
func createTemp(root *os.Root, dir, base string) (*os.File, string, error) {
	for range tempAttempts {
		name := filepath.Join(dir, "."+base+"."+strconv.FormatUint(rand.Uint64(), 36)) //nolint:gosec // O_EXCL names it

		file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, filePerm)
		switch {
		case err == nil:
			return file, name, nil
		case errors.Is(err, fs.ErrExist):
			continue
		default:
			return nil, "", err
		}
	}

	return nil, "", fmt.Errorf("no free temporary name beside %q after %d tries: %w", base, tempAttempts, fs.ErrExist)
}

// scratchLink clears the path so that a rename lands on a regular file.
//
// A symbolic link is removed rather than followed. [os.Root] refuses a link pointing out of the
// root but follows one that stays inside it, so the root alone does not cover this case: a link
// inside a generated tree still redirects the write.
//
// A regular file is left where it is, since the rename replaces it. Anything else — a directory, a
// device, a socket, a fifo — is refused: a generator rendering a file over one of those has been
// pointed at the wrong path.
//
// Removing the link rather than the file it points at also keeps the rename safe for a hard link.
// A rename replaces a name, so another name for the same file keeps the content it had.
func scratchLink(d destination) error {
	info, err := d.root.Lstat(d.rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("cannot read %q: %w: %w", d.target, err, ErrGenApp)
	}

	switch mode := info.Mode(); {
	case mode&fs.ModeSymlink != 0:
		if err := d.root.Remove(d.rel); err != nil {
			return fmt.Errorf("cannot remove the symbolic link at %q: %w: %w", d.target, err, ErrGenApp)
		}

		return nil
	case mode.IsRegular():
		return nil
	default:
		return fmt.Errorf("%q is a %s, which genapp does not overwrite: %w", d.target, modeName(mode), ErrGenApp)
	}
}

// modeName names a file type as an error message says it.
func modeName(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "directory"
	case mode&fs.ModeDevice != 0:
		return "device"
	case mode&fs.ModeNamedPipe != 0:
		return "named pipe"
	case mode&fs.ModeSocket != 0:
		return "socket"
	default:
		return "not a regular file"
	}
}

// commit moves a temporary file onto the target, once nothing but a regular file stands there.
func commit(d destination, temporary string) error {
	if err := scratchLink(d); err != nil {
		return err
	}

	if err := d.root.Rename(temporary, d.rel); err != nil {
		return fmt.Errorf("cannot write %q: %w: %w", d.target, err, ErrGenApp)
	}

	return nil
}
