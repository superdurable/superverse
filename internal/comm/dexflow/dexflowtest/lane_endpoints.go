// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflowtest

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// endpoint is an address of a local lane that a test's inputs name.
type endpoint int

// The endpoints of a lane that the inputs of a Dex integration test name.
const (
	// dexEndpoint is the platform Dex API as the host reaches it.
	dexEndpoint endpoint = iota
	// apiEndpoint is the merged business gRPC API of the server under test.
	apiEndpoint
	// workerHostEndpoint is the host through which the lane's platform Dex dials a
	// Worker that the test process runs.
	workerHostEndpoint
	// localGitEndpoint is the server's local Git host as the host reaches it.
	localGitEndpoint
	endpointCount
)

var endpointNames = [endpointCount]string{"platform Dex API", "business gRPC API", "test Worker host", "local Git host"}

// endpoints are the addresses of one lane, indexed by endpoint.
type endpoints [endpointCount]string

// laneInputs are the environment variables of a test that hold a lane's
// addresses, and the endpoint each one names.
var laneInputs = []struct {
	name     string
	endpoint endpoint
}{
	{"SUPERVERSE_TEST_DEX_ADDRESS", dexEndpoint},
	{"SUPERVERSE_TEST_API_ADDRESS", apiEndpoint},

	{"SUPERVERSE_TEST_WORKER_HOST", workerHostEndpoint},
}

// Local Kind's host forwards are owned by kind/cluster.py. The test Worker
// host matches HostFromContainers in projects/infra/addresses.go; the static
// lane check verifies both without a comm-to-product import.
const (
	kindDex = "127.0.0.1:28811"
	kindAPI = "127.0.0.1:28080"

	kindWorkerHost = "host.docker.internal"
	kindLocalGit   = "127.0.0.1:28085"
	// kindPostgres is the forward of the lane's PostgreSQL.
	kindPostgres = "127.0.0.1:28432"
)

// nativeInstanceSetting selects the effective Native topology whose addresses
// an integration test may use. The launcher writes that topology into the
// running instance's private run directory.
const nativeInstanceSetting = "SUPERVERSE_TEST_NATIVE_INSTANCE"

// endpoints returns the addresses of the lane.
func (l localLane) endpoints(t testing.TB) endpoints {
	t.Helper()
	switch l {
	case LaneKindForTest:
		return endpoints{dexEndpoint: kindDex, apiEndpoint: kindAPI, workerHostEndpoint: kindWorkerHost, localGitEndpoint: kindLocalGit}
	case laneNative:
		return nativeEndpoints(t)
	}
	panic("unknown local lane " + l.name)
}

// nativeEndpoints reads the effective addresses of the selected running Native
// instance. Reading its run layer, rather than native.yaml, makes a test fail
// before any connection when it combines one instance's inputs with another.
func nativeEndpoints(t testing.TB) endpoints {
	t.Helper()
	profile := nativeRunProfile(t)
	return endpoints{dexEndpoint: profile.Dex.ServerAddress, apiEndpoint: profile.Server.GRPCAddress, workerHostEndpoint: kindWorkerHost,
		localGitEndpoint: profile.LocalGit.Listen}
}

// LocalGitURLForTest is the origin of the lane's local Git host as the test
// process reaches it: Kind's forward, or the selected Native instance's
// listener.
func LocalGitURLForTest(t testing.TB) string {
	t.Helper()
	requireLane(t)
	address := LocalLaneForTest(t).endpoints(t)[localGitEndpoint]
	if address == "" {
		t.Fatalf("the %s lane names no local Git host", LocalLaneForTest(t))
	}
	return "http://" + address
}

// NativeNetworkPrefixForTest returns the selected Native instance's effective Docker
// range. Integration helpers that inspect Docker objects must use this rather
// than the compatible default from native.yaml.
func NativeNetworkPrefixForTest(t testing.TB) string {
	t.Helper()
	return nativeRunProfile(t).Native.NetworkPrefix
}

// NativeInstanceForTest returns the selected Native instance, as its effective run
// layer names it. Integration helpers that build the Docker runtime must pass
// it, so they find the instance's own objects.
func NativeInstanceForTest(t testing.TB) string {
	t.Helper()
	return nativeRunProfile(t).Native.Instance
}

