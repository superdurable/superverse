// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// dataPlaneTimeout bounds one data-plane check: the dial and DescribeNamespace.
const dataPlaneTimeout = 10 * time.Second

// EnsureTemporal converges a project's Temporal namespace and the credential
// that reaches it, on the project's accepted storage: in Local Kind its own
// Temporal on its own volume, hosted its Temporal Cloud namespace, service
// account, API key and the temporal-credential Secret that holds the key. It
// ends with one data-plane check: an answer other than Ready is ErrPending, so
// an API key that the data plane does not accept yet keeps the owning Flow
// waiting. A credential only an operator can repair is ErrTemporalCredential.
// It returns the references it observed even with an error, and a reference
// the call does not reach stays the accepted one.
func (k *Infrastructure) EnsureTemporal(ctx context.Context, project string, storage infra.StorageRefs, accepted infra.ExecutionRefs) (infra.ExecutionRefs, error) {
	refs := accepted
	refs.TemporalNamespace = k.Backend.TemporalNamespace(k, project)
	if accepted.TemporalNamespace != "" && accepted.TemporalNamespace != refs.TemporalNamespace {
		return accepted, errors.New("project Temporal namespace changed")
	}
	if err := k.VerifyProjectStorage(ctx, project, storage); err != nil {
		return refs, err
	}
	access, credential, err := k.Backend.EnsureTemporal(ctx, k, project, storage, refs.TemporalNamespace, accepted.TemporalCredential)
	if credential.UID != "" {
		refs.TemporalCredential = credential
	}
	if err != nil {
		return refs, err
	}
	answer, err := k.describeTemporalNamespace(ctx, project, storage, credential, access)
	if err == nil && answer != infra.DataPlaneReady {
		err = fmt.Errorf("%w: the Temporal data plane is %s", infra.ErrPending, answer)
	}
	return refs, err
}

// CheckTemporalDataPlane reads a project's accepted Temporal access without
// changing anything and asks its data plane once, within 10 seconds, to
// describe the namespace. Ready proves that a project Dex with this access
// reaches its namespace; Unauthorized and Unreachable are answers the caller
// decides on. Any other outcome is an error that carries only its gRPC code.
func (k *Infrastructure) CheckTemporalDataPlane(ctx context.Context, project string, storage infra.StorageRefs, accepted infra.ExecutionRefs) (infra.DataPlaneAnswer, error) {
	access, err := k.temporalAccess(ctx, project, storage, accepted)
	if err != nil {
		return "", err
	}
	return k.describeTemporalNamespace(ctx, project, storage, accepted.TemporalCredential, access)
}

// temporalAccess reads a project's accepted Temporal access without changing
// anything; the namespace and its credential must still be the accepted ones.
func (k *Infrastructure) temporalAccess(ctx context.Context, project string, storage infra.StorageRefs, accepted infra.ExecutionRefs) (infra.TemporalAccess, error) {
	if accepted.TemporalNamespace != k.Backend.TemporalNamespace(k, project) || accepted.TemporalCredential.UID == "" {
		return infra.TemporalAccess{}, errors.New("project Temporal resource identity is incomplete")
	}
	return k.Backend.ReadTemporalAccess(ctx, k, project, storage, accepted.TemporalNamespace, accepted.TemporalCredential)
}

// describeTemporalNamespace asks a project's Temporal data plane once to
// describe the namespace with the project's credential, over the connection
// the Backend dials.
func (k *Infrastructure) describeTemporalNamespace(ctx context.Context, project string, storage infra.StorageRefs, credential infra.ResourceRef, access infra.TemporalAccess) (answer infra.DataPlaneAnswer, err error) {
	ctx, cancel := context.WithTimeout(ctx, dataPlaneTimeout)
	defer cancel()
	ctx, connection, closeConnection, err := k.Backend.DialTemporal(ctx, k, project, storage, credential, access)
	if err != nil {
		return "", fmt.Errorf("dial the Temporal data plane: %w", err)
	}
	defer func() { err = errors.Join(err, closeConnection()) }()
	return infra.DescribeTemporalNamespace(ctx, connection, access.Namespace)
}

