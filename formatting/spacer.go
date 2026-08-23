// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0
//
// The import-line matching follows golang.org/x/tools/internal/imports/imports.go, which carries:
//
//	Copyright 2013 The Go Authors. All rights reserved.
//	Use of this source code is governed by a BSD-style license.

package formatting

import (
	"bytes"
	"io"
	"regexp"
)

// importLine matches a line inside an import block and captures the path it imports.
var importLine = regexp.MustCompile(`^\s+(?:[\w.]+\s+)?"(.+?)"`)

// importKeyword opens the import block; declarationKeywords close it.
//
// Lines are handed about as bytes rather than strings: the printer emits one line per output line
// and turning each into a string allocated once per line of every file formatted.
var (
	importKeyword       = []byte("import")
	declarationKeywords = [...][]byte{[]byte("var"), []byte("func"), []byte("const"), []byte("type")}
)

// spacer writes a blank line before each import that opens a group.
//
// The printer lays a sorted import block out on consecutive lines and offers no way to ask for a
// blank line between two specs, so the separation is written here, as the output goes past. Holding
// one line at a time keeps [Format] from buffering the whole file.
type spacer struct {
	out io.Writer

	breaks  []string // import paths still waiting for a blank line, in the order they appear
	line    bytes.Buffer
	inBlock bool // inside the import block
	past    bool // the import block is behind us
	err     error
}

func newSpacer(out io.Writer, breaks []string) *spacer {
	return &spacer{out: out, breaks: breaks}
}

func (s *spacer) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}

	written := len(p)

	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		if end < 0 {
			s.line.Write(p)

			break
		}

		s.line.Write(p[:end+1])
		p = p[end+1:]

		if err := s.emit(s.line.Bytes()); err != nil {
			s.err = err

			return 0, err
		}
		s.line.Reset()
	}

	return written, nil
}

// Flush writes the last line, when the output did not end with a newline.
func (s *spacer) Flush() error {
	if s.err != nil {
		return s.err
	}

	if s.line.Len() == 0 {
		return nil
	}

	err := s.emit(s.line.Bytes())
	s.line.Reset()

	return err
}

// emit writes one line, preceded by a blank line when it opens a group.
func (s *spacer) emit(line []byte) error {
	s.track(line)

	if s.inBlock && len(s.breaks) > 0 {
		if match := importLine.FindSubmatch(line); match != nil && string(match[1]) == s.breaks[0] {
			s.breaks = s.breaks[1:]

			if _, err := io.WriteString(s.out, "\n"); err != nil {
				return err
			}
		}
	}

	_, err := s.out.Write(line)

	return err
}

// track follows the output into and out of the import block.
func (s *spacer) track(line []byte) {
	if s.past {
		return
	}

	if !s.inBlock && bytes.HasPrefix(line, importKeyword) {
		s.inBlock = true

		return
	}

	if s.inBlock && opensDeclaration(line) {
		s.inBlock = false
		s.past = true
	}
}

// opensDeclaration reports whether a line starts a top-level declaration, which puts the import
// block behind us.
func opensDeclaration(line []byte) bool {
	for _, keyword := range declarationKeywords {
		if bytes.HasPrefix(line, keyword) {
			return true
		}
	}

	return false
}
