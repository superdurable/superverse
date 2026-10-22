// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package local

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
)

// errLocalOutcomeUnknown requires inspecting the accepted build before
// retrying an effect.
var errLocalOutcomeUnknown = errors.New("local build provider outcome is uncertain")

// Builder reaches the authenticated Local Kind build fixture on the host,
// not an application API.
type Builder struct {
	client          *http.Client
	endpoint, token string
}

func New(endpoint, token string) (*Builder, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{"127.0.0.1": true, "localhost": true, "host.docker.internal": true, "superverse-v2-local-build.superverse-v2-system.svc.cluster.local": true}
	if u.Scheme != "http" || !allowed[u.Hostname()] || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(token) < 32 {
		return nil, errors.New("isolated local build endpoint and fixture token are required")
	}
	return &Builder{client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: strings.TrimRight(endpoint, "/"), token: token}, nil
}

func (b *Builder) Inspect(ctx context.Context, in build.BuildRequest) (build.BuildResult, error) {
	return b.call(ctx, "inspect", in, "")
}

func (b *Builder) Start(ctx context.Context, in build.BuildRequest, repositoryToken string) (build.BuildResult, error) {
	return b.call(ctx, "start", in, repositoryToken)
}

func (b *Builder) Cancel(ctx context.Context, in build.BuildRequest) error {
	result, err := b.call(ctx, "cancel", in, "")
	if err != nil {
		return err
	}
	switch result.State {
	case build.BuildRunning:
		return fmt.Errorf("%w: the local build accepted its cancellation but still runs", build.ErrPending)
	case build.BuildUnknown:
		// A Native cancellation, or a lost job, leaves the outcome unknown for good.
		return fmt.Errorf("%w: the local build to cancel is %s with no running job", build.ErrCancelUnconfirmed, cmp.Or(result.Failure, "UNKNOWN"))
	default:
		return nil
	}
}

func (b *Builder) CleanupRelease(ctx context.Context, in build.BuildRequest) error {
	result, err := b.call(ctx, "cleanup-release", in, "")
	if err != nil {
		return err
	}
	switch result.State {
	case build.BuildMissing:
		return nil
	case build.BuildRunning:
		return fmt.Errorf("%w: a local build of the Release is still stopping or more revisions remain", build.ErrPending)
	case build.BuildUnknown:
		return fmt.Errorf("%w: a local build of the Release has no running job and an unknown outcome", errLocalOutcomeUnknown)
	default:
		return fmt.Errorf("local release build cleanup answered %s", result.State)
	}
}

// ReadLog reads one page of the log the fixture keeps of the build's commands.
// Its cursor is the absolute byte offset after the page, in decimal.
func (b *Builder) ReadLog(ctx context.Context, in build.BuildRequest, read build.LogRead) (build.LogPage, error) {
	headers := map[string]string{logLimitHeader: strconv.Itoa(read.MaximumBytes)}
	switch {
	case read.Tail:
		headers[logTailHeader] = "true"
	case read.Cursor != "":
		if _, err := strconv.ParseUint(read.Cursor, 10, 63); err != nil {
			return build.LogPage{}, fmt.Errorf("%w: %q is not a byte offset", build.ErrInvalidLogCursor, read.Cursor)
		}
		headers[logOffsetHeader] = read.Cursor
	default:
		headers[logOffsetHeader] = "0"
	}
	var answer struct {
		Text     string `json:"text"`
		Next     uint64 `json:"next"`
		Complete bool   `json:"complete"`
	}
	if err := b.post(ctx, "log-read", in, headers, &answer); err != nil {
		return build.LogPage{}, err
	}
	return build.LogPage{Text: answer.Text, Next: strconv.FormatUint(answer.Next, 10), Complete: answer.Complete}, nil
}

// The headers of a log read: the absolute offset to read from, or a read of
// the log's end, and the most bytes to answer.
const (
	logOffsetHeader = "X-Superverse-Log-Offset"
	logTailHeader   = "X-Superverse-Log-Tail"
	logLimitHeader  = "X-Superverse-Log-Limit"
)

// call posts the request to one typed fixture endpoint that answers a build
// result.
func (b *Builder) call(ctx context.Context, action string, in build.BuildRequest, repositoryToken string) (build.BuildResult, error) {
	headers := map[string]string{"X-Superverse-SDK-Failure-Category": build.FailureUnsupportedDexSDK.Name()}
	if repositoryToken != "" {
		headers[repositoryCredentialHeader] = repositoryToken
	}
	var wire struct {
		build.BuildResult
		Category string `json:"failureCategory"`
	}
	if err := b.post(ctx, action, in, headers, &wire); err != nil {
		return build.BuildResult{}, err
	}
	result := wire.BuildResult
	if wire.Category != "" {
		category, known := build.ParseFailureCategory(wire.Category)
		if !known {
			return build.BuildResult{}, fmt.Errorf("local builder returned an unknown failure category")
		}
		result.FailureCategory = category
	}
	if result.FailureCategory == build.FailureUnsupportedDexSDK {
		result.Failure = result.FailureCategory.Hint()
	}
	switch result.State {
	case build.BuildMissing, build.BuildRunning, build.BuildReady, build.BuildFailed, build.BuildUnknown:
		return result, nil
	default:
		return build.BuildResult{}, fmt.Errorf("local build %s answered the unknown state %q", action, result.State)
	}
}

// repositoryCredentialHeader carries the repository token, never the request
// body.
const repositoryCredentialHeader = "X-Superverse-Repository-Credential"

// post sends the request with headers to one typed fixture endpoint and
// decodes its answer into output.
func (b *Builder) post(ctx context.Context, action string, in build.BuildRequest, headers map[string]string, output any) error {
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint+"/releases/"+action, bytes.NewReader(comm.Must(json.Marshal(in)))))
	request.Header.Set("Authorization", "Bearer "+b.token)
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := b.client.Do(request)
	if err != nil {
		return &platform.OutcomeUnknownError{Sentinel: errLocalOutcomeUnknown, Message: "local build " + action + " is unconfirmed", Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		// The fixture answers a failure with its own fixed text, never provider
		// output; a bounded tail keeps it readable.
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("local build %s was rejected with %s: %s", action, response.Status, responseTail(body))
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 128<<20)).Decode(output); err != nil {
		return &platform.OutcomeUnknownError{Sentinel: errLocalOutcomeUnknown, Message: "invalid local build " + action + " response", Cause: err}
	}
	return nil
}

// responseTail is the last 512 bytes of a response body as one line of text.
func responseTail(body []byte) string {
	if len(body) > 512 {
		body = body[len(body)-512:]
	}
	return strings.Join(strings.Fields(strings.ToValidUTF8(string(body), "")), " ")
}
