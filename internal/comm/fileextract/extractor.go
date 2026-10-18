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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// Subcommand is the argument that runs the server binary as the extractor
// child (ServeChild).
const Subcommand = "file-extract"

// childTimeout is when the parent kills a child that has not finished.
const childTimeout = 120 * time.Second

// maxInputBytes bounds every file the extractor reads: the largest object the
// platform's store holds.
const maxInputBytes = 32 << 20

// Request is one extraction: the declared format and its bounds, which the
// caller's configuration sets.
type Request struct {
	Format Format `json:"format"`
	// MaxBytes bounds the file, at most 32 MiB and for JSON at most
	// MaxJSONBytes.
	MaxBytes int64 `json:"maxBytes"`
	// MaxImageEdge bounds an image's long edge in pixels.
	MaxImageEdge int `json:"maxImageEdge,omitempty"`
	// Queue names whose extraction this is, such as a tenant. The slot serves
	// the waiting queues in turn, so one queue's backlog never starves
	// another's. The child never sees it.
	Queue string `json:"-"`
}

func (r Request) validate() error {
	switch {
	case r.Format == Format{}:
		return errors.New("the request names no format")
	case r.MaxBytes < 1 || r.MaxBytes > maxInputBytes || r.Format.kind == KindJSON && r.MaxBytes > MaxJSONBytes:
		return fmt.Errorf("the bound of %d bytes is outside its range", r.MaxBytes)
	case r.Format.kind == KindImage && (r.MaxImageEdge < 1 || r.MaxImageEdge > 8000):
		return fmt.Errorf("the image edge bound %d is outside 1 to 8000", r.MaxImageEdge)
	}
	return nil
}

// Extractor runs extractions in child processes of the server binary, one at
// a time per process: Extract waits for the process's one slot, which every
// owner of extractions shares through the one Extractor the composition root
// creates. The slot goes to the waiting queues in turn (Request.Queue).
type Extractor struct {
	executable string
	slot       *fairSlot
}

// NewExtractor runs the children from executable, the server binary.
func NewExtractor(executable string) *Extractor {
	return &Extractor{executable: executable, slot: &fairSlot{waiting: map[string][]chan struct{}{}}}
}

// fairSlot is one slot that its waiters share by queue: when it frees, the
// queue whose turn it is gets it for its oldest waiter and goes to the back
// of the turns if it still waits. A waiter costs one channel, so the slot
// holds no more than the extractions that wait for it.
type fairSlot struct {
	mu      sync.Mutex
	held    bool
	waiting map[string][]chan struct{}
	turns   []string
}

// acquire waits for the slot in queue until ctx ends.
func (s *fairSlot) acquire(ctx context.Context, queue string) error {
	s.mu.Lock()
	if !s.held {
		s.held = true
		s.mu.Unlock()
		return nil
	}
	grant := make(chan struct{})
	if len(s.waiting[queue]) == 0 {
		s.turns = append(s.turns, queue)
	}
	s.waiting[queue] = append(s.waiting[queue], grant)
	s.mu.Unlock()
	select {
	case <-grant:
		return nil
	case <-ctx.Done():
	}
	s.mu.Lock()
	withdrawn := s.withdraw(queue, grant)
	s.mu.Unlock()
	if !withdrawn {
		// The slot was granted as the wait ended: it goes to the next waiter.
		s.release()
	}
	return ctx.Err()
}

// withdraw removes a waiter whose wait ended; false when it was granted the
// slot already. The caller holds mu.
func (s *fairSlot) withdraw(queue string, grant chan struct{}) bool {
	index := slices.Index(s.waiting[queue], grant)
	if index < 0 {
		return false
	}
	if rest := slices.Delete(s.waiting[queue], index, index+1); len(rest) > 0 {
		s.waiting[queue] = rest
	} else {
		delete(s.waiting, queue)
		s.turns = slices.DeleteFunc(s.turns, func(turn string) bool { return turn == queue })
	}
	return true
}

// release frees the slot, or grants it to the oldest waiter of the queue
// whose turn it is.
func (s *fairSlot) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.turns) == 0 {
		s.held = false
		return
	}
	queue := s.turns[0]
	s.turns = slices.Delete(s.turns, 0, 1)
	grant, rest := s.waiting[queue][0], s.waiting[queue][1:]
	if len(rest) > 0 {
		s.waiting[queue] = rest
		s.turns = append(s.turns, queue)
	} else {
		delete(s.waiting, queue)
	}
	close(grant)
}

