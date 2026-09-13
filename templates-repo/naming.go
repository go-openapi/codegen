// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"strings"

	"github.com/go-openapi/codegen/mangling"
)

// nameMangler recases an asset path into the name a template answers to.
//
// It reads "/" as a token separator on top of the usual ones, so a whole address is recased in one
// pass and a directory carries no more weight than any other word of a name.
//
// A [mangling.Mangler] is an immutable value, safe for concurrent use, so one instance serves
// every repository.
var nameMangler = mangling.MakeMangler(
	mangling.WithTokenOptions(mangling.WithExtraTokenSeparator('/')),
)

// DefaultExtension is the file extension a repository recognizes as a template when the caller
// declares no other with [WithExtensions].
const DefaultExtension = ".gotmpl"

// TemplateName returns the name a repository gives an asset at a path.
//
// Call it before a repository exists, to name the roots a build is scoped to or to pick the
// sources to declare. [Repository.NameOf] answers the same question once the repository is
// built.
//
// The extensions are those the repository recognizes, [DefaultExtension] when none is given.
//
// Example:
//
//	repo.TemplateName("validation/primitive.gotmpl")  ->  validationPrimitive
func TemplateName(assetPath string, extensions ...string) string {
	if len(extensions) == 0 {
		extensions = []string{DefaultExtension}
	}

	return templateName(assetPath, extensions)
}

// templateName derives the name of a template from the path of the asset that declares it.
//
// The path is expected to be slash-separated and cleaned, which every source guarantees. The
// supported extension is trimmed, and the rest is camel-cased, "/" counting as a word boundary
// like any other.
//
// Example:
//
//	validation/primitive.gotmpl  ->  validationPrimitive
//	swagger_json_embed.gotmpl    ->  swaggerJsonEmbed
func templateName(assetPath string, extensions []string) string {
	return nameMangler.Camelize(trimExtension(assetPath, extensions))
}

// templateName derives the name of a template with the extensions this repository recognizes.
func (o options) templateName(assetPath string) string {
	return templateName(assetPath, o.extensions)
}

// trimmedPath is the address an asset declares its own template at.
//
// It is the asset path with the extension trimmed, and nothing else. An address is never mangled,
// so an author writes a reference the way the tree is laid out on disk.
func (o options) trimmedPath(assetPath string) string {
	return trimExtension(assetPath, o.extensions)
}

// trimExtension removes from a name the first supported extension it ends with.
func trimExtension(name string, extensions []string) string {
	for _, extension := range extensions {
		if trimmed, found := strings.CutSuffix(name, extension); found {
			return trimmed
		}
	}

	return name
}

// hasSupportedExtension reports whether an asset is recognized as a template.
func (o options) hasSupportedExtension(name string) bool {
	for _, extension := range o.extensions {
		if strings.HasSuffix(name, extension) {
			return true
		}
	}

	return false
}
