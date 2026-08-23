// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package reports

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/template"
)

// DumpOption configures a single call to [Repository.Dump].
//
// Rendering settings belong to the call rather than to the repository: how a document looks is the
// business of whoever asks for it, and the template that lays it out is compiled when it is used.
//
// An option that cannot be honoured reports an error from [Dump], rather than at the point where it
// is constructed.
type (
	DumpOption func(dumpOptions) dumpOptions

	dumpOptions struct {
		layout string
		funcs  template.FuncMap
		err    error
	}
)

// withError keeps the first failure and lets the rest of the chain run.
func (o dumpOptions) withError(err error) dumpOptions {
	if o.err == nil {
		o.err = err
	}

	return o
}

// applyDumpWithDefaults folds the chain over the defaults, left to right.
//
// The defaults are built afresh on every call, so no two dumps share the funcmap.
func applyDumpWithDefaults(opts []DumpOption) (dumpOptions, error) {
	o := dumpOptions{
		layout: markdownLayout,
		funcs:  template.FuncMap{"anchor": anchor, "weigh": weigh, "plural": plural},
	}

	for _, apply := range opts {
		o = apply(o)
	}

	if o.err != nil {
		return dumpOptions{}, o.err
	}

	return o, nil
}

// WithTemplate lays the document out with a template of the caller's own.
//
// The template is executed against a [Documentation]. It is compiled when [Repository.Dump] runs,
// so a template that does not parse is reported by that call.
func WithTemplate(text string) DumpOption {
	return func(o dumpOptions) dumpOptions {
		if strings.TrimSpace(text) == "" {
			return o.withError(fmt.Errorf("the dump template is empty: %w", ErrReport))
		}

		o.layout = text

		return o
	}
}

// WithFuncMap adds functions a dump template of the caller's own may call.
func WithFuncMap(funcs template.FuncMap) DumpOption {
	return func(o dumpOptions) dumpOptions {
		maps.Copy(o.funcs, funcs)

		return o
	}
}

// Dump writes a documentation, as markdown by default.
//
// Use [WithTemplate] to lay the document out otherwise, or walk the [Documentation] directly for a
// format a text template cannot produce.
//
// Example:
//
//	documentation, err := repository.Documentation()
//	if err != nil {
//		return err
//	}
//
//	err = reports.Dump(w, documentation)
func Dump(w io.Writer, documentation Documentation, opts ...DumpOption) error {
	settings, err := applyDumpWithDefaults(opts)
	if err != nil {
		return err
	}

	layout, err := template.New("dump").Funcs(settings.funcs).Parse(settings.layout)
	if err != nil {
		return fmt.Errorf("could not parse the dump template: %w: %w", err, ErrReport)
	}

	if err := layout.Execute(w, documentation); err != nil {
		return fmt.Errorf("could not write the documentation: %w: %w", err, ErrReport)
	}

	return nil
}

// anchor turns a heading into the fragment a markdown renderer derives from it.
//
// The rule is the one github applies: lower case, spaces to dashes, everything else that is not a
// letter, a digit, a dash or an underscore dropped.
func anchor(heading string) string {
	var fragment strings.Builder

	for _, char := range strings.ToLower(heading) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9', char == '-', char == '_':
			fragment.WriteRune(char)
		case char == ' ':
			fragment.WriteRune('-')
		}
	}

	return fragment.String()
}

// plural writes a count with its noun, singular or not.
func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}

	if strings.HasSuffix(noun, "s") {
		return strconv.Itoa(count) + " " + noun + "es"
	}

	return strconv.Itoa(count) + " " + noun + "s"
}

// Root is a field of the data, with the number of paths that hang from it.
type Root struct {
	// Field is the field the paths start at.
	Field string

	// Paths is how many of them there are.
	Paths int
}

// Weights groups the paths of a fold by the fields they hang from.
//
// They spread very unevenly: a handful of fields hold a subtree the templates walk into, and
// everything else is read once. Separating the two shows which part of the data a template works
// on, without a list of counts that are all one.
type Weights struct {
	// Heavy holds the fields more than one path hangs from, heaviest first.
	Heavy []Root

	// Single holds the fields read once, sorted.
	Single []string
}

