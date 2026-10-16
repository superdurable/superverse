// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/local"

	"github.com/google/uuid"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi/kubeapitest"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/connect"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// A project's storage, its Temporal namespace, its Dex access and its Dex are
// provisioned one after another against the real Local Kind API, MinIO and
// Temporal, each on the references the previous accepted: configuration works
// on storage alone, the Temporal data plane answers with the project's
// namespace, and a Live grant is issued before any Dex exists. This actual
// provider integration complements, but does not replace, complete Studio
// business acceptance.
func TestKindLiveInfrastructureAndFDG(t *testing.T) {
	fixture := newKindProjectFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	live := fixture.prepareStorage(ctx)
	liveConfiguration := fixture.verifyConfigurationBeforeRelease(ctx, live)
	// The Project's Preview keeps its configuration in the Live storage,
	// below its own prefix.
	preview := fixture.previewOf(live)
	previewConfiguration := fixture.verifyConfigurationBeforeRelease(ctx, preview)
	if liveConfiguration.Key == previewConfiguration.Key || !strings.HasPrefix(previewConfiguration.Key, infra.PreviewKeysOf(fixture.project).ConfigurationPrefix) {
		t.Fatal("Live and Preview configuration keys are not isolated")
	}
	if _, err := fixture.k.ReadConfiguration(ctx, live.ref(fixture.project), previewConfiguration); err == nil {
		t.Fatal("Live accepted a Preview configuration reference")
	}
	if _, err := fixture.k.ReadConfiguration(ctx, preview.ref(fixture.project), liveConfiguration); err == nil {
		t.Fatal("Preview accepted a Live configuration reference")
	}

	fixture.prepareTemporal(ctx, live)

	// Release and generation identities are introduced only after both scopes have validated projectconfig.
	const deploymentID = "2d0f939d-5771-4cfd-90ba-159722cb6327"
	// A Live grant is a storage grant: it is issued before any Dex exists.
	grant := infra.Workload{ProjectID: fixture.project, WorkloadID: deploymentID, Scope: live.scope, Generation: 1, ConfigurationRevision: 1, Storage: live.storage, Configuration: liveConfiguration, GrantID: infra.WorkloadName(deploymentID, 1)}
	fixture.awaitReady(ctx, "Live grant", func() error { return fixture.k.IssueWorkload(ctx, grant) })
	fixture.assertDeployments(ctx, live, "storage", "temporal")
	fixture.prepareDex(ctx, live)
	fixture.verifyProjectDexConnection(ctx, live)
	fixture.rollDexCredential(ctx, live)

	s := deployments.Snapshot{ProjectID: fixture.project, DeploymentID: deploymentID, ReleaseID: strings.Repeat("a", 40) + "-bp1", Lease: projects.DeploymentLease{Generation: 1}, Inputs: deployments.Inputs{ApplicationPort: 8080, HealthPath: "/healthz", BundleDigest: "sha256:" + strings.Repeat("b", 64)}, Resources: deployments.DeploymentResources{Storage: live.storage, Execution: live.execution}}
	generation, err := fixture.k.EnsureGenerationService(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if generation.Name != "app-"+s.DeploymentID+"-1" || generation.UID == "" {
		t.Fatal("Live Service did not preserve the accepted deployment identity")
	}
	again, err := fixture.k.EnsureGenerationService(ctx, s)
	if err != nil || again != generation {
		t.Fatalf("reconciling the generation's Service again replaced it: %v", err)
	}
	s.Resources.Service = generation
	definition := kindGeneratedDefinition(t)
	if err = deployments.PublishFlowDefinitionsForTest(ctx, fixture.k.Infrastructure, s, []appmanifest.FlowDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	publication, err := deployments.ReadPublication(ctx, fixture.k.Infrastructure, s)
	if err != nil {
		t.Fatal(err)
	}
	client, err := fixture.k.S3(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(publication.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := client.Put(ctx, "flow-definitions/active-manifest", manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	publishedManifest, manifestVersion, err := client.Get(ctx, ref.Key, ref.Version)
	if err != nil || manifestVersion != ref.Version || !bytes.Equal(publishedManifest, manifest) || objectstore.Digest(publishedManifest) != ref.Digest {
		t.Fatalf("published FDG manifest exact version or bytes changed: %v", err)
	}
	publishedGraph, graphVersion, err := client.Get(ctx, publication.Manifest.BundlePrefix+"0000.json", "")
	if err != nil || graphVersion == "" || !bytes.Equal(publishedGraph, definition.Graph) || objectstore.Digest(publishedGraph) != definition.Digest {
		t.Fatalf("published FDG bytes or digest changed: %v", err)
	}
	exactGraph, exactVersion, err := client.Get(ctx, publication.Manifest.BundlePrefix+"0000.json", graphVersion)
	if err != nil || exactVersion != graphVersion || !bytes.Equal(exactGraph, publishedGraph) {
		t.Fatalf("published FDG exact version changed: %v", err)
	}
	catalog, err := fdg.ParseBundle([]json.RawMessage{exactGraph})
	if err != nil || len(catalog.Definitions) != 1 || catalog.Revision != publication.Manifest.BundleDigest {
		t.Fatalf("published FDG catalog did not match the exact manifest: %v", err)
	}
	if _, present := catalog.Definitions[definition.FlowType]; !present {
		t.Fatal("published FDG catalog omitted the generated Flow type")
	}
	if _, err = fixture.k.ReadConfiguration(ctx, live.ref(fixture.project), liveConfiguration); err != nil {
		t.Fatalf("publishing FDG invalidated exact configuration: %v", err)
	}
	// Removing the generation is retry safe: a second removal, as a retried
	// cleanup round runs it, finds nothing left to delete and succeeds.
	for removal := 1; removal <= 2; removal++ {
		for err = fixture.k.CleanupDeployment(ctx, s); errors.Is(err, projects.ErrCleanupPending); err = fixture.k.CleanupDeployment(ctx, s) {
			fixture.awaitConvergence(ctx, "generation removal")
		}
		if err != nil {
			t.Fatalf("generation removal %d failed: %v", removal, err)
		}
	}
	service, _, err := fixture.k.API.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+fixture.k.ProjectNamespace(s.ProjectID)+"/services/"+s.Resources.Service.Name, nil)
	if err != nil || service != nil && !kubeapi.Deleting(service) {
		t.Fatalf("generation removal left Service %s: %v", s.Resources.Service.Name, err)
	}
	t.Logf("verified native FDG after pre-Release configuration: namespace=%s temporal=%s dexUID=%s FDGVersion=%s", live.storage.Boundary.Name, live.execution.TemporalNamespace, live.execution.DexService.UID, ref.Version)

	// Destroying the scope leaves nothing of the project behind.
	fixture.destroyLive(ctx, live)
	namespace, code, err := fixture.k.API.Call(ctx, http.MethodGet, fixture.k.NamespacePath(fixture.project), nil)
	if err != nil || code != http.StatusNotFound {
		t.Fatalf("scope destruction left namespace UID %s: %v", kubeapi.UID(namespace), err)
	}
}

type kindProjectScope struct {
	scope     infra.Scope
	storage   infra.StorageRefs
	execution infra.ExecutionRefs
	host      *confighost.ConfigurationHost
	admission confighost.ConfigurationAdmission
}

// ref is the scope of project with the storage it uses.
func (state *kindProjectScope) ref(project string) infra.ScopeRef {
	return infra.ScopeRef{ProjectID: project, Scope: state.scope, Storage: state.storage}
}

// refs are the references of the project's whole Live scope.
func (state *kindProjectScope) refs() infra.ScopeRefs {
	return infra.ScopeRefs{Storage: state.storage, Execution: state.execution}
}

// verifyProjectDexConnection inspects the listeners of the project Dex Pod and
// dials the Dex through the verified connection, whose port-forward reaches
// the Service as the server in the cluster would.
func (f *kindProjectFixture) verifyProjectDexConnection(ctx context.Context, state *kindProjectScope) {
	f.test.Helper()
	f.verifyEngineOnlyListener(ctx, state)
	callContext, callCancel := context.WithTimeout(ctx, 30*time.Second)
	defer callCancel()
	connection, closeConnection, err := (&connect.Connections{Infrastructure: f.k.Infrastructure}).DialProjectDex(callContext, f.project, state.refs())
	if err != nil {
		f.test.Fatal(err)
	}
	defer func() {
		if closeErr := closeConnection(); closeErr != nil {
			f.test.Errorf("close the owned Dex connection: %v", closeErr)
		}
	}()
	found, err := dexpb.NewFlowServiceClient(connection).SearchFlows(callContext, &dexpb.SearchFlowsRequest{PageSize: 1})
	if err != nil || len(found.GetFlowRuns()) != 0 || found.GetNextPageToken() != "" {
		f.test.Fatalf("new project Dex namespace did not answer an empty real gRPC search: %v", err)
	}
}

// Inspect the project Dex's one Pod. The formal Dex image uses Debian
// bookworm-slim, which supplies /usr/bin/cat; image EXPOSE metadata is not
// evidence that a process listens on that port.
func (f *kindProjectFixture) verifyEngineOnlyListener(ctx context.Context, state *kindProjectScope) {
	f.test.Helper()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	service := f.readDiagnostics(ctx, state, "service/"+state.execution.DexService.Name, "")
	if infrakube.ResourceReference(service) != state.execution.DexService {
		f.test.Fatal("Dex Service identity changed before listener inspection")
	}
	ports, _ := kubeapi.Child(service, "spec")["ports"].([]any)
	if len(ports) != 1 {
		f.test.Fatal("Dex Service must expose only its engine port")
	}
	port, _ := ports[0].(map[string]any)
	if port["port"] != float64(8801) || port["targetPort"] != float64(8801) || port["protocol"] != "TCP" {
		f.test.Fatal("Dex Service must route only TCP 8801 to engine 8801")
	}
	pods := f.readDiagnostics(ctx, state, "pods", kubeapi.ComponentLabel+"="+infrakube.ComponentDex.String())
	items, _ := pods["items"].([]any)
	if len(items) != 1 {
		f.test.Fatalf("the project Dex has %d Pods, not one, at listener inspection", len(items))
	}
	pod, _ := items[0].(map[string]any)
	podUID := kubeapi.String(kubeapi.Child(pod, "metadata"), "uid")
	podName := kubeapi.String(kubeapi.Child(pod, "metadata"), "name")
	containers, _ := kubeapi.Child(pod, "spec")["containers"].([]any)
	if len(containers) != 1 {
		f.test.Fatal("the owned Dex Pod must contain only the engine container")
	}
	container, _ := containers[0].(map[string]any)
	args, _ := container["args"].([]any)
	if container["name"] != infrakube.ComponentDex.String() || container["image"] != f.k.DexImage || len(args) != 2 || args[0] != "--services" || args[1] != "api,interpreter" {
		f.test.Fatal("actual Dex Pod must run the accepted image with only api,interpreter services")
	}
	declaredPorts, _ := container["ports"].([]any)
	if len(declaredPorts) != 1 {
		f.test.Fatal("actual Dex container must declare only the engine port")
	}
	declaredPort, _ := declaredPorts[0].(map[string]any)
	if declaredPort["containerPort"] != float64(8801) || declaredPort["protocol"] != "TCP" {
		f.test.Fatal("actual Dex container port must be TCP 8801")
	}
	before := kindDexContainerStatus(pod)
	containerID := kubeapi.String(before, "containerID")
	if containerID == "" || before["ready"] != true {
		f.test.Fatal("the owned Dex container is not running and ready for listener inspection")
	}
	// This exact-Pod read uses the integration kubeconfig, never production exec
	// privileges. Both kernel tables are mandatory; an unreadable table is an
	// observation failure, not evidence that the HTTP listener is absent.
	command := exec.CommandContext(ctx, "kubectl", "--kubeconfig", f.kubeconfig, "--context", "kind-superverse-v2", "--request-timeout=15s", "-n", state.storage.Boundary.Name, "exec", "pod/"+podName, "-c", "dex", "--", "/usr/bin/cat", "/proc/net/tcp", "/proc/net/tcp6")
	command.WaitDelay = time.Second
	stdout, err := command.StdoutPipe()
	if err != nil {
		f.test.Fatal(err)
	}
	if err = command.Start(); err != nil {
		f.test.Fatalf("cannot inspect the owned Dex Pod listener tables: %v", err)
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, (1<<20)+1))
	if readErr != nil || len(output) > 1<<20 {
		cancel()
	}
	waitErr := command.Wait()
	if readErr != nil || waitErr != nil || len(output) > 1<<20 {
		f.test.Fatalf("bounded Dex listener observation failed (read=%v, process=%v, bytes=%d)", readErr, waitErr, len(output))
	}
	listening := make(map[uint64]bool)
	headers := 0
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "sl" {
			headers++
			continue
		}
		if len(fields) < 4 || !strings.Contains(fields[1], ":") {
			f.test.Fatal("invalid kernel TCP listener table")
		}
		if fields[3] != "0A" {
			continue
		}
		_, portHex, _ := strings.Cut(fields[1], ":")
		port, err := strconv.ParseUint(portHex, 16, 16)
		if err != nil {
			f.test.Fatal("invalid kernel TCP listener port")
		}
		listening[port] = true
	}
	if headers != 2 || !listening[8801] || listening[8802] {
		f.test.Fatalf("Dex socket observation requires both TCP tables, engine 8801 and no HTTP 8802 (tables=%d, engine=%t, http=%t)", headers, listening[8801], listening[8802])
	}
	after := f.readDiagnostics(ctx, state, "pod/"+podName, "")
	if kubeapi.String(kubeapi.Child(after, "metadata"), "uid") != podUID || kubeapi.String(kindDexContainerStatus(after), "containerID") != containerID {
		f.test.Fatal("Dex Pod or container changed during listener inspection")
	}
	f.test.Logf("verified engine-only listener: scope=%s namespace=%s serviceUID=%s podUID=%s imageID=%s enginePort=8801 http8802=false", state.scope.Kind, state.storage.Boundary.Name, state.execution.DexService.UID, podUID, kubeapi.String(before, "imageID"))
}

func kindDexContainerStatus(pod kubeapi.Object) kubeapi.Object {
	statuses, _ := kubeapi.Child(pod, "status")["containerStatuses"].([]any)
	for _, value := range statuses {
		status, _ := value.(map[string]any)
		if status["name"] == infrakube.ComponentDex.String() {
			return status
		}
	}
	return nil
}

type kindProjectFixture struct {
	test       *testing.T
	k          *LiveServing
	cluster    kubeapitest.KindCluster
	kubeconfig string
	project    string
	// live is the provisioned Live scope that cleanup removes.
	live *kindProjectScope
}

// emptyConfigurationSchemaVersion is the schema version of a configuration without shared connections and
// credential brokers: the SDK writes such a document with the previous version, so applications built before
// ConfigurationSchemaVersion still read it.
const emptyConfigurationSchemaVersion = "connectors.dex.dev/project-configuration/v1alpha2"

func newKindProjectFixture(t *testing.T) *kindProjectFixture {
	t.Helper()
	cluster := kubeapitest.KindPlatform(t)
	project := flowid.RandomID(4)
	fixture := &kindProjectFixture{test: t, cluster: cluster, kubeconfig: cluster.KubeconfigForTest, project: project}
	fixture.k = &LiveServing{Infrastructure: &infrakube.Infrastructure{API: cluster.ServerAPI(t), Dial: cluster.ServiceForwards(t), Environment: deploymentnames.EnvironmentTest, DexImage: os.Getenv("SUPERVERSE_LIVE_DEX_IMAGE"), Backend: &local.Backend{TemporalImage: os.Getenv("SUPERVERSE_LIVE_TEMPORAL_IMAGE"), S3Image: os.Getenv("SUPERVERSE_LIVE_S3_IMAGE")}}, Routes: infra.PublicRoutes{Environment: deploymentnames.EnvironmentTest, Local: true, LocalDomain: "localhost", LocalPort: 23080}}
	t.Cleanup(fixture.cleanup)
	t.Logf("isolated fixture project=%s dexImage=%s", project, fixture.k.DexImage)
	return fixture
}

// prepareStorage provisions the project's configuration storage alone; no Dex
// or Temporal object exists after it.
func (f *kindProjectFixture) prepareStorage(ctx context.Context) *kindProjectScope {
	f.test.Helper()
	state := &kindProjectScope{scope: infra.Scope{Kind: infra.ScopeLive}}
	f.live = state
	f.awaitReady(ctx, "project storage", func() (err error) {
		state.storage, err = f.k.EnsureStorage(ctx, f.project, state.storage)
		return err
	})
	if !f.k.Complete(state.storage) {
		f.test.Fatalf("project storage references are incomplete: %+v", state.storage)
	}
	return state
}

// prepareTemporal runs the project's Temporal on its accepted storage. Its
// data plane answers with the project's namespace, a second call reuses
// everything, and a replaced namespace or Service is refused, not awaited.
func (f *kindProjectFixture) prepareTemporal(ctx context.Context, state *kindProjectScope) {
	f.test.Helper()
	f.awaitReady(ctx, "project Temporal", func() (err error) {
		state.execution, err = f.k.EnsureTemporal(ctx, f.project, state.storage, state.execution)
		return err
	})
	credential := state.execution.TemporalCredential
	if state.execution.TemporalNamespace != "app-"+f.project || credential.Boundary != state.storage.Boundary.Name || credential.Name != "temporal" || credential.UID == "" || state.execution.DexService != (infra.ResourceRef{}) {
		f.test.Fatalf("Temporal references are not the project's own: %+v", state.execution)
	}
	if reused, err := f.k.EnsureTemporal(ctx, f.project, state.storage, state.execution); err != nil || reused != state.execution {
		f.test.Fatalf("reusing the project Temporal changed its references to %+v: %v", reused, err)
	}
	answer, err := f.k.CheckTemporalDataPlane(ctx, f.project, state.storage, state.execution)
	if err != nil || answer != infra.DataPlaneReady {
		f.test.Fatalf("the Temporal data plane answered %q: %v", answer, err)
	}
	replacedNamespace, replacedService := state.execution, state.execution
	replacedNamespace.TemporalNamespace += "x"
	replacedService.TemporalCredential.UID = "replaced-service"
	if _, err = f.k.EnsureTemporal(ctx, f.project, state.storage, replacedNamespace); err == nil || errors.Is(err, infra.ErrPending) {
		f.test.Fatalf("a replaced Temporal namespace was not refused: %v", err)
	}
	if _, err = f.k.CheckTemporalDataPlane(ctx, f.project, state.storage, replacedService); err == nil || errors.Is(err, infra.ErrPending) {
		f.test.Fatalf("a replaced Temporal Service was not refused: %v", err)
	}
	f.assertDeployments(ctx, state, "storage", "temporal")
}

// prepareDex grants the project Dex its access and starts it on the accepted
// storage and Temporal.
func (f *kindProjectFixture) prepareDex(ctx context.Context, state *kindProjectScope) {
	f.test.Helper()
	f.awaitReady(ctx, "project Dex access", func() error { return f.k.EnsureExecutionAccess(ctx, f.project, state.storage) })
	f.awaitReady(ctx, "project Dex", func() (err error) {
		f.assertDexStartup(ctx, state)
		state.execution, err = f.k.StartProjectDex(ctx, f.project, state.storage, state.execution)
		return err
	})
	if !state.execution.Complete() {
		f.test.Fatalf("project execution references are incomplete: %+v", state.execution)
	}
	f.assertDeployments(ctx, state, "storage", "temporal", "dex")
}

// rollDexCredential starts the project Dex on credential generation 1, as a
// rotated hosted API key does: the Pod is replaced on that generation's
// configuration Secret and reported ready on it, the superseded Secret is
// removed, and the Dex still reaches its Temporal.
func (f *kindProjectFixture) rollDexCredential(ctx context.Context, state *kindProjectScope) {
	f.test.Helper()
	if generation, ready, found, err := f.k.ProjectDexCredentialGeneration(ctx, f.project); err != nil || !found || !ready || generation != 0 {
		f.test.Fatalf("the project Dex runs credential generation %d (ready %t, found %t): %v", generation, ready, found, err)
	}
	rotated := state.execution
	rotated.TemporalCredential.Generation = 1
	f.awaitReady(ctx, "project Dex on the rotated credential", func() (err error) {
		state.execution, err = f.k.StartProjectDex(ctx, f.project, state.storage, rotated)
		return err
	})
	if state.execution != rotated {
		f.test.Fatalf("rolling the Dex changed its references to %+v", state.execution)
	}
	if generation, ready, found, err := f.k.ProjectDexCredentialGeneration(ctx, f.project); err != nil || !found || !ready || generation != 1 {
		f.test.Fatalf("the rolled project Dex runs credential generation %d (ready %t, found %t): %v", generation, ready, found, err)
	}
	superseded, code, err := f.k.API.Call(ctx, http.MethodGet, f.k.NamespacePath(f.project)+"/secrets/dex-server-config", nil)
	if err != nil || code != http.StatusNotFound && !kubeapi.Deleting(superseded) {
		f.test.Fatalf("the superseded Dex configuration Secret remains (%d): %v", code, err)
	}
	// A kubectl service forward binds the Pod selected when it opened, so the
	// forward opened for generation 0 reaches the replaced Pod; dial afresh.
	f.k.Dial = f.cluster.ServiceForwards(f.test)
	f.verifyProjectDexConnection(ctx, state)
}

// awaitReady calls ensure until it no longer reports infra.ErrPending and
// fails the test on any other error.
func (f *kindProjectFixture) awaitReady(ctx context.Context, description string, ensure func() error) {
	f.test.Helper()
	for {
		err := ensure()
		if err == nil {
			return
		}
		if !errors.Is(err, infra.ErrPending) {
			f.test.Fatalf("%s: %v", description, err)
		}
		f.awaitConvergence(ctx, description)
	}
}

// previewOf is the Project's Preview, whose configuration uses live's storage.
func (f *kindProjectFixture) previewOf(live *kindProjectScope) *kindProjectScope {
	return &kindProjectScope{scope: infra.PreviewKeysOf(f.project).Scope, storage: live.storage}
}

func (f *kindProjectFixture) assertDexStartup(ctx context.Context, state *kindProjectScope) {
	f.test.Helper()
	if state.execution.DexService.UID == "" {
		return
	}
	object := f.readDiagnostics(ctx, state, "pods", kubeapi.ComponentLabel+"="+infrakube.ComponentDex.String())
	items, _ := object["items"].([]any)
	for _, item := range items {
		pod, _ := item.(map[string]any)
		statuses, _ := kubeapi.Child(pod, "status")["containerStatuses"].([]any)
		for _, value := range statuses {
			container, _ := value.(map[string]any)
			terminated := kubeapi.Child(kubeapi.Child(container, "lastState"), "terminated")
			if code, _ := terminated["exitCode"].(float64); code != 0 {
				f.test.Fatalf("Dex startup failed in namespace %s (exit %.0f); inspect bounded startup logs", state.storage.Boundary.Name, code)
			}
		}
	}
}

func (f *kindProjectFixture) verifyConfigurationBeforeRelease(ctx context.Context, state *kindProjectScope) objectstore.ObjectRef {
	f.test.Helper()
	f.assertDeployments(ctx, state, "storage")
	client, err := f.k.S3(ctx, f.project, state.storage.Boundary.UID)
	if err != nil {
		f.test.Fatal(err)
	}
	store, err := objectstore.NewStore(client, client.Bucket, "", "fixture-source", true)
	if err != nil {
		f.test.Fatal(err)
	}
	// A Preview's freeze replaces the scope's connection usages, which live in the lane's PostgreSQL.
	usages := connections.NewUsages(connectiondao.New(postgrestest.OpenForTest(f.test)), nil)
	service := &projectconfig.Configuration{Objects: store, Storage: f.k.Infrastructure,
		Shared: confighost.SharedConnections{Providers: connections.NewProviderHTTPClient(true), Usages: usages}}
	defer service.Close()
	objects, err := f.k.ConfigurationObjects(ctx, state.ref(f.project))
	if err != nil {
		f.test.Fatal(err)
	}
	sdkScope := connectorconfig.Scope{ProjectID: f.project, Kind: state.scope.Kind.String()}
	// The manifest declares no connection, so no TeamSpace connection is read.
	state.host, err = confighost.NewConfigurationHost(objects, sdkScope, connectorlibrary.OfficialReleaseDownloadURL,
		confighost.SharedConnections{Providers: connections.NewProviderHTTPClient(true)})
	if err != nil {
		f.test.Fatal(err)
	}
	f.test.Cleanup(state.host.Close)
	state.admission = confighost.ConfigurationAdmission{}
	if rejected, invokeErr := state.host.Invoke(ctx, confighost.ConfigurationAdmission{}, http.MethodGet, "/api/v2/project-configuration", nil); invokeErr != nil || rejected.StatusCode != http.StatusForbidden {
		f.test.Fatalf("configuration host admitted a caller that is neither internal nor an actor: %v", invokeErr)
	}
	commit := strings.Repeat("c", 40)
	scope := state.ref(f.project)
	if _, err = service.ReadMetadata(ctx, scope, commit); !errors.Is(err, projectconfig.ErrNotFoundForTest) {
		f.test.Fatalf("new project scope had prepared source metadata: %v", err)
	}
	// Source preparation requires a real generated graph but no application,
	// Worker, Release execution or immutable build has been started.
	definition := kindGeneratedDefinition(f.test)
	raw, err := json.Marshal(map[string]any{
		"schemaVersion":   "superverse.dev/dex-app/v1",
		"application":     map[string]any{"port": 8080, "healthPath": "/healthz"},
		"flowDefinitions": []any{map[string]any{"sourcePath": definition.SourcePath}},
		"connectors":      []any{},
	})
	if err != nil {
		f.test.Fatal(err)
	}
	manifest, err := appmanifest.ExtractAppManifest(commit, raw)
	if err != nil {
		f.test.Fatal(err)
	}
	object, err := store.Put(ctx, "dex-app.yaml", "application/json", raw)
	if err != nil {
		f.test.Fatal(err)
	}
	manifest.ManifestObject = &pb.ObjectReference{Key: object.Key, Version: object.Version, Digest: object.Digest, MediaType: object.MediaType}
	graphs := []json.RawMessage{definition.Graph}
	prepared, err := service.RegisterPreparedSource(ctx, scope, manifest, graphs)
	if err != nil {
		f.test.Fatal(err)
	}
	definitions, err := fdg.ParseBundle(graphs)
	if err != nil || prepared.DefinitionRevision != definitions.Revision {
		f.test.Fatalf("source admission changed the generated FDG revision: %v", err)
	}
	metadata, err := service.ReadMetadata(ctx, scope, commit)
	if err != nil || metadata != prepared.Metadata || metadata.AppManifestRevision != 1 || metadata.ConfigurationRevision != 1 || metadata.SourceCommit != manifest.SourceCommit || metadata.ConnectorContractDigest != manifest.ConnectorContractDigest || metadata.EnvironmentContractDigest != manifest.EnvironmentContractDigest {
		f.test.Fatalf("source admission did not preserve exact source and semantic digests: %v", err)
	}
	retried, err := service.RegisterPreparedSource(ctx, scope, manifest, graphs)
	if err != nil || retried.Metadata != metadata || retried.DefinitionRevision != prepared.DefinitionRevision {
		f.test.Fatalf("source retry changed the admitted manifest or definition revision: %v", err)
	}
	repeated, err := service.ReadMetadata(ctx, scope, commit)
	if err != nil || repeated != metadata {
		f.test.Fatal("source retry changed the admitted configuration revision")
	}
	state.admission.SourceCommit = commit
	state.admission.DefinitionRevision = prepared.DefinitionRevision
	state.admission.AppManifestRevision = metadata.AppManifestRevision
	state.admission.ConfigurationRevision = metadata.ConfigurationRevision
	status, summary := f.request(ctx, state, http.MethodGet, "/api/v2/project-configuration", "kind-owner")
	var browserMetadata projectconfig.Metadata
	metadataDecoder := json.NewDecoder(bytes.NewReader(summary))
	metadataDecoder.DisallowUnknownFields()
	if err = metadataDecoder.Decode(&browserMetadata); status != http.StatusOK || err != nil || browserMetadata != metadata {
		f.test.Fatal("safe metadata did not match the admitted source or exposed unexpected fields")
	}
	status, catalog := f.request(ctx, state, http.MethodGet, "/api/v2/connector-connections", "kind-owner")
	var connections struct {
		AppManifestRevision uint64            `json:"appManifestRevision"`
		DefinitionRevision  string            `json:"definitionRevision"`
		Connections         []json.RawMessage `json:"connections"`
	}
	if err = json.Unmarshal(catalog, &connections); status != http.StatusOK || err != nil || connections.AppManifestRevision != 1 || connections.Connections == nil || len(connections.Connections) != 0 {
		f.test.Fatalf("project connections did not reflect the admitted empty manifest: status=%d", status)
	}
	if connections.DefinitionRevision != prepared.DefinitionRevision {
		f.test.Fatal("project connections did not read the admitted FDG revision")
	}
	for _, operation := range []struct{ method, route string }{
		{http.MethodPut, "/api/v2/project-configuration/app-manifest"},
		{http.MethodPut, "/api/v2/project-configuration/sources"},
		{http.MethodPost, "/api/v2/project-configuration/validate"},
	} {
		status, _ := f.request(ctx, state, operation.method, operation.route, "kind-owner")
		if status != http.StatusForbidden {
			f.test.Fatalf("actor-only request reached internal admission %s: status=%d", operation.route, status)
		}
	}
	var ref objectstore.ObjectRef
	var canonicalRevision uint64
	if state.scope.Kind == infra.ScopeLive {
		project := projects.Project{ProjectID: f.project, ProjectStatus: projects.StatusReady, Storage: state.storage, LiveExecution: state.execution}
		buildID := "build-" + uuid.NewString()
		frozen, freezeErr := service.FreezeReleaseConfiguration(ctx, project, manifest, buildID, metadata.ConfigurationRevision)
		if freezeErr != nil {
			f.test.Fatalf("pre-Release configuration freeze failed: %v", freezeErr)
		}
		ref, canonicalRevision = frozen.Object, frozen.ConfigurationRevision
		retried, retryErr := service.FreezeReleaseConfiguration(ctx, project, manifest, buildID, metadata.ConfigurationRevision)
		if retryErr != nil || retried != frozen {
			f.test.Fatalf("same-build freeze retry replaced the immutable version: %v", retryErr)
		}
		if _, err = service.FreezeReleaseConfiguration(ctx, project, manifest, "build-"+uuid.NewString(), 0); !errors.Is(err, projectconfig.ErrConflict) {
			f.test.Fatalf("new-build stale draft revision was not rejected: %v", err)
		}
	} else {
		validated, validateErr := service.ValidatePreparedPreview(ctx, scope, manifest, metadata)
		if validateErr != nil || validated.Status != "READY" || validated.ConfigurationRevision != metadata.ConfigurationRevision {
			f.test.Fatalf("prepared Preview validation failed: %v", validateErr)
		}
		ref, err = infra.ScopedObjectRef(scope, validated.Snapshot)
		if err != nil {
			f.test.Fatal(err)
		}
		canonicalRevision = validated.SnapshotConfigurationRevision
		retried, retryErr := service.ValidatePreparedPreview(ctx, scope, manifest, metadata)
		if retryErr != nil || retried.Snapshot != validated.Snapshot {
			f.test.Fatalf("Preview validation retry replaced the immutable version: %v", retryErr)
		}
		stale := metadata
		stale.ConfigurationRevision = 0
		if _, err = service.ValidatePreparedPreview(ctx, scope, manifest, stale); !errors.Is(err, projectconfig.ErrConflict) {
			f.test.Fatalf("stale Preview configuration revision was not rejected: %v", err)
		}
	}
	if canonicalRevision != 1 || ref.Version == "" || ref.Digest == "" {
		f.test.Fatal("validation did not return the initial canonical exact version")
	}
	contents, err := f.k.ReadConfiguration(ctx, scope, ref)
	if err != nil || objectstore.Digest(contents) != ref.Digest {
		f.test.Fatalf("exact configuration version/digest was not readable: %v", err)
	}
	canonical, err := connectorconfig.NewConfigurationStore(&connectorconfig.ConfigurationStoreConfig{Objects: objects, Scope: sdkScope})
	if err != nil {
		f.test.Fatal(err)
	}
	snapshot, err := canonical.ReadSnapshot(ctx, connectorconfig.SnapshotRef{Key: strings.TrimPrefix(ref.Key, state.storage.BlobPrefix), Version: ref.Version, Digest: ref.Digest, MediaType: ref.MediaType})
	if err != nil || snapshot.SchemaVersion != emptyConfigurationSchemaVersion || snapshot.Scope != sdkScope || snapshot.Revision != canonicalRevision || len(snapshot.Connections)+len(snapshot.TriggerBindings)+len(snapshot.OperationConfigurations)+len(snapshot.Environment) != 0 {
		f.test.Fatalf("official SDK rejected the exact empty canonical configuration: %v", err)
	}
	wrongDigest := ref
	wrongDigest.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err = f.k.ReadConfiguration(ctx, scope, wrongDigest); err == nil {
		f.test.Fatal("tampered configuration digest was accepted")
	}
	wrongVersion := ref
	wrongVersion.Version = "00000000-0000-0000-0000-000000000000"
	if _, err = f.k.ReadConfiguration(ctx, scope, wrongVersion); err == nil {
		f.test.Fatal("missing configuration version was accepted")
	}
	f.assertDeployments(ctx, state, "storage")
	f.test.Logf("prepared configuration verified: scope=%s namespace=%s namespaceUID=%s storageUID=%s source=%s manifestDigest=%s configKey=%s configVersion=%s configDigest=%s", state.scope.Kind, state.storage.Boundary.Name, state.storage.Boundary.UID, state.storage.Storage.UID, manifest.SourceCommit, manifest.ManifestDigest, ref.Key, ref.Version, ref.Digest)
	return ref
}

func kindGeneratedDefinition(t *testing.T) appmanifest.FlowDefinition {
	t.Helper()
	graph, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "docs", "flows", "sv2-platform-issue-flow.json"))
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		SchemaVersion string `json:"schemaVersion"`
		Flow          struct {
			Name string `json:"name"`
		} `json:"flow"`
		Source struct {
			Path string `json:"path"`
		} `json:"source"`
	}
	if err = json.Unmarshal(graph, &header); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err = json.Compact(&compact, graph); err != nil {
		t.Fatal(err)
	}
	definition := appmanifest.FlowDefinition{FlowType: header.Flow.Name, SourcePath: header.Source.Path, SchemaVersion: header.SchemaVersion, Valid: true, Graph: compact.Bytes(), Digest: objectstore.Digest(compact.Bytes())}
	if err = appmanifest.ValidateFlowDefinition(definition); err != nil {
		t.Fatal(err)
	}
	return definition
}

