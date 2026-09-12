module github.com/go-openapi/codegen/formatting/enable/gofumpt

go 1.26.0

require (
	github.com/go-openapi/codegen v0.0.2
	github.com/go-openapi/testify/v2 v2.8.0
	mvdan.cc/gofumpt v0.11.0
)

replace github.com/go-openapi/codegen => ../../..

require golang.org/x/tools v0.50.0 // indirect

replace github.com/go-openapi/codegen/mangling => ../../../mangling
