// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents"
	remoteconnections "github.com/superdurable/superversev2/internal/agents/remoteagent"
	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/api/boundary"
	remoteapi "github.com/superdurable/superversev2/internal/api/remoteagent"
	"github.com/superdurable/superversev2/internal/api/tenancy"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/platform/repohost"
	"github.com/superdurable/superversev2/internal/projects"
	projectconfig "github.com/superdurable/superversev2/internal/projects/config"
	"github.com/superdurable/superversev2/internal/projects/setup"
	"github.com/superdurable/superversev2/internal/studio/runs"
	"github.com/superdurable/superversev2/internal/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/tenancy/teamspacedao"
	"github.com/superdurable/superversev2/internal/tenancy/teamspaces"
	"google.golang.org/grpc"
)

// Server holds what the business gRPC services share: the trust Boundary and
// the Flow clients and Services that bootstrap composes. Each service gets the
// Boundary and only the clients it uses.
type Server struct {
	*boundary.Boundary
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
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(8<<20),
		grpc.ChainUnaryInterceptor(s.LogUnaryFailure, s.RecoverUnary),
		grpc.ChainStreamInterceptor(s.LogStreamFailure, s.RecoverStream),
	)
	b := s.Boundary
	tenancy.Register(server, b, s.TeamSpaces, s.TeamSpaceReads, s.Members, s.Connections)
	pb.RegisterProjectServiceServer(server, &projectServer{Boundary: b, projectIDLength: s.ProjectIDLength,
		projects: s.Projects, members: s.Members, teamSpaces: s.TeamSpaces, setups: s.Setups, configuration: s.Configuration,
		deployments: s.Deployments, sessions: s.Sessions, recentEvents: s.RecentEvents})
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
