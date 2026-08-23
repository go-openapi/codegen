// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

package formatting_test

import (
	"bytes"
	"go/format"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/go-openapi/testify/v2/require"

	"github.com/go-openapi/codegen/formatting"
)

// TestCRLF pins what Format makes of Windows line endings.
//
// go/printer writes \n and offers no way to ask for anything else, so a whole file comes back with
// LF whatever went in. A fragment is the exception: [Format] puts back the bytes that surrounded it,
// and those keep their \r\n. go/format.Source answers the same on both, which is the point — the
// fixtures cannot check this, since .gitattributes checks them out with LF everywhere.
func TestCRLF(t *testing.T) {
	t.Parallel()

	t.Run("should write a whole file with LF, as gofmt does", func(t *testing.T) {
		t.Parallel()

		const src = "package p\r\n\r\nimport (\r\n\"strings\"\r\n\"bytes\"\r\n)\r\n\r\n" +
			"var _ bytes.Buffer\r\nvar _ = strings.NewReader\r\n"

		var out bytes.Buffer
		_, err := formatting.Format(&out, []byte(src))
		require.NoError(t, err)

		assert.NotContains(t, out.String(), "\r", "go/printer writes \\n")

		gofmted, err := format.Source([]byte(src))
		require.NoError(t, err)
		assert.Equal(t, string(gofmted), out.String())
	})

	t.Run("should restore the space that surrounded a fragment", func(t *testing.T) {
		t.Parallel()

		const src = "\r\n\r\n\tx := 1\r\n\t_ = x\r\n\r\n"

		var out bytes.Buffer
		_, err := formatting.Format(&out, []byte(src))
		require.NoError(t, err)

		assert.True(t, strings.HasPrefix(out.String(), "\r\n\r\n\t"),
			"the leading space comes back as written: %q", out.String())
		assert.True(t, strings.HasSuffix(out.String(), "\r\n\r\n"), "and so does the trailing space")

		gofmted, err := format.Source([]byte(src))
		require.NoError(t, err)
		assert.Equal(t, string(gofmted), out.String(),
			"mixed endings and all, this is what go/format produces")
	})
}
