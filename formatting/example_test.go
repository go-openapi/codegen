// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/go-openapi/codegen/formatting"
)

// rendered stands for what a template produced: misformatted, unsorted, and importing more than the
// code uses.
const rendered = `package petstore

import (
"context"
"github.com/go-openapi/strfmt"
"strings"

"github.com/go-openapi/swag/conv"
)

func New(ctx context.Context) *strfmt.DateTime {
_ = ctx
_ = conv.Pointer(1)
return nil
}
`

func ExampleFormat() {
	if _, err := formatting.Format(os.Stdout, []byte(rendered)); err != nil {
		log.Fatal(err)
	}
	// Output:
	// package petstore
	//
	// import (
	//	"context"
	//
	//	"github.com/go-openapi/strfmt"
	//	"github.com/go-openapi/swag/conv"
	// )
	//
	// func New(ctx context.Context) *strfmt.DateTime {
	//	_ = ctx
	//	_ = conv.Pointer(1)
	//	return nil
	// }
}

func ExampleWithImportGroups() {
	if _, err := formatting.Format(os.Stdout, []byte(rendered),
		formatting.WithImportGroups("github.com/go-openapi/swag", "github.com/go-openapi"),
	); err != nil {
		log.Fatal(err)
	}
	// Output:
	// package petstore
	//
	// import (
	//	"context"
	//
	//	"github.com/go-openapi/swag/conv"
	//
	//	"github.com/go-openapi/strfmt"
	// )
	//
	// func New(ctx context.Context) *strfmt.DateTime {
	//	_ = ctx
	//	_ = conv.Pointer(1)
	//	return nil
	// }
}

// ExampleWithForceImportsPruning shows what the promise buys, on an import nothing uses.
func ExampleWithForceImportsPruning() {
	const src = `package p

import (
	"bytes"
	"github.com/go-openapi/strfmt"
)

var _ bytes.Buffer
`

	show := func(label string, opts ...formatting.Option) {
		fmt.Println(label)

		if _, err := formatting.Format(os.Stdout, []byte(src), opts...); err != nil {
			log.Fatal(err)
		}
	}

	show("// strfmt stays: nothing states what that package declares")
	show("// and goes once the caller promises it follows the convention",
		formatting.WithForceImportsPruning())

	// Output:
	// // strfmt stays: nothing states what that package declares
	// package p
	//
	// import (
	//	"bytes"
	//
	//	"github.com/go-openapi/strfmt"
	// )
	//
	// var _ bytes.Buffer
	// // and goes once the caller promises it follows the convention
	// package p
	//
	// import (
	//	"bytes"
	// )
	//
	// var _ bytes.Buffer
}

// ExampleWithResolvedImports shows the map covering a package the promise gets wrong.
//
// github.com/json-iterator/go declares jsoniter, and no rule reading the path says so.
func ExampleWithResolvedImports() {
	const src = `package p

import "github.com/json-iterator/go"

var _ = jsoniter.Marshal
`

	show := func(label string, opts ...formatting.Option) {
		fmt.Println(label)

		if _, err := formatting.Format(os.Stdout, []byte(src), opts...); err != nil {
			log.Fatal(err)
		}
	}

	show("// the promise alone deletes an import the code uses",
		formatting.WithForceImportsPruning())
	show("// naming the package keeps it, and the promise still covers the rest",
		formatting.WithForceImportsPruning(),
		formatting.WithResolvedImports(map[string]string{
			"github.com/json-iterator/go": "jsoniter",
		}),
	)

	// Output:
	// // the promise alone deletes an import the code uses
	// package p
	//
	// var _ = jsoniter.Marshal
	// // naming the package keeps it, and the promise still covers the rest
	// package p
	//
	// import "github.com/json-iterator/go"
	//
	// var _ = jsoniter.Marshal
}

// ExampleWithSimplifiedImportAliases shows an alias written for safety being taken back out.
//
// A template that aliases every import gets exact pruning with no option at all, because an alias
// states the name. This hands the reader ordinary Go once the name is proven.
func ExampleWithSimplifiedImportAliases() {
	const src = `package p

import (
	fmt "fmt"
	strfmt "github.com/go-openapi/strfmt"
	jsoniter "github.com/json-iterator/go"
)

var (
	_ = fmt.Sprint
	_ = strfmt.Date{}
	_ = jsoniter.Marshal
)
`

	if _, err := formatting.Format(os.Stdout, []byte(src),
		formatting.WithSimplifiedImportAliases(),
		formatting.WithResolvedImports(map[string]string{
			"github.com/go-openapi/strfmt": "strfmt",
			"github.com/json-iterator/go":  "jsoniter",
		}),
	); err != nil {
		log.Fatal(err)
	}

	// jsoniter keeps its alias although the name is proven: the path does not say jsoniter, so the
	// bare import would leave nothing that does.

	// Output:
	// package p
	//
	// import (
	//	"fmt"
	//
	//	"github.com/go-openapi/strfmt"
	//	jsoniter "github.com/json-iterator/go"
	// )
	//
	// var (
	//	_ = fmt.Sprint
	//	_ = strfmt.Date{}
	//	_ = jsoniter.Marshal
	// )
}

// ExampleImportsReport shows what Format could and could not decide.
func ExampleImportsReport() {
	const src = `package p

import (
	"bytes"
	_ "embed"
	"strings"
	sf "github.com/go-openapi/swag"
	"github.com/go-openapi/strfmt"
	"github.com/json-iterator/go"
)

var (
	_ bytes.Buffer
	_ = jsoniter.Marshal
)
`

	report, err := formatting.Format(io.Discard, []byte(src))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(report)
	fmt.Println()
	fmt.Println("in doubt:", report.PathsInDoubt())

	// Output:
	// bytes (bytes) used
	// embed (_) blank
	// github.com/go-openapi/strfmt (?) in doubt
	// github.com/go-openapi/swag (sf) pruned
	// github.com/json-iterator/go (?) in doubt
	// strings (strings) pruned
	//
	// in doubt: [github.com/go-openapi/strfmt github.com/json-iterator/go]
}

// ExampleImportsReport_HasImportsInDoubt shows the check worth making before trusting the output.
func ExampleImportsReport_HasImportsInDoubt() {
	const settled = `package p

import "bytes"

var _ bytes.Buffer
`

	report, err := formatting.Format(io.Discard, []byte(settled))
	if err != nil {
		log.Fatal(err)
	}

	// nothing left in doubt: pruning was exact, and the output holds no import the file does not use
	fmt.Println(report.HasImportsInDoubt())

	// Output:
	// false
}
