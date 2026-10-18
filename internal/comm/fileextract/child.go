// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fileextract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"

	"github.com/superdurable/superversev2/internal/comm"
)

// The limits the child sets on its own process before it reads its input:
// one thread of Go code, a soft memory limit, CPU time and file descriptors
// everywhere, and on Linux, the only platform that enforces it, an address
// space of 1 GiB beyond what the process holds when it starts: the Go runtime
// reserves about 1.3 GB of address space before main (measured with the server
// binary on linux/arm64), so an absolute 1 GiB would leave no room. Past the
// CPU limit the kernel signals the child, which exits with exitCPU; a second
// later it kills it.
const (
	childMemoryLimit  = 512 << 20
	childAddressSpace = 1 << 30
	childCPUSeconds   = 60
	childFiles        = 16
	// maxOutputBytes leaves room for the redacted copy of the largest JSON
	// file beside its summary.
	maxOutputBytes = 8 << 20
)

// The exit codes of a child that wrote no result.
const (
	exitUsage  = 64
	exitLimits = 65
	exitCPU    = 66
	exitInput  = 67
)

// ServeChild is the extractor child, the server binary's file-extract
// subcommand. It limits its own process, reads the file on stdin, prepares it
// as the request that args holds says and writes one JSON Result, at most
// 8 MiB, on stdout. It reaches nothing but its pipes, and returns the
// process's exit code.
func ServeChild(args []string, stdin io.Reader, stdout io.Writer) int {
	if err := limitProcess(); err != nil {
		fmt.Fprintln(os.Stderr, "file-extract: process limits:", err)
		return exitLimits
	}
	cpu := make(chan os.Signal, 1)
	signal.Notify(cpu, syscall.SIGXCPU)
	go func() {
		<-cpu
		os.Exit(exitCPU)
	}()
	var request Request
	if len(args) != 1 || comm.DecodeStrictJSON([]byte(args[0]), &request) != nil || request.validate() != nil {
		fmt.Fprintln(os.Stderr, "file-extract: the one argument must be a valid extraction request")
		return exitUsage
	}
	input, err := io.ReadAll(io.LimitReader(stdin, request.MaxBytes+1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "file-extract: read the input:", err)
		return exitInput
	}
	encoded := encodeResult(prepare(request, input))
	if _, err = stdout.Write(encoded); err != nil {
		fmt.Fprintln(os.Stderr, "file-extract: write the result:", err)
		return exitInput
	}
	return 0
}

// limitProcess sets the child's limits, never above a lower one it inherited.
func limitProcess() error {
	runtime.GOMAXPROCS(1)
	debug.SetMemoryLimit(childMemoryLimit)
	limits := []struct {
		resource   int
		soft, hard uint64
	}{{syscall.RLIMIT_CPU, childCPUSeconds, childCPUSeconds + 1}, {syscall.RLIMIT_NOFILE, childFiles, childFiles}}
	if runtime.GOOS == "linux" {
		held, err := addressSpace()
		if err != nil {
			return err
		}
		limits = append(limits, struct {
			resource   int
			soft, hard uint64
		}{syscall.RLIMIT_AS, held + childAddressSpace, held + childAddressSpace})
	}
	for _, limit := range limits {
		var current syscall.Rlimit
		if err := syscall.Getrlimit(limit.resource, &current); err != nil {
			return fmt.Errorf("read limit %d: %w", limit.resource, err)
		}
		hard := min(limit.hard, current.Max)
		if err := syscall.Setrlimit(limit.resource, &syscall.Rlimit{Cur: min(limit.soft, hard), Max: hard}); err != nil {
			return fmt.Errorf("set limit %d: %w", limit.resource, err)
		}
	}
	return nil
}

// addressSpace is the address space the process holds, from the first field
// of /proc/self/statm, in pages.
func addressSpace() (uint64, error) {
	statm, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, fmt.Errorf("read the address space: %w", err)
	}
	fields := bytes.Fields(statm)
	if len(fields) == 0 {
		return 0, errors.New("read the address space: /proc/self/statm is empty")
	}
	pages, err := strconv.ParseUint(string(fields[0]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("read the address space: %w", err)
	}
	return pages * uint64(os.Getpagesize()), nil
}

// prepare reads input as request's format.
func prepare(request Request, input []byte) Result {
	format := request.Format
	switch {
	case int64(len(input)) > request.MaxBytes:
		return refuse(format, CodeTooLarge, fmt.Sprintf("the file is larger than %d bytes", request.MaxBytes))
	case len(input) == 0:
		return refuse(format, CodeUnsupportedContent, "the file is empty")
	case format.kind == KindImage:
		return probeImage(format, input, request.MaxImageEdge)
	case format.kind == KindJSON:
		return prepareJSON(input)
	}
	into := &sections{}
	var refusal *Refusal
	switch format {
	case FormatMarkdown, FormatText:
		text, ok := decodeText(input)
		if !ok {
			return refuse(format, CodeUnsupportedContent, "the file is not UTF-8 or UTF-16 text, or holds a NUL")
		}
		if format == FormatMarkdown {
			markdownSections(text, into)
		} else {
			textSections(text, into)
		}
	case FormatDOCX:
		refusal = docxSections(input, into)
	default:
		refusal = pdfSections(input, into)
	}
	if refusal != nil {
		return Result{Format: format, Version: extractorVersion, Refusal: refusal}
	}
	if into.document.TextBytes == 0 {
		return refuse(format, CodeNoText, "the document has no text")
	}
	return Result{Format: format, Version: extractorVersion, Document: &into.document}
}

// encodeResult encodes result within the output limit: a document keeps the
// sections that fit and is truncated, and a JSON file whose redacted copy does
// not fit is too large.
func encodeResult(result Result) []byte {
	encoded := encodeJSON(result)
	if len(encoded) <= maxOutputBytes {
		return encoded
	}
	if document := result.Document; document != nil {
		budget := maxOutputBytes - (len(encoded) - len(encodeJSON(document.Sections))) - 1024
		kept := 0
		for _, section := range document.Sections {
			if budget -= len(encodeJSON(section)) + 1; budget < 0 {
				break
			}
			kept++
		}
		trimmed := *document
		trimmed.Sections, trimmed.Truncated, trimmed.TextBytes = document.Sections[:kept], true, 0
		for _, section := range trimmed.Sections {
			trimmed.TextBytes += len(section.Text)
		}
		result.Document = &trimmed
		return encodeJSON(result)
	}
	code := CodeExtractionFailed
	if result.JSON != nil {
		code = CodeTooLarge
	}
	return encodeJSON(refuse(result.Format, code, fmt.Sprintf("the prepared file is larger than the %d bytes the extractor returns", maxOutputBytes)))
}

// encodeJSON encodes value without HTML escaping; the extractor's own types
// always encode.
func encodeJSON(value any) []byte {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	comm.MustDo(encoder.Encode(value))
	return bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
}
