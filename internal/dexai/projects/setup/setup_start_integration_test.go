// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package setup_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/projects/repobinding"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// setupSettings are the real dependencies of every Setup scenario: the
// loopback Dex and merged API, the existing merged Worker that runs every
// Step with its real GitHub, Kubernetes or Docker and object store, the
// Project ID length and the projects of the local lane that Worker
// provisions in.
type setupSettings struct {
	dexAddress, apiAddress string
	worker                 dex.WorkerTarget
	idLength               int
	projects               projectObjects
}

func requireSetupSettings(t *testing.T) setupSettings {
	t.Helper()
	settings := setupSettings{
		dexAddress: os.Getenv("SUPERVERSE_TEST_DEX_ADDRESS"), apiAddress: os.Getenv("SUPERVERSE_TEST_API_ADDRESS"),
		worker: dex.WorkerTarget{Pull: true}, projects: newProjectObjects(t),
	}
	var lengthErr error

	settings.idLength, lengthErr = strconv.Atoi(os.Getenv("SUPERVERSE_TEST_PROJECT_ID_LENGTH"))
	loopback := func(address string) bool {
		host, _, err := net.SplitHostPort(address)
		return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	}
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" || !loopback(settings.dexAddress) || !loopback(settings.apiAddress) ||
		lengthErr != nil || settings.idLength < 1 || settings.idLength > 32 {
		t.Fatal("requires explicit isolated effects, loopback Dex and merged API, platform pull Worker and Project ID length")
	}
	return settings
}

// projectObjects reads a project's objects in the runtime of the local lane,
// with the operator's tools: the UID of its boundary (its namespace, or its
// Docker network), empty once the boundary is gone; the UID of its storage
// service; and the components that run in the boundary. It also holds the
// project's storage as an operator would stop it: holdStorage keeps the
// storage the lane runs from serving and reports false while the lane runs
// none, and releaseStorage lets a held storage serve again.
type projectObjects struct {
	boundaryUID    func(ctx context.Context, boundary string) (string, error)
	storageUID     func(ctx context.Context, boundary string) (string, error)
	components     func(ctx context.Context, boundary string) (string, error)
	holdStorage    func(ctx context.Context, project string) (bool, error)
	releaseStorage func(ctx context.Context, project string) error
}

