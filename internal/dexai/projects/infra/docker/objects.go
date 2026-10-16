// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// subnetBits is the size of the subnet every network takes from the prefix.
const subnetBits = 24

// The address layout of every network's /24: the gateway is the first
// address, the engine allocates container addresses only from the upper half
// (allocatedBits), and the lower half holds fixed addresses, which a workload
// may keep for the network's life because no allocation can take them.
const (
	gatewayHost   = 1
	allocatedBits = 25
	// storageHost is the fixed address of the project storage on its project
	// network, which the workload grants name.
	storageHost = 2
)

// networkAddresses is the address configuration of a new network on subnet.
func networkAddresses(subnet netip.Prefix) dockerapi.IPAMConfig {
	upper := subnet.Addr().As4()
	upper[3] = 1 << (32 - allocatedBits)
	return dockerapi.IPAMConfig{Subnet: subnet.String(), IPRange: netip.PrefixFrom(netip.AddrFrom4(upper), allocatedBits).String(),
		Gateway: hostAddress(subnet, gatewayHost).String()}
}

// hostAddress is the address host of subnet.
func hostAddress(subnet netip.Prefix, host byte) netip.Addr {
	address := subnet.Addr().As4()
	address[3] = host
	return netip.AddrFrom4(address)
}

// fixedAddress is the fixed address host of network, whose configuration
// must be the layout of networkAddresses. A network without it predates the
// fixed addresses and could hand the address to another container.
func fixedAddress(network *dockerapi.Network, host byte) (netip.Addr, error) {
	if len(network.IPAM.Config) == 1 {
		subnet, err := netip.ParsePrefix(network.IPAM.Config[0].Subnet)
		if err == nil && subnet.Addr().Is4() && subnet.Bits() == subnetBits && network.IPAM.Config[0] == networkAddresses(subnet) {
			return hostAddress(subnet, host), nil
		}
	}
	return netip.Addr{}, fmt.Errorf("network %s reserves no fixed addresses: it predates them, so delete its project and create it again", network.Name)
}

// EnsureNetwork returns the network name, creating it with labels and a free
// subnet of the network prefix when it is absent. An existing network must
// carry every label of labels, and one whose ID differs from a non-empty
// acceptedID is a replacement that is never adopted.
func (k *Infrastructure) EnsureNetwork(ctx context.Context, name string, labels map[string]string, acceptedID string) (infra.ResourceRef, error) {
	network, err := k.api.InspectNetwork(ctx, name)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	if network == nil {
		if err = k.createNetwork(ctx, name, labels); err != nil {
			return infra.ResourceRef{}, err
		}
		if network, err = k.api.InspectNetwork(ctx, name); err != nil {
			return infra.ResourceRef{}, err
		}
		if network == nil {
			return infra.ResourceRef{}, fmt.Errorf("%w: network %s was removed while it was created", infra.ErrPending, name)
		}
	}
	if err = VerifyLabels(network.Labels, labels); err != nil {
		return infra.ResourceRef{}, fmt.Errorf("network %s: %w", name, err)
	}
	if acceptedID != "" && network.ID != acceptedID {
		return infra.ResourceRef{}, fmt.Errorf("%w: network %s was replaced", infra.ErrOwnershipConflict, name)
	}
	return infra.ResourceRef{Name: name, UID: network.ID}, nil
}

// createNetwork creates the network on the first /24 of the prefix that no
// network uses. Another network may take that subnet first, which the next
// round sees; a name another creation took is left to the caller's read.
func (k *Infrastructure) createNetwork(ctx context.Context, name string, labels map[string]string) error {
	for range 8 {
		networks, err := k.api.ListNetworks(ctx, nil)
		if err != nil {
			return err
		}
		subnet, free := freeSubnet(k.config.NetworkPrefix, networks)
		if !free {
			return fmt.Errorf("the network prefix %s has no free /%d subnet left", k.config.NetworkPrefix, subnetBits)
		}
		_, err = k.api.CreateNetwork(ctx, dockerapi.NetworkSpec{Name: name, Labels: labels, Address: networkAddresses(subnet)})
		if err == nil || dockerapi.IsConflict(err) {
			return nil
		}
		if networks, err2 := k.api.ListNetworks(ctx, nil); err2 != nil || !subnetUsed(subnet, networks) {
			return errors.Join(fmt.Errorf("create network %s: %w", name, err), err2)
		}
	}
	return fmt.Errorf("%w: other networks keep taking the free subnets of %s", infra.ErrPending, k.config.NetworkPrefix)
}

