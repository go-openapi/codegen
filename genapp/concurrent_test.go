// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package genapp_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/genapp"
)

// TestConcurrentRender renders from many goroutines at once.
//
// A [genapp.GoGenApp] keeps no buffer of its own: each render borrows one from the shared pool and
// gives it back. Run under -race, this is what says the buffers never overlap, and comparing every
// result against a serial render says none of them was handed a buffer another goroutine was still
// filling.
func TestConcurrentRender(t *testing.T) {
	t.Parallel()

	const goroutines = 24

	dir := t.TempDir()
	app := newApp(t, genapp.WithOutputPath(dir))

	expected := make([]string, goroutines)
	for i := range expected {
		var out bytes.Buffer
		require.NoError(t, app.Render(&out, "model", model{Package: "models", Name: fmt.Sprintf("thing_%d", i)}))
		expected[i] = out.String()
	}

	var wg sync.WaitGroup
	rendered := make([]string, goroutines)

	wg.Add(goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()

			data := model{Package: "models", Name: fmt.Sprintf("thing_%d", i)}

			var out bytes.Buffer
			if err := app.Render(&out, "model", data); err != nil {
				t.Error(err)

				return
			}
			rendered[i] = out.String()

			if err := app.RenderFile(fmt.Sprintf("thing_%d.go", i), "model", data); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	for i := range goroutines {
		assert.Equal(t, expected[i], rendered[i], "goroutine %d rendered what a serial render does", i)

		written, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("thing_%d.go", i)))
		require.NoError(t, err)
		assert.Equal(t, expected[i], string(written))
	}
}

func BenchmarkRender(b *testing.B) {
	app, err := genapp.New(genapp.WithTemplates(newRepo(b)))
	if err != nil {
		b.Fatal(err)
	}

	data := model{Package: "models", Name: "pet_owner"}

	b.Run("Render", func(b *testing.B) {
		b.ReportAllocs()

		var out bytes.Buffer
		for b.Loop() {
			out.Reset()
			if err := app.Render(&out, "model", data); err != nil {
				b.Fatal(err)
			}
		}
	})
}