// newProjectObjects reads through kubectl with the operator's kubeconfig
// (SUPERVERSE_TEST_KIND_KUBECONFIG) on Local Kind, and through the Docker CLI
// of the engine the operator's shell uses on Local Native. Kind holds the
// storage by scaling its MinIO Deployment to zero, which the storage
// reconciliation keeps, and Docker by pausing its MinIO container. Both find
// the storage by the owner labels of the project and its storage component.
func newProjectObjects(t *testing.T) projectObjects {
	t.Helper()
	if dexflowtest.LocalLaneForTest(t) == dexflowtest.LaneKindForTest {
		kubeconfig := dexflowtest.Setting(t, "SUPERVERSE_TEST_KIND_KUBECONFIG")
		kubectl := func(ctx context.Context, args ...string) (string, error) {
			return run(ctx, "kubectl", append([]string{"--kubeconfig", kubeconfig, "--context", "kind-superverse-v2", "--request-timeout=15s"}, args...)...)
		}
		// scale scales the project's MinIO Deployment, if the lane runs one.
		scale := func(ctx context.Context, project, replicas string) (bool, error) {
			selector := strings.Join(storageLabels(project), ",")
			namespace, err := kubectl(ctx, "get", "deployments", "--all-namespaces", "-l", selector, "-o", "jsonpath={.items[*].metadata.namespace}")
			if err != nil || namespace == "" {
				return false, err
			}
			if out, err := kubectl(ctx, "-n", namespace, "scale", "deployments", "-l", selector, "--replicas="+replicas); err != nil {
				return false, fmt.Errorf("%w: %s", err, out)
			}
			return true, nil
		}
		return projectObjects{
			boundaryUID: func(ctx context.Context, boundary string) (string, error) {
				return kubectl(ctx, "get", "namespace", boundary, "--ignore-not-found", "-o", "jsonpath={.metadata.uid}")
			},
			storageUID: func(ctx context.Context, boundary string) (string, error) {
				return kubectl(ctx, "-n", boundary, "get", "service", "storage", "-o", "jsonpath={.metadata.uid}")
			},
			components: func(ctx context.Context, boundary string) (string, error) {
				return kubectl(ctx, "-n", boundary, "get", "deployments", "-o", "jsonpath={.items[*].metadata.name}")
			},
			holdStorage: func(ctx context.Context, project string) (bool, error) { return scale(ctx, project, "0") },
			releaseStorage: func(ctx context.Context, project string) error {
				_, err := scale(ctx, project, "1")
				return err
			},
		}
	}
	// change pauses or unpauses only this Native instance's Project storage.
	instance := dexflowtest.NativeInstanceForTest(t)
	change := func(ctx context.Context, project, state, command string) (bool, error) {
		labels := storageLabels(project)
		id, err := run(ctx, "docker", "ps", "--quiet", "--filter", "label="+labels[0], "--filter", "label="+labels[1], "--filter", "label="+infradocker.NativeInstanceLabel+"="+instance, "--filter", "status="+state)
		if err != nil || id == "" {
			return false, err
		}
		if out, err := run(ctx, "docker", command, id); err != nil {
			return false, fmt.Errorf("%w: %s", err, out)
		}
		return true, nil
	}
	return projectObjects{
		boundaryUID: func(ctx context.Context, boundary string) (string, error) {
			return run(ctx, "docker", "network", "ls", "--no-trunc", "--filter", "name=^"+boundary+"$", "--format", "{{.ID}}")
		},
		storageUID: func(ctx context.Context, boundary string) (string, error) {
			return run(ctx, "docker", "container", "inspect", boundary+"-storage", "--format", `{{index .Config.Labels "superdurable.io/service-uid"}}`)
		},
		components: func(ctx context.Context, boundary string) (string, error) {
			names, err := run(ctx, "docker", "network", "inspect", boundary, "--format", "{{range .Containers}}{{.Name}} {{end}}")
			return strings.ReplaceAll(names, boundary+"-", ""), err
		},
		holdStorage: func(ctx context.Context, project string) (bool, error) {
			return change(ctx, project, "running", "pause")
		},
		releaseStorage: func(ctx context.Context, project string) error {
			_, err := change(ctx, project, "paused", "unpause")
			return err
		},
	}
}

// storageLabels are the owner labels, as key=value, that mark the objects of
// a project's storage in either runtime.
func storageLabels(project string) []string {
	return []string{deploymentnames.ProjectLabel + "=" + project, deploymentnames.ComponentLabel + "=" + infra.ComponentStorage.String()}
}

// run runs one bounded operator command and returns its trimmed output.
func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// requireStorageOnly checks a READY Project against the lane's runtime:
// complete configuration storage whose boundary and MinIO service are the
// recorded ones, no Live execution references, and no component in the
// boundary but the storage, so Setup started neither Temporal nor a project
// Dex.
func requireStorageOnly(ctx context.Context, t *testing.T, settings setupSettings, project projects.Project) {
	t.Helper()
	storage := project.Storage
	if storage.Boundary.UID == "" || storage.StorageBucket == "" || storage.BlobPrefix == "" || storage.Storage.UID == "" || storage.StorageBucket != "application" || project.LiveExecution != (infra.ExecutionRefs{}) {
		t.Fatalf("READY Project %s does not hold exactly complete storage without Live execution references", project.ProjectID)
	}
	boundaryUID, err := settings.projects.boundaryUID(ctx, storage.Boundary.Name)
	if err != nil || boundaryUID != storage.Boundary.UID {
		t.Fatalf("boundary %s is not the recorded one: %s", storage.Boundary.Name, boundaryUID)
	}
	serviceUID, err := settings.projects.storageUID(ctx, storage.Boundary.Name)
	if err != nil || serviceUID != storage.Storage.UID {
		t.Fatalf("the storage service of %s is not the recorded one: %s", storage.Boundary.Name, serviceUID)
	}
	components, err := settings.projects.components(ctx, storage.Boundary.Name)
	if err != nil || components != "storage" {
		t.Fatalf("boundary %s runs %q, not only the storage", storage.Boundary.Name, components)
	}
}

