// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/go-openapi/codegen/formatting"
	"github.com/go-openapi/codegen/formatting/resolve"
)

// ExampleNames reads the name a package declares, which its import path does not give.
func ExampleNames() {
	names, err := resolve.Names(context.Background(), []string{
		"net/http",
		"math/rand/v2",
		"golang.org/x/tools/go/ast/astutil",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, importPath := range []string{"net/http", "math/rand/v2", "golang.org/x/tools/go/ast/astutil"} {
		fmt.Printf("%-34s %s\n", importPath, names[importPath])
	}

	// Output:
	// net/http                           http
	// math/rand/v2                       rand
	// golang.org/x/tools/go/ast/astutil  astutil
}

// Example runs the whole loop: format, resolve what the report could not settle, format again.
//
// One pass over the generated tree produces the map. Commit it, and every later run needs only
// [formatting.WithResolvedImports], with no toolchain and no module cache.
func Example() {
	const rendered = `package p

import (
	"bytes"
	"golang.org/x/tools/go/ast/astutil"
)

var _ bytes.Buffer
`

	report, err := formatting.Format(io.Discard, []byte(rendered))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("first pass leaves in doubt:", report.PathsInDoubt())

	names, err := resolve.Names(context.Background(), report.PathsInDoubt())
	if err != nil {
		log.Fatal(err)
	}

	settled, err := formatting.Format(os.Stdout, []byte(rendered), formatting.WithResolvedImports(names))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("still in doubt:", settled.HasImportsInDoubt())

	// Output:
	// first pass leaves in doubt: [golang.org/x/tools/go/ast/astutil]
	// package p
	//
	// import (
	//	"bytes"
	// )
	//
	// var _ bytes.Buffer
	// still in doubt: false
}
