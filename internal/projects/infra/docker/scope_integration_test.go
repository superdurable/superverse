// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package docker_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/docker"
	"github.com/superdurable/superversev2/internal/projects/infra/docker/dockertest"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Requires the local Docker Engine and the pinned project images, which it
// pulls when absent. It provisions one project's Live scope on Docker as the
// server does: the network with its MinIO, pending in the call that starts it
// with every reference its owner records, Temporal start-dev and Dex, a Live
// workload grant that reads exactly its configuration, its revocation fence,
// and a refusal of every replaced identity. The project's deletion then leaves
// no container, network, volume or credential file.
func TestDockerProjectScope(t *testing.T) {
	k := dockertest.InfrastructureForTest(t)
	project := flowid.RandomID(4)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, k, project) })

	storage, err := k.EnsureStorage(ctx, project, infra.StorageRefs{})
	if !errors.Is(err, infra.ErrPending) || !k.Complete(storage) {
		t.Fatalf("the call that starts the storage returned %v with references %+v", err, storage)
	}
	dockertest.EventuallyForTest(ctx, t, "the project storage", func() (err error) {
		storage, err = k.EnsureStorage(ctx, project, storage)
		return err
	})
	if !k.Complete(storage) || storage.Boundary.Name != k.ProjectNetwork(project) {
		t.Fatalf("incomplete storage references %+v", storage)
	}
	if _, err := k.EnsureStorage(ctx, project, infra.StorageRefs{Boundary: infra.ResourceRef{Name: storage.Boundary.Name, UID: "replaced-network"}}); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a replaced project network was adopted: %v", err)
	}
	replacedStorage := storage
	replacedStorage.Storage.UID = "replaced-storage"
	if _, err := k.EnsureStorage(ctx, project, replacedStorage); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a replaced storage service was adopted: %v", err)
	}

	workload := liveWorkload(ctx, t, k, project, storage)
	environment, err := k.GrantEnvironment(ctx, workload)
	if err != nil {
		t.Fatalf("grant the Live workload: %v", err)
	}
	endpoint, err := k.StorageEndpointForTest(ctx, project, storage)
	if err != nil || environment["DEX_PROJECT_STORAGE_ENDPOINT"] != endpoint || environment["DEX_PROJECT_CONFIG_VERSION"] != workload.Configuration.Version {
		t.Fatalf("unexpected grant environment for %s: %v", environment["DEX_PROJECT_ID"], err)
	}
	grantClient := s3Client(ctx, t, k, project, environment[infra.AccessKeyIDVariable], environment[infra.SecretAccessKeyVariable])
	if _, err = grantClient.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(storage.StorageBucket), Key: aws.String(workload.Configuration.Key), VersionId: aws.String(workload.Configuration.Version)}); err != nil {
		t.Fatalf("the grant cannot read its configuration: %v", err)
	}
	other := infra.ConfigurationKeyForTest(project, infra.Scope{Kind: infra.ScopeLive}, "configuration/other")
	if _, err = grantClient.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(storage.StorageBucket), Key: aws.String(other)}); !platform.HasAWSErrorCode(err, "AccessDenied") {
		t.Fatalf("the grant reads outside its configuration: %v", err)
	}
	// A Trigger application keeps the events it acknowledged in an inbox of its scope, which the Connector SDK
	// writes under this grant; the Preview scope's inbox stays out of reach.
	inboxStore, err := connectorconfig.NewS3ObjectStore(ctx, &connectorconfig.S3StoreConfig{Client: grantClient, Bucket: storage.StorageBucket, Prefix: infra.ConfigurationPrefix, AllowUnencrypted: true})
	if err != nil {
		t.Fatal(err)
	}
	inbox := func(kind infra.ScopeKind) *connectorconfig.TriggerInbox {
		inbox, inboxErr := connectorconfig.NewTriggerInbox(inboxStore, connectorconfig.Scope{ProjectID: project, Kind: kind.String()},
			connectorconfig.TriggerInboxKey{ConnectorID: "gmail", ConnectionName: "mailbox", TriggerName: "messageReceived", BindingName: "inbox"})
		if inboxErr != nil {
			t.Fatal(inboxErr)
		}
		return inbox
	}
	event := connectorconfig.PendingTriggerEvent{ID: "event-1", Event: json.RawMessage(`{"subject":"hello"}`)}
	if err = inbox(infra.ScopeLive).Add(ctx, event); err != nil {
		t.Fatalf("the grant cannot add an event to its trigger inbox: %v", err)
	}
	if pending, readErr := inbox(infra.ScopeLive).Pending(ctx); readErr != nil || len(pending) != 1 || pending[0].ID != event.ID {
		t.Fatalf("the grant read %d pending events (%v), not the one it added", len(pending), readErr)
	}
	if err = inbox(infra.ScopeLive).Remove(ctx, event.ID); err != nil {
		t.Fatalf("the grant cannot remove a consumed event from its trigger inbox: %v", err)
	}
	if err = inbox(infra.ScopePreview).Add(ctx, event); err == nil {
		t.Fatal("the Live grant added an event to the Preview scope's trigger inbox")
	}
	if err = k.RevokeWorkload(ctx, workload); err != nil {
		t.Fatalf("revoke the Live workload: %v", err)
	}
	if err = k.IssueWorkload(ctx, workload); err == nil {
		t.Fatal("a revoked grant generation was issued again")
	}

	var execution infra.ExecutionRefs
	dockertest.EventuallyForTest(ctx, t, "the project Temporal", func() (err error) {
		execution, err = k.EnsureTemporal(ctx, project, storage, execution)
		return err
	})
	if answer, err := k.CheckTemporalDataPlane(ctx, project, storage, execution); err != nil || answer != infra.DataPlaneReady {
		t.Fatalf("the Temporal data plane answered %s: %v", answer, err)
	}
	if err = k.EnsureExecutionAccess(ctx, project, storage); err != nil {
		t.Fatal(err)
	}
	dockertest.EventuallyForTest(ctx, t, "the project Dex", func() (err error) {
		execution, err = k.StartProjectDex(ctx, project, storage, execution)
		return err
	})
	if !execution.Complete() {
		t.Fatalf("incomplete execution references %+v", execution)
	}
	dockertest.EventuallyForTest(ctx, t, "the project Dex health", func() error {
		connection, closeConnection, err := k.DialProjectDex(ctx, project, infra.ScopeRefs{Storage: storage, Execution: execution})
		if err != nil {
			return err
		}
		defer func() { _ = closeConnection() }()
		if _, err = dexpb.NewFlowServiceClient(connection).HealthCheck(ctx, &emptypb.Empty{}); err != nil {
			return errors.Join(infra.ErrPending, err)
		}
		return nil
	})
	replacedDex := execution
	replacedDex.DexService.UID = "replaced-dex"
	if _, err = k.StartProjectDex(ctx, project, storage, replacedDex); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a replaced project Dex was adopted: %v", err)
	}
	if _, _, err = k.DialProjectDex(ctx, project, infra.ScopeRefs{Storage: storage, Execution: replacedDex}); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a replaced project Dex was dialed: %v", err)
	}
	if redacted := k.Redact(project, "dial "+k.ComponentNameForTest(project, infra.ComponentDex)+" for "+workload.Name()); redacted != "dial <project-namespace>-dex for <workload>" {
		t.Fatalf("unexpected redaction %q", redacted)
	}
	dockertest.DestroyProjectForTest(t, k, project)
}

