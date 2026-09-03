// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Command gen_runewords builds the compact rune -> word table (default tables.go) from the UCD DerivedName.txt and
// emoji-data.txt extracts.
//
// Pipeline:
//  1. exclude runes already handled elsewhere (ASCII, Latin+diacritics, digits) or elided
//     (combining marks, controls/format, separators/modifiers);
//  2. exclude runes we drop during asciify (CJK Han, Hangul syllables — algorithmic, no useful word);
//  3. extract a lowercase "distinctive remainder" from the formal name (strip taxonomy);
//  4. emit an interned word blob + interval-encoded rune keys (maximal runs of consecutive codepoints) +
//     18-bit split offsets (uint16 low array + a 2-bit high sidecar).
//
// Usage:
//
//	go run github.com/go-openapi/codegen/mangling/ucd/cmd/gen_runewords [package [outfile [ucd-dir]]]
//
// package and outfile default to "runewords" and "tables.go"; ucd-dir defaults to the versioned UCD data directory
// resolved from the repo git root (see ucd/internal/locate).
// Normally invoked via go generate from the runewords package:
//
//	//go:generate go run ../ucd/cmd/gen_runewords runewords tables.go
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/go-openapi/codegen/mangling/ucd/internal/locate"
)

const (
	defaultPackageName = "runewords"
	defaultOutBase     = "tables"
	ucdFile            = "DerivedName.txt"
	ucdEmojiFile       = "emoji-data.txt"
)

type rrange struct{ lo, hi rune }

// pictRanges holds the Extended_Pictographic runes (loaded from emoji-data.txt).
//
// They are protected from block elision so real emoji survive even inside mixed decorative blocks.
var pictRanges []rrange

func main() {
	pkg, outFile, ucdDir, buildTag := resolveArgs()

	if err := run(pkg, outFile, ucdDir, buildTag); err != nil {
		log.Fatalf("error: %v", err)
	}
}

func run(pkg, outFile, ucdDir, buildTag string) error {
	inFile := filepath.Join(ucdDir, ucdFile)
	emojiFile := filepath.Join(ucdDir, ucdEmojiFile)

	var err error
	if pictRanges, err = loadPictographic(emojiFile); err != nil {
		return err
	}

	f, err := os.Open(inFile)
	if err != nil {
		return err
	}
	defer f.Close()

	var st stats
	var entries []kept

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// algorithmic ranges ("3400..4DBF ; CJK UNIFIED IDEOGRAPH-*") — no per-rune word.
		if strings.Contains(line, "..") || strings.Contains(line, "*") {
			st.rangeLine++
			continue
		}
		st.total++

		cp, rest, ok := strings.Cut(line, ";")
		if !ok {
			continue
		}
		var r rune
		if _, err := fmt.Sscanf(strings.TrimSpace(cp), "%X", &r); err != nil {
			continue
		}
		name := strings.TrimSpace(rest)

		switch reason := classify(r); reason {
		case "":
			// keep
		case "ascii":
			st.ascii++
			continue
		case "nonprint":
			st.nonprint++
			continue
		case "combining":
			st.combining++
			continue
		case "separator":
			st.separator++
			continue
		case "digit":
			st.digit++
			continue
		case "numeral":
			st.numeral++
			continue
		case "latin":
			st.latin++
			continue
		case "han":
			st.han++
			continue
		case "hangul":
			st.hangul++
			continue
		case "block":
			st.block++
			continue
		}

		word, ok := summarize(name)
		if !ok {
			st.unsummar++
			if len(st.unsummExamp) < 25 {
				st.unsummExamp = append(st.unsummExamp, fmt.Sprintf("U+%04X %s -> %s", r, name, word))
			}
		}
		st.kept++
		entries = append(entries, kept{r: r, word: word})
	}
	if err := sc.Err(); err != nil {
		return err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].r < entries[j].r })

	// intern distinct words -> blob + offsets.
	idOf := map[string]int{}
	var order []string
	for _, e := range entries {
		if _, seen := idOf[e.word]; !seen {
			idOf[e.word] = len(order)
			order = append(order, e.word)
		}
	}

	var blob strings.Builder
	offsets := make([]int, len(order)+1)
	for i, w := range order {
		offsets[i] = blob.Len()
		blob.WriteString(w)
	}
	offsets[len(order)] = blob.Len()

	inFile, err = filepath.Rel(filepath.Dir(ucdDir), inFile)
	if err != nil {
		return err
	}
	// The generated file records this path, so keep it slash-separated: regenerating on Windows must not rewrite
	// "v15/DerivedName.txt" as "v15\DerivedName.txt" and churn every table.
	inFile = filepath.ToSlash(inFile)

	if err := emit(inFile, pkg, outFile, buildTag, entries, idOf, order, blob.String(), offsets); err != nil {
		return err
	}

	report(&st, entries, order, blob.Len(), offsets)

	return nil
}

