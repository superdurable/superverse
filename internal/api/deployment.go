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
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/api/boundary"
	"github.com/superdurable/superversev2/internal/api/watch"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/projects"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// deploymentServer serves DeploymentService. A Deployment serves one READY
// Release build with its frozen configuration as the Project's Live
// application; ProjectService.RemoveLive ends Live serving.
type deploymentServer struct {
	pb.UnimplementedDeploymentServiceServer
	*boundary.Boundary
	projects    *projects.ProjectFlowClient
	deployments *deployments.ProjectDeployFlowClient
}

func (s *deploymentServer) StartDeployment(ctx context.Context, in *pb.StartDeploymentRequest) (*pb.StartDeploymentResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	start := deployments.Start{BuildRevisionID: in.GetBuildRevisionId(), ProjectID: in.GetProjectId(), DeploymentID: in.GetDeploymentId(), ReleaseID: in.GetReleaseId(), ConfigurationRevision: in.GetConfigurationRevision(), ExpectedLiveRevision: in.GetExpectedLiveRevision()}
	deployment, err := s.deployments.DeployReadyBuild(ctx, account.AccountID, in.GetRequestId(), start)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.StartDeploymentResponse{DeploymentId: deployment}, nil
}

// CancelDeployment cancels a deployment before it commits the new Live; the
// running Live is untouched.
func (s *deploymentServer) CancelDeployment(ctx context.Context, in *pb.CancelDeploymentRequest) (*pb.CancelDeploymentResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	if err = s.deployments.Cancel(ctx, account.AccountID, in.GetProjectId(), in.GetDeploymentId(), in.GetRequestId()); err != nil {
		return nil, boundary.CancellationRPCStatus(err)
	}
	return &pb.CancelDeploymentResponse{}, nil
}

func (s *deploymentServer) GetDeployment(ctx context.Context, in *pb.GetDeploymentRequest) (*pb.GetDeploymentResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if _, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityBuild); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	deployment, err := s.deployments.ReadProjection(ctx, in.GetProjectId(), in.GetDeploymentId())
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.GetDeploymentResponse{Deployment: deploymentSnapshot(deployment)}, nil
}

func (s *deploymentServer) ListDeployments(ctx context.Context, in *pb.ListDeploymentsRequest) (*pb.ListDeploymentsResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	statuses, err := boundary.DomainValues(in.GetStatuses())
	if err != nil {
		return nil, err
	}
	if _, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityBuild); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	page, err := s.ReadPage("deployments/"+account.AccountID+"/"+in.GetProjectId()+"/"+strings.Join(statuses, ","), in)
	if err != nil {
		return nil, err
	}
	listed, next, err := s.deployments.ListProjections(ctx, in.GetProjectId(), statuses, page.After, page.Size)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.ListDeploymentsResponse{NextPageToken: s.NextPageToken(page, next)}
	for _, projection := range listed {
		out.Deployments = append(out.Deployments, deploymentSnapshot(projection))
	}
	return out, nil
}

func (s *deploymentServer) WatchDeployment(in *pb.WatchDeploymentRequest, stream grpc.ServerStreamingServer[pb.WatchDeploymentResponse]) error {
	ctx := stream.Context()
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return err
	}
	flowID, err := deployments.FlowID(in.GetProjectId(), in.GetDeploymentId())
	if err != nil {
		return boundary.RPCStatus(err)
	}
	sources := []watch.Source[dexflow.ProgressEvent]{watch.DexStreamSource(s.Dex, flowID, deployments.DeploymentProgress)}
	resource := watch.Resource[*pb.DeploymentSnapshot, dexflow.ProgressEvent]{
		Connection: s.BindWatch(in, account.AccountID+"/"+flowID),
		Authorize: func(ctx context.Context, accountID string) error {
			_, err := s.projects.Authorize(ctx, accountID, in.GetProjectId(), projects.CapabilityBuild)
			return err
		},
		ReadSnapshot: func(ctx context.Context, accountID string) (*pb.DeploymentSnapshot, []watch.Source[dexflow.ProgressEvent], error) {
			if _, err := s.projects.Authorize(ctx, accountID, in.GetProjectId(), projects.CapabilityBuild); err != nil {
				return nil, nil, err
			}
			deployment, err := s.deployments.ReadProjection(ctx, in.GetProjectId(), in.GetDeploymentId())
			if err != nil {
				return nil, nil, err
			}
			return deploymentSnapshot(deployment), sources, nil
		},
		SendSnapshot: func(snapshot *pb.DeploymentSnapshot, cursor string) error {
			return stream.Send(&pb.WatchDeploymentResponse{Event: &pb.WatchDeploymentResponse_Snapshot{Snapshot: snapshot}, ResumeCursor: cursor})
		},
		SendProgress: func(event dexflow.ProgressEvent, cursor string) error {
			return stream.Send(&pb.WatchDeploymentResponse{Event: &pb.WatchDeploymentResponse_Progress{Progress: resourceProgress(event)}, ResumeCursor: cursor})
		},
	}
	return boundary.RPCStatus(resource.Run(ctx))
}

// deploymentSnapshot shows a deployment with the version of its Release.
func deploymentSnapshot(p deployments.DeploymentProjection) *pb.DeploymentSnapshot {
	d := p.Deployment
	return &pb.DeploymentSnapshot{Version: versionSnapshot(p.ReleaseVersion), BuildRevisionId: d.BuildRevisionID, Generation: d.Lease.Generation, ProjectId: d.ProjectID, DeploymentId: d.DeploymentID, ReleaseId: d.ReleaseID, Status: boundary.Enum[pb.DeploymentStatus](d.Status), Revision: d.Revision, ConfigurationRevision: d.ConfigurationRevision, Url: d.URL, Failure: boundary.OwnedFailureSnapshot(d.Failure, d.FailureOwner()), CreatedAt: timestamppb.New(d.CreatedAt), PhaseTimings: boundary.PhaseTimingSnapshots(d.Timings),
		Cancellation: deploymentCancellationSnapshot(d.Cancellation)}
}

// deploymentCancellationSnapshot is who cancelled a deployment and when, or
// nil.
func deploymentCancellationSnapshot(c *deployments.DeploymentCancellation) *pb.Cancellation {
	if c == nil {
		return nil
	}
	return &pb.Cancellation{ActorAccountId: c.ActorAccountID, RequestedAt: timestamppb.New(c.RequestedAt)}
}