// assertDeployments requires exactly the named Deployments in the project
// namespace: the components provisioned so far and no application.
func (f *kindProjectFixture) assertDeployments(ctx context.Context, state *kindProjectScope, want ...string) {
	f.test.Helper()
	object := f.readDiagnostics(ctx, state, "deployments", "")
	items, ok := object["items"].([]any)
	if !ok {
		f.test.Fatal("invalid deployment list response")
	}
	found := make([]string, 0, len(items))
	for _, item := range items {
		deployment, _ := item.(map[string]any)
		found = append(found, kubeapi.String(kubeapi.Child(deployment, "metadata"), "name"))
	}
	slices.Sort(found)
	slices.Sort(want)
	if !slices.Equal(found, want) {
		f.test.Fatalf("project Deployments are %v, want %v", found, want)
	}
}

func (f *kindProjectFixture) readDiagnostics(ctx context.Context, state *kindProjectScope, resource, selector string) kubeapi.Object {
	f.test.Helper()
	// Diagnostics use the test kubeconfig; the production provider deliberately has no list permissions.
	args := []string{"--kubeconfig", f.kubeconfig, "--context", "kind-superverse-v2", "-n", state.storage.Boundary.Name, "get", resource, "-o", "json"}
	if selector != "" {
		args = append(args, "-l", selector)
	}
	data, err := exec.CommandContext(ctx, "kubectl", args...).Output()
	if err != nil {
		f.test.Fatalf("cannot inspect test-owned %s", resource)
	}
	var object kubeapi.Object
	if err = json.Unmarshal(data, &object); err != nil {
		f.test.Fatalf("invalid %s diagnostic response", resource)
	}
	return object
}