// Extract prepares input, declared as request's format, in a child process
// with an empty environment and an empty working directory, once the slot
// comes to the request's queue, and kills it after two minutes. The result's Refusal is a business decision about the bytes,
// never retried: a refusal of the extractor, or extraction_failed for a crash,
// a process limit or the time limit. An error is a fault of this process or
// ctx's end, which the caller may retry. A JSON result's Redacted copy comes
// back indented by two spaces.
func (e *Extractor) Extract(ctx context.Context, request Request, input []byte) (Result, error) {
	if err := request.validate(); err != nil {
		panic("fileextract: " + err.Error())
	}
	if int64(len(input)) > request.MaxBytes {
		return refuse(request.Format, CodeTooLarge, fmt.Sprintf("the file is larger than %d bytes", request.MaxBytes)), nil
	}
	if err := e.slot.acquire(ctx, request.Queue); err != nil {
		return Result{}, fmt.Errorf("wait for the extraction slot: %w", err)
	}
	defer e.slot.release()
	directory, err := os.MkdirTemp("", "file-extract-")
	if err != nil {
		return Result{}, fmt.Errorf("create the extractor's working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(directory) }()
	run, cancel := context.WithTimeout(ctx, childTimeout)
	defer cancel()
	stdout, stderr := &boundedBuffer{limit: maxOutputBytes}, &boundedBuffer{limit: 4 << 10}
	child := exec.CommandContext(run, e.executable, Subcommand, string(encodeJSON(request))) //nolint:gosec // the server binary itself
	child.Env, child.Dir = []string{}, directory
	child.Stdin, child.Stdout, child.Stderr = bytes.NewReader(input), stdout, stderr
	child.WaitDelay = 5 * time.Second
	runErr := child.Run()
	switch {
	case ctx.Err() != nil:
		return Result{}, fmt.Errorf("extraction of a %s file: %w", request.Format, ctx.Err())
	case errors.Is(run.Err(), context.DeadlineExceeded):
		return refuse(request.Format, CodeExtractionFailed, fmt.Sprintf("the extractor did not finish within %s", childTimeout)), nil
	case runErr != nil && child.ProcessState == nil:
		return Result{}, fmt.Errorf("start the extractor: %w", runErr)
	case runErr != nil:
		return refuse(request.Format, CodeExtractionFailed, childFailure(child.ProcessState, stderr.String())), nil
	case stdout.overflow:
		return refuse(request.Format, CodeExtractionFailed, fmt.Sprintf("the extractor wrote more than %d bytes", maxOutputBytes)), nil
	}
	var result Result
	if err = comm.DecodeStrictJSON(stdout.Bytes(), &result); err != nil || result.Format != request.Format || result.Version != extractorVersion {
		return refuse(request.Format, CodeExtractionFailed, "the extractor wrote no valid result"), nil
	}
	if result.JSON != nil {
		var indented bytes.Buffer
		comm.MustDo(json.Indent(&indented, result.JSON.Redacted, "", "  "))
		result.JSON.Redacted = indented.Bytes()
	}
	return result, nil
}

// childFailure describes how a child ended without a result: its limit, its
// signal or its status, and a runtime's fatal line, which holds no content.
func childFailure(state *os.ProcessState, stderr string) string {
	detail := "the extractor exited with status " + fmt.Sprint(state.ExitCode())
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		detail = "the extractor was stopped by " + status.Signal().String()
	}
	switch state.ExitCode() {
	case exitCPU:
		detail = fmt.Sprintf("the extractor used its %d seconds of CPU time", childCPUSeconds)
	case exitLimits, exitUsage, exitInput:
		detail = "the extractor could not run: " + strings.TrimSpace(comm.TruncateUTF8(stderr, 300))
	}
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "fatal error: ") || strings.HasPrefix(line, "panic: runtime error: ") {
			return detail + ": " + comm.TruncateUTF8(line, 200)
		}
	}
	return detail
}

// boundedBuffer keeps at most limit bytes of a child's output and drains the
// rest, so the child never blocks on a full pipe.
type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if room := b.limit - b.Len(); room < len(data) {
		b.overflow = true
		b.Buffer.Write(data[:max(room, 0)])
		return len(data), nil
	}
	return b.Buffer.Write(data)
}
