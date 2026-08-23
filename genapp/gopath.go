// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// gopathPackage returns the import path the output path has under GOPATH.
//
// A tree under GOPATH/src builds without a go.mod when GO111MODULE is off, and the way down from
// src is its import path. Modules came later and win where both apply, so this is reached only
// after the walk for a go.mod has found none.
//
// Every entry of GOPATH is tried, and each of them twice: once as written, once with its symlinks
// resolved, since a GOPATH reached through a link does not match a target that was not.
func gopathPackage(target string) (string, bool) {
	for _, entry := range filepath.SplitList(goPath()) {
		if entry == "" {
			continue
		}

		src := filepath.Join(entry, "src")

		if within, ok := descendantOf(src, target); ok {
			return filepath.ToSlash(within), true
		}

		resolved, err := filepath.EvalSymlinks(src)
		if err != nil {
			continue
		}

		if within, ok := descendantOf(resolved, target); ok {
			return filepath.ToSlash(within), true
		}
	}

	return "", false
}

// goPath returns GOPATH, or the directory the go command falls back to when it is unset.
func goPath() string {
	if fromEnv := os.Getenv("GOPATH"); fromEnv != "" {
		return fromEnv
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, "go")
}

// descendantOf reports whether target sits below parent, and the way down to it.
//
// [filepath.Rel] does the comparing, which is what separates this from a prefix test: it reads ".."
// and "." rather than matching text, and it reports an error where no relative path exists at all —
// a GOPATH on one Windows volume and an output path on another. Such an entry is one this target
// does not belong to, not a failure.
//
// The way down is cut from the target rather than taken from Rel, so a case-insensitive match on
// Windows still yields the import path with the case the directories actually have.
func descendantOf(parent, target string) (string, bool) {
	parent, target = filepath.Clean(parent), filepath.Clean(target)

	comparedParent, comparedTarget := parent, target
	if runtime.GOOS == "windows" {
		comparedParent, comparedTarget = strings.ToLower(parent), strings.ToLower(target)
	}

	within, err := filepath.Rel(comparedParent, comparedTarget)
	if err != nil {
		return "", false
	}

	if within == "." || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", false
	}

	return target[len(parent)+1:], true
}
