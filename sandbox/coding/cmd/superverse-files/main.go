// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Command superverse-files serves the Agent's file tools inside one coding
// Sandbox: read, edit, write, grep and glob, under the path rules of the agent
// root and the read-only roots. It reads one JSON request on stdin and prints
// one JSON answer on stdout: a result, or a typed failure of the call, with
// exit code 0; a helper error has exit code 1. README.md is the contract.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"syscall"

	"github.com/superdurable/superversev2/sandbox/coding/internal/confinement"
)

// maximumRequestBytes bounds the request on stdin: a write of 4 MiB of content
// escaped in JSON fits.
const maximumRequestBytes = 32 << 20

// answer is the one JSON object every operation prints.
type answer struct {
	Result  string   `json:"result,omitempty"`
	Failure *failure `json:"failure,omitempty"`
	// Path is the confined path the operation acted on.
	Path string `json:"path,omitempty"`
	// NextOffset is the first line a read did not return, or zero at the end.
	NextOffset int `json:"nextOffset,omitempty"`
	// Error is set only on exit code 1.
	Error string `json:"error,omitempty"`
}

// failure is a failure of the call that the model corrects: nothing changed.
type failure struct {
	Code    string `json:"code"`
	Owner   string `json:"owner"`
	Effect  string `json:"effect"`
	Message string `json:"message"`
}

// failureCode is a typed failure the helper answers, with its owner, spelled
// as README.md and the server's toolfailure codes spell it. Every failure
// changed nothing.
type failureCode struct{ name, owner string }

// The owners of the helper's failures.
const (
	ownerCall     = "call"
	ownerPlatform = "platform"
)

// effectNone is the effect of every failure the helper answers.
const effectNone = "none"

var (
	invalidArguments           = failureCode{name: "invalid_arguments", owner: ownerCall}
	pathNotFound               = failureCode{name: "path_not_found", owner: ownerCall}
	pathIsDirectory            = failureCode{name: "path_is_directory", owner: ownerCall}
	pathOutsideRoots           = failureCode{name: "path_outside_roots", owner: ownerCall}
	pathPermissionDenied       = failureCode{name: "path_permission_denied", owner: ownerCall}
	binaryFile                 = failureCode{name: "binary_file", owner: ownerCall}
	fileTooLarge               = failureCode{name: "file_too_large", owner: ownerCall}
	editNoMatch                = failureCode{name: "edit_no_match", owner: ownerCall}
	editAmbiguous              = failureCode{name: "edit_ambiguous", owner: ownerCall}
	editNoop                   = failureCode{name: "edit_noop", owner: ownerCall}
	invalidPattern             = failureCode{name: "invalid_pattern", owner: ownerCall}
	platformCommandUnavailable = failureCode{name: "platform_command_unavailable", owner: ownerPlatform}
)

// refusalError is an error that the answer reports as a typed failure.
type refusalError struct {
	code    failureCode
	message string
}

func (failure *refusalError) Error() string { return failure.code.name + ": " + failure.message }

func refuse(code failureCode, format string, arguments ...any) error {
	return &refusalError{code: code, message: fmt.Sprintf(format, arguments...)}
}

const usage = "usage: superverse-files read|edit|write|grep|glob --root DIR [--id ID] < request.json"

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func main() {
	reply, err := run(os.Args[1:], os.Stdin)
	var refused *refusalError
	switch {
	case errors.As(err, &refused):
		reply = answer{Failure: &failure{Code: refused.code.name, Owner: refused.code.owner, Effect: effectNone, Message: refused.message}}
	case err != nil:
		reply = answer{Error: err.Error()}
	}
	if err = json.NewEncoder(os.Stdout).Encode(reply); err != nil || reply.Error != "" {
		os.Exit(1)
	}
}