func loadPictographic(path string) ([]rrange, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []rrange
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		codes, prop, ok := strings.Cut(line, ";")
		if !ok || strings.TrimSpace(prop) != "Extended_Pictographic" {
			continue
		}
		codes = strings.TrimSpace(codes)
		var lo, hi rune
		if a, b, isRange := strings.Cut(codes, ".."); isRange {
			_, _ = fmt.Sscanf(a, "%X", &lo)
			_, _ = fmt.Sscanf(strings.TrimLeft(b, "."), "%X", &hi)
		} else {
			_, _ = fmt.Sscanf(codes, "%X", &lo)
			hi = lo
		}
		out = append(out, rrange{lo, hi})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].lo < out[j].lo })
	return out, sc.Err()
}

func isPictographic(r rune) bool {
	i := sort.Search(len(pictRanges), func(i int) bool { return pictRanges[i].hi >= r })
	return i < len(pictRanges) && r >= pictRanges[i].lo && r <= pictRanges[i].hi
}

type kept struct {
	r    rune
	word string
}

// exclusion tallies, in evaluation order.
type stats struct {
	total       int
	rangeLine   int
	ascii       int
	nonprint    int
	combining   int
	separator   int
	digit       int
	latin       int
	numeral     int
	han         int
	hangul      int
	block       int
	kept        int
	unsummar    int
	unsummExamp []string
}

// ─── classification
// ────────────────────────────────────────────────────────.

func classify(r rune) string {
	switch {
	case r < 0x80:
		return "ascii" // handled directly by the mangler / symbol map
	case !unicode.IsGraphic(r) || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Cs, unicode.Co):
		return "nonprint"
	case unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me):
		return "combining"
	case unicode.In(r, unicode.Zs, unicode.Zl, unicode.Zp,
		unicode.Pd, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf, unicode.Pc, unicode.Po,
		unicode.Sk, unicode.Lm):
		return "separator" // elided as separators / spacing modifiers
	case unicode.Is(unicode.Nd, r):
		return "digit" // handled by numbers digit-offset
	case unicode.In(r, unicode.No, unicode.Nl):
		return "numeral" // Unicode numerals (½, Ⅶ, ②) routed to the numbers engine (numbers.RuneNumber)
	case unicode.Is(unicode.Latin, r):
		return "latin" // handled by the ASCII fold map
	case unicode.Is(unicode.Han, r):
		return "han" // CJK ideographs: no phonetic name -> elide
	case unicode.Is(unicode.Hangul, r):
		// all Hangul: the algorithmic syllables AND the standalone Jamo letters. The Jamo do carry real names
		// (ㄱ = "HANGUL LETTER KIYEOK") — naming them is a possible future improvement, elided for now.
		return "hangul"
	case inElideBlock(r) && !isPictographic(r):
		return "block" // decorative / technical symbol blocks, minus real emoji (Extended_Pictographic)
	}
	return ""
}