// registerOwner admits a real account through the merged API and ends its
// login at once: the Flow-level scenarios need only the owner identity.
func registerOwner(ctx context.Context, t *testing.T, settings setupSettings) string {
	t.Helper()
	connection, err := grpc.NewClient(settings.apiAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal("merged API connection failed")
	}
	defer func() { _ = connection.Close() }()
	accounts := pb.NewAccountServiceClient(connection)
	request := flowid.RandomID(24)
	registered, err := accounts.RegisterAccount(ctx, &pb.RegisterAccountRequest{RequestId: request, Email: "setup-storage-" + request + "@example.com", Password: request})
	login := registered.GetLogin()
	if err != nil || login.GetAccount().GetAccountId() == "" {
		t.Fatal("actual account admission failed")
	}
	caller := &pb.CallerContext{AccountId: login.Account.AccountId, CanonicalEmail: login.Account.Email, SessionToken: login.SessionToken}
	if _, err = accounts.Logout(ctx, &pb.LogoutRequest{Caller: caller}); err != nil {
		t.Fatal("integration login revocation failed")
	}
	return caller.AccountId
}

// storageScenario is one managed Project that a scenario starts through
// ProjectFlow, as CreateProject does, so its Setup runs on the existing
// Worker, and whose storage the scenario holds from the moment the lane runs
// it; the Project is deleted through ProjectFlow when the test ends.
type storageScenario struct {
	settings               setupSettings
	projects               *projects.ProjectFlowClient
	setups                 *setup.ProjectSetupFlowClient
	bindings               *repobinding.RepositoryBindingFlowClient
	reader                 dexpb.FlowServiceClient
	projectID, setupFlowID string
	requestID              string
	// held is closed once the lane holds the storage, or once the hold
	// stopped without it for holdErr.
	held    chan struct{}
	holdErr error
}

func startStorageScenario(ctx context.Context, t *testing.T, name string) *storageScenario {
	t.Helper()
	s, input := prepareStorageScenario(ctx, t, name)
	if err := s.projects.Create(ctx, input); err != nil {
		t.Fatalf("actual Project start requires reconciliation (%T)", err)
	}
	return s
}

// prepareStorageScenario creates the real owner and TeamSpace before saturation,
// retaining cleanup ownership while leaving Project and Setup admission to the caller.
func prepareStorageScenario(ctx context.Context, t *testing.T, name string) (*storageScenario, projects.ProjectStart) {
	t.Helper()
	settings := requireSetupSettings(t)
	owner := registerOwner(ctx, t, settings)
	projectFlow, setupFlow, repositoryFlow := &projects.ProjectFlow{}, &setup.ProjectSetupFlow{}, &repobinding.RepositoryBindingFlow{}
	sdk := dexflowtest.ClientForTest(t, settings.dexAddress, &settings.worker, projectFlow, setupFlow, repositoryFlow)
	s := &storageScenario{
		settings: settings, projects: &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, ProjectIDLength: settings.idLength, PlatformAdminEmails: []string{"admin@superverse.test"}},
		setups: &setup.ProjectSetupFlowClient{Dex: sdk, Flow: setupFlow}, bindings: &repobinding.RepositoryBindingFlowClient{Dex: sdk, Flow: repositoryFlow}, reader: dexflowtest.ExecutionReaderForTest(t, settings.dexAddress), requestID: flowid.RandomID(24),
	}
	projectID, setupID := flowid.ProjectCreationIDs(owner, s.requestID, settings.idLength)
	s.projectID, s.setupFlowID = projectID, flowid.SetupFlowID(projectID, setupID)
	input := projects.ProjectStart{ProjectID: projectID, CreatorAccountID: owner, Name: name, SetupFlowID: s.setupFlowID, TemplateRevision: apptemplate.Current().Revision,
		TeamSpaceID: projecttest.TeamSpaceForTest(ctx, t, owner)}
	fingerprint := flowid.Fingerprint(input)
	input.Identity = dexflow.StartIdentity{RequestID: fingerprint, Fingerprint: fingerprint, ActorAccountID: owner}
	// Own cleanup before admission, including an ambiguous start result.
	t.Cleanup(func() { s.deleteProject(context.WithoutCancel(ctx), t) })
	t.Logf("cleanup ownership before admission: Project=project-%s Setup=%s", projectID, s.setupFlowID)
	s.holdStorage(ctx, t)
	return s, input
}

