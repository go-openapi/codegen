// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Command gen_numerals builds the rune -> numeric value table (default numerals.go) from the UCD
// DerivedNumericValues.txt extract.
//
// It keeps the No (Number, other: vulgar fractions, superscripts, circled digits, ...) and Nl
// (Number, letter: roman/acrophonic/cuneiform numerals) categories, and drops:
//   - Nd (decimal digits) — the mangler already handles those via a digit-value offset;
//   - Lo (CJK ideographic numbers) — Han script, elided during asciification;
//   - runes whose value is NaN or infinite — UCD writes NaN for "no numeric value", and Go has no literal for either
//     (strconv.FormatFloat emits the undefined identifiers NaN, +Inf and -Inf).
//
// The emitted map[rune]float64 feeds numbers.RuneNumber, so a Unicode numeral verbalizes through the same engine as an
// ASCII number (½ -> "one half"), and the asciify tier can render it as a plain number (½ -> "0.5").
//
// Usage:
//
//	go run github.com/go-openapi/codegen/mangling/ucd/cmd/gen_numerals [package [outfile [ucd-dir]]]
//
// package and outfile default to "numbers" and "numerals.go"; ucd-dir defaults to the versioned UCD data directory
// resolved from the repo git root (see ucd/internal/locate).
// Normally invoked via go generate from the numbers package:
//
//	//go:generate go run ../ucd/cmd/gen_numerals numbers numerals.go
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/format"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-openapi/codegen/mangling/ucd/internal/locate"
)

const (
	defaultPackageName = "numbers"
	defaultOutBase     = "numerals"
	ucdFile            = "DerivedNumericValues.txt"
)

type numeral struct {
	r rune
	v float64
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

	var nums []numeral
	var nonFinite int
	kept := map[string]int{} // category tallies, for the report

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields, cat, ok := parse(line)
		if !ok {
			continue
		}
		if cat != "No" && cat != "Nl" {
			continue // Nd handled by digit offset; Lo (CJK) elided
		}
		kept[cat]++

		v, err := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
		if err != nil {
			continue
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			// UCD writes NaN for a rune with no numeric value, and ParseFloat also accepts the literal words "nan" and
			// "inf". Go has no constant for either: strconv.FormatFloat would emit NaN, +Inf or -Inf, which are
			// undefined identifiers in the generated table. Drop the rune instead.
			nonFinite++

			continue
		}
		lo, hi, err := codeRange(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		for r := lo; r <= hi; r++ {
			nums = append(nums, numeral{r: r, v: v})
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	sort.Slice(nums, func(i, j int) bool { return nums[i].r < nums[j].r })

	inFile, err = filepath.Rel(filepath.Dir(ucdDir), inFile)
	if err != nil {
		return err
	}
	// The generated file records this path, so keep it slash-separated: regenerating on Windows must not rewrite
	// "v15/DerivedName.txt" as "v15\DerivedName.txt" and churn every table.
	inFile = filepath.ToSlash(inFile)

	if err := emit(inFile, pkg, outFile, buildTag, nums); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr,
		"numerals: kept No=%d Nl=%d, %d runes after range expansion (%d KiB map data)\n",
		kept["No"], kept["Nl"], len(nums), (len(nums)*(4+8))/1024,
	)
	if nonFinite > 0 {
		fmt.Fprintf(os.Stderr, "numerals: dropped %d rune(s) with a NaN or infinite value\n", nonFinite)
	}

	return nil
}

// parse splits a data line into its ';'-separated fields (before the '#') and the general category (first token of the
// trailing comment).
func parse(line string) (fields []string, cat string, ok bool) {
	body, comment, found := strings.Cut(line, "#")
	if !found {
		return nil, "", false
	}

	fields = strings.Split(body, ";")
	if len(fields) < 4 {
		return nil, "", false
	}

	cf := strings.Fields(comment)
	if len(cf) == 0 {
		return nil, "", false
	}

	return fields, cf[0], true
}

// codeRange parses "1F100" or "11FC9..11FCA" into an inclusive rune range.
func codeRange(s string) (lo, hi rune, err error) {
	a, b, isRange := strings.Cut(s, "..")
	x, err := strconv.ParseInt(a, 16, 32)
	if err != nil {
		return 0, 0, err
	}
	lo, hi = rune(x), rune(x)
	if isRange {
		y, err := strconv.ParseInt(b, 16, 32)
		if err != nil {
			return 0, 0, err
		}
		hi = rune(y)
	}
	return lo, hi, nil
}

func emit(source, packageName, outFile, buildTag string, nums []numeral) error {
	var b bytes.Buffer
	fmt.Fprint(&b, "// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers\n// SPDX-License-Identifier: Apache-2.0\n\n")
	if buildTag != "" {
		fmt.Fprintf(&b, "//go:build %s\n\n", buildTag)
	}
	fmt.Fprintf(&b, "// Code generated by gen_numerals.go. DO NOT EDIT.\n\npackage %s\n\n", packageName)
	fmt.Fprint(&b, "// runeNumericValue maps a Unicode numeral (categories No and Nl) to its numeric value.\n")
	fmt.Fprintf(&b, "// Nd digits and Lo (CJK) numbers are excluded. Generated from %s.\n", source)
	fmt.Fprintf(&b, "var runeNumericValue = map[rune]float64{\n")
	for _, n := range nums {
		fmt.Fprintf(&b, "\t0x%04X: %s,\n", n.r, strconv.FormatFloat(n.v, 'g', -1, 64))
	}
	b.WriteString("}\n")

	out, err := format.Source(b.Bytes())
	if err != nil {
		_ = os.WriteFile(outFile, b.Bytes(), 0o644) //nolint:gosec // permissions are okay for our codegen
		return fmt.Errorf("format: %w", err)
	}

	return os.WriteFile(outFile, out, 0o644) //nolint:gosec // permissions are okay for our codegen
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
