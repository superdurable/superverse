// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	remoteconnections "github.com/superdurable/superversev2/internal/dexai/agents/remoteagent"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	remoteapi "github.com/superdurable/superversev2/internal/dexai/api/remoteagent"
	"github.com/superdurable/superversev2/internal/dexai/api/tenancy"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"github.com/superdurable/superversev2/internal/dexai/studio/runs"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspacedao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc"
)

// Server holds what the business gRPC services share: the trust Boundary and
// the Flow clients and Services that bootstrap composes. Each service gets the
// Boundary and only the clients it uses.
type Server struct {
	*boundary.Boundary
	RPCCoverage     *RPCCoverage
	ProjectIDLength int
	Projects        *projects.ProjectFlowClient
	TeamSpaces      *teamspaces.TeamSpaceFlowClient
	TeamSpaceReads  *teamspacedao.TeamSpaceDAO
	Members         *memberdao.MemberDAO
	Connections     tenancy.Connections
	Setups          *setup.ProjectSetupFlowClient
	Sessions        *agents.SessionFlowService
	SessionAgents   *sessionagent.SessionAgentFlowClient
	Subagents       *subagent.SubagentFlowClient
	SessionModels   SessionModels
	RemoteAgents    *remoteconnections.Connections
	Host            repohost.Host
	Configuration   *projectconfig.Configuration
	RecentEvents    RecentTriggerEvents
	// Runs accepts only the fixed operations in flow_api.go. The protocol
	// exposes no HTTP path, Worker, server address, storage selector or
	// authority.
	Runs        *runs.Runs
	Releases    *releases.ProjectReleaseFlowClient
	Deployments *deployments.ProjectDeployFlowClient
	Publishing  *deployments.PublishingFlowService
}

// NewGRPCServer serves the business services behind panic recovery.
func NewGRPCServer(s *Server) *grpc.Server {
	unary := []grpc.UnaryServerInterceptor{s.LogUnaryFailure, s.RecoverUnary}
	stream := []grpc.StreamServerInterceptor{s.LogStreamFailure, s.RecoverStream}
	if s.RPCCoverage != nil {
		unary = append(unary, s.RPCCoverage.unary)
		stream = append(stream, s.RPCCoverage.stream)
	}
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(8<<20),
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
	)
	b := s.Boundary
	tenancy.Register(server, b, s.TeamSpaces, s.TeamSpaceReads, s.Members, s.Connections)
	pb.RegisterProjectServiceServer(server, &projectServer{Boundary: b, projectIDLength: s.ProjectIDLength,
		projects: s.Projects, members: s.Members, teamSpaces: s.TeamSpaces, setups: s.Setups, configuration: s.Configuration,
		deployments: s.Deployments, sessions: s.Sessions, recentEvents: s.RecentEvents, models: s.SessionModels})
	pb.RegisterProjectMemberServiceServer(server, &projectMemberServer{Boundary: b, projects: s.Projects, members: s.Members})
	pb.RegisterSessionServiceServer(server, &sessionServer{Boundary: b, sessions: s.Sessions, sessionAgents: s.SessionAgents, subagents: s.Subagents})
	pb.RegisterAgentServiceServer(server, &agentServer{Boundary: b, sessions: s.Sessions, sessionAgents: s.SessionAgents, models: s.SessionModels})
	remoteapi.Register(server, b, s.RemoteAgents)
	pb.RegisterPreviewServiceServer(server, &previewServer{Boundary: b, sessions: s.Sessions})
	pb.RegisterReleaseServiceServer(server, &releaseServer{Boundary: b, projects: s.Projects, host: s.Host, releases: s.Releases, configuration: s.Configuration, publishing: s.Publishing})
	pb.RegisterDeploymentServiceServer(server, &deploymentServer{Boundary: b, projects: s.Projects, deployments: s.Deployments})
	flows := flowRuns{Boundary: b, runs: s.Runs}
	pb.RegisterFlowRunServiceServer(server, &flowRunServer{flowRuns: flows})
	pb.RegisterFlowDiagnosticsServiceServer(server, &flowDiagnosticsServer{flowRuns: flows})
	configuration := flowConfiguration{Boundary: b, configuration: s.Configuration}
	pb.RegisterConnectorServiceServer(server, &connectorServer{flowConfiguration: configuration, publishing: s.Publishing})
	pb.RegisterApplicationEnvironmentServiceServer(server, &applicationEnvironmentServer{flowConfiguration: configuration})
	return server
}

// RPCCoverage records only declared business method names in a local test file.
type RPCCoverage struct {
	mu    sync.Mutex
	path  string
	known map[string]bool
	seen  map[string]bool
}

// NewRPCCoverage starts a new observation file for one local test server.
func NewRPCCoverage(path string) (*RPCCoverage, error) {
	known := make(map[string]bool)
	services := pb.File_superverse_v2_superverse_proto.Services()
	for i := 0; i < services.Len(); i++ {
		service := services.Get(i)
		methods := service.Methods()
		for j := 0; j < methods.Len(); j++ {
			known["/"+string(service.FullName())+"/"+string(methods.Get(j).Name())] = true
		}
	}
	recorder := &RPCCoverage{path: path, known: known, seen: make(map[string]bool)}
	if data, err := os.ReadFile(path); err == nil {
		var previous struct {
			Methods []string `json:"methods"`
		}
		if err := json.Unmarshal(data, &previous); err != nil {
			return nil, err
		}
		for _, method := range previous.Methods {
			if !known[method] {
				return nil, fmt.Errorf("RPC coverage has undeclared method %q", method)
			}
			recorder.seen[method] = true
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := recorder.write(); err != nil {
		return nil, err
	}
	return recorder, nil
}

func (r *RPCCoverage) unary(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if err := r.record(info.FullMethod); err != nil {
		return nil, err
	}
	return handler(ctx, request)
}

func (r *RPCCoverage) stream(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := r.record(info.FullMethod); err != nil {
		return err
	}
	return handler(server, stream)
}

func (r *RPCCoverage) record(method string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.known[method] {
		return fmt.Errorf("RPC coverage received undeclared method %q", method)
	}
	if r.seen[method] {
		return nil
	}
	r.seen[method] = true
	return r.write()
}

func (r *RPCCoverage) write() error {
	methods := make([]string, 0, len(r.seen))
	for method := range r.seen {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	data, err := json.MarshalIndent(struct {
		Methods []string `json:"methods"`
	}{methods}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	temporary := r.path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(temporary, r.path)
}
