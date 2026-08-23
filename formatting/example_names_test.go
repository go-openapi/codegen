// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"fmt"

	"github.com/go-openapi/codegen/formatting"
)

// ExampleImportedPackageName shows a generator naming the imports it is about to write.
//
// The two Kubernetes packages both declare v1, so they collide under that name. The alias carries the
// name this function picked into the file.
func ExampleImportedPackageName() {
	for _, importPath := range []string{
		"context",
		"k8s.io/api/apps/v1",
		"k8s.io/api/core/v1",
		"github.com/go-openapi/testify/v2",
	} {
		fmt.Printf("%s %q\n", formatting.ImportedPackageName(importPath), importPath)
	}

	// Output:
	// context "context"
	// apps "k8s.io/api/apps/v1"
	// core "k8s.io/api/core/v1"
	// testify "github.com/go-openapi/testify/v2"
}