// Requires the local Docker Engine and builds the configuration probe for
// Linux. A Live workload grant's environment loads the project configuration
// through the Connector SDK, as the application of a Live deployment or a
// Preview does when it starts: in a container on the project network, against
// the project's MinIO, with an ordinary value and an application secret. The
// storage endpoint is the storage container's fixed private address, which the
// SDK accepts as a local endpoint, and it stays valid when the storage
// container is replaced, so the same environment loads again.
func TestDockerGrantLoadsProjectConfiguration(t *testing.T) {
	k := dockertest.InfrastructureForTest(t)
	project := flowid.RandomID(4)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, k, project) })
	var storage infra.StorageRefs
	dockertest.EventuallyForTest(ctx, t, "the project storage", func() (err error) {
		storage, err = k.EnsureStorage(ctx, project, storage)
		return err
	})

	live := infra.Scope{Kind: infra.ScopeLive}
	scope := infra.ScopeRef{ProjectID: project, Scope: live, Storage: storage}
	objects, err := k.ConfigurationObjects(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	store, err := connectorconfig.NewConfigurationStore(&connectorconfig.ConfigurationStoreConfig{Objects: objects, Scope: connectorconfig.Scope{ProjectID: project, Kind: live.Kind.String()}})
	if err != nil {
		t.Fatal(err)
	}
	greeting, token := "hello from "+project, flowid.RandomID(32)
	secret, err := store.CreateApplicationSecret(ctx, "PROBE_TOKEN", token)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.WriteConfiguration(ctx, 0, connectorconfig.Configuration{
		Connections: []connectorconfig.ConnectionConfiguration{}, TriggerBindings: []connectorconfig.TriggerConfiguration{},
		OperationConfigurations: []connectorconfig.OperationConfiguration{},
		Environment:             map[string]connectorconfig.EnvironmentValue{"PROBE_GREETING": {Value: &greeting}, "PROBE_TOKEN": {SecretRef: &secret}},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.FreezeConfiguration(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	head, err := infra.ScopedObjectRef(scope, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	deployment := "deploy-" + flowid.RandomID(6)
	workload := infra.Workload{ProjectID: project, WorkloadID: deployment, Scope: live, Generation: 1, ConfigurationRevision: 1, Storage: storage,
		Configuration: head, GrantID: infra.WorkloadName(deployment, 1)}
	environment, err := k.GrantEnvironment(ctx, workload)
	if err != nil {
		t.Fatalf("grant the Live workload: %v", err)
	}
	endpoint, err := url.Parse(environment["DEX_PROJECT_STORAGE_ENDPOINT"])
	if err != nil {
		t.Fatal(err)
	}
	if address, err := netip.ParseAddr(endpoint.Hostname()); err != nil || !address.Is4() || !address.IsPrivate() || !dockertest.NetworkPrefixForTest.Contains(address) {
		t.Fatalf("the storage endpoint %s is not a private address of the lane's networks", endpoint)
	}
	want := fmt.Sprintf("revision 1\nPROBE_GREETING %x\nPROBE_TOKEN %x\n", sha256.Sum256([]byte(greeting)), sha256.Sum256([]byte(token)))
	if output := dockertest.LoadConfigurationForTest(ctx, t, k, project, storage.Boundary.Name, environment, "PROBE_GREETING", "PROBE_TOKEN"); output != want {
		t.Fatalf("the probe loaded %q, not the configuration's values", output)
	}

	// The storage container goes, as after an engine restart that removed it,
	// and comes back under its accepted service at the same address.
	name, labels := k.ComponentNameForTest(project, infra.ComponentStorage), k.OwnerLabels(project, infra.ComponentStorage)
	dockertest.EventuallyForTest(ctx, t, "the storage container's removal", func() error {
		gone, err := k.RemoveContainer(ctx, name, labels, time.Second)
		if err == nil && !gone {
			err = infra.ErrPending
		}
		return err
	})
	dockertest.EventuallyForTest(ctx, t, "the project storage again", func() (err error) {
		storage, err = k.EnsureStorage(ctx, project, storage)
		return err
	})
	if again, err := k.StorageEndpointForTest(ctx, project, storage); err != nil || again != environment["DEX_PROJECT_STORAGE_ENDPOINT"] {
		t.Fatalf("the replaced storage container moved to %s: %v", again, err)
	}
	if output := dockertest.LoadConfigurationForTest(ctx, t, k, project, storage.Boundary.Name, environment, "PROBE_GREETING", "PROBE_TOKEN"); output != want {
		t.Fatalf("after the storage replacement the probe loaded %q", output)
	}
	dockertest.DestroyProjectForTest(t, k, project)
}

// liveWorkload stores a configuration head with one application secret in the
// project's storage and returns the first Live generation's workload of it.
func liveWorkload(ctx context.Context, t *testing.T, k *docker.Infrastructure, project string, storage infra.StorageRefs) infra.Workload {
	t.Helper()
	objects, err := k.S3(ctx, project, storage.Boundary.UID)
	if err != nil {
		t.Fatal(err)
	}
	live := infra.Scope{Kind: infra.ScopeLive}
	secret := infra.ScopeKeyPrefixForTest(project, live) + "app-secrets/TOKEN/v1"
	if _, err = objects.Put(ctx, infra.ConfigurationPrefix+secret, []byte(`{"value":"token"}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err = objects.Put(ctx, infra.ConfigurationKeyForTest(project, live, "configuration/other"), []byte(`{}`), true); err != nil {
		t.Fatal(err)
	}
	head, err := objects.Put(ctx, infra.ConfigurationKeyForTest(project, live, "configuration/head"), []byte(`{"environment":{"TOKEN":{"secretRef":{"key":"`+secret+`"}}}}`), false)
	if err != nil {
		t.Fatal(err)
	}
	deployment := "deploy-" + flowid.RandomID(6)
	return infra.Workload{ProjectID: project, WorkloadID: deployment, Scope: live, Generation: 1, ConfigurationRevision: 1, Storage: storage,
		Configuration: head, GrantID: infra.WorkloadName(deployment, 1)}
}

// s3Client reaches the project's MinIO through the port it publishes now,
// with a key pair.
func s3Client(ctx context.Context, t *testing.T, k *docker.Infrastructure, project, access, secret string) *s3.Client {
	t.Helper()
	address, err := k.PublishedForTest(ctx, k.ComponentNameForTest(project, infra.ComponentStorage), k.OwnerLabels(project, infra.ComponentStorage), infra.StoragePort)
	if err != nil {
		t.Fatal(err)
	}
	return infra.ProjectS3("http://"+address, access, secret, nil)
}

// Requires the local Docker Engine. A route is published, replaced by a newer
// generation, refused to a stale generation or another holder, and withdrawn
// only by its holder; the gateway loads it from the provider and forwards the
// route's host to the port its container publishes now.
func TestDockerRoutes(t *testing.T) {
	k := dockertest.InfrastructureForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	project := flowid.RandomID(4)
	first, second := dockertest.WebContainerForTest(ctx, t, k, project), dockertest.WebContainerForTest(ctx, t, k, project)
	host := k.PublicRoutes().Host(infra.ScopePreview, project)
	owner := docker.RouteOwner{Scope: infra.ScopePreview.String(), ProjectID: project, WorkspaceRequestID: "request-1"}
	newer := func(current docker.Route) bool { return current.Generation < 2 }
	routes := k.Routes()
	if err := routes.Publish(ctx, docker.Route{Host: host, Owner: owner, Generation: 1, Container: first, Port: dockertest.WebPortForTest}, newer); err != nil {
		t.Fatal(err)
	}
	second2 := owner
	second2.WorkspaceRequestID = "request-2"
	route := docker.Route{Host: host, Owner: second2, Generation: 2, Container: second, Port: dockertest.WebPortForTest}
	if err := routes.Publish(ctx, route, newer); err != nil {
		t.Fatalf("a newer generation did not take the route: %v", err)
	}
	stale := docker.Route{Host: host, Owner: owner, Generation: 1, Container: first, Port: dockertest.WebPortForTest}
	if err := routes.Publish(ctx, stale, func(current docker.Route) bool { return current.Generation < stale.Generation }); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a stale generation replaced the route: %v", err)
	}
	dockertest.EventuallyForTest(ctx, t, "the gateway route", func() error {
		loaded, observed, err := routes.Loaded(ctx, route)
		if err != nil || !loaded {
			return errors.Join(infra.ErrPending, errors.New(observed), err)
		}
		return nil
	})
	// A browser names the public port in the Host header.
	for _, header := range []string{host, net.JoinHostPort(host, strconv.Itoa(k.PublicRoutes().LocalPort))} {
		if status, unanswered := routes.Probe(ctx, header, dockertest.WebPathForTest); unanswered != "" || status != 200 {
			t.Fatalf("the gateway answered %d for %s: %s", status, header, unanswered)
		}
	}
	if remaining, err := routes.Withdraw(ctx, host, func(current docker.Route) bool { return current.Owner == owner }); err != nil || remaining == nil || *remaining != route {
		t.Fatalf("another holder withdrew the route: %v", err)
	}
	if remaining, err := routes.Withdraw(ctx, host, func(current docker.Route) bool { return current.Owner == second2 }); err != nil || remaining != nil {
		t.Fatalf("the holder could not withdraw its route: %v", err)
	}
	dockertest.EventuallyForTest(ctx, t, "the route's removal", func() error {
		if status, unanswered := routes.Probe(ctx, host, dockertest.WebPathForTest); unanswered != "" || status != 404 {
			return fmt.Errorf("%w: the gateway answered %d: %s", infra.ErrPending, status, unanswered)
		}
		return nil
	})
	if _, exists, err := routes.Read(host); err != nil || exists {
		t.Fatalf("the withdrawn route file remains: %v", err)
	}
}

// Requires the local Docker Engine. The root key pair of a project's MinIO is
// a private file created once: the next provisioning keeps it, and a file of
// another owner is refused instead of used.
func TestDockerCredentialFiles(t *testing.T) {
	k := dockertest.InfrastructureForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	project := flowid.RandomID(4)
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, k, project) })
	var storage infra.StorageRefs
	dockertest.EventuallyForTest(ctx, t, "the project storage", func() (err error) {
		storage, err = k.EnsureStorage(ctx, project, storage)
		return err
	})
	files := dockertest.CredentialFilesForTest(t, k, project)
	if len(files) != 1 {
		t.Fatalf("the project has %d credential files, not its root key pair alone", len(files))
	}
	info, err := os.Stat(files[0])
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the root key pair is not a private file: %v", err)
	}
	created, err := k.CredentialValues(project)
	if err != nil || len(created) != 2 {
		t.Fatalf("the project has %d credential values: %v", len(created), err)
	}
	if storage, err = k.EnsureStorage(ctx, project, storage); err != nil {
		t.Fatal(err)
	}
	if kept, err := k.CredentialValues(project); err != nil || kept[0] != created[0] || kept[1] != created[1] {
		t.Fatalf("the root key pair changed: %v", err)
	}
	original, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	foreign := strings.Replace(string(original), storage.Boundary.UID, "another-network", 1)
	if err = os.WriteFile(files[0], []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = k.EnsureStorage(ctx, project, storage); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatalf("a root key pair of another owner was used: %v", err)
	}
	if err = os.WriteFile(files[0], original, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Requires the local Docker Engine. Two Native instances may hold a project
// of the same ID: deleting it in one instance removes that instance's
// containers, networks, volumes and credential files and leaves the other
// instance's scope untouched.
func TestDockerProjectDeletionKeepsOtherInstance(t *testing.T) {
	deleted, kept := dockertest.InfrastructureForTest(t), dockertest.InstanceInfrastructureForTest(t, "isolated")
	project := flowid.RandomID(4)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, kept, project) })
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, deleted, project) })
	for _, k := range []*docker.Infrastructure{deleted, kept} {
		var storage infra.StorageRefs
		dockertest.EventuallyForTest(ctx, t, "the project storage", func() (err error) {
			storage, err = k.EnsureStorage(ctx, project, storage)
			return err
		})
	}
	if deleted.ProjectNetwork(project) == kept.ProjectNetwork(project) {
		t.Fatalf("both instances name the project network %s", kept.ProjectNetwork(project))
	}
	dockertest.DestroyProjectForTest(t, deleted, project)
	labels := kept.OwnerLabels(project, infra.ComponentStorage)
	delete(labels, deploymentnames.ComponentLabel)
	scope := dockerapi.Labels(labels)
	containers, err := kept.API().ListContainers(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := kept.API().ListVolumes(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	network, err := kept.API().InspectNetwork(ctx, kept.ProjectNetwork(project))
	if err != nil || len(containers) == 0 || len(volumes) == 0 || len(dockertest.CredentialFilesForTest(t, kept, project)) == 0 {
		t.Fatalf("deleting project %s in the default instance removed the other instance's scope: %d containers, %d volumes, network %v (%v)",
			project, len(containers), len(volumes), network != nil, err)
	}
	t.Logf("project %s deleted in the default instance; the isolated instance kept its network, %d containers and %d volumes", project, len(containers), len(volumes))
}

// Requires the local Docker Engine. A project's deletion lists its Live scope
// by labels: the default instance's deletion also finds and removes the
// objects it created before the instance label existed, and a named
// instance's deletion leaves them in place.
func TestDockerProjectDeletionRemovesUnlabeledObjects(t *testing.T) {
	defaults, named := dockertest.InfrastructureForTest(t), dockertest.InstanceInfrastructureForTest(t, "isolated")
	project := flowid.RandomID(4)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	t.Cleanup(func() { dockertest.DestroyProjectForTest(t, defaults, project) })
	legacy := defaults.OwnerLabels(project, infra.ComponentStorage)
	delete(legacy, docker.NativeInstanceLabel)
	name := defaults.ProjectNetwork(project) + "-legacy"
	if _, err := defaults.API().CreateVolume(ctx, name, legacy); err != nil {
		t.Fatal(err)
	}
	if docker.VerifyLabels(legacy, defaults.OwnerLabels(project, infra.ComponentStorage)) != nil {
		t.Fatal("the default instance refused its object without an instance label")
	}
	if docker.VerifyLabels(legacy, named.OwnerLabels(project, infra.ComponentStorage)) == nil {
		t.Fatal("a named instance accepted an object without its instance label")
	}
	dockertest.DestroyProjectForTest(t, named, project)
	if volume, err := defaults.API().InspectVolume(ctx, name); err != nil || volume == nil {
		t.Fatalf("deleting the project in a named instance removed the default instance's unlabeled volume: %v", err)
	}
	dockertest.DestroyProjectForTest(t, defaults, project)
	if volume, err := defaults.API().InspectVolume(ctx, name); err != nil || volume != nil {
		t.Fatalf("the default instance's project deletion left its unlabeled volume %s: %v", name, err)
	}
	t.Logf("project %s: the named instance kept and the default instance deleted the unlabeled volume %s", project, name)
}