// elideBlocks are decorative or technical symbol ranges whose names ("BOX DRAWINGS LIGHT HORIZONTAL", "BRAILLE PATTERN
// DOTS-…") are noise as identifiers.
//
// Kept explicit and tunable.
// Real emoji inside these ranges are protected by the Extended_Pictographic gate (classify), so mixed blocks (Misc
// Technical/Symbols, Dingbats) can be listed here: only their non-emoji members are elided (❤ ✈ ⌚ ☀ ☯
// survive; ✓ ⌂ ☈ ─ ⠁ do not).
var elideBlocks = []struct{ lo, hi rune }{
	{0x2300, 0x23FF},   // Miscellaneous Technical (⌚⌛⏰ kept via gate)
	{0x2400, 0x243F},   // Control Pictures
	{0x2440, 0x245F},   // Optical Character Recognition
	{0x2500, 0x257F},   // Box Drawing
	{0x2580, 0x259F},   // Block Elements
	{0x25A0, 0x25FF},   // Geometric Shapes
	{0x2600, 0x26FF},   // Miscellaneous Symbols (☀☂☯ kept via gate)
	{0x2700, 0x27BF},   // Dingbats (✂✈❤ kept via gate)
	{0x2800, 0x28FF},   // Braille Patterns
	{0x4DC0, 0x4DFF},   // Yijing Hexagram Symbols
	{0x1CC00, 0x1CEBF}, // Symbols for Legacy Computing Supplement
	{0x1D000, 0x1D24F}, // Musical / Byzantine / Ancient Greek Musical
	{0x1D300, 0x1D356}, // Tai Xuan Jing Symbols
	{0x1D360, 0x1D37F}, // Counting Rod Numerals
	{0x1F000, 0x1F02F}, // Mahjong Tiles
	{0x1F030, 0x1F09F}, // Domino Tiles
	{0x1FB00, 0x1FBFF}, // Symbols for Legacy Computing
}

func inElideBlock(r rune) bool {
	for _, b := range elideBlocks {
		if r >= b.lo && r <= b.hi {
			return true
		}
	}
	return false
}

// ─── name summarization (strip taxonomy, keep the distinctive remainder) ─────

var (
	reLetter    = regexp.MustCompile(`(?:^|\s)LETTER\s+(.+)$`)
	reSyllable  = regexp.MustCompile(`(?:^|\s)SYLLABLE\s+(.+)$`)
	reCharacter = regexp.MustCompile(`(?:^|\s)CHARACTER\s+(.+)$`)
	reSign      = regexp.MustCompile(`^(.+?)\s+(?:SIGN|SYMBOL)$`)
	reNumber    = regexp.MustCompile(`(?:VULGAR FRACTION|ROMAN NUMERAL|DIGIT|NUMBER)\s+(.+)$`)
	reWith      = regexp.MustCompile(`\s+WITH\s+.*$`)
	reSpaces    = regexp.MustCompile(`\s+`)
)

// summarize returns the lowercase distinctive remainder and whether a taxonomy rule matched.
//
// The remainder keeps internal spaces/hyphens — the mangler re-segments and re-cases it. skipwords are dropped when
// collapsing a 3+ word phrase to its distinctive token: grammatical glue plus the common Unicode symbol-name qualifiers
// (colors, weights, orientations) that precede the real noun — so HEAVY BLACK HEART reduces to "heart", not "heavy".
var skipwords = map[string]struct{}{
	// grammatical
	"of": {}, "the": {}, "and": {}, "for": {}, "with": {}, "in": {}, "to": {},
	"a": {}, "an": {}, "el": {}, "de": {}, "or": {}, "on": {}, "by": {},
	// qualifiers (color / weight / size / orientation)
	"black": {}, "white": {}, "heavy": {}, "light": {}, "medium": {}, "bold": {},
	"small": {}, "large": {}, "big": {}, "tall": {}, "short": {}, "double": {}, "triple": {},
	"left": {}, "right": {}, "up": {}, "down": {}, "upper": {}, "lower": {},
	"upwards": {}, "downwards": {}, "leftwards": {}, "rightwards": {},
	"north": {}, "south": {}, "east": {}, "west": {},
	"vertical": {}, "horizontal": {}, "diagonal": {}, "turned": {}, "reversed": {},
	"rotated": {}, "inverted": {}, "outlined": {}, "filled": {}, "dotted": {}, "dashed": {},
	"open": {}, "closed": {}, "solid": {}, "circled": {}, "squared": {}, "negative": {},
}