// weigh reduces a list of data paths to the fields they hang from, heaviest first.
//
// A folded contract holds thousands of paths for a template at the top of a call graph, which no
// reader gets through. The fields they start at are few, and their weight shows which part of
// the data a template mostly works on.
func weigh(paths []string) Weights {
	counted := make(map[string]int, len(paths))
	for _, path := range paths {
		field, _, _ := strings.Cut(strings.TrimPrefix(path, "."), ".")
		field, _, _ = strings.Cut(field, "[")

		if field != "" {
			counted["."+field]++
		}
	}

	var weights Weights
	for field, count := range counted {
		if count == 1 {
			weights.Single = append(weights.Single, field)

			continue
		}

		weights.Heavy = append(weights.Heavy, Root{Field: field, Paths: count})
	}

	slices.SortFunc(weights.Heavy, func(a, b Root) int {
		if a.Paths != b.Paths {
			return b.Paths - a.Paths
		}

		return strings.Compare(a.Field, b.Field)
	})
	slices.Sort(weights.Single)

	return weights
}

// markdownLayout is the document [Repository.Dump] produces when the caller asks for no other.
//
// It reports a fold by its weight rather than by its paths. A template at the top of a call graph
// reads thousands of them, and the detail of each is one hop away, on the page of the template
// that reads it.
const markdownLayout = `# Templates
{{ range .Assets }}
- [{{ .Path }}](#{{ anchor .Path }}){{ range .Templates }}
  - [{{ .Name }}](#{{ anchor .Name }}){{ end }}{{ end }}
{{ range $asset := .Assets }}
## {{ $asset.Path }}
{{ range $tpl := .Templates }}
### {{ .Name }}
{{ if .Inner }}
Declared by a define statement in {{ $asset.Path }}.
{{ end }}{{ if .Empty }}
{{ if .Inner }}This template renders nothing.{{ else }}This asset declares define statements only, so the template named after it renders nothing.{{ end }}
{{ end }}{{ range .Doc }}
{{ . }}
{{ end }}{{ if .Reads }}
**Reads** {{ range $index, $path := .Reads }}{{ if $index }}, {{ end }}` + "`" + `{{ $path }}` + "`" + `{{ end }}
{{ end }}{{ if .RootReads }}
**Reaches back to the root of its own data** for {{ range $index, $path := .RootReads }}{{ if $index }}, {{ end }}` + "`" + `{{ $path }}` + "`" + `{{ end }}
{{ end }}{{ if .Dependencies }}
**Calls**
{{ range .Dependencies }}- ` + "`" + `{{ .Name }}` + "`" + `, with {{ if .Data }}` + "`" + `{{ .Data }}` + "`" + `{{ else }}a value built at the call site{{ end }}{{ if .Folded }} ({{ plural .Folded "path" }}){{ end }}
{{ end }}{{ end }}{{ if .Transitive.Funcs }}
**Needs from the func map** {{ range $index, $name := .Transitive.Funcs }}{{ if $index }}, {{ end }}` + "`" + `{{ $name }}` + "`" + `{{ end }}
{{ end }}{{ if .UsedBy }}
**Called by** {{ range $index, $name := .UsedBy }}{{ if $index }}, {{ end }}[{{ $name }}](#{{ anchor $name }}){{ end }}
{{ end }}{{ if and .Transitive.Reaches .Transitive.Reads }}{{ with weigh .Transitive.Reads }}
**Folded** {{ plural (len $tpl.Transitive.Reads) "path" }}, through {{ plural (len $tpl.Transitive.Reaches) "template" }}.
{{ if .Heavy }}
Mostly under {{ range $index, $root := .Heavy }}{{ if $index }}, {{ end }}` + "`" + `{{ $root.Field }}` + "`" + ` ({{ $root.Paths }}){{ end }}.
{{ end }}{{ if .Single }}
Read once: {{ range $index, $field := .Single }}{{ if $index }}, {{ end }}` + "`" + `{{ $field }}` + "`" + `{{ end }}.
{{ end }}{{ end }}{{ end }}{{ if or .Transitive.Recursive .Dynamic .Unresolved }}
> {{ if .Transitive.Recursive }}Some of the templates it calls loop back, so the fold stops at the loop. {{ end }}{{ if .Dynamic }}It invokes a function held by its data, so what it reads is not fully known. {{ end }}{{ if .Unresolved }}{{ plural .Unresolved "access" }} hang from a value that could not be placed.{{ end }}
{{ end }}{{ end }}{{ end }}`
