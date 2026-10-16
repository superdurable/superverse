// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// remoteHTTPClient uses the actual separately configured endpoint returned by
// CreateRemoteAgentPairing; it never starts a loopback server or replaces a port.
type remoteHTTPClient struct {
	t               *testing.T
	endpoint, token string
	client          *http.Client
	after           uint64
	digest          string
}

func (remote *remoteHTTPClient) post(ctx context.Context, path string, input, output any, expected int) {
	remote.t.Helper()
	backoff := 50 * time.Millisecond
	for {
		status, data, err := remote.request(ctx, path, input)
		if err != nil {
			remote.t.Fatalf("remote %s request failed (%T)", path, err)
		}
		if path == bridgeapi.PathSync && expected == http.StatusOK && (status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests) {
			select {
			case <-ctx.Done():
				remote.t.Fatalf("remote %s remained unavailable: %v", path, ctx.Err())
			case <-time.After(backoff):
			}
			backoff = min(2*backoff, time.Second)
			continue
		}
		if status != expected {
			remote.t.Fatalf("remote %s returned %d, expected %d", path, status, expected)
		}
		if err = json.Unmarshal(data, output); err != nil {
			remote.t.Fatalf("remote %s invalid typed response (%T)", path, err)
		}
		return
	}
}

func (remote *remoteHTTPClient) awaitResult(ctx context.Context, epoch uint64, callID conversation.CallID, content string) {
	remote.t.Helper()
	remote.awaitToolResult(ctx, epoch, callID, content, false)
}

func (remote *remoteHTTPClient) awaitToolResult(ctx context.Context, epoch uint64, callID conversation.CallID, content string, isError bool) {
	remote.t.Helper()
	for {
		var response bridgeapi.SyncResponse
		remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: epoch, After: remote.after, StateDigest: remote.digest}, &response, http.StatusOK)
		if response.ExaminedThrough < remote.after || response.ExaminedThrough > response.LastSequence || len(response.Entries) > bridgeapi.MaxSyncEntries {
			remote.t.Fatal("sync cursor/entry bounds violated")
		}
		remote.after, remote.digest = response.ExaminedThrough, response.StateDigest
		for _, entry := range response.Entries {
			if entry.Kind == bridgeapi.EntryKindModelReply || entry.Kind == bridgeapi.EntryKindSegmentStart || entry.Role != bridgeapi.EntryRoleUser {
				remote.t.Fatal("sync exposed model-only context")
			}
			for _, result := range entry.ToolResults {
				if result.CallID == string(callID) {
					if result.IsError != isError || !strings.Contains(result.Content, content) {
						remote.t.Fatalf("remote result %s did not establish expected success", callID)
					}
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			remote.t.Fatalf("remote result %s did not arrive", callID)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (remote *remoteHTTPClient) request(ctx context.Context, path string, input any) (int, []byte, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return 0, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(remote.endpoint, "/")+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if remote.token != "" {
		request.Header.Set("Authorization", "Bearer "+remote.token)
	}
	response, err := remote.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, bridgeapi.MaxSyncResponseBytes+1))
	if err != nil {
		return 0, nil, err
	}
	if len(data) > bridgeapi.MaxSyncResponseBytes {
		return 0, nil, errors.New("remote response exceeds hard wire bound")
	}
	return response.StatusCode, data, nil
}