// freeSubnet is the first /24 of prefix that overlaps no network's subnet.
func freeSubnet(prefix netip.Prefix, networks []dockerapi.Network) (netip.Prefix, bool) {
	count := 1 << (subnetBits - prefix.Bits())
	base := prefix.Masked().Addr().As4()
	for index := range count {
		address := base
		address[1] += byte(index >> 8)
		address[2] += byte(index)
		subnet := netip.PrefixFrom(netip.AddrFrom4(address), subnetBits)
		if !subnetUsed(subnet, networks) {
			return subnet, true
		}
	}
	return netip.Prefix{}, false
}

// subnetUsed reports whether subnet overlaps the subnet of any network.
func subnetUsed(subnet netip.Prefix, networks []dockerapi.Network) bool {
	for _, network := range networks {
		for _, config := range network.IPAM.Config {
			if used, err := netip.ParsePrefix(config.Subnet); err == nil && used.Overlaps(subnet) {
				return true
			}
		}
	}
	return false
}

// CurrentNetwork returns the network name when it is still the accepted one:
// owned per labels and with ID id. Absence is an error; only cleanup treats
// it as done, through CleanupNetwork.
func (k *Infrastructure) CurrentNetwork(ctx context.Context, name, id string, labels map[string]string) (*dockerapi.Network, error) {
	network, err := k.CleanupNetwork(ctx, name, id, labels)
	if err == nil && network == nil {
		err = fmt.Errorf("network %s is absent", name)
	}
	return network, err
}

// CleanupNetwork returns the network name for cleanup, or nil when it is
// absent. A network of another owner, or whose ID differs from a non-empty
// id, is an ownership conflict.
func (k *Infrastructure) CleanupNetwork(ctx context.Context, name, id string, labels map[string]string) (*dockerapi.Network, error) {
	network, err := k.api.InspectNetwork(ctx, name)
	if err != nil || network == nil {
		return nil, err
	}
	if err = VerifyLabels(network.Labels, labels); err != nil {
		return nil, fmt.Errorf("network %s: %w", name, err)
	}
	if id != "" && network.ID != id {
		return nil, fmt.Errorf("%w: network %s was replaced", infra.ErrOwnershipConflict, name)
	}
	return network, nil
}

// RemoveNetwork removes the owned network name with ID id and reports whether
// it is gone. A network that containers still use is not gone yet.
func (k *Infrastructure) RemoveNetwork(ctx context.Context, name, id string, labels map[string]string) (bool, error) {
	network, err := k.CleanupNetwork(ctx, name, id, labels)
	if err != nil || network == nil {
		return network == nil && err == nil, err
	}
	if err = k.api.RemoveNetwork(ctx, network.ID); dockerapi.IsConflict(err) {
		return false, nil
	}
	return err == nil, err
}

// EnsureVolume returns the volume name, creating it with labels when it is
// absent. An existing volume must carry every label of labels.
func (k *Infrastructure) EnsureVolume(ctx context.Context, name string, labels map[string]string) (*dockerapi.Volume, error) {
	volume, err := k.api.InspectVolume(ctx, name)
	if err != nil {
		return nil, err
	}
	if volume == nil {
		if volume, err = k.api.CreateVolume(ctx, name, labels); err != nil {
			return nil, err
		}
	}
	if err = VerifyLabels(volume.Labels, labels); err != nil {
		return nil, fmt.Errorf("volume %s: %w", name, err)
	}
	return volume, nil
}

// RemoveVolume removes the volume name when it carries every label of labels
// and reports whether it is gone. A volume a container still mounts is not
// gone yet.
func (k *Infrastructure) RemoveVolume(ctx context.Context, name string, labels map[string]string) (bool, error) {
	volume, err := k.api.InspectVolume(ctx, name)
	if err != nil || volume == nil {
		return err == nil, err
	}
	if err = VerifyLabels(volume.Labels, labels); err != nil {
		return false, fmt.Errorf("volume %s: %w", name, err)
	}
	if err = k.api.RemoveVolume(ctx, name); dockerapi.IsConflict(err) {
		return false, nil
	}
	return err == nil, err
}

