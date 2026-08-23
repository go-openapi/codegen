// The corpus is a module of its own so that the import paths a fixture needs are not requirements
// of github.com/go-openapi/codegen. The go tool ignores a directory named testdata, so nothing here
// joins the parent module, and go.work leaves it out.
//
// Every .go file here is a golden: `go build ./...` from this directory proves that what the
// formatter produced still compiles, which is what catches an import dropped by mistake. The .input
// files beside them are what the formatter is given.
module github.com/go-openapi/codegen/formatting/testdata/corpus

go 1.25.0

require (
	github.com/go-openapi/swag/conv v0.29.0
	gopkg.in/yaml.v3 v3.0.1
)
