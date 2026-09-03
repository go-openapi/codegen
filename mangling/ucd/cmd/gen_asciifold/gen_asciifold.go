// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Command gen_asciifold builds the Latin diacritic-fold table (default asciifold_table.go) from the UCD
// DerivedName.txt extract.
//
// It folds a Latin letter bearing a diacritic to its plain ASCII base, case-preserving, across every Latin block
// (Latin-1 Supplement, Latin Extended-A/B/C/D, Latin Extended Additional, …). The rule is name-driven:
//
//   - "LATIN {SMALL|CAPITAL} LETTER <BASE> WITH <marks>" folds to <BASE> when <BASE> is a single A–Z letter
//     (é → e, ǐ → i, ế → e, ơ → o) or a known digraph (Ǣ → AE);
//   - a bare "LATIN {SMALL|CAPITAL} LETTER <DIGRAPH>" (no WITH) folds when <DIGRAPH> is AE/OE/DZ/LJ/NJ/IJ (æ → ae);
//   - distinct letters whose name is not a foldable base (OPEN O, SCHWA, ESH, EZH, GAMMA, TURNED E, clicks, …) have no
//     ASCII base and are deliberately skipped — they fall through to the rune-name stage or are elided.
//
// Atomic letters that carry a conventional ASCII rendering not derivable from their name (eth, thorn, sharp s, eng,
// long s, dotless i, the titlecase digraphs Dž/Lj/Nj/Dz) are supplied by the curated seeds table below and take
// precedence over derivation.
//
// The emitted map[rune]string feeds foldToASCII / ToASCII / RuneToASCII: with folding on, no Latin letter with an ASCII
// base can leak unfolded into generated identifiers.
//
// Usage:
//
//	go run github.com/go-openapi/codegen/mangling/ucd/cmd/gen_asciifold [package [outfile [ucd-dir]]]
//
// package and outfile default to "mangling" and "asciifold_table.go"; ucd-dir defaults to the versioned UCD data
// directory resolved from the repo git root (see ucd/internal/locate).
// Normally invoked via go generate from the mangling package:
//
//	//go:generate go run ./ucd/cmd/gen_asciifold mangling asciifold_table.go
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/format"
	"log"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-openapi/codegen/mangling/ucd/internal/locate"
)

const (
	defaultPackageName = "mangling"
	defaultOutBase     = "asciifold_table"
	ucdFile            = "DerivedName.txt"
)

// multiBase are the multi-letter bases (digraphs and typographic ligatures) whose fold is their lowercase/uppercase
// spelling.
var multiBase = map[string]string{
	// digraphs ("LATIN … LETTER <X>", "… WITH …")
	"AE": "ae", "OE": "oe", "DZ": "dz", "LJ": "lj", "NJ": "nj", "IJ": "ij",
	// typographic ligatures ("LATIN … LIGATURE <X>")
	"FF": "ff", "FI": "fi", "FL": "fl", "FFI": "ffi", "FFL": "ffl", "ST": "st",
}

// seeds are atomic Latin letters whose ASCII rendering is a deliberate convention, not derivable from the Unicode name.
//
// They override any derived value.
var seeds = map[rune]string{
	'ð': "d", 'Ð': "D", // eth
	'þ': "th", 'Þ': "Th", // thorn (titlecase Th, matching prior convention)
	'ß': "ss", 'ẞ': "SS", // sharp s
	'ŋ': "n", 'Ŋ': "N", // eng
	'ə': "e",  // schwa
	'ı': "i",  // dotless i (İ folds via "WITH DOT ABOVE")
	'ſ': "s",  // long s
	'ŉ': "n",  // n preceded by apostrophe (deprecated)
	'ǅ': "Dz", // titlecase digraphs (name is "D WITH SMALL LETTER Z …", not a single base)
	'ǈ': "Lj",
	'ǋ': "Nj",
	'ǲ': "Dz",
}

type fold struct {
	r rune
	s string
}

func main() {
	pkg, outFile, ucdDir, buildTag := resolveArgs()

	if err := run(pkg, outFile, ucdDir, buildTag); err != nil {
		log.Fatalf("error: %v", err)
	}
}

