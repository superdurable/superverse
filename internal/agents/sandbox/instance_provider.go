// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// InstanceProvider is the runtime of Sandbox instances: it creates, finds,
// exposes and removes the objects an instance runs in. It queries exact
// resource identities before every effect and owns no lifecycle state: the
// Project's Session owns the instance, SandboxProvisionFlow provisions it,
// PreviewFlow serves the Project's Preview, and the deleting Project releases
// what is left. Package
// kube implements it for Kubernetes and package docker for Docker.
type InstanceProvider interface {
	// Provision creates the instance's resources, adopting the ones the instance
	// records, and returns them; ready reports that the Sandbox is Ready and
	// serves its Service. A recorded identity that no longer matches is a
	// failure, never adopted. ErrProfileMismatch and ErrVolumeInUse refuse an
	// instance whose retained workspace volume cannot be used.
	Provision(ctx context.Context, s Instance) (resources Resources, ready bool, err error)
	// DiscoverResources completes the references a reclamation or the
	// Project's release needs from the deterministic names of the instance's
	// resources: the release may know none of them, and a provisioning whose
	// attempts failed after creating objects recorded none. An absent object
	// keeps the zero reference.
	DiscoverResources(ctx context.Context, s Instance) (Resources, error)
	// SessionBoundary names the isolation boundary of the Project's Session
	// as the runtime names it: its namespace or its network, which a Native
	// instance's name suffixes.
	SessionBoundary(projectID string) string
	// EnsurePreviewEnvironment starts the instance's Preview engine, or finds it
	// running, and once it answers ready verifies that its Dex answers through
	// the instance's own Service: infra.ErrPending while it does not yet,
	// infra.ErrOwnershipConflict when a recorded object was replaced.
	EnsurePreviewEnvironment(ctx context.Context, s Instance) (workspace.PreviewAnswer, error)
	// ExposePreview reconciles the Project's Preview route to the instance's
	// application, as the platform wants the route now, under the Project's
	// stable host, and probes it through the real gateway. It returns the
	// public URL once the application answers, and until then an empty URL
	// with what it waits for.
	ExposePreview(ctx context.Context, s Instance) (publicURL, unanswered string, err error)
	// StopPreview removes the Project's Preview route and then stops the
	// instance's application, so a stopped Preview exposes nothing and holds
	// no rule of a shared load balancer. done is false while the instance's
	// boundary still terminates.
	StopPreview(ctx context.Context, s Instance, commandID string) (done bool, err error)
	// ReleasePreviewRoute removes the Project's Preview route objects,
	// whichever instance published them: StopPreview calls it, and so does the
	// deleting Project's release for a route no stop removed. done is false
	// while the boundary still terminates.
	ReleasePreviewRoute(ctx context.Context, s Instance) (done bool, err error)
	// DeletePod, DeleteVolume and DeleteNamespace delete the recorded runtime,
	// workspace volume and boundary of the instance, each only if it still is the
	// recorded object, and report whether it is gone.
	DeletePod(ctx context.Context, s Instance) (done bool, err error)
	DeleteVolume(ctx context.Context, s Instance) (done bool, err error)
	DeleteNamespace(ctx context.Context, s Instance) (done bool, err error)
}

// The coding profile infra.CodingSandboxProfile is an immutable rendering of
// the chart that declares the resources of every Sandbox instance, which both
// runtimes read; the coding image implements its contract revision.
const (
	ImageContractRevision = 8
	// ImageContractLabel carries the revision on a Sandbox's objects.
	ImageContractLabel = "superdurable.io/sandbox-image-contract-revision"
)

// VerifyCodingProfile checks the data of the rendered coding profile named
// name: its contract declares infra.CodingSandboxProfile with
// ImageContractRevision, and every resource of its namespace baseline matches
// its digest.
func VerifyCodingProfile(name string, data map[string]string) error {
	var contract struct {
		Profile  string `json:"profile"`
		Revision int    `json:"sandboxImageContractRevision"`
		Baseline struct {
			Resources []struct {
				Key    string `json:"key"`
				SHA256 string `json:"sha256"`
			} `json:"resources"`
		} `json:"namespaceBaseline"`
	}
	if err := json.Unmarshal([]byte(data["contract.json"]), &contract); err != nil {
		return fmt.Errorf("decode the contract of coding profile %s: %w", name, err)
	}
	if contract.Profile != infra.CodingSandboxProfile || contract.Revision != ImageContractRevision {
		return fmt.Errorf("coding profile %s declares %q revision %d; this platform implements %q revision %d", name, contract.Profile, contract.Revision, infra.CodingSandboxProfile, ImageContractRevision)
	}
	for _, item := range contract.Baseline.Resources {
		sum := sha256.Sum256([]byte(data[item.Key]))
		if hex.EncodeToString(sum[:]) != item.SHA256 {
			return fmt.Errorf("coding profile %s: %s does not match its baseline digest", name, item.Key)
		}
	}
	return nil
}

// Typed refusals of provisioning, which asking again cannot change.
var (
	// ErrProfileMismatch: a retained workspace volume of another coding
	// profile, whose repository and Preview state layout this profile does not
	// read.
	ErrProfileMismatch = errors.New("the workspace volume belongs to another coding profile")
	// ErrVolumeInUse: a Pod of another instance, or of anything else, mounts the
	// workspace volume and nothing is deleting it.
	ErrVolumeInUse = errors.New("another Pod uses the workspace volume")
)
