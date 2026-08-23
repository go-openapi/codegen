// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/genapp"
)

// TestErrorsWrapOnce walks every error this package returns and counts the sentinel.
//
// A wrapper that attaches ErrGenApp to an error already carrying it says "code generation error"
// twice in one message and tells the reader nothing the first one did not. The sentinel is attached
// where a foreign error crosses into this package — from the repository, from text/template, from
// the formatter, from os — and a call that already wrapped one returns it as it is. This test is
// what keeps that true.
func TestErrorsWrapOnce(t *testing.T) {
	t.Parallel()

	sentinel := string(genapp.ErrGenApp)

	tests := []struct {
		name string
		call func(t *testing.T) error
	}{
		{
			name: "no repository",
			call: func(t *testing.T) error {
				_, err := genapp.New(genapp.WithOutputPath(t.TempDir()))

				return err
			},
		},
		{
			name: "no such template",
			call: func(t *testing.T) error {
				return newApp(t).Render(&bytes.Buffer{}, "noSuchTemplate", pet)
			},
		},
		{
			name: "template execution fails",
			call: func(t *testing.T) error {
				return newApp(t).Render(&bytes.Buffer{}, "model", struct{ Package string }{})
			},
		},
		{
			name: "rendered Go does not format",
			call: func(t *testing.T) error {
				return newApp(t).Render(&bytes.Buffer{}, "broken", pet)
			},
		},
		{
			name: "the writer refuses, formatted",
			call: func(t *testing.T) error {
				return newApp(t).Render(refusingWriter{}, "model", pet)
			},
		},
		{
			name: "the writer refuses, unformatted",
			call: func(t *testing.T) error {
				return newApp(t, genapp.WithSkipFormat(true)).Render(refusingWriter{}, "model", pet)
			},
		},
		{
			name: "the target directory cannot be created",
			call: func(t *testing.T) error {
				blocker := filepath.Join(t.TempDir(), "blocker")
				require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o600))

				return newApp(t, genapp.WithOutputPath(blocker)).RenderFile("sub/pet.go", "model", pet)
			},
		},
		{
			name: "a file target does not format",
			call: func(t *testing.T) error {
				return newApp(t, genapp.WithOutputPath(t.TempDir())).RenderFile("broken.go", "broken", pet)
			},
		},
		{
			name: "a file target names no template",
			call: func(t *testing.T) error {
				return newApp(t, genapp.WithOutputPath(t.TempDir())).RenderFile("pet.go", "noSuchTemplate", pet)
			},
		},
	}

	for _, toPin := range tests {
		test := toPin

		t.Run("should wrap "+test.name+" once", func(t *testing.T) {
			t.Parallel()

			err := test.call(t)

			require.Error(t, err)
			assert.ErrorIs(t, err, genapp.ErrGenApp)
			assert.Equal(t, 1, strings.Count(err.Error(), sentinel), "%q", err)
		})
	}
}

var errRefused = errors.New("writer refused")

type refusingWriter struct{}

func (refusingWriter) Write([]byte) (int, error) { return 0, errRefused }