// EnsureExecutionAccess converges what a project's Live Dex and applications
// need to run on its accepted storage: their service accounts, the traffic
// policies of Dex and the applications, and the identity the Dex assumes to
// reach the storage, if the Backend needs one. Where the Backend gives every
// application generation a service account of its own, its grant creates it.
func (k *Infrastructure) EnsureExecutionAccess(ctx context.Context, project string, storage infra.StorageRefs) error {
	if err := k.VerifyProjectStorage(ctx, project, storage); err != nil {
		return err
	}
	accounts := []Component{ComponentDex}
	if k.Backend.SharedApplicationServiceAccount() {
		accounts = append(accounts, ComponentApplication)
	}
	for _, component := range accounts {
		if err := k.EnsureServiceAccount(ctx, project, component); err != nil {
			return err
		}
	}
	for _, component := range []Component{ComponentDex, ComponentApplication} {
		if err := k.EnsureComponentTraffic(ctx, project, component); err != nil {
			return err
		}
	}
	return k.Backend.EnsureDexIdentity(ctx, k, project, storage)
}

// StartProjectDex converges a project's Live Dex on its accepted storage and
// Temporal access, which it reads without changing anything: the immutable
// server configuration Secret of the accepted credential's generation, the
// Deployment and Service, updated in place, and their readiness. A rotated
// credential replaces the Pod; once it is ready, the superseded generation's
// configuration Secret is removed. It returns the references with the Dex
// Service, also while the Deployment becomes ready.
func (k *Infrastructure) StartProjectDex(ctx context.Context, project string, storage infra.StorageRefs, accepted infra.ExecutionRefs) (infra.ExecutionRefs, error) {
	if err := k.VerifyProjectStorage(ctx, project, storage); err != nil {
		return accepted, err
	}
	access, err := k.temporalAccess(ctx, project, storage, accepted)
	if err != nil {
		return accepted, err
	}
	dex, err := k.Backend.DexSpec(ctx, k, project, storage, access)
	if err != nil {
		return accepted, err
	}
	refs := accepted
	generation := accepted.TemporalCredential.Generation
	service, err := k.ensureProjectDex(ctx, project, accepted.DexService.UID, generation, dex)
	if service.UID != "" {
		refs.DexService = service
	}
	if err == nil && generation > 0 {
		err = k.RemoveDexSecret(ctx, project, dexServerConfigName(generation-1))
	}
	return refs, err
}

// ProjectDexCredentialGeneration reports the Temporal credential generation
// whose configuration the project Dex Deployment runs and whether it is ready
// on it; found is false without a Dex Deployment, so no Pod holds a key.
func (k *Infrastructure) ProjectDexCredentialGeneration(ctx context.Context, project string) (generation uint64, ready, found bool, err error) {
	deployment, ready, err := k.DeploymentReady(ctx, project, ComponentDex.String(), ComponentDex, "")
	if err != nil || deployment == nil {
		return 0, false, false, err
	}
	annotations := kubeapi.Child(kubeapi.Child(kubeapi.Child(kubeapi.Child(deployment, "spec"), "template"), "metadata"), "annotations")
	if value := kubeapi.String(annotations, credentialGenerationAnnotation); value != "" {
		if generation, err = strconv.ParseUint(value, 10, 64); err != nil {
			return 0, false, true, errors.New("project Dex credential generation is invalid")
		}
	}
	return generation, ready, true, nil
}

// RemoveDexSecret deletes the project Dex's Secret name when it exists.
func (k *Infrastructure) RemoveDexSecret(ctx context.Context, project, name string) error {
	secrets := k.NamespacePath(project) + "/secrets"
	found, err := k.GetOwned(ctx, secrets+"/"+name, project, ComponentDex)
	if err != nil || found == nil {
		return err
	}
	return k.Remove(ctx, secrets, ResourceReference(found), project, ComponentDex)
}

// ProjectDexSpec is what a project's storage and Temporal access decide about
// its Live Dex.
type ProjectDexSpec struct {
	// Temporal is the interpreter's Temporal connection; its hostPort is also
	// the container's TEMPORAL_HOST_PORT.
	Temporal kubeapi.Object
	// Storage holds the provider fields of the a0 blob storage.
	Storage kubeapi.Object
	// HistoryRetentionDays bounds the blob history; zero omits it.
	HistoryRetentionDays int32
	// Env is the container environment after the shared entries.
	Env []any
	// NodeSelector and TmpSizeLimit constrain the hosted Pod.
	NodeSelector kubeapi.Object
	TmpSizeLimit string
}

// projectDexConfig is the Dex server configuration of a project Dex.
func projectDexConfig(dex ProjectDexSpec) kubeapi.Object {
	return infra.ProjectDexConfig(dex.Temporal, dex.Storage, dex.HistoryRetentionDays)
}

// dexServerConfig names the immutable Secret that holds the project Dex
// server configuration of credential generation 0; a later generation's
// carries its number, so a rotated credential never rewrites a Secret.
const dexServerConfig = "dex-server-config"

