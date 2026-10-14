// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dockerapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pullTimeout bounds one pull, which downloads the image's layers.
const pullTimeout = 10 * time.Minute

// Image is what the engine reports of one local image.
type Image struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
}

// InspectImage reads the local image a reference names, such as
// repository@sha256:...; an absent image is nil.
func (c *Client) InspectImage(ctx context.Context, name string) (*Image, error) {
	var image Image
	status, err := c.call(ctx, http.MethodGet, "/images/"+reference(name)+"/json", nil, nil, &image)
	if err != nil || status == http.StatusNotFound {
		return nil, err
	}
	return &image, nil
}

// PullImage pulls the image of a reference pinned by digest,
// repository@sha256:..., from its registry.
func (c *Client) PullImage(ctx context.Context, name string) error {
	repository, digest, pinned := strings.Cut(name, "@")
	if !pinned {
		return fmt.Errorf("pull %s: the reference is not pinned by digest", name)
	}
	ctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	data, status, err := c.send(ctx, http.MethodPost, "/images/create", url.Values{"fromImage": {repository}, "tag": {digest}}, nil, "", 16<<20)
	if err == nil && status == http.StatusNotFound {
		err = fmt.Errorf("pull %s: the registry has no such image", name)
	}
	if err != nil {
		return err
	}
	// The engine streams the pull's progress; a failure is a message with an error.
	lines := bufio.NewScanner(bytes.NewReader(data))
	lines.Buffer(make([]byte, 64<<10), 1<<20)
	for lines.Scan() {
		var message struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(lines.Bytes(), &message) == nil && message.Error != "" {
			return fmt.Errorf("pull %s: %s", name, message.Error)
		}
	}
	if lines.Err() != nil {
		return errors.Join(fmt.Errorf("read the pull progress of %s", name), lines.Err())
	}
	return nil
}