// holdStorage holds the Project's storage from the moment the lane runs it,
// so the Setup waits in ReconcileProjectStorage until the scenario releases
// it. It asks the lane's runtime every 50 milliseconds from before the
// Project exists and holds the storage within a fraction of a second of its
// start; the runtime reports the storage pending in the round that starts it,
// and the Setup's next round comes two seconds later. When the test ends, the
// hold stops and releases the storage, before the Project's deletion.
func (s *storageScenario) holdStorage(ctx context.Context, t *testing.T) {
	holding, stop := context.WithCancel(ctx)
	s.held = make(chan struct{})
	go func() {
		defer close(s.held)
		for {
			held, err := s.settings.projects.holdStorage(holding, s.projectID)
			if held || err != nil {
				s.holdErr = err
				return
			}
			select {
			case <-holding.Done():
				s.holdErr = holding.Err()
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
	t.Cleanup(func() {
		stop()
		<-s.held
		if err := s.releaseStorage(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("releasing the storage of Project %s failed: %v", s.projectID, err)
		}
	})
}

// releaseStorage lets the held storage serve, within a minute; a storage the
// lane no longer runs has nothing to release.
func (s *storageScenario) releaseStorage(ctx context.Context) error {
	release, done := context.WithTimeout(ctx, time.Minute)
	defer done()
	return s.settings.projects.releaseStorage(release, s.projectID)
}

// awaitHeldWait waits until the Setup waits in ReconcileProjectStorage for
// the storage the lane holds, with the boundary recorded in the Project, and
// returns the boundary. A Setup past the wait, or a Project that left
// CREATING, means that the storage served before the hold, which fails the
// run.
func (s *storageScenario) awaitHeldWait(ctx context.Context, t *testing.T) string {
	t.Helper()
	// The hold ends once the storage is held, or with ctx.
	<-s.held
	if s.holdErr != nil {
		t.Fatalf("the lane did not hold the storage of Project %s: %v", s.projectID, s.holdErr)
	}
	var boundary string
	await(ctx, t, "the storage wait on the held storage with a recorded boundary", func() bool {
		active, _ := s.storageWait(ctx)
		project, err := s.projects.Read(ctx, s.projectID)
		if err != nil {
			t.Fatalf("authoritative Project read failed (%T)", err)
		}
		started, err := s.setups.Read(ctx, s.setupFlowID)
		if err != nil {
			t.Fatalf("authoritative Setup read failed (%T)", err)
		}
		if started.Phase != storageWaitStep || project.ProjectStatus != projects.StatusCreating {
			t.Fatalf("Setup %s reached %s and Project %s is %s: the storage served before the hold", s.setupFlowID, started.Phase, s.projectID, project.ProjectStatus)
		}
		boundary = project.Storage.Boundary.Name
		return active && project.Storage.Boundary.UID != ""
	})
	return boundary
}

// deleteProject requests the Project's deletion until its snapshot is
// DELETED, joining a deletion the scenario already requested.
func (s *storageScenario) deleteProject(ctx context.Context, t *testing.T) {
	cleanup, done := context.WithTimeout(ctx, 10*time.Minute)
	defer done()
	for cleanup.Err() == nil {
		project, err := s.projects.Read(cleanup, s.projectID)
		var absent *dex.FlowNotFoundError
		switch {
		case errors.As(err, &absent):
			return
		case err == nil && project.ProjectStatus == projects.StatusDeleted:
			t.Logf("cleanup terminal Project=%s revision=%d", s.projectID, project.Revision)
			return
		case err == nil:
			_, err = s.projects.RequestDeletion(cleanup, s.projectID, s.requestID+"-delete", project.Revision)
			if aborted(err) {
				err = nil
			}
		}
		if err != nil {
			// The Worker may be restarting; the next read decides.
			t.Logf("cleanup of Project %s waits (%s)", s.projectID, dexflowtest.ErrorCategory(err))
		}
		select {
		case <-cleanup.Done():
		case <-time.After(time.Second):
		}
	}
	t.Errorf("Project %s did not reach its DELETED snapshot; reconcile its exact identity", s.projectID)
}

// aborted reports a rejection whose revision fence moved: the caller rereads
// the Project and asks again.
func aborted(err error) bool {
	var rejected *dexflow.Rejection
	return errors.As(err, &rejected) && rejected.HasCode(dexflow.RejectionAborted)
}

// storageWaitStep is the Step type of the storage wait, its Go type name, and
// the Setup's phase while it waits.
var storageWaitStep = reflect.TypeFor[setup.ReconcileProjectStorage]().Name()

// storageWait reports whether the Setup's ReconcileProjectStorage is active
// and whether an attempt of it failed, as the attempt of a replaced Worker
// does. A Setup the engine does not know yet reads as not active.
func (s *storageScenario) storageWait(ctx context.Context) (active, failed bool) {
	state, err := s.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: s.setupFlowID})
	if err != nil {
		return false, false
	}
	for _, step := range state.GetActiveStepExecutions() {
		if step.GetStepType() == storageWaitStep {
			return true, step.GetLastFailureInfo().GetAttempt() > 0
		}
	}
	return false, false
}

// await polls done every 200 milliseconds until it holds.
func await(ctx context.Context, t *testing.T, what string, done func() bool) {
	t.Helper()
	for !done() {
		select {
		case <-ctx.Done():
			t.Fatalf("%s did not converge before the deadline", what)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// This scenario uses the existing merged Worker and its real GitHub, Kubernetes or Docker,
// object store and project storage. It starts no substitute Worker or provider. The
// initiating Client is closed before the durable delayed root can execute; only
// the Project's Execute handler can start Setup. Actual transport response loss
// and Worker replacement remain separate operational failure-injection cases.
// The READY Project holds its configuration storage only.
func TestProjectSetupSurvivesInitiatingClientExitIntegration(t *testing.T) {
	settings := requireSetupSettings(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	connection, err := grpc.NewClient(settings.apiAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal("merged API connection failed")
	}
	defer func() { _ = connection.Close() }()
	accountAPI, projectAPI := pb.NewAccountServiceClient(connection), pb.NewProjectServiceClient(connection)
	request := flowid.RandomID(24)
	registered, err := accountAPI.RegisterAccount(ctx, &pb.RegisterAccountRequest{RequestId: request, Email: "setup-start-" + request + "@example.com", Password: request})
	login := registered.GetLogin()
	if err != nil || login.GetAccount().GetAccountId() == "" {
		t.Fatal("actual account admission failed")
	}
	caller := &pb.CallerContext{AccountId: login.Account.AccountId, CanonicalEmail: login.Account.Email, SessionToken: login.SessionToken}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if _, logoutErr := accountAPI.Logout(cleanup, &pb.LogoutRequest{Caller: caller}); logoutErr != nil {
			t.Error("integration login revocation failed")
		}
	}()
	projectID, setupID := flowid.ProjectCreationIDs(caller.AccountId, request, settings.idLength)
	setupFlowID := flowid.SetupFlowID(projectID, setupID)
	projectFlow, setupFlow := &projects.ProjectFlow{}, &setup.ProjectSetupFlow{}
	newClient := func() *dex.Client {
		t.Helper()
		return dexflowtest.ClientForTest(t, settings.dexAddress, &settings.worker, projectFlow, setupFlow)
	}
	observer := newClient()
	defer func() { _ = observer.Close() }()
	// UpdateFlowConfig admits a system signal, not synchronous configuration
	// application. A delayed root can apply it only after its start delay. Reads
	// below wait for that actual Worker callback within a fixed bound.
	awaitProjectRead := func(parent context.Context, limit time.Duration) (projects.Project, error) {
		wait, done := context.WithTimeout(parent, limit)
		defer done()
		var lastErr error
		for wait.Err() == nil {
			read, readDone := context.WithTimeout(wait, 5*time.Second)
			var retained projects.Project
			lastErr = observer.InvokeRPC(read, "project-"+projectID, projectFlow.GetProject, nil, &retained)
			readDone()
			if lastErr == nil {
				return retained, nil
			}
			select {
			case <-wait.Done():
			case <-time.After(100 * time.Millisecond):
			}
		}
		return projects.Project{}, lastErr
	}
	admissionAttempted := false
	// Own cleanup before admission, including an ambiguous StartFlow result.
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Minute)
		defer done()
		retained, readErr := awaitProjectRead(cleanup, 2*time.Minute)
		if readErr != nil {
			var absent *dex.FlowNotFoundError
			if errors.As(readErr, &absent) && !admissionAttempted {
				return
			}
			t.Error("Project cleanup ownership read failed; exact identity requires reconciliation")
			return
		}
		if retained.ProjectID != projectID || retained.CreatorAccountID != caller.AccountId || retained.SetupFlowID != setupFlowID {
			t.Error("Project cleanup ownership mismatch")
			return
		}
		for cleanup.Err() == nil {
			readCtx, readDone := context.WithTimeout(cleanup, 15*time.Second)
			watch, readErr := projectAPI.WatchProject(readCtx, &pb.WatchProjectRequest{ProjectId: projectID, Caller: caller})
			var snapshot *pb.ProjectSnapshot
			if readErr == nil {
				frame, err := watch.Recv()
				readErr, snapshot = err, frame.GetSnapshot()
			}
			readDone()
			if readErr != nil || snapshot.GetProjectId() != projectID || snapshot.GetCreatorAccountId() != caller.AccountId {
				t.Error("authenticated cleanup snapshot failed")
				return
			}
			if snapshot.Status == pb.ProjectStatus_PROJECT_STATUS_DELETED {
				t.Logf("cleanup terminal Project=%s revision=%d", projectID, snapshot.Revision)
				return
			}
			if snapshot.Status != pb.ProjectStatus_PROJECT_STATUS_DELETING {
				_, err := projectAPI.DeleteProject(cleanup, &pb.DeleteProjectRequest{ProjectId: projectID, RequestId: request + "-delete", ExpectedRevision: snapshot.Revision, Caller: caller})
				if err != nil && status.Code(err) != codes.Aborted {
					t.Error("normal Project deletion failed")
					return
				}
			}
			select {
			case <-cleanup.Done():
			case <-time.After(time.Second):
			}
		}
		t.Error("Project cleanup did not reach its authoritative DELETED snapshot")
	}()
	template := apptemplate.Current()
	input := projects.ProjectStart{ProjectID: projectID, CreatorAccountID: caller.AccountId, Name: "Durable Setup start", SetupFlowID: setupFlowID, TemplateRevision: template.Revision,
		TeamSpaceID: projecttest.TeamSpaceForTest(ctx, t, caller.AccountId)}
	fingerprint := flowid.Fingerprint(input)
	input.Identity = dexflow.StartIdentity{RequestID: fingerprint, Fingerprint: fingerprint, ActorAccountID: caller.AccountId}
	attributes := projectFlow.InitialAttributes(input)
	delay := 30 * time.Second
	// Only this new, delayed root initially advertises an unavailable Worker.
	// Dex can accept the start even though no snapshot RPC can reach that target.
	// No substitute Worker/provider or existing execution is modified.
	options := dex.StartFlowOptions{RequestID: &fingerprint, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: attributes, StartDelay: &delay, ConfigOverride: &dex.FlowConfig{WorkerTarget: &dex.WorkerTarget{Address: "127.0.0.1:1"}, AttributeStoreNames: postgres.ProjectStore.Names()}}
	restoreWorker := func() error {
		restore, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		return observer.UpdateFlowConfig(restore, "project-"+projectID, dex.FlowConfig{WorkerTarget: &settings.worker})
	}
	workerRestored := false
	defer func() {
		if admissionAttempted && !workerRestored {
			if err := restoreWorker(); err != nil {
				t.Error("test root Worker restoration failed; normal cleanup remains unresolved")
			}
		}
	}()
	initiator := newClient()
	startCtx, startDone := context.WithTimeout(ctx, 10*time.Second)
	admissionAttempted = true
	t.Logf("cleanup ownership before admission: Project=project-%s Setup=%s account=%s", projectID, setupFlowID, caller.AccountId)
	startedAt := time.Now()
	_, startErr := initiator.StartFlow(startCtx, projectFlow, "project-"+projectID, projects.StartInputForTest(input), options)
	startDone()
	closeErr := initiator.Close()
	if startErr != nil || closeErr != nil {
		t.Fatal("root admission or initiating Client shutdown failed; cleanup retains the exact identity")
	}
	if time.Since(startedAt) >= delay {
		t.Fatal("initiating Client did not close within the durable start delay; interruption proof is incomplete")
	}
	// Retry only the accepted root start from a fresh Client. No caller starts Setup.
	if _, err := observer.StartFlow(ctx, projectFlow, "project-"+projectID, projects.StartInputForTest(input), options); err != nil {
		t.Fatal("same-request root replay failed")
	}
	reader := &projects.ProjectFlowClient{Dex: observer, Flow: projectFlow, ProjectIDLength: settings.idLength, PlatformAdminEmails: []string{"admin@superverse.test"}}
	setups := &setup.ProjectSetupFlowClient{Dex: observer, Flow: setupFlow}
	if err := reader.Create(ctx, input); err != nil {
		t.Fatal("matching Project start depended on an unavailable snapshot Worker")
	}
	readCtx, readDone := context.WithTimeout(ctx, 500*time.Millisecond)
	_, unavailableRead := reader.Read(readCtx, projectID)
	readDone()
	if err := restoreWorker(); err != nil {
		t.Fatal("test root Worker restoration failed")
	}
	workerRestored = true
	if unavailableRead == nil {
		t.Fatal("snapshot-unavailable precondition was not established")
	}
	t.Logf("matching accepted start returned Project=%s while its real snapshot RPC was unavailable", projectID)
	if _, err := awaitProjectRead(ctx, 2*time.Minute); err != nil {
		t.Fatalf("restored Worker callback did not become readable before its deadline (%T)", err)
	}
	for ctx.Err() == nil {
		readCtx, done := context.WithTimeout(ctx, 8*time.Second)
		project, readErr := reader.Read(readCtx, projectID)
		done()
		if readErr != nil {
			t.Fatalf("authoritative Project read failed (%s)", dexflowtest.ErrorCategory(readErr))
		}
		if project.ProjectStatus == projects.StatusSetupFailedForTest || project.LastFailure != nil {
			t.Fatal("durable Setup start or actual provider setup failed")
		}
		if project.ProjectStatus == projects.StatusReady {
			started, readErr := setups.Read(ctx, setupFlowID)
			if readErr != nil || started.Identity != input.Identity || started.ProjectID != projectID || started.SetupID != setupID || started.TemplateRevision != input.TemplateRevision || started.Repository.RepositoryID != project.RepositoryBinding.RepositoryID || project.RepositoryBinding.RepositoryID == 0 {
				t.Fatal("Setup did not preserve the accepted root identity and actual repository result")
			}
			requireStorageOnly(ctx, t, settings, project)
			// Exercise the exact Setup StartFlow replay used after a lost response,
			// once genuine orchestration and providers have already established it.
			// This does not inject or claim actual transport response loss.
			setupAttributes := setupFlow.InitialAttributes(setup.SetupStartForTest{Identity: input.Identity, ProjectID: projectID, SetupID: setupID, TemplateRevision: input.TemplateRevision})
			if _, err := observer.StartFlow(ctx, setupFlow, setupFlowID, nil, dex.StartFlowOptions{RequestID: &fingerprint, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: setupAttributes}); err != nil {
				t.Fatal("exact accepted Setup start replay failed")
			}
			setupAgain, err := setups.Read(ctx, setupFlowID)
			if err != nil || setupAgain.Identity != started.Identity || setupAgain.Repository != started.Repository || setupAgain.SourceCommit != started.SourceCommit {
				t.Fatal("Setup start replay changed the accepted identity or source")
			}
			if replayErr := reader.Create(ctx, input); replayErr != nil {
				t.Fatal("normal Client replay did not join the same durable Project and Setup")
			}
			current, err := reader.Read(ctx, projectID)
			if err != nil || current.RepositoryBinding != project.RepositoryBinding || current.SetupFlowID != setupFlowID || current.Storage != project.Storage {
				t.Fatal("authoritative Project changed after accepted retry")
			}
			t.Logf("durable orchestration verified: Project=project-%s Setup=%s repository=%d boundary=%s", projectID, setupFlowID, project.RepositoryBinding.RepositoryID, project.Storage.Boundary.Name)
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	t.Fatal("actual Project setup did not converge before its deadline")
}

// A Project deleted while its Setup waits for the configuration storage that
// Setup already recorded cancels the Setup in that phase, and the Project's
// deletion removes the storage boundary with every object in it. The lane
// holds the storage throughout, so only the deletion can end the wait, and the
// deletion removes the held storage.
func TestProjectSetupDeletionDuringStorageIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	s := startStorageScenario(ctx, t, "Setup deleted during storage")
	namespace := s.awaitHeldWait(ctx, t)
	await(ctx, t, "the deletion admission", func() bool {
		project, err := s.projects.Read(ctx, s.projectID)
		if err != nil {
			t.Fatalf("authoritative Project read failed (%T)", err)
		}
		if project.ProjectStatus == projects.StatusDeleting {
			return true
		}
		_, err = s.projects.RequestDeletion(ctx, s.projectID, s.requestID+"-delete", project.Revision)
		if err != nil && !aborted(err) {
			t.Fatalf("actual deletion admission failed (%T)", err)
		}
		return err == nil
	})
	await(ctx, t, "the Project deletion", func() bool {
		project, err := s.projects.Read(ctx, s.projectID)
		if err != nil {
			t.Fatalf("authoritative Project read failed (%T)", err)
		}
		if project.ProjectStatus == projects.StatusDeleting && project.ActiveCommand != nil && project.ActiveCommand.Phase == "FAILED" {
			t.Fatalf("deletion of Project %s failed: %s", s.projectID, project.LastFailure.Message)
		}
		return project.ProjectStatus == projects.StatusDeleted
	})
	cancelled, err := s.setups.Read(ctx, s.setupFlowID)
	if err != nil || cancelled.Status != setup.StatusCancelled || cancelled.Phase != storageWaitStep || cancelled.Failure != nil {
		t.Fatalf("Setup %s did not cancel in its storage wait: %s at %s (%T)", s.setupFlowID, cancelled.Status, cancelled.Phase, err)
	}
	remaining, err := s.settings.projects.boundaryUID(ctx, namespace)
	if err != nil || remaining != "" {
		t.Fatalf("the deleted Project left boundary %s: %s", namespace, remaining)
	}
	// The Setup's cleanup archived its repository and then ended its claim, which completes the binding.
	bindingID := flowid.RepositoryBindingFlowID(cancelled.Repository.RepositoryID)
	binding, err := s.bindings.Find(ctx, cancelled.Repository.RepositoryID)
	if err != nil || binding == nil || binding.Status != repobinding.StatusReleased || binding.ProjectID != s.projectID || binding.Generation != cancelled.Repository.Generation {
		t.Fatalf("the cancelled Setup did not end its binding %s (%s)", bindingID, dexflowtest.ErrorCategory(err))
	}
	if _, err = s.bindings.Dex.WaitForFlow(ctx, bindingID, dex.WaitForFlowOptions{}); err != nil {
		t.Fatalf("binding %s did not complete (%s)", bindingID, dexflowtest.ErrorCategory(err))
	}
	dexflowtest.RequireCompletedForTest(ctx, t, s.bindings.Dex, bindingID)
	t.Logf("Setup=%s CANCELLED in ReconcileProjectStorage; Project=%s DELETED without boundary %s; binding %s RELEASED and completed", s.setupFlowID, s.projectID, namespace, bindingID)
}

// This replaces the actual application Worker of the local lane while a Setup
// waits for its configuration storage, which the lane holds until the wait
// resumed. The attempt of the replaced Worker must fail, by the Worker's
// shutdown or the heartbeat timeout, and the wait resume in a new attempt and
// converge on the boundary and storage it recorded, never on replacements;
// the Project becomes READY with its storage only.
func TestProjectSetupStorageWorkerRestartIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	s := startStorageScenario(ctx, t, "Setup storage across a Worker replacement")
	s.awaitHeldWait(ctx, t)
	if _, failed := s.storageWait(ctx); failed {
		t.Fatal("a ReconcileProjectStorage attempt failed before the Worker replacement, so a failure after it would not show the replacement")
	}
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error { _, readErr := s.projects.Read(ctx, s.projectID); return readErr })
	await(ctx, t, "the failure of the ReconcileProjectStorage attempt of the replaced Worker", func() bool {
		active, failed := s.storageWait(ctx)
		return active && failed
	})
	if err := s.releaseStorage(ctx); err != nil {
		t.Fatalf("releasing the storage of Project %s failed: %v", s.projectID, err)
	}
	var ready projects.Project
	await(ctx, t, "the READY Project after the Worker replacement", func() bool {
		project, err := s.projects.Read(ctx, s.projectID)
		if err != nil {
			t.Fatalf("authoritative Project read failed (%T)", err)
		}
		if project.ProjectStatus == projects.StatusSetupFailedForTest {
			t.Fatalf("Setup of Project %s failed: %s", s.projectID, project.LastFailure.Message)
		}
		ready = project
		return project.ProjectStatus == projects.StatusReady
	})
	requireStorageOnly(ctx, t, s.settings, ready)
	t.Logf("Setup=%s READY after the retry of the replaced Worker's attempt on boundary %s (UID %s)", s.setupFlowID, ready.Storage.Boundary.Name, ready.Storage.Boundary.UID)
}