// credentialGenerationAnnotation marks the Dex Pod template of a credential
// generation above 0; without it the generation is 0.
const credentialGenerationAnnotation = "superdurable.io/temporal-credential-generation"

func dexServerConfigName(generation uint64) string {
	if generation == 0 {
		return dexServerConfig
	}
	return dexServerConfig + "-" + strconv.FormatUint(generation, 10)
}

// ensureProjectDex converges the project Dex: its immutable configuration
// Secret, the Deployment and Service updated in place, and their readiness. It
// returns the Service reference, also while the Deployment becomes ready.
func (k *Infrastructure) ensureProjectDex(ctx context.Context, project, acceptedServiceUID string, generation uint64, dex ProjectDexSpec) (infra.ResourceRef, error) {
	configName := dexServerConfigName(generation)
	config := kubeapi.ImmutableSecret(k.Metadata(project, configName, ComponentDex), kubeapi.Object{"config.yaml": base64.StdEncoding.EncodeToString(comm.Must(json.Marshal(projectDexConfig(dex))))})
	MarkSpec(config)
	if err := k.replaceChangedSecret(ctx, config, project, ComponentDex); err != nil {
		return infra.ResourceRef{}, err
	}
	if _, err := k.Ensure(ctx, config, project, ComponentDex); err != nil {
		return infra.ResourceRef{}, err
	}
	env := append([]any{kubeapi.EnvVar("CONFIG_TEMPLATE_PATH", "/etc/dex/config.yaml"), kubeapi.EnvVar("TEMPORAL_HOST_PORT", kubeapi.String(dex.Temporal, "hostPort"))}, dex.Env...)
	configMount := kubeapi.VolumeMount("config", "/etc/dex")
	configMount["readOnly"] = true
	mounts := []any{configMount, kubeapi.VolumeMount("tmp", "/tmp")}
	volumes := []any{kubeapi.SecretVolume("config", configName), kubeapi.EmptyDirVolume("tmp", dex.TmpSizeLimit)}
	deployment := k.InfrastructureDeployment(project, ComponentDex, k.DexImage, []string{"--services", "api,interpreter"}, infra.DexPort, env, mounts, volumes)
	template := kubeapi.Child(kubeapi.Child(deployment, "spec"), "template")
	annotations := kubeapi.Object{specDigestAnnotation: specDigest(config)}
	if generation > 0 {
		annotations[credentialGenerationAnnotation] = strconv.FormatUint(generation, 10)
	}
	kubeapi.Child(template, "metadata")["annotations"] = annotations
	if dex.NodeSelector != nil {
		kubeapi.Child(template, "spec")["nodeSelector"] = dex.NodeSelector
	}
	if _, err := k.ensureInPlace(ctx, deployment, project, ComponentDex, ""); err != nil {
		return infra.ResourceRef{}, err
	}
	name := ComponentDex.String()
	service, err := k.ensureInPlace(ctx, k.Service(project, name, ComponentDex, k.OwnerLabels(project, ComponentDex), kubeapi.ServicePort("grpc", infra.DexPort, infra.DexPort)), project, ComponentDex, acceptedServiceUID)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	_, ready, err := k.DeploymentReady(ctx, project, name, ComponentDex, "")
	if err == nil && !ready {
		err = fmt.Errorf("%w: the project Dex Deployment is not ready", infra.ErrPending)
	}
	return ResourceReference(service), err
}

// VerifyProjectDex checks the project's storage references, that its Dex
// Service is the accepted one, and that the Dex Deployment is ready.
func (k *Infrastructure) VerifyProjectDex(ctx context.Context, project string, refs infra.ScopeRefs) error {
	if err := k.VerifyProjectStorage(ctx, project, refs.Storage); err != nil {
		return err
	}
	dex := refs.Execution.DexService
	name := ComponentDex.String()
	if dex.Boundary != refs.Storage.Boundary.Name || dex.Name != name || dex.UID == "" {
		return errors.New("project Dex resource identity is incomplete")
	}
	service, err := k.GetOwned(ctx, k.NamespacePath(project)+"/services/"+name, project, ComponentDex)
	if err != nil {
		return err
	}
	if service == nil {
		return errors.New("project Dex service is absent")
	}
	if kubeapi.UID(service) != dex.UID {
		return errors.New("project Dex service UID changed")
	}
	_, ready, err := k.DeploymentReady(ctx, project, name, ComponentDex, "")
	if err == nil && !ready {
		err = fmt.Errorf("%w: the project Dex Deployment is not ready", infra.ErrPending)
	}
	return err
}