// collapse enforces "one readable word is enough": remainders of <=2 words are kept whole (GrinningFace, ThumbsUp,
// KoKai), but 3+ word phrases reduce to their single most-distinctive token — the longest word that is neither glue
// nor a qualifier (heavy black heart -> heart; place of sajdah -> sajdah; fehu feoh fe f -> fehu).
func collapse(s string) string {
	f := strings.Fields(s)
	if len(f) <= 2 {
		return s
	}
	best := ""
	for _, w := range f {
		if _, skip := skipwords[w]; skip {
			continue
		}
		if len(w) > len(best) {
			best = w
		}
	}
	if best == "" {
		return f[len(f)-1]
	}
	return best
}

// wordOverrides replaces a collapsed word with a more natural spelling than the (deliberately technical) Unicode name.
//
// Applied after collapse, as an exact whole-word match. Keep it tiny and defensible: only for cases where Unicode's
// choice reads as a database artifact and the natural English word is unambiguous.
var wordOverrides = map[string]string{
	"lamda": "lambda", // Unicode names λ "LAMDA"; "lambda" is the standard English spelling
}

func summarize(name string) (string, bool) {
	norm := func(s string) string {
		s = reWith.ReplaceAllString(s, "") // drop "... WITH ACUTE" diacritic tails
		s = reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
		w := collapse(strings.ToLower(s))
		if o, ok := wordOverrides[w]; ok {
			return o
		}
		return w
	}
	for _, re := range []*regexp.Regexp{reLetter, reSyllable, reCharacter, reNumber} {
		if m := re.FindStringSubmatch(name); m != nil {
			return norm(m[1]), true
		}
	}
	if m := reSign.FindStringSubmatch(name); m != nil {
		return norm(m[1]), true
	}
	// no taxonomy rule matched: strip a leading run of script tokens ("ARABIC PLACE OF SAJDAH" -> "place of sajdah"), then
	// keep the remainder as a failsafe.
	if stripped, ok := stripScriptPrefix(name); ok {
		return norm(stripped), true
	}
	return norm(name), false
}

// scriptTokens is the set of uppercase, single-word script-name fragments derived from unicode.Scripts (e.g.
// "Old_Persian" -> {OLD, PERSIAN}) — used to peel taxonomy prefixes.
var scriptTokens = buildScriptTokens()

func buildScriptTokens() map[string]struct{} {
	set := map[string]struct{}{}
	for name := range unicode.Scripts {
		for _, part := range strings.Split(name, "_") {
			set[strings.ToUpper(part)] = struct{}{}
		}
	}
	// common block words that head descriptive names but aren't script keys.
	for _, w := range []string{"ARABIC-INDIC", "MODIFIER", "SMALL", "CAPITAL"} {
		set[w] = struct{}{}
	}
	return set
}

func stripScriptPrefix(name string) (string, bool) {
	words := strings.Fields(name)
	i := 0
	for i < len(words)-1 { // never strip the last word
		if _, ok := scriptTokens[strings.ToUpper(words[i])]; !ok {
			break
		}
		i++
	}
	if i == 0 {
		return name, false
	}
	return strings.Join(words[i:], " "), true
}

// ─── emit
// ────────────────────────────────────────────────────────────────────.

// offsetBits is the width of a blob offset.
//
// 17 bits suffice for the current blob (~103 KiB); 18 gives Unicode-17 / Go-1.27 headroom (256 KiB ceiling) and,
// crucially, addresses the whole blob so no banking is needed.
// Stored as uint16 low + 2-bit high sidecar.
const offsetBits = 18