func run(pkg, outFile, ucdDir, buildTag string) error {
	inFile := filepath.Join(ucdDir, ucdFile)
	f, err := os.Open(inFile)
	if err != nil {
		return err
	}
	defer f.Close()

	folds := map[rune]string{}
	maps.Copy(folds, seeds)
	seedCount := len(folds)

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// algorithmic ranges / patterns ("..", "*") are CJK/Hangul/Tangut — never Latin letters.
		if strings.Contains(line, "..") || strings.Contains(line, "*") {
			continue
		}

		cpField, name, ok := strings.Cut(line, ";")
		if !ok {
			continue
		}
		var r rune
		if _, err := fmt.Sscanf(strings.TrimSpace(cpField), "%X", &r); err != nil {
			continue
		}
		if r < 0x80 {
			continue // ASCII letters are handled directly, never folded
		}
		if _, seeded := folds[r]; seeded {
			continue // seed wins
		}

		base, ok := deriveFold(strings.TrimSpace(name))
		if !ok {
			continue
		}
		folds[r] = base
	}
	if err := sc.Err(); err != nil {
		return err
	}

	out := make([]fold, 0, len(folds))
	for r, s := range folds {
		out = append(out, fold{r, s})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].r < out[j].r })

	inFile, err = filepath.Rel(filepath.Dir(ucdDir), inFile)
	if err != nil {
		return err
	}
	// The generated file records this path, so keep it slash-separated: regenerating on Windows must not rewrite
	// "v15/DerivedName.txt" as "v15\DerivedName.txt" and churn every table.
	inFile = filepath.ToSlash(inFile)

	if err := emit(inFile, pkg, outFile, buildTag, out); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr,
		"asciifold: %d entries (%d seeds + %d name-derived) from %s\n",
		len(out), seedCount, len(out)-seedCount, inFile,
	)

	return nil
}

// deriveFold returns the ASCII fold for a Latin-letter name, or ("", false) when the name has no plain ASCII base.
func deriveFold(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, "LATIN ")
	if !ok {
		return "", false
	}

	var upper bool
	switch {
	case strings.HasPrefix(rest, "CAPITAL LETTER "):
		upper, rest = true, rest[len("CAPITAL LETTER "):]
	case strings.HasPrefix(rest, "SMALL LETTER "):
		rest = rest[len("SMALL LETTER "):]
	case strings.HasPrefix(rest, "CAPITAL LIGATURE "):
		upper, rest = true, rest[len("CAPITAL LIGATURE "):]
	case strings.HasPrefix(rest, "SMALL LIGATURE "):
		rest = rest[len("SMALL LIGATURE "):]
	default:
		// uncased "LATIN LETTER …" forms are IPA/phonetic distinct letters — no fold.
		return "", false
	}

	head, _, _ := strings.Cut(rest, " WITH ")

	// single A–Z base
	if len(head) == 1 && head[0] >= 'A' && head[0] <= 'Z' {
		if upper {
			return head, true
		}
		return strings.ToLower(head), true
	}

	// known digraph / ligature base
	if lower, ok := multiBase[head]; ok {
		if upper {
			return strings.ToUpper(lower), true
		}
		return lower, true
	}

	return "", false
}

func emit(source, packageName, outFile, buildTag string, folds []fold) error {
	var b bytes.Buffer
	fmt.Fprint(&b, "// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers\n// SPDX-License-Identifier: Apache-2.0\n\n")
	if buildTag != "" {
		fmt.Fprintf(&b, "//go:build %s\n\n", buildTag)
	}
	fmt.Fprintf(&b, "// Code generated by gen_asciifold.go. DO NOT EDIT.\n\npackage %s\n\n", packageName)
	fmt.Fprint(&b, "// asciiFold maps a Latin letter bearing a diacritic (or a distinct letter such as æ, ß, þ) to its\n")
	fmt.Fprint(&b, "// plain ASCII equivalent, preserving case. It is the data that supports [RuneToASCII] / [ToASCII]\n")
	fmt.Fprint(&b, "// and the fold stage. Distinct letters with no ASCII base (OPEN O, SCHWA, ESH, clicks, …) are absent\n")
	fmt.Fprint(&b, "// and fall through to the rune-name stage.\n")
	fmt.Fprintf(&b, "//\n// Generated from %s.\n", source)
	fmt.Fprint(&b, "var asciiFold = map[rune]string{\n")
	for _, f := range folds {
		fmt.Fprintf(&b, "\t0x%04X: %q, // %c\n", f.r, f.s, f.r)
	}
	b.WriteString("}\n")

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		_ = os.WriteFile(outFile, b.Bytes(), 0o644) //nolint:gosec // permissions are okay for our codegen
		return fmt.Errorf("format: %w", err)
	}

	return os.WriteFile(outFile, formatted, 0o644) //nolint:gosec // permissions are okay for our codegen
}

// resolveArgs parses [package [outbase [version [ucd-root]]]] and resolves the version to its input directory,
// //go:build tag and versioned output filename (outbase + version + ".go").
func resolveArgs() (pkg, outFile, ucdDir, buildTag string) {
	pkg = defaultPackageName
	outBase := defaultOutBase
	version := locate.Versions[0].UCD
	var rootOverride string

	if len(os.Args) > 1 {
		pkg = os.Args[1]
	}

	if len(os.Args) > 2 {
		outBase = os.Args[2]
	}

	if len(os.Args) > 3 {
		version = os.Args[3]
	}

	if len(os.Args) > 4 {
		rootOverride = os.Args[4]
	}

	dir, tag, suffix, err := locate.Resolve(version, rootOverride)
	if err != nil {
		log.Fatalf("cannot resolve ucd source: %v", err)
	}

	return pkg, outBase + suffix + ".go", dir, tag
}