// OwnedContainer reads the container named name and returns it only when it
// carries every label of labels; an absent container is nil.
func (k *Infrastructure) OwnedContainer(ctx context.Context, name string, labels map[string]string) (*dockerapi.Container, error) {
	container, err := k.api.InspectContainer(ctx, name)
	if err != nil || container == nil {
		return nil, err
	}
	if container.Name != "/"+name {
		// The engine also matches an ID prefix; the name must match exactly.
		return nil, nil
	}
	if err = VerifyLabels(container.Config.Labels, labels); err != nil {
		return nil, fmt.Errorf("container %s: %w", name, err)
	}
	return container, nil
}

// EnsureContainer returns the container name, creating it from spec when it
// is absent and starting it when it is not running. An existing container
// must carry every label of spec.Labels; the caller compares what else it
// requires. prepare, when given, runs on a container that never started, such
// as one that needs a file before its process starts.
func (k *Infrastructure) EnsureContainer(ctx context.Context, name string, spec dockerapi.ContainerSpec, prepare func(ctx context.Context, id string) error) (*dockerapi.Container, error) {
	container, err := k.OwnedContainer(ctx, name, spec.Labels)
	if err != nil {
		return nil, err
	}
	if container == nil {
		if err = k.EnsureImage(ctx, spec.Image); err != nil {
			return nil, err
		}
		if _, err = k.api.CreateContainer(ctx, name, spec); err != nil && !dockerapi.IsConflict(err) {
			return nil, err
		}
		if container, err = k.OwnedContainer(ctx, name, spec.Labels); err != nil {
			return nil, err
		}
		if container == nil {
			return nil, fmt.Errorf("%w: container %s was removed while it was created", infra.ErrPending, name)
		}
	}
	switch container.State.Status {
	case dockerapi.StateCreated:
		if prepare != nil {
			if err = prepare(ctx, container.ID); err != nil {
				return nil, err
			}
		}
	case dockerapi.StateExited, dockerapi.StateDead:
	default:
		return container, nil
	}
	if err = k.api.StartContainer(ctx, container.ID); err != nil {
		return nil, err
	}
	return k.OwnedContainer(ctx, name, spec.Labels)
}

// EnsureImage pulls the image reference, pinned by digest, when the engine
// does not have it yet.
func (k *Infrastructure) EnsureImage(ctx context.Context, reference string) error {
	image, err := k.api.InspectImage(ctx, reference)
	if err != nil || image != nil {
		return err
	}
	return k.api.PullImage(ctx, reference)
}

// RemoveContainer stops the owned container name, sending its stop signal and
// killing it after grace, removes it with its anonymous volumes and reports
// whether it is gone. A container that is still stopping or being removed is
// not gone yet; the engine completes a stop it started.
func (k *Infrastructure) RemoveContainer(ctx context.Context, name string, labels map[string]string, grace time.Duration) (bool, error) {
	container, err := k.OwnedContainer(ctx, name, labels)
	if err != nil || container == nil {
		return err == nil, err
	}
	if container.State.Running || container.State.Restarting {
		if err = k.api.StopContainer(ctx, container.ID, grace); err != nil {
			return false, err
		}
	}
	if err = k.api.RemoveContainer(ctx, container.ID); dockerapi.IsConflict(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	remaining, err := k.api.InspectContainer(ctx, container.ID)
	return remaining == nil, err
}

// published is the loopback address on which the running container name
// publishes port. The engine assigns it when the container starts, so it is
// looked up for every use and never kept.
func (k *Infrastructure) published(ctx context.Context, name string, labels map[string]string, port int) (string, error) {
	container, err := k.OwnedContainer(ctx, name, labels)
	if err != nil {
		return "", err
	}
	if container == nil {
		return "", fmt.Errorf("container %s is absent", name)
	}
	return container.PublishedAddress(port)
}

// accepts reports whether address accepts a TCP connection within two
// seconds, which is how the runtime observes that a component listens.
func accepts(ctx context.Context, address string) bool {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}
