module github.com/go-openapi/codegen

go 1.25.0

require (
	github.com/go-openapi/codegen/mangling v0.0.0
	github.com/go-openapi/inflect v1.0.0
	github.com/go-openapi/swag/conv v0.29.0
	github.com/go-openapi/swag/pools v0.29.0
	github.com/go-openapi/testify/v2 v2.6.1
	golang.org/x/mod v0.40.0
	golang.org/x/tools v0.49.0
)

replace github.com/go-openapi/codegen/mangling => ./mangling

require (
	github.com/google/go-cmp v0.7.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
)
