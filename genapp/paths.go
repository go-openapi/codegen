// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// checkedTarget cleans a target into a slash-separated relative name, and rejects one that would
// write outside the output path.
//
// A target names a file under the output path, so checkedTarget refuses an absolute path instead of
// rebasing it. A generator renders no file addressed from the root of the file system, so a target
// that starts there came from somewhere else.
//
// [github.com/go-openapi/swag/loading.WithRoot] rebases such a path instead, because
// github.com/go-openapi/spec normalizes every $ref it loads to an absolute path. RenderFile reads
// no such input.
//
// The checks run whether or not [WithRoot] is set. A spec supplies the target names, so RenderFile
// keeps writing under the output path even when nothing else confines it.
func checkedTarget(target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("the target names no file: %w", ErrGenApp)
	}

	// filepath.IsAbs reads "/x" as relative on Windows, and a volume name covers "C:x" and
	// "\\\\server\\share" alike, neither of which filepath.IsAbs reports on its own.
	if filepath.IsAbs(target) || path.IsAbs(filepath.ToSlash(target)) || filepath.VolumeName(target) != "" {
		return "", fmt.Errorf(
			"the target %q is an absolute path, RenderFile writes under the output path: %w", target, ErrGenApp,
		)
	}

	cleaned := path.Clean(filepath.ToSlash(target))

	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("the target %q climbs out of the output path: %w", target, ErrGenApp)
	}

	if cleaned == "." {
		return "", fmt.Errorf("the target %q names a directory, not a file: %w", target, ErrGenApp)
	}

	return cleaned, nil
}

// within returns the way down from base to target, and rejects a target that does not sit below it.
//
// [filepath.Abs] resolves both paths first, so a relative base still compares against an absolute
// target. [filepath.Rel] then follows ".." and ".", where a prefix test would match the names as
// text and miss the traversal.
func within(base, target string) (string, error) {
	absoluteBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the root %q: %w: %w", base, err, ErrGenApp)
	}

	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the output path %q: %w: %w", target, err, ErrGenApp)
	}

	down, err := filepath.Rel(absoluteBase, absoluteTarget)
	if err != nil || down == ".." || strings.HasPrefix(down, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"the output path %q is outside the root %q: %w", target, base, ErrGenApp,
		)
	}

	return down, nil
}
