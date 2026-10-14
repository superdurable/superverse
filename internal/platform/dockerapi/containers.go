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
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Container is what the engine reports of one container.
type Container struct {
	ID              string          `json:"Id"`
	Name            string          `json:"Name"`
	Created         string          `json:"Created"`
	State           ContainerState  `json:"State"`
	Config          ContainerConfig `json:"Config"`
	NetworkSettings networkSettings `json:"NetworkSettings"`
	Mounts          []mountPoint    `json:"Mounts"`
	RestartCount    int             `json:"RestartCount"`
}

// The states of a container.
const (
	StateCreated    = "created"
	stateRunning    = "running"
	stateRestarting = "restarting"
	StateRemoving   = "removing"
	StateExited     = "exited"
	StateDead       = "dead"
)

// ContainerState is the life cycle state of a container.
type ContainerState struct {
	Status     string `json:"Status"`
	Running    bool   `json:"Running"`
	Restarting bool   `json:"Restarting"`
	OOMKilled  bool   `json:"OOMKilled"`
	ExitCode   int    `json:"ExitCode"`
	Error      string `json:"Error"`
	StartedAt  string `json:"StartedAt"`
	FinishedAt string `json:"FinishedAt"`
}

// ContainerConfig is the part of a container's configuration the platform
// reads back.
type ContainerConfig struct {
	Image  string            `json:"Image"`
	Labels map[string]string `json:"Labels"`
	Env    []string          `json:"Env"`
}

// networkSettings are a container's published ports and the networks it is
// attached to, by network name.
type networkSettings struct {
	Ports    map[string][]portBinding    `json:"Ports"`
	Networks map[string]endpointSettings `json:"Networks"`
}

// endpointSettings are a container's attachment to one network: its aliases
// and its address, which IPAMConfig names when the container was created at a
// fixed one.
type endpointSettings struct {
	NetworkID  string              `json:"NetworkID"`
	Aliases    []string            `json:"Aliases"`
	DNSNames   []string            `json:"DNSNames"`
	IPAddress  string              `json:"IPAddress"`
	IPAMConfig *endpointIPAMConfig `json:"IPAMConfig"`
}

// endpointIPAMConfig is the fixed address of a container on one network.
type endpointIPAMConfig struct {
	IPv4Address string `json:"IPv4Address,omitempty"`
}

// portBinding is a container port published on the host.
type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// mountPoint is one mount of a container: a named volume's Name, or a bind's
// Source.
type mountPoint struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Destination string `json:"Destination"`
}

// Loopback is the only host address the platform publishes ports on.
const Loopback = "127.0.0.1"

// PublishedAddress is the loopback address on which the running container
// publishes its TCP port. The engine assigns a published port when the
// container starts, so the address of a restarted container differs and no
// caller keeps it.
func (c Container) PublishedAddress(port int) (string, error) {
	if !c.State.Running {
		return "", fmt.Errorf("container %s is %s, not running", c.Name, c.State.Status)
	}
	hostPort, published := c.LoopbackPort(port)
	if !published {
		return "", fmt.Errorf("container %s publishes no loopback port for %d", c.Name, port)
	}
	return net.JoinHostPort(Loopback, hostPort), nil
}

// LoopbackPort is the port on 127.0.0.1 where the running container publishes
// port, if it does.
func (c Container) LoopbackPort(port int) (string, bool) {
	if !c.State.Running {
		return "", false
	}
	for _, binding := range c.NetworkSettings.Ports[portKey(port)] {
		if binding.HostIP == Loopback && binding.HostPort != "" {
			return binding.HostPort, true
		}
	}
	return "", false
}

// Attached reports whether the container is attached to the network with ID
// network under alias.
func (c Container) Attached(network, alias string) bool {
	for _, endpoint := range c.NetworkSettings.Networks {
		if endpoint.NetworkID != network {
			continue
		}
		for _, name := range append(endpoint.Aliases, endpoint.DNSNames...) {
			if name == alias {
				return true
			}
		}
	}
	return false
}

// Joined reports whether the container is attached to the network with ID
// network.
func (c Container) Joined(network string) bool {
	for _, endpoint := range c.NetworkSettings.Networks {
		if endpoint.NetworkID == network {
			return true
		}
	}
	return false
}

// portKey is the engine's spelling of a TCP port.
func portKey(port int) string { return strconv.Itoa(port) + "/tcp" }