func (f *kindProjectFixture) request(ctx context.Context, state *kindProjectScope, method, route, actor string) (int, []byte) {
	f.test.Helper()
	admission := state.admission
	admission.ActorID = actor
	response, err := state.host.Invoke(ctx, admission, method, route, nil)
	if err != nil {
		f.test.Fatal(err)
	}
	return response.StatusCode, response.JSON
}

func (f *kindProjectFixture) awaitConvergence(ctx context.Context, description string) {
	f.test.Helper()
	select {
	case <-ctx.Done():
		f.test.Fatalf("%s deadline: %v", description, ctx.Err())
	case <-time.After(200 * time.Millisecond):
	}
}

func (f *kindProjectFixture) cleanup() {
	if f.live != nil {
		f.cleanupLive(f.live)
	}
}

// destroyLive destroys the project's Live scope within the test.
func (f *kindProjectFixture) destroyLive(ctx context.Context, state *kindProjectScope) {
	f.test.Helper()
	f.awaitReady(ctx, "scope destruction", func() error { return f.k.DestroyScope(ctx, f.project, state.refs()) })
	f.live = nil
}

func (f *kindProjectFixture) cleanupLive(state *kindProjectScope) {
	if state.storage.Boundary.UID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	for {
		err := f.k.DestroyScope(ctx, f.project, state.refs())
		if err == nil {
			f.test.Logf("removed isolated scope namespace=%s UID=%s", state.storage.Boundary.Name, state.storage.Boundary.UID)
			return
		}
		if !errors.Is(err, infra.ErrPending) {
			f.test.Errorf("project infrastructure cleanup: %v", err)
			return
		}
		select {
		case <-ctx.Done():
			f.test.Error("project cleanup deadline")
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