func emit(source, packageName, outFile, buildTag string, entries []kept, idOf map[string]int, order []string, blob string, offsets []int) error {
	// Interval-encode the sorted rune keys into maximal runs of consecutive codepoints.
	var runStart []rune
	var runFirstIndex []int
	for i, e := range entries {
		if i == 0 || e.r != entries[i-1].r+1 {
			runStart = append(runStart, e.r)
			runFirstIndex = append(runFirstIndex, i)
		}
	}
	runFirstIndex = append(runFirstIndex, len(entries)) // sentinel = N

	// 18-bit offsets must fit; otherwise the blob outgrew the offset width and needs a wider scheme.
	if maxOff := offsets[len(offsets)-1]; maxOff >= 1<<offsetBits {
		return fmt.Errorf("blob offset %d exceeds %d-bit range", maxOff, offsetBits)
	}

	var b bytes.Buffer
	fmt.Fprint(&b, "// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers\n// SPDX-License-Identifier: Apache-2.0\n\n")
	if buildTag != "" {
		fmt.Fprintf(&b, "//go:build %s\n\n", buildTag)
	}
	fmt.Fprintf(&b, "// Code generated by gen_runewords.go. DO NOT EDIT.\n\npackage %s\n\n", packageName)
	fmt.Fprintf(&b, "// Generated from %s.\n", source)
	fmt.Fprintf(&b, "// wordBlob concatenates every distinct word (%d of them).\n", len(order))
	fmt.Fprintf(&b, "const wordBlob = %q\n\n", blob)

	// Offsets: 18-bit, split into a uint16 low array + a 2-bit high sidecar (4 entries per byte). off(id) =
	// uint32(wordOffHi[id>>2]>>(2*(id&3))&3)<<16 | uint32(wordOffLo[id]).
	fmt.Fprint(&b, "// wordOffLo[id]:wordOffLo[id+1] (with high bits from wordOffHi) slices wordBlob for word id.\n")
	fmt.Fprint(&b, "var wordOffLo = []uint16{")
	for i, off := range offsets {
		if i%16 == 0 {
			b.WriteString("\n\t")
		}
		fmt.Fprintf(&b, "%d, ", off&0xFFFF)
	}
	b.WriteString("\n}\n\n")

	hi := make([]byte, (len(offsets)+3)/4)
	for i, off := range offsets {
		hi[i>>2] |= byte((off>>16)&0x3) << (2 * (i & 3))
	}
	fmt.Fprintf(&b, "// wordOffHi holds the high 2 bits of each 18-bit offset, packed 4 entries per byte.\n")
	fmt.Fprintf(&b, "var wordOffHi = []byte{")
	for i, v := range hi {
		if i%16 == 0 {
			b.WriteString("\n\t")
		}
		fmt.Fprintf(&b, "0x%02X, ", v)
	}
	b.WriteString("\n}\n\n")

	// Interval keys: runStart[i] starts maximal run i; runFirstIndex[i] is that run's first global rune index.
	fmt.Fprintf(&b, "// runStart[i] is the first rune of maximal consecutive run i;\n")
	fmt.Fprintf(&b, "// runFirstIndex[i] is its global rune index (runFirstIndex[len]=N sentinel).\n")
	fmt.Fprintf(&b, "var runStart = []uint32{")
	for i, r := range runStart {
		if i%12 == 0 {
			b.WriteString("\n\t")
		}
		fmt.Fprintf(&b, "0x%04X, ", r)
	}
	b.WriteString("\n}\n\n")

	fmt.Fprintf(&b, "var runFirstIndex = []uint32{")
	for i, v := range runFirstIndex {
		if i%16 == 0 {
			b.WriteString("\n\t")
		}
		fmt.Fprintf(&b, "%d, ", v)
	}
	b.WriteString("\n}\n\n")

	// nameWordID: word id per global rune index (i = runFirstIndex[run] + (r - runStart[run])).
	fmt.Fprintf(&b, "// nameWordID[i] is the word id for the i-th covered rune.\n")
	fmt.Fprintf(&b, "var nameWordID = []uint16{")
	for i, e := range entries {
		if i%16 == 0 {
			b.WriteString("\n\t")
		}
		fmt.Fprintf(&b, "%d, ", idOf[e.word])
	}
	_, _ = b.WriteString("\n}\n")

	out, err := format.Source(b.Bytes())
	if err != nil {
		// write unformatted for debugging
		_ = os.WriteFile(outFile, b.Bytes(), 0o644) //nolint:gosec // permissions are okay for our codegen
		return fmt.Errorf("format: %w", err)
	}
	return os.WriteFile(outFile, out, 0o644) //nolint:gosec // permissions are okay for our codegen
}

