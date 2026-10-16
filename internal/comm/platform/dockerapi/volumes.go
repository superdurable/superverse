// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dockerapi

import (
	"context"
	"net/http"
)

// Volume is what the engine reports of one named volume. A volume has no ID;
// its owner identifies it by a label.
type Volume struct {
	Name   string            `json:"Name"`
	Labels map[string]string `json:"Labels"`
}

// InspectVolume reads the volume named name; an absent volume is nil.
func (c *Client) InspectVolume(ctx context.Context, name string) (*Volume, error) {
	var volume Volume
	status, err := c.call(ctx, http.MethodGet, "/volumes/"+segment(name), nil, nil, &volume)
	if err != nil || status == http.StatusNotFound {
		return nil, err
	}
	return &volume, nil
}

// CreateVolume creates the local volume name with labels. Creating a volume
// that exists returns the existing one, whose labels the caller verifies.
func (c *Client) CreateVolume(ctx context.Context, name string, labels map[string]string) (*Volume, error) {
	var volume Volume
	body := struct {
		Name, Driver string
		Labels       map[string]string
	}{Name: name, Driver: "local", Labels: labels}
	_, err := c.call(ctx, http.MethodPost, "/volumes/create", nil, body, &volume)
	return &volume, err
}

// RemoveVolume removes the volume; an absent volume is removed. A volume that a
// container mounts is a conflict.
func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	_, err := c.call(ctx, http.MethodDelete, "/volumes/"+segment(name), nil, nil, nil)
	return err
}

// ListVolumes lists the volumes the filters select.
func (c *Client) ListVolumes(ctx context.Context, filters Filters) ([]Volume, error) {
	var listed struct {
		Volumes []Volume `json:"Volumes"`
	}
	_, err := c.call(ctx, http.MethodGet, "/volumes", filters.query(), nil, &listed)
	return listed.Volumes, err
}
