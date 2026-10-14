// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"
)

// viewRequest selects the parts of a run's merged output an answer carries: a
// projection of its head and tail cut at line boundaries, a window from a byte
// offset, or both.
type viewRequest struct {
	head, tail   int
	from         int64
	maximum      int
	windowWanted bool
}

func (view *viewRequest) register(set *flag.FlagSet, window bool) {
	set.IntVar(&view.head, "head", 0, "bytes of the output's beginning")
	set.IntVar(&view.tail, "tail", 0, "bytes of the output's end")
	if window {
		set.Int64Var(&view.from, "from", 0, "byte offset of the output window")
		set.IntVar(&view.maximum, "max", 0, "bytes of the output window")
	}
}

func (view *viewRequest) validate() error {
	switch {
	case view.head < 0 || view.tail < 0 || view.head+view.tail > maximumAnswerOutput:
		return fmt.Errorf("--head and --tail must not be negative and at most %d bytes together", maximumAnswerOutput)
	case view.from < 0 || view.maximum < 0 || view.maximum > maximumAnswerOutput:
		return fmt.Errorf("--from must not be negative and --max at most %d bytes", maximumAnswerOutput)
	}
	view.windowWanted = view.maximum > 0
	return nil
}

// outputView is what an answer carries of the output file.
type outputView struct {
	Path       string      `json:"path"`
	Bytes      int64       `json:"bytes"`
	Projection *projection `json:"projection,omitempty"`
	Window     *window     `json:"window,omitempty"`
}

// projection is the output's head and tail. When the whole output fits, Head
// holds all of it and nothing is omitted.
type projection struct {
	Head         string `json:"head"`
	Tail         string `json:"tail"`
	OmittedBytes int64  `json:"omittedBytes"`
}

// window is the output from From up to Next, cut at a character boundary.
type window struct {
	From int64  `json:"from"`
	Next int64  `json:"next"`
	Data string `json:"data"`
}

// read renders the requested view of the output file, which grows while the
// command runs: every part is read within one observed size.
func (view viewRequest) read(path string) (*outputView, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &outputView{Path: path}, nil
	}
	if err != nil {
		return nil, err
	}
	output := &outputView{Path: path, Bytes: info.Size()}
	if view.head == 0 && view.tail == 0 && !view.windowWanted {
		return output, nil
	}
	file, err := os.Open(path) //nolint:gosec // a run's own output file
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	if view.head > 0 || view.tail > 0 {
		if output.Projection, err = project(file, output.Bytes, view.head, view.tail); err != nil {
			return nil, err
		}
	}
	if view.windowWanted {
		if output.Window, err = readWindow(file, output.Bytes, view.from, view.maximum); err != nil {
			return nil, err
		}
	}
	return output, nil
}

// project keeps at most head bytes of the beginning and tail bytes of the end
// of the first size bytes. The head ends after its last complete line and the
// tail starts after its first line break, unless that would leave nothing.
func project(file *os.File, size int64, head, tail int) (*projection, error) {
	if size <= int64(head+tail) {
		all, err := readAt(file, 0, int(size))
		return &projection{Head: text(all, head+tail)}, err
	}
	beginning, err := readAt(file, 0, head)
	if err != nil {
		return nil, err
	}
	if cut := bytes.LastIndexByte(beginning, '\n'); cut >= 0 {
		beginning = beginning[:cut+1]
	} else {
		beginning = beginning[:runeStart(beginning, len(beginning))]
	}
	end, err := readAt(file, size-int64(tail), tail)
	if err != nil {
		return nil, err
	}
	if cut := bytes.IndexByte(end, '\n'); cut >= 0 && cut+1 < len(end) {
		end = end[cut+1:]
	} else {
		end = end[continuationLength(end):]
	}
	omitted := size - int64(len(beginning)) - int64(len(end))
	return &projection{Head: text(beginning, head), Tail: text(end, tail), OmittedBytes: omitted}, nil
}

// readWindow reads up to maximum bytes from offset within size, ending at a
// character boundary unless the window holds a single partial character.
func readWindow(file *os.File, size, offset int64, maximum int) (*window, error) {
	if offset >= size {
		return &window{From: offset, Next: max(offset, size)}, nil
	}
	data, err := readAt(file, offset, int(min(int64(maximum), size-offset)))
	if err != nil {
		return nil, err
	}
	if cut := runeStart(data, len(data)); cut > 0 && offset+int64(len(data)) < size {
		data = data[:cut]
	}
	return &window{From: offset, Next: offset + int64(len(data)), Data: text(data, maximum)}, nil
}

func readAt(file *os.File, offset int64, length int) ([]byte, error) {
	data := make([]byte, length)
	read, err := file.ReadAt(data, offset)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return data[:read], err
}

// runeStart is the largest index at most end that does not split a UTF-8
// character whose encoding started within data.
func runeStart(data []byte, end int) int {
	for back := 1; back <= utf8.UTFMax && end-back >= 0; back++ {
		if utf8.RuneStart(data[end-back]) {
			if !utf8.FullRune(data[end-back : end]) {
				return end - back
			}
			return end
		}
	}
	return end
}

// continuationLength counts the leading continuation bytes of a character
// whose start was cut off.
func continuationLength(data []byte) int {
	skip := 0
	for skip < len(data) && skip < utf8.UTFMax-1 && !utf8.RuneStart(data[skip]) {
		skip++
	}
	return skip
}

// text is data as valid UTF-8 of at most limit bytes: invalid sequences become
// U+FFFD, and a replacement that grows the text is cut at a character boundary.
func text(data []byte, limit int) string {
	valid := strings.ToValidUTF8(string(data), "�")
	if len(valid) <= limit {
		return valid
	}
	return valid[:runeStart([]byte(valid), limit)]
}