// nativeRunProfileDocument is the part of a server configuration layer the
// tests read: a Native run layer, or the Kind profile.
type nativeRunProfileDocument struct {
	Server struct {
		GRPCAddress string `yaml:"grpcAddress"`
	} `yaml:"server"`
	Dex struct {
		ServerAddress string `yaml:"serverAddress"`
	} `yaml:"dex"`
	Native struct {
		Instance      string `yaml:"instance"`
		NetworkPrefix string `yaml:"networkPrefix"`
	} `yaml:"native"`
	LocalGit struct {
		Listen string `yaml:"listen"`
	} `yaml:"localgit"`
	Postgres struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"postgres"`
	Connections struct {
		ReadURL    string `yaml:"readURL"`
		ReadListen string `yaml:"readListen"`
		SlackRelay struct {
			ListenAddress string `yaml:"listenAddress"`
		} `yaml:"slackRelay"`
	} `yaml:"connections"`
}

func nativeRunProfile(t testing.TB) nativeRunProfileDocument {
	t.Helper()
	instance := nativeInstance(t)
	if len(instance) > 12 {
		t.Fatalf("%s must name a Local Native instance, not %q", nativeInstanceSetting, instance)
	}
	state := nativeStateDir(t, instance)
	if configured := os.Getenv("SUPERVERSE_TEST_NATIVE_STATE_DIR"); configured != "" && filepath.Clean(configured) != filepath.Clean(state) {
		t.Fatalf("SUPERVERSE_TEST_NATIVE_STATE_DIR is %q, not selected Native instance %q state %q", configured, instance, state)
	}
	owner, err := os.ReadFile(filepath.Join(state, "owner.json")) //nolint:gosec // the selected instance's own state file
	if err != nil {
		t.Fatalf("the effective Native instance %q has no owner record at %s: %v", instance, state, err)
	}
	var retained struct {
		RunID string `json:"runId"`
	}
	if err = json.Unmarshal(owner, &retained); err != nil || retained.RunID == "" {
		t.Fatalf("the effective Native instance %q has no retained run: %v", instance, err)
	}
	raw, err := os.ReadFile(filepath.Join(state, "runs", retained.RunID, "native-run.yaml")) //nolint:gosec // the retained run's own layer
	if err != nil {
		t.Fatalf("the effective Native instance %q run layer is unreadable: %v", instance, err)
	}
	var profile nativeRunProfileDocument
	if err = yaml.Unmarshal(raw, &profile); err != nil {
		t.Fatalf("the effective Native instance %q run layer does not parse: %v", instance, err)
	}
	if profile.Native.Instance != instance {
		t.Fatalf("the effective Native instance %q run layer names %q", instance, profile.Native.Instance)
	}
	if profile.Native.NetworkPrefix == "" {
		t.Fatalf("the effective Native instance %q run layer has no network prefix", instance)
	}
	return profile
}

func nativeStateDir(t testing.TB, instance string) string {
	t.Helper()
	base := os.Getenv("SUPERVERSE_TEST_NATIVE_STATE_DIR")
	if base != "" {
		base = filepath.Dir(base)
	} else {
		base = laneStateRoot(t)
	}
	if instance == defaultNativeInstance {
		return filepath.Join(base, "native")
	}
	return filepath.Join(base, "native-"+instance)
}

// laneStateRoot is the main checkout's .local/superverse-v2, which holds the
// Kind lane's private state and each Native instance's directory.
func laneStateRoot(t testing.TB) string {
	t.Helper()
	common, err := exec.Command("git", "-C", repositoryRoot(t), "rev-parse", "--path-format=absolute", "--git-common-dir").Output() //nolint:gosec,noctx // a short local read of the repository's own path, before any test context exists
	if err != nil {
		t.Fatalf("find the lane state root: %v", err)
	}
	return filepath.Join(filepath.Dir(strings.TrimSpace(string(common))), ".local", "superverse-v2")
}

// LanePostgres returns the address at which the host reaches the lane's
// PostgreSQL and the lane's private file of its role passwords, which the
// lane scripts write.
func LanePostgres(t testing.TB) (address, credentialsFile string) {
	t.Helper()
	switch LocalLaneForTest(t) {
	case LaneKindForTest:
		return kindPostgres, filepath.Join(laneStateRoot(t), postgresCredentials)
	case laneNative:
		profile := nativeRunProfile(t)
		if profile.Postgres.Port == 0 {
			t.Fatal("the effective Native run layer has no postgres.port")
		}
		return net.JoinHostPort(profile.Postgres.Host, strconv.Itoa(profile.Postgres.Port)), filepath.Join(nativeStateDir(t, nativeInstance(t)), postgresCredentials)
	}
	panic("unknown local lane")
}

// CredentialReadURLForTest is the credential read endpoint as the lane's
// applications and Sandboxes reach it, which a configuration's credential
// broker names: the Kind profile's, or the selected Native instance's run
// layer's.
func CredentialReadURLForTest(t testing.TB) string {
	t.Helper()
	var url string
	switch LocalLaneForTest(t) {
	case LaneKindForTest:
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "cmd", "superverse-server", "config", "local-kind.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var profile nativeRunProfileDocument
		if err = yaml.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("the Kind profile does not parse: %v", err)
		}
		url = profile.Connections.ReadURL
	case laneNative:
		url = nativeRunProfile(t).Connections.ReadURL
	}
	if url == "" {
		t.Fatalf("the %s lane names no connections.readURL", LocalLaneForTest(t))
	}
	return url
}

// kindSlackEvents is the `kind ports` forward of the server's Slack events
// listener (FORWARDS in infra-as-code/scripts/kind/cluster.py).
const kindSlackEvents = "127.0.0.1:28088"

// kindCredentialRead is the `kind ports` forward of the server's credential
// read listener.
const kindCredentialRead = "127.0.0.1:28087"

// CredentialReadHostOriginForTest is the origin at which the host reaches the
// lane server's credential read listener, which also serves the Slack trigger
// registrations.
func CredentialReadHostOriginForTest(t testing.TB) string {
	t.Helper()
	if LocalLaneForTest(t) == laneNative {
		return "http://" + nativeRunProfile(t).Connections.ReadListen
	}
	return "http://" + kindCredentialRead
}

// slackSigningSecret is the lane's retained test Signing Secret of the Slack
// event relay, which the lane scripts generate.
const slackSigningSecret = "slack-signing-secret"

// SlackRelayForTest is the Slack event relay's events URL as the host reaches
// the lane's server, and the lane's test Signing Secret, which a relay test
// signs events with. The secret never reaches a failure or a log.
func SlackRelayForTest(t testing.TB) (eventsURL string, signingSecret []byte) {
	t.Helper()
	address, file := kindSlackEvents, filepath.Join(laneStateRoot(t), slackSigningSecret)
	if LocalLaneForTest(t) == laneNative {
		address, file = nativeRunProfile(t).Connections.SlackRelay.ListenAddress, filepath.Join(nativeStateDir(t, nativeInstance(t)), slackSigningSecret)
	}
	secret, err := os.ReadFile(file) //nolint:gosec // the lane's own private secret file
	if err != nil || len(strings.TrimSpace(string(secret))) == 0 || address == "" {
		t.Fatalf("the %s lane has no Slack relay listener or test Signing Secret (%T)", LocalLaneForTest(t), err)
	}
	return "http://" + address + "/api/slack/events", []byte(strings.TrimSpace(string(secret)))
}

// postgresCredentials is the file of a lane's PostgreSQL role passwords, keyed
// by role name.
const postgresCredentials = "postgres-credentials.json"

// passedInput is an address that a constructor of this package was given.
type passedInput struct {
	what     string
	endpoint endpoint
	address  string
}

// requireLane fails the test at once unless the lane that SUPERVERSE_TEST_LANE
// names exists and every address of the test belongs to it: each lane input in
// the environment that is set, and each address passed to the constructor that
// calls it. A Flow that one lane's Dex stores with another lane's Worker target
// is later dispatched to the wrong server, which fails on blobs it cannot load
// or, with small inline values, runs the Flow's handlers against another
// lane's state.
func requireLane(t testing.TB, passed ...passedInput) {
	t.Helper()
	if os.Getenv(laneSetting) == "" {
		t.Fatalf("requires %s (%s or %s): the lane that the Dex, Worker and API addresses of the test belong to (docs/runbooks/local-native.md#test-inputs)", laneSetting, LaneKindForTest, laneNative)
	}
	lane := LocalLaneForTest(t)
	want := lane.endpoints(t)
	var mismatches []string
	check := func(what string, endpoint endpoint, address string) {
		if address == "" || address == want[endpoint] {
			return
		}
		mismatch := fmt.Sprintf("%s is %q, not the %s lane's %s (%s)", what, address, lane, endpointNames[endpoint], want[endpoint])
		for _, other := range []localLane{LaneKindForTest, laneNative} {
			if other != lane && other.endpoints(t)[endpoint] == address {
				mismatch = fmt.Sprintf("%s; it is the %s lane's", mismatch, other)
			}
		}
		mismatches = append(mismatches, mismatch)
	}
	for _, input := range laneInputs {
		check(input.name, input.endpoint, os.Getenv(input.name))
	}
	for _, input := range passed {
		check(input.what, input.endpoint, input.address)
	}
	if len(mismatches) > 0 {
		t.Fatalf("%s=%s, but the test environment mixes local lanes:\n  %s\nUse the Dex, API and Worker addresses of one lane (docs/runbooks/local-native.md#test-inputs)",
			laneSetting, lane, strings.Join(mismatches, "\n  "))
	}
}
