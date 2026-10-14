// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
)

const maximumErrorBodyBytes = 4 << 10

var (
	contentRangePattern     = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+|\*)$`)
	unsatisfiedRangePattern = regexp.MustCompile(`^bytes \*/([0-9]+)$`)
)

// ReadFile reads up to request.Limit bytes of a file from request.offset with
// one HTTP range request.
func (client *SandboxClient) ReadFile(ctx context.Context, request ReadFileRequest) (ReadFileResult, error) {
	lastByte := request.offset + int64(request.Limit) - 1
	if lastByte < request.offset {
		return ReadFileResult{}, errors.New("file read range overflow")
	}
	httpRequest := client.newFilesystemRequest(ctx, http.MethodGet, request.Path, "", nil)
	httpRequest.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", request.offset, lastByte))
	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("sandbox read file request: %w", err)
	}
	defer closeBody(response)

	if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		size, parseErr := parseUnsatisfiedRange(response.Header.Get("Content-Range"))
		if parseErr == nil && request.offset >= size {
			return ReadFileResult{nextOffset: request.offset, EOF: true}, nil
		}
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return ReadFileResult{}, filesystemHTTPError(response, "read")
	}
	if isJSONContent(response.Header.Get("Content-Type")) {
		return ReadFileResult{}, NewToolFailure(toolfailure.PathIsDirectory, errors.New("sandbox read path is a directory, not a file"))
	}
	if response.StatusCode == http.StatusOK && request.offset != 0 {
		return ReadFileResult{}, errors.New("sandbox filesystem ignored a non-zero byte range")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(request.Limit)+1))
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("read sandbox file body: %w", err)
	}
	if len(data) > request.Limit {
		return ReadFileResult{}, errors.New("sandbox filesystem returned more bytes than requested")
	}
	eof := len(data) < request.Limit
	if response.StatusCode == http.StatusPartialContent {
		start, end, total, knownTotal, err := parseContentRange(response.Header.Get("Content-Range"))
		if err != nil {
			return ReadFileResult{}, err
		}
		if start != request.offset || end-start+1 != int64(len(data)) {
			return ReadFileResult{}, errors.New("sandbox filesystem returned an inconsistent byte range")
		}
		if knownTotal {
			eof = end+1 >= total
		}
	}
	return ReadFileResult{Data: data, nextOffset: request.offset + int64(len(data)), EOF: eof}, nil
}

// WriteFile replaces a file with data and the octal mode, such as "0600".
func (client *SandboxClient) WriteFile(ctx context.Context, target WorkspacePath, data []byte, mode string) error {
	request := client.newFilesystemRequest(ctx, http.MethodPut, target, "mode="+mode, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("sandbox write file request: %w", err)
	}
	defer closeBody(response)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return filesystemHTTPError(response, "write")
	}
	return nil
}

func (client *SandboxClient) newFilesystemRequest(ctx context.Context, method string, target WorkspacePath, query string, body io.Reader) *http.Request {
	endpoint := client.baseURL + "/v1/files/" + encodePathParameter(string(target))
	if query != "" {
		endpoint += "?" + query
	}
	// The method is a constant and the URL is built from a validated base and
	// an encoded path, so only a programming error can fail here.
	return comm.Must(http.NewRequestWithContext(ctx, method, endpoint, body))
}

// closeBody drains a bounded rest of the body so the connection can be reused.
func closeBody(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumErrorBodyBytes))
	_ = response.Body.Close()
}

// filesystemStatusError is a sandboxd file API answer with an unexpected status.
type filesystemStatusError struct {
	operation string
	// statusCode is the HTTP status sandboxd answered.
	statusCode int
	code       string
	message    string
}

func (failure *filesystemStatusError) Error() string {
	detail := strings.TrimSpace(failure.message)
	if detail == "" {
		detail = http.StatusText(failure.statusCode)
	}
	if len(detail) > 512 {
		detail = comm.TruncateUTF8(detail, 512) + "... [truncated]"
	}
	if failure.code != "" {
		detail = failure.code + ": " + detail
	}
	return fmt.Sprintf("sandbox filesystem %s returned HTTP %d: %s", failure.operation, failure.statusCode, detail)
}

// filesystemHTTPError classifies a rejected request as a typed failure: a
// request the Sandbox refuses is the call's to correct, a refused permission
// is the platform's. Any other status stays an ordinary error that IsTransient
// classifies.
func filesystemHTTPError(response *http.Response, operation string) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maximumErrorBodyBytes))
	var answer struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &answer)
	if answer.Message == "" {
		answer.Message = string(body)
	}
	failure := &filesystemStatusError{operation: operation, statusCode: response.StatusCode, code: answer.Code, message: answer.Message}
	switch response.StatusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return NewToolFailure(toolfailure.SandboxRequestRejected, failure)
	case http.StatusForbidden:
		return NewToolFailure(toolfailure.SandboxAccessDenied, failure)
	case http.StatusNotFound:
		return NewToolFailure(toolfailure.PathNotFound, failure)
	case http.StatusConflict:
		return NewToolFailure(toolfailure.SandboxConflict, failure)
	case http.StatusRequestEntityTooLarge:
		return NewToolFailure(toolfailure.SandboxPayloadTooLarge, failure)
	default:
		return failure
	}
}

// encodePathParameter percent-encodes every byte outside the unreserved set,
// including '/', so the whole path is one route parameter.
func encodePathParameter(value string) string {
	const upperHex = "0123456789ABCDEF"
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
			character == '-' || character == '_' || character == '.' || character == '~' {
			builder.WriteByte(character)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(upperHex[character>>4])
		builder.WriteByte(upperHex[character&0x0f])
	}
	switch encoded := builder.String(); encoded {
	case ".":
		return "%2E"
	case "..":
		return "%2E%2E"
	default:
		return encoded
	}
}

func isJSONContent(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}

func parseContentRange(value string) (start, end, total int64, knownTotal bool, err error) {
	invalid := fmt.Errorf("invalid sandbox Content-Range %q", value)
	matches := contentRangePattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, 0, 0, false, invalid
	}
	if start, err = strconv.ParseInt(matches[1], 10, 64); err != nil {
		return 0, 0, 0, false, invalid
	}
	if end, err = strconv.ParseInt(matches[2], 10, 64); err != nil || end < start {
		return 0, 0, 0, false, invalid
	}
	if matches[3] == "*" {
		return start, end, 0, false, nil
	}
	if total, err = strconv.ParseInt(matches[3], 10, 64); err != nil || total <= end {
		return 0, 0, 0, false, invalid
	}
	return start, end, total, true, nil
}

func parseUnsatisfiedRange(value string) (int64, error) {
	matches := unsatisfiedRangePattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, fmt.Errorf("invalid unsatisfied Content-Range %q", value)
	}
	return strconv.ParseInt(matches[1], 10, 64)
}
