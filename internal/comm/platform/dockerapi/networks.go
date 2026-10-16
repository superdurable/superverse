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
	"fmt"
	"net/http"
)

// Network is what the engine reports of one network.
type Network struct {
	Name   string            `json:"Name"`
	ID     string            `json:"Id"`
	Labels map[string]string `json:"Labels"`
	IPAM   IPAM              `json:"IPAM"`
	// Containers are the containers attached to the network, by container ID.
	Containers map[string]struct {
		Name string `json:"Name"`
	} `json:"Containers"`
}

// IPAM is the address configuration of a network.
type IPAM struct {
	Config []IPAMConfig `json:"Config"`
}

// IPAMConfig is one subnet of a network: the subnet, the range of it the
// engine allocates container addresses from, and the gateway.
type IPAMConfig struct {
	Subnet  string `json:"Subnet"`
	IPRange string `json:"IPRange,omitempty"`
	Gateway string `json:"Gateway,omitempty"`
}

// NetworkSpec is the configuration of a new bridge network.
type NetworkSpec struct {
	Name   string            `json:"Name"`
	Labels map[string]string `json:"Labels,omitempty"`
	// Address is the IPv4 configuration the network takes; the engine refuses
	// a subnet that overlaps another network's. A container then gets its
	// address from IPRange unless it names a fixed one of the subnet.
	Address IPAMConfig `json:"-"`
}

// InspectNetwork reads the network named name; an absent network is nil. The
// engine also matches IDs, so the network returned has exactly that name.
func (c *Client) InspectNetwork(ctx context.Context, name string) (*Network, error) {
	var network Network
	status, err := c.call(ctx, http.MethodGet, "/networks/"+segment(name), nil, nil, &network)
	if err != nil || status == http.StatusNotFound || network.Name != name {
		return nil, err
	}
	return &network, nil
}

// CreateNetwork creates a bridge network and returns its ID. A taken name is a
// conflict, and so is a subnet that overlaps another network's.
func (c *Client) CreateNetwork(ctx context.Context, spec NetworkSpec) (string, error) {
	type ipam struct {
		Driver string
		Config []IPAMConfig
	}
	body := struct {
		Name, Driver string
		Labels       map[string]string
		IPAM         *ipam `json:",omitempty"`
	}{Name: spec.Name, Driver: "bridge", Labels: spec.Labels}
	if spec.Address.Subnet != "" {
		body.IPAM = &ipam{Driver: "default", Config: []IPAMConfig{spec.Address}}
	}
	var created struct {
		ID string `json:"Id"`
	}
	if _, err := c.call(ctx, http.MethodPost, "/networks/create", nil, body, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", fmt.Errorf("create network %s: the engine returned no ID", spec.Name)
	}
	return created.ID, nil
}

// RemoveNetwork removes the network with ID id; an absent network is removed.
// A network with attached containers is a conflict.
func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	_, err := c.call(ctx, http.MethodDelete, "/networks/"+segment(id), nil, nil, nil)
	return err
}

// ListNetworks lists the networks the filters select.
func (c *Client) ListNetworks(ctx context.Context, filters Filters) ([]Network, error) {
	var networks []Network
	_, err := c.call(ctx, http.MethodGet, "/networks", filters.query(), nil, &networks)
	return networks, err
}

// ConnectNetwork attaches the container with ID container to the network with
// ID network under aliases. A container already attached is a conflict.
func (c *Client) ConnectNetwork(ctx context.Context, network, container string, aliases ...string) error {
	body := struct {
		Container      string
		EndpointConfig struct{ Aliases []string }
	}{Container: container}
	body.EndpointConfig.Aliases = aliases
	_, err := c.call(ctx, http.MethodPost, "/networks/"+segment(network)+"/connect", nil, body, nil)
	return err
}

// DisconnectNetwork detaches the container with ID container from the
// network with ID network; a container that is not attached is an error.
func (c *Client) DisconnectNetwork(ctx context.Context, network, container string) error {
	body := struct{ Container string }{Container: container}
	_, err := c.call(ctx, http.MethodPost, "/networks/"+segment(network)+"/disconnect", nil, body, nil)
	return err
}