func run(arguments []string, input io.Reader) (answer, error) {
	if len(arguments) == 0 {
		return answer{}, errors.New(usage)
	}
	operation := arguments[0]
	set := flag.NewFlagSet(operation, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	root := set.String("root", "", "absolute agent root")
	id := set.String("id", "", "call identity of an edit")
	if err := set.Parse(arguments[1:]); err != nil || set.NArg() != 0 {
		return answer{}, errors.New(usage)
	}
	roots, err := confinement.New(*root)
	if err != nil {
		return answer{}, err
	}
	request, err := io.ReadAll(io.LimitReader(input, maximumRequestBytes+1))
	if err != nil {
		return answer{}, err
	}
	if len(request) > maximumRequestBytes {
		return answer{}, fmt.Errorf("the request exceeds %d bytes", maximumRequestBytes)
	}
	switch operation {
	case "read":
		return decodeAndRun(request, func(input readRequest) (answer, error) { return read(roots, input) })
	case "write":
		return decodeAndRun(request, func(input writeRequest) (answer, error) { return write(roots, input) })
	case "edit":
		if !idPattern.MatchString(*id) {
			return answer{}, errors.New("edit needs --id, 1 to 128 letters, digits, '_' or '-'")
		}
		return decodeAndRun(request, func(input editRequest) (answer, error) { return edit(roots, *id, input) })
	case "grep":
		return decodeAndRun(request, func(input grepRequest) (answer, error) { return grep(roots, input) })
	case "glob":
		return decodeAndRun(request, func(input globRequest) (answer, error) { return glob(roots, input) })
	}
	return answer{}, errors.New(usage)
}

// decodeAndRun decodes the request strictly: one object, no unknown field.
func decodeAndRun[T any](request []byte, operation func(T) (answer, error)) (answer, error) {
	decoder := json.NewDecoder(bytes.NewReader(request))
	decoder.DisallowUnknownFields()
	var input T
	if err := decoder.Decode(&input); err != nil {
		return answer{}, refuse(invalidArguments, "the request is not a valid object of this operation: %v", err)
	}
	if decoder.More() {
		return answer{}, refuse(invalidArguments, "the request holds more than one object")
	}
	return operation(input)
}

// confine resolves a request path, mapping the path rules and a missing
// directory on the way to the call's failures.
func confine(roots confinement.Roots, path string, access confinement.Access) (confinement.Path, error) {
	if path == "" {
		path = "."
	}
	if bytes.IndexByte([]byte(path), 0) >= 0 {
		return confinement.Path{}, refuse(invalidArguments, "the path must not contain NUL")
	}
	confined, err := roots.Resolve(path, access)
	switch {
	case errors.Is(err, confinement.ErrOutsideRoots):
		return confinement.Path{}, refuse(pathOutsideRoots, "%v; use a path in the agent root or, for reading, in a read-only root", err)
	case errors.Is(err, syscall.ENOTDIR) || errors.Is(err, fs.ErrNotExist):
		return confinement.Path{}, refuse(pathNotFound, "%s does not exist", path)
	case err != nil:
		return confinement.Path{}, err
	}
	return confined, nil
}

// display is how a result names a path: relative to the agent root inside it,
// absolute elsewhere.
func display(confined confinement.Path) string {
	if confined.ReadOnly {
		return confined.Absolute
	}
	return confined.Relative
}

// statFile requires an existing regular file that the operation, such as
// "read", may reach, or reports the call's failure.
func statFile(confined confinement.Path, original, operation string) (fs.FileInfo, error) {
	info, err := os.Stat(confined.Absolute)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return nil, refuse(pathNotFound, "%s does not exist", original)
	case err != nil:
		return nil, denied(err, original, operation)
	case info.IsDir():
		return nil, refuse(pathIsDirectory, "%s is a directory", original)
	case !info.Mode().IsRegular():
		return nil, refuse(pathNotFound, "%s is not a regular file", original)
	}
	return info, nil
}

// denied is the call's failure for an operation on a confined path, or on a
// directory of it in its root, that the permissions of the path or of a
// directory on its way deny. Those are the agent's own files: its commands
// share the helper's UID and may change them. Any other error is returned as
// it is, a helper error.
func denied(err error, original, operation string) error {
	if errors.Is(err, fs.ErrPermission) {
		return refuse(pathPermissionDenied, "%s cannot be %s: permission denied", original, operation)
	}
	return err
}
