// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting

import (
	"testing"

	"github.com/go-openapi/testify/v2/assert"
)

func TestImportedPackageNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		importPath string
		expected   []string
	}{
		{
			name:       "should name a standard library package after its path",
			importPath: "strings",
			expected:   []string{"strings"},
		},
		{
			name:       "should name a package after the last element",
			importPath: "github.com/go-openapi/strfmt",
			expected:   []string{"strfmt"},
		},
		{
			name:       "should offer the directory above a major version suffix",
			importPath: "github.com/go-openapi/testify/v2",
			expected:   []string{"v2", "testify"},
		},
		{
			name:       "should keep the version itself, since a package may be named v1",
			importPath: "k8s.io/api/apps/v1",
			expected:   []string{"v1", "apps"},
		},
		{
			name:       "should drop a gopkg.in version suffix",
			importPath: "gopkg.in/yaml.v3",
			expected:   []string{"yaml"},
		},
		{
			name:       "should drop a go- prefix",
			importPath: "github.com/jessevdk/go-flags",
			expected:   []string{"flags", "goflags"},
		},
		{
			name:       "should offer the last element of a hyphenated name first",
			importPath: "example.com/my-pkg",
			expected:   []string{"pkg", "mypkg", "my"},
		},
		{
			name:       "should drop a -go suffix",
			importPath: "github.com/googleapis/gax-go",
			expected:   []string{"gax", "gaxgo"},
		},
		{
			name:       "should name nothing when no candidate is an identifier",
			importPath: "example.com/2fa",
			expected:   nil,
		},
		{
			name:       "should offer the directory above a version element that is a real directory",
			importPath: "go.mongodb.org/mongo-driver/internal/aws/signer/v4",
			expected:   []string{"v4", "signer"},
		},
		{
			name:       "should not name a keyword",
			importPath: "example.com/range",
			expected:   nil,
		},
		{
			name:       "should not name the blank identifier",
			importPath: "example.com/_",
			expected:   nil,
		},
		{
			name:       "should offer the likelier name first",
			importPath: "gopkg.in/check.v1",
			expected:   []string{"check"},
		},
	}

	for _, toPin := range tests {
		test := toPin
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expected, importedPackageNames(test.importPath))
		})
	}
}