func report(st *stats, entries []kept, order []string, blobLen int, offsets []int) {
	e := func(f string, a ...any) { fmt.Fprintf(os.Stderr, f, a...) }
	e("\n=== rune-word generator report ===\n")
	e("data lines (excl. ranges):   %6d   (+%d algorithmic range lines skipped)\n", st.total, st.rangeLine)
	e("excluded — ascii:            %6d\n", st.ascii)
	e("excluded — nonprint:         %6d\n", st.nonprint)
	e("excluded — combining:        %6d\n", st.combining)
	e("excluded — separator/mod:    %6d\n", st.separator)
	e("excluded — digit (Nd):       %6d\n", st.digit)
	e("excluded — latin (fold):     %6d\n", st.latin)
	e("excluded — numeral (No/Nl):  %6d\n", st.numeral)
	e("excluded — han (CJK):        %6d\n", st.han)
	e("excluded — hangul:           %6d\n", st.hangul)
	e("excluded — block (decor):    %6d\n", st.block)
	e("KEPT:                        %6d   (%.1f%% of data lines)\n", st.kept, 100*float64(st.kept)/float64(st.total))
	e("  of which unsummarized:     %6d   (no taxonomy rule matched)\n", st.unsummar)
	e("distinct words (vocabulary): %6d\n", len(order))

	// short (<=3 words) vs phrase (4+ words) split, and blob size if we kept short only.
	var shortN, phraseN int
	shortSet := map[string]struct{}{}
	for _, en := range entries {
		if strings.Count(en.word, " ") <= 2 {
			shortN++
			shortSet[en.word] = struct{}{}
		} else {
			phraseN++
		}
	}
	shortBlob := 0
	for w := range shortSet {
		shortBlob += len(w)
	}
	e("  short (<=3 words):         %6d   -> distinct %d, blob %d B (~%d KiB)\n",
		shortN, len(shortSet), shortBlob, shortBlob/1024)
	e("  phrase (4+ words):         %6d   (the verbose tail)\n", phraseN)

	nRuns := 0
	for i := range entries {
		if i == 0 || entries[i].r != entries[i-1].r+1 {
			nRuns++
		}
	}
	blobB := blobLen
	runStartB := 4 * nRuns
	runFirstB := 4 * (nRuns + 1)
	loB := 2 * len(offsets)
	hiB := (len(offsets) + 3) / 4
	widB := 2 * len(entries)
	total := blobB + runStartB + runFirstB + loB + hiB + widB
	e("\n--- table footprint (data bytes, excl. Go source overhead) ---\n")
	e("wordBlob:      %8d B\n", blobB)
	e("wordOffLo:     %8d B  (%d x uint16)\n", loB, len(offsets))
	e("wordOffHi:     %8d B  (%d entries, 2b packed 4/byte)\n", hiB, len(offsets))
	e("runStart:      %8d B  (%d runs x uint32)\n", runStartB, nRuns)
	e("runFirstIndex: %8d B  (%d x uint32)\n", runFirstB, nRuns+1)
	e("nameWordID:    %8d B  (%d x uint16)\n", widB, len(entries))
	e("TOTAL:         %8d B  (~%d KiB)\n", total, total/1024)

	if len(st.unsummExamp) > 0 {
		e("\n--- unsummarized samples (candidates for more rules / exclusion) ---\n")
		for _, s := range st.unsummExamp {
			e("  %s\n", s)
		}
	}

	// most-shared words (interning payoff)
	count := map[string]int{}
	for _, en := range entries {
		count[en.word]++
	}
	type wc struct {
		w string
		n int
	}
	top := make([]wc, 0, len(count))
	for w, n := range count {
		top = append(top, wc{w, n})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
	e("\n--- most-shared words (interning payoff) ---\n")
	for i := 0; i < 12 && i < len(top); i++ {
		e("  %4d x  %q\n", top[i].n, top[i].w)
	}
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
