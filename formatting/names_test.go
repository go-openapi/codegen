// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"testing"

	"github.com/go-openapi/testify/v2/assert"

	"github.com/go-openapi/codegen/formatting"
)

func TestImportedPackageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		importPath string
		expected   string
	}{
		{
			name:       "should name a standard library package after its path",
			importPath: "strings",
			expected:   "strings",
		},
		{
			name:       "should name a package after the last element",
			importPath: "github.com/go-openapi/strfmt",
			expected:   "strfmt",
		},
		{
			name:       "should look past a module major version",
			importPath: "github.com/go-openapi/testify/v2",
			expected:   "testify",
		},
		{
			name:       "should look past a version element that is a real directory",
			importPath: "go.mongodb.org/mongo-driver/internal/aws/signer/v4",
			expected:   "signer",
		},
		{
			name:       "should look past an api version directory",
			importPath: "k8s.io/api/apps/v1",
			expected:   "apps",
		},
		{
			name:       "should drop a gopkg.in version suffix",
			importPath: "gopkg.in/yaml.v3",
			expected:   "yaml",
		},
		{
			name:       "should drop a go- prefix",
			importPath: "github.com/jessevdk/go-flags",
			expected:   "flags",
		},
		{
			name:       "should drop a -go suffix",
			importPath: "github.com/googleapis/gax-go",
			expected:   "gax",
		},
		{
			name:       "should take the last segment of a hyphenated name",
			importPath: "example.com/my-pkg",
			expected:   "pkg",
		},
		{
			name:       "should name nothing when no part of the path is an identifier",
			importPath: "example.com/2fa",
			expected:   "",
		},
		{
			name:       "should not name a keyword",
			importPath: "example.com/range",
			expected:   "",
		},
	}

	for _, toPin := range tests {
		test := toPin
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expected, formatting.ImportedPackageName(test.importPath))
		})
	}
}
