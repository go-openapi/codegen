// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"go/ast"
	"go/token"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"
)

func TestRegister(t *testing.T) {
	t.Cleanup(func() { Register(nil) })

	t.Run("should register nothing to begin with", func(t *testing.T) {
		Register(nil)

		assert.Nil(t, Registered())
	})

	t.Run("should return what was registered", func(t *testing.T) {
		called := 0
		Register(func(*token.FileSet, *ast.File) { called++ })

		rules := Registered()
		require.NotNil(t, rules)

		rules(nil, nil)
		assert.Equal(t, 1, called)
	})

	t.Run("should let a second registration replace the first", func(t *testing.T) {
		first, second := 0, 0
		Register(func(*token.FileSet, *ast.File) { first++ })
		Register(func(*token.FileSet, *ast.File) { second++ })

		Registered()(nil, nil)

		assert.Zero(t, first)
		assert.Equal(t, 1, second)
	})

	t.Run("should forget a rule set when nil is registered", func(t *testing.T) {
		Register(func(*token.FileSet, *ast.File) {})
		Register(nil)

		assert.Nil(t, Registered())
	})
}