// ContainerSpec is the configuration of a new container.
type ContainerSpec struct {
	Image        string              `json:"Image"`
	Entrypoint   []string            `json:"Entrypoint,omitempty"`
	Cmd          []string            `json:"Cmd,omitempty"`
	Env          []string            `json:"Env,omitempty"`
	User         string              `json:"User,omitempty"`
	Labels       map[string]string   `json:"Labels,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	// StopTimeout is the seconds a stop waits before it kills the container.
	StopTimeout      int              `json:"StopTimeout,omitempty"`
	HostConfig       HostConfig       `json:"HostConfig"`
	NetworkingConfig networkingConfig `json:"NetworkingConfig"`
}

// HostConfig is the isolation and the resources of a new container.
type HostConfig struct {
	// NetworkMode is the first network of EndpointsConfig.
	NetworkMode    string                   `json:"NetworkMode,omitempty"`
	Mounts         []Mount                  `json:"Mounts,omitempty"`
	PortBindings   map[string][]portBinding `json:"PortBindings,omitempty"`
	ReadonlyRootfs bool                     `json:"ReadonlyRootfs,omitempty"`
	CapDrop        []string                 `json:"CapDrop,omitempty"`
	SecurityOpt    []string                 `json:"SecurityOpt,omitempty"`
	NanoCPUs       int64                    `json:"NanoCpus,omitempty"`
	Memory         int64                    `json:"Memory,omitempty"`
	RestartPolicy  RestartPolicy            `json:"RestartPolicy"`
}

// Mount mounts a named volume, or an anonymous one without Source, at Target.
type Mount struct {
	Type   string `json:"Type"`
	Source string `json:"Source,omitempty"`
	Target string `json:"Target"`
}

// VolumeMount mounts the named volume source at target; an empty source is an
// anonymous volume, which the container's removal deletes.
func VolumeMount(source, target string) Mount {
	return Mount{Type: "volume", Source: source, Target: target}
}

// RestartPolicy is what the engine does when the container exits: "no" or
// "unless-stopped".
type RestartPolicy struct {
	Name string `json:"Name"`
}

// networkingConfig attaches a new container to networks, by name.
type networkingConfig struct {
	EndpointsConfig map[string]endpointConfig `json:"EndpointsConfig"`
}

// endpointConfig names the aliases of a container on one network and, for a
// fixed address, the address.
type endpointConfig struct {
	Aliases    []string            `json:"Aliases,omitempty"`
	IPAMConfig *endpointIPAMConfig `json:"IPAMConfig,omitempty"`
}

// Publish publishes each TCP port of a new container on an ephemeral loopback
// port of the host.
func (s *ContainerSpec) Publish(ports ...int) {
	if s.ExposedPorts == nil {
		s.ExposedPorts = map[string]struct{}{}
	}
	if s.HostConfig.PortBindings == nil {
		s.HostConfig.PortBindings = map[string][]portBinding{}
	}
	for _, port := range ports {
		s.ExposedPorts[portKey(port)] = struct{}{}
		s.HostConfig.PortBindings[portKey(port)] = []portBinding{{HostIP: Loopback}}
	}
}

// Attach attaches a new container to network under aliases. The first network
// is also the container's network mode.
func (s *ContainerSpec) Attach(network string, aliases ...string) {
	s.AttachAt(network, "", aliases...)
}

// AttachAt attaches a new container to network at the fixed IPv4 address, or
// at an address the engine allocates when address is empty. The engine
// refuses an address another container holds.
func (s *ContainerSpec) AttachAt(network, address string, aliases ...string) {
	if s.NetworkingConfig.EndpointsConfig == nil {
		s.NetworkingConfig.EndpointsConfig = map[string]endpointConfig{}
	}
	if s.HostConfig.NetworkMode == "" {
		s.HostConfig.NetworkMode = network
	}
	endpoint := endpointConfig{Aliases: aliases}
	if address != "" {
		endpoint.IPAMConfig = &endpointIPAMConfig{IPv4Address: address}
	}
	s.NetworkingConfig.EndpointsConfig[network] = endpoint
}

// FixedAddress is the fixed IPv4 address the container was created at on
// network, if it was.
func (c Container) FixedAddress(network string) (string, bool) {
	endpoint, attached := c.NetworkSettings.Networks[network]
	if !attached || endpoint.IPAMConfig == nil || endpoint.IPAMConfig.IPv4Address == "" {
		return "", false
	}
	return endpoint.IPAMConfig.IPv4Address, true
}

// ContainerSummary is one container of a list.
type ContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Labels map[string]string `json:"Labels"`
	State  string            `json:"State"`
	Mounts []mountPoint      `json:"Mounts"`
}

// InspectContainer reads the container named or identified by nameOrID; an
// absent container is nil.
func (c *Client) InspectContainer(ctx context.Context, nameOrID string) (*Container, error) {
	var container Container
	status, err := c.call(ctx, http.MethodGet, "/containers/"+segment(nameOrID)+"/json", nil, nil, &container)
	if err != nil || status == http.StatusNotFound {
		return nil, err
	}
	return &container, nil
}

// CreateContainer creates the container name from spec and returns its ID. A
// taken name is a conflict: the caller inspects the container that holds it.
func (c *Client) CreateContainer(ctx context.Context, name string, spec ContainerSpec) (string, error) {
	var created struct {
		ID string `json:"Id"`
	}
	status, err := c.call(ctx, http.MethodPost, "/containers/create", url.Values{"name": {name}}, spec, &created)
	if err == nil && (status == http.StatusNotFound || created.ID == "") {
		// The engine answers 404 for a missing image or network.
		err = fmt.Errorf("create container %s: an image or network it names is absent", name)
	}
	return created.ID, err
}

// StartContainer starts the container; one that runs already is started.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	status, err := c.call(ctx, http.MethodPost, "/containers/"+segment(id)+"/start", nil, nil, nil)
	if err == nil && status == http.StatusNotFound {
		return fmt.Errorf("start container %s: it is absent", id)
	}
	return err
}

// StopContainer sends the container its stop signal and kills it after
// timeout. The engine completes the stop even when ctx ends first.
func (c *Client) StopContainer(ctx context.Context, id string, timeout time.Duration) error {
	seconds := int(timeout / time.Second)
	ctx, cancel := context.WithTimeout(ctx, timeout+callTimeout)
	defer cancel()
	_, err := c.call(ctx, http.MethodPost, "/containers/"+segment(id)+"/stop", url.Values{"t": {strconv.Itoa(seconds)}}, nil, nil)
	return err
}

// RemoveContainer removes a stopped container with its anonymous volumes; an
// absent container is removed. A running container is a conflict.
func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	_, err := c.call(ctx, http.MethodDelete, "/containers/"+segment(id), url.Values{"v": {queryTrue}}, nil, nil)
	return err
}

// ListContainers lists every container the filters select, in any state.
func (c *Client) ListContainers(ctx context.Context, filters Filters) ([]ContainerSummary, error) {
	query := filters.query()
	query.Set("all", queryTrue)
	var containers []ContainerSummary
	_, err := c.call(ctx, http.MethodGet, "/containers/json", query, nil, &containers)
	return containers, err
}

// CopyToContainer extracts the tar archive into the directory path of a
// container, which may not have started.
func (c *Client) CopyToContainer(ctx context.Context, id, path string, archive []byte) error {
	_, status, err := c.send(ctx, http.MethodPut, "/containers/"+segment(id)+"/archive", url.Values{"path": {path}}, archive, "application/x-tar", 1<<20)
	if err == nil && status == http.StatusNotFound {
		return fmt.Errorf("copy into container %s: it or %s is absent", id, path)
	}
	return err
}

// ContainerLogs reads the last tail lines of the container's output and error
// streams, at most limit bytes of them.
func (c *Client) ContainerLogs(ctx context.Context, id string, tail, limit int) (string, error) {
	query := url.Values{"stdout": {queryTrue}, "stderr": {queryTrue}, "tail": {strconv.Itoa(tail)}}
	// The frame headers come on top of the text.
	data, status, err := c.send(ctx, http.MethodGet, "/containers/"+segment(id)+"/logs", query, nil, "", int64(limit)+(1<<16))
	if err != nil || status == http.StatusNotFound {
		return "", err
	}
	text, err := demultiplex(data)
	if len(text) > limit {
		text = text[len(text)-limit:]
	}
	return text, err
}

// demultiplex joins the payloads of a container's multiplexed log stream:
// frames of an 8-byte header, whose last four bytes are the big-endian payload
// size, and the payload.
func demultiplex(data []byte) (string, error) {
	var text strings.Builder
	for len(data) > 0 {
		if len(data) < 8 {
			return text.String(), errors.New("container log ends inside a frame header")
		}
		size := int(binary.BigEndian.Uint32(data[4:8]))
		data = data[8:]
		if size > len(data) {
			size = len(data)
		}
		text.Write(data[:size])
		data = data[size:]
	}
	return text.String(), nil
}

// Exec runs command in the running container as its own user, without a
// terminal or output, and returns its exit code once it ends. It waits at most
// until ctx ends.
func (c *Client) Exec(ctx context.Context, id string, command []string) (int, error) {
	var created struct {
		ID string `json:"Id"`
	}
	status, err := c.call(ctx, http.MethodPost, "/containers/"+segment(id)+"/exec", nil, map[string]any{"Cmd": command}, &created)
	if err == nil && (status == http.StatusNotFound || created.ID == "") {
		err = fmt.Errorf("exec in container %s: it is absent", id)
	}
	if err != nil {
		return 0, err
	}
	if _, err = c.call(ctx, http.MethodPost, "/exec/"+segment(created.ID)+"/start", nil, map[string]any{"Detach": true}, nil); err != nil {
		return 0, err
	}
	for {
		var execution struct {
			Running bool `json:"Running"`
			// ExitCode is null until the command ended.
			ExitCode *int `json:"ExitCode"`
		}
		if _, err = c.call(ctx, http.MethodGet, "/exec/"+segment(created.ID)+"/json", nil, nil, &execution); err != nil {
			return 0, err
		}
		if !execution.Running && execution.ExitCode != nil {
			return *execution.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
