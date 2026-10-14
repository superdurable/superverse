// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package tenancy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/api/boundary"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
	"github.com/superdurable/superversev2/internal/tenancy/teamspaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// oauthCallbackPath is Studio's OAuth callback, which a provider returns the
// actor to and which tells a TeamSpace authorization from its state.
const oauthCallbackPath = "/api/connector-oauth/callback"

// maxConnectionJSONBytes bounds a request's settings, credentials and
// command parameters.
const maxConnectionJSONBytes = 64 << 10

// builderRoles are the Project roles that build: they let a TeamSpace member
// manage connections and change the credential of a connection their Live
// versions use.
var builderRoles = []string{string(projects.RoleOwner), string(projects.RoleBuilder)}

// teamSpaceConnectionServer serves TeamSpaceConnectionService. The database
// owns connections (internal/tenancy/connections); the read model decides who
// may read and manage them.
type teamSpaceConnectionServer struct {
	pb.UnimplementedTeamSpaceConnectionServiceServer
	*boundary.Boundary
	connections  *connections.TeamSpaceConnections
	access       *connectiondao.ConnectionDAO
	studioOrigin string
	unrouted     UnroutedCounts
}

// connectionCaller is an authenticated caller's standing in one TeamSpace.
type connectionCaller struct {
	account accounts.AccountSummary
	access  connectiondao.TeamSpaceAccess
}

func (c connectionCaller) platformAdmin() bool { return accounts.IsAdministrator(c.account) }

func (c connectionCaller) admin() bool {
	return c.platformAdmin() || c.access.Role == string(teamspaces.RoleAdmin)
}

// manages reports whether the caller creates, edits and deletes the
// TeamSpace's connections: an admin, or the owner or builder of any Project.
func (c connectionCaller) manages() bool { return c.admin() || c.access.BuildsAny }

// caller authenticates the caller and reads its standing in the TeamSpace,
// which only its members and the platform admin may read.
func (s *teamSpaceConnectionServer) caller(ctx context.Context, caller *pb.CallerContext, teamSpaceID string) (connectionCaller, error) {
	account, err := s.Authenticate(ctx, caller)
	if err != nil {
		return connectionCaller{}, err
	}
	if err = flowid.ValidateTeamSpaceID(teamSpaceID); err != nil {
		return connectionCaller{}, boundary.RPCStatus(err)
	}
	access, err := s.access.Access(ctx, teamSpaceID, account.AccountID, builderRoles, string(teamspaces.StatusDeleted), string(projects.StatusDeleted))
	if err != nil {
		return connectionCaller{}, boundary.ConnectionRPCStatus(err)
	}
	c := connectionCaller{account: account, access: access}
	if access.Role == "" && !c.platformAdmin() {
		return connectionCaller{}, errTeamSpaceForbidden
	}
	return c, nil
}

// manager is caller for a request that changes connections, which only a
// READY TeamSpace admits.
func (s *teamSpaceConnectionServer) manager(ctx context.Context, caller *pb.CallerContext, teamSpaceID string) (connectionCaller, error) {
	c, err := s.caller(ctx, caller, teamSpaceID)
	switch {
	case err != nil:
		return c, err
	case !c.manages():
		return c, status.Error(codes.PermissionDenied, "only a TeamSpace admin or a builder of one of its Projects manages its connections")
	case c.access.Status != string(teamspaces.StatusReady):
		return c, status.Error(codes.FailedPrecondition, "the TeamSpace is being deleted")
	}
	return c, nil
}

// mayChangeCredential admits a credential change: a connection a Live
// version uses reaches that version at its next read, so it needs an admin or
// a builder of every Project with a Live usage.
func (s *teamSpaceConnectionServer) mayChangeCredential(c connectionCaller, teamSpaceID, connectionID string) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		if c.admin() {
			return true, nil
		}
		return s.access.BuildsEveryUser(ctx, teamSpaceID, connectionID, infra.ScopeLive.String(), c.account.AccountID, builderRoles, string(projects.StatusDeleted))
	}
}

func (s *teamSpaceConnectionServer) ListConnectorCatalog(ctx context.Context, in *pb.ListConnectorCatalogRequest) (*pb.ListConnectorCatalogResponse, error) {
	if _, err := s.Authenticate(ctx, in.GetCaller()); err != nil {
		return nil, err
	}
	entries, err := s.connections.Catalog(ctx)
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	out := &pb.ListConnectorCatalogResponse{}
	for _, entry := range entries {
		reason, endpointBound := connectormanifest.EndpointBoundReason(entry.ConnectorID)
		out.Connectors = append(out.Connectors, &pb.ConnectorCatalogEntry{ConnectorId: entry.ConnectorID, DisplayName: entry.DisplayName,
			Description: entry.Description, ModulePath: entry.ModulePath, Version: entry.Version, VersionLine: entry.VersionLine, LogoSvg: entry.LogoSVG,
			EndpointBound: endpointBound, EndpointBoundReason: reason})
	}
	return out, nil
}

func (s *teamSpaceConnectionServer) ListTeamSpaceConnections(ctx context.Context, in *pb.ListTeamSpaceConnectionsRequest) (*pb.ListTeamSpaceConnectionsResponse, error) {
	if _, err := s.caller(ctx, in.GetCaller(), in.GetTeamSpaceId()); err != nil {
		return nil, err
	}
	values, err := boundary.DomainValues(in.GetStatuses())
	if err != nil {
		return nil, err
	}
	statuses := []connectiondao.ConnectionStatus{connectiondao.ConnectionActive}
	if len(values) > 0 {
		statuses = statuses[:0]
		for _, value := range values {
			statuses = append(statuses, connectiondao.ConnectionStatus(value))
		}
	}
	page, err := s.ReadPage(fmt.Sprintf("connections/%s/%s/%s", in.GetTeamSpaceId(), in.GetConnectorId(), strings.Join(values, ",")), in)
	if err != nil {
		return nil, err
	}
	rows, usages, next, err := s.connections.Connections(ctx, in.GetTeamSpaceId(), in.GetConnectorId(), statuses, page.After, page.Size)
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	out := &pb.ListTeamSpaceConnectionsResponse{NextPageToken: s.NextPageToken(page, next)}
	for _, row := range rows {
		out.Connections = append(out.Connections, s.connectionSnapshot(row, usages))
	}
	return out, nil
}

func (s *teamSpaceConnectionServer) GetTeamSpaceConnection(ctx context.Context, in *pb.GetTeamSpaceConnectionRequest) (*pb.GetTeamSpaceConnectionResponse, error) {
	if _, err := s.caller(ctx, in.GetCaller(), in.GetTeamSpaceId()); err != nil {
		return nil, err
	}
	row, usages, stored, err := s.connections.Connection(ctx, in.GetTeamSpaceId(), in.GetConnectionId())
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	return &pb.GetTeamSpaceConnectionResponse{Connection: s.connectionSnapshot(row, usages), StoredCredentialFields: stored}, nil
}

func (s *teamSpaceConnectionServer) CreateTeamSpaceConnection(ctx context.Context, in *pb.CreateTeamSpaceConnectionRequest) (*pb.CreateTeamSpaceConnectionResponse, error) {
	c, err := s.manager(ctx, in.GetCaller(), in.GetTeamSpaceId())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	creation := connections.Creation{
		TeamSpaceID: in.GetTeamSpaceId(), RequestID: in.GetRequestId(), ActorAccountID: c.account.AccountID, Name: in.GetName(),
		Release:      connectormanifest.Module{ConnectorID: in.GetConnectorId(), ModulePath: in.GetModulePath(), ModuleVersion: in.GetVersion()},
		AuthMethodID: in.GetAuthMethodId(), ClientSource: clientSource(in.GetClientSource()), CopyFrom: in.GetCopyFromConnectionId(),
	}
	if creation.Settings, err = jsonFields("settings_json", in.GetSettingsJson()); err != nil {
		return nil, err
	}
	if creation.Credentials, err = jsonFields("credentials_json", in.GetCredentialsJson()); err != nil {
		return nil, err
	}
	created, err := s.connections.Create(ctx, creation)
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	return &pb.CreateTeamSpaceConnectionResponse{ConnectionId: created.ConnectionID}, nil
}

// UpdateTeamSpaceConnection edits the settings, or replaces the entered
// credential; the update admits a credential change.
func (s *teamSpaceConnectionServer) UpdateTeamSpaceConnection(ctx context.Context, in *pb.UpdateTeamSpaceConnectionRequest) (*pb.UpdateTeamSpaceConnectionResponse, error) {
	c, err := s.manager(ctx, in.GetCaller(), in.GetTeamSpaceId())
	if err != nil {
		return nil, err
	}
	update := connections.ConnectionUpdate{
		TeamSpaceID: in.GetTeamSpaceId(), ConnectionID: in.GetConnectionId(), ActorAccountID: c.account.AccountID,
		ExpectedRevision: in.GetExpectedRevision(), ExpectedCredentialRevision: in.GetExpectedCredentialRevision(), KeepFields: in.GetKeepCredentialFields(),
		MayChangeCredential: s.mayChangeCredential(c, in.GetTeamSpaceId(), in.GetConnectionId()),
	}
	if update.Settings, err = jsonFields("settings_json", in.GetSettingsJson()); err != nil {
		return nil, err
	}
	if update.Credentials, err = jsonFields("credentials_json", in.GetCredentialsJson()); err != nil {
		return nil, err
	}
	if _, err = s.connections.Update(ctx, update); err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	row, usages, _, err := s.connections.Connection(ctx, in.GetTeamSpaceId(), in.GetConnectionId())
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	return &pb.UpdateTeamSpaceConnectionResponse{Connection: s.connectionSnapshot(row, usages)}, nil
}

func (s *teamSpaceConnectionServer) DeleteTeamSpaceConnection(ctx context.Context, in *pb.DeleteTeamSpaceConnectionRequest) (*pb.DeleteTeamSpaceConnectionResponse, error) {
	c, err := s.manager(ctx, in.GetCaller(), in.GetTeamSpaceId())
	if err != nil {
		return nil, err
	}
	err = s.connections.Delete(ctx, in.GetTeamSpaceId(), in.GetConnectionId(), in.GetExpectedRevision(), c.account.AccountID, string(projects.StatusDeleted))
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	return &pb.DeleteTeamSpaceConnectionResponse{}, nil
}

// StartTeamSpaceConnectionOAuth starts an authorization, a credential change
// of an existing connection or the creation of a new one, which a manager
// may make as CreateTeamSpaceConnection admits it.
func (s *teamSpaceConnectionServer) StartTeamSpaceConnectionOAuth(ctx context.Context, in *pb.StartTeamSpaceConnectionOAuthRequest) (*pb.StartTeamSpaceConnectionOAuthResponse, error) {
	c, err := s.manager(ctx, in.GetCaller(), in.GetTeamSpaceId())
	if err != nil {
		return nil, err
	}
	start := connections.OAuthStart{
		TeamSpaceID: in.GetTeamSpaceId(), ConnectionID: in.GetConnectionId(), ActorAccountID: c.account.AccountID, Version: in.GetVersion(),
		ClientID: in.GetClientId(), ClientSecret: in.GetClientSecret(), RedirectURI: s.studioOrigin + oauthCallbackPath,
	}
	if created := in.GetNewConnection(); created != nil {
		if in.GetConnectionId() != "" {
			return nil, status.Error(codes.InvalidArgument, "connection_id and new_connection are exclusive")
		}
		if err = boundary.ValidateRequestID(created.GetRequestId()); err != nil {
			return nil, err
		}
		start.Creation = &connections.Creation{
			TeamSpaceID: in.GetTeamSpaceId(), RequestID: created.GetRequestId(), ActorAccountID: c.account.AccountID, Name: created.GetName(),
			Release:      connectormanifest.Module{ConnectorID: created.GetConnectorId(), ModulePath: created.GetModulePath(), ModuleVersion: created.GetVersion()},
			AuthMethodID: created.GetAuthMethodId(), ClientSource: clientSource(created.GetClientSource()),
		}
		if start.Creation.Settings, err = jsonFields("new_connection.settings_json", created.GetSettingsJson()); err != nil {
			return nil, err
		}
	} else {
		admitted, err := s.mayChangeCredential(c, in.GetTeamSpaceId(), in.GetConnectionId())(ctx)
		if err == nil && !admitted {
			err = connections.ErrCredentialChangeForbidden
		}
		if err != nil {
			return nil, boundary.ConnectionRPCStatus(err)
		}
	}
	if start.CredentialValues, err = jsonFields("credential_values_json", in.GetCredentialValuesJson()); err != nil {
		return nil, err
	}
	if start.CredentialSecrets, err = jsonStrings("credential_secrets_json", in.GetCredentialSecretsJson()); err != nil {
		return nil, err
	}
	authorizationURL, expiresAt, err := s.connections.StartOAuth(ctx, start)
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	return &pb.StartTeamSpaceConnectionOAuthResponse{AuthorizationUrl: authorizationURL, ExpiresAt: timestamppb.New(expiresAt)}, nil
}

// CompleteTeamSpaceConnectionOAuth completes the caller's own attempt, which
// its state names. The completion is authorized again where it takes effect:
// the caller must still manage the TeamSpace's connections and pass the
// credential rule of the connection.
func (s *teamSpaceConnectionServer) CompleteTeamSpaceConnectionOAuth(ctx context.Context, in *pb.CompleteTeamSpaceConnectionOAuthRequest) (*pb.CompleteTeamSpaceConnectionOAuthResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	completed, err := s.connections.CompleteOAuth(ctx, connections.OAuthCallback{ActorAccountID: account.AccountID, State: in.GetState(), Code: in.GetCode(),
		ProviderError: in.GetProviderError(), RedirectURI: s.studioOrigin + oauthCallbackPath,
		MayChangeCredential: func(ctx context.Context, teamSpaceID, connectionID string) error {
			c, err := s.manager(ctx, in.GetCaller(), teamSpaceID)
			if err != nil {
				return err
			}
			admitted, err := s.mayChangeCredential(c, teamSpaceID, connectionID)(ctx)
			if err == nil && !admitted {
				err = connections.ErrCredentialChangeForbidden
			}
			return err
		}})
	if st, isStatus := status.FromError(err); err != nil && isStatus {
		return nil, st.Err()
	}
	if err != nil {
		// The errors name the failed step and a provider's error code, never a
		// code, token or secret, so the reason is logged.
		s.Logger.WarnContext(ctx, "connection authorization did not complete", "account_id", account.AccountID, "error", comm.TailUTF8(err.Error(), 1024))
		return nil, boundary.ConnectionRPCStatus(err)
	}
	row, usages, _, err := s.connections.Connection(ctx, completed.TeamSpaceID, completed.ConnectionID)
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	return &pb.CompleteTeamSpaceConnectionOAuthResponse{Connection: s.connectionSnapshot(row, usages)}, nil
}

// StartTeamSpaceConnectionUiSession opens the editor's form, whose provider
// commands use the connection's credential, so only a manager opens it.
func (s *teamSpaceConnectionServer) StartTeamSpaceConnectionUiSession(ctx context.Context, in *pb.StartTeamSpaceConnectionUiSessionRequest) (*pb.StartTeamSpaceConnectionUiSessionResponse, error) {
	c, err := s.manager(ctx, in.GetCaller(), in.GetTeamSpaceId())
	if err != nil {
		return nil, err
	}
	module := connectormanifest.Module{ConnectorID: in.GetConnectorId(), ModulePath: in.GetModulePath(), ModuleVersion: in.GetVersion()}
	form, err := s.connections.OpenForm(ctx, c.account.AccountID, in.GetTeamSpaceId(), in.GetConnectionId(), module)
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	return &pb.StartTeamSpaceConnectionUiSessionResponse{SessionNonce: form.SessionNonce, ManifestJson: string(form.Manifest),
		PlatformClientMethodIds: form.PlatformClientMethods}, nil
}

func (s *teamSpaceConnectionServer) InvokeTeamSpaceConnectionCommand(ctx context.Context, in *pb.InvokeTeamSpaceConnectionCommandRequest) (*pb.InvokeTeamSpaceConnectionCommandResponse, error) {
	c, err := s.manager(ctx, in.GetCaller(), in.GetTeamSpaceId())
	if err != nil {
		return nil, err
	}
	parameters, err := jsonStrings("parameters_json", in.GetParametersJson())
	if err != nil {
		return nil, err
	}
	result, err := s.connections.RunFormCommand(ctx, c.account.AccountID, in.GetTeamSpaceId(), in.GetSessionNonce(), in.GetCommandId(), parameters)
	if err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, status.Error(codes.Internal, "the provider's answer is not JSON")
	}
	return &pb.InvokeTeamSpaceConnectionCommandResponse{ResultJson: string(encoded)}, nil
}

// connectionSnapshot shows a connection with the usages of it among usages
// and the events of its Slack installation that found no route.
func (s *teamSpaceConnectionServer) connectionSnapshot(row connectiondao.Connection, usages []connectiondao.Usage) *pb.TeamSpaceConnectionSnapshot {
	snapshot := connectionSnapshot(row, usages)
	if unrouted := s.unrouted.UnroutedEvents(row.TeamSpaceID, row.ConnectionID); unrouted.Count > 0 {
		snapshot.UnroutedEvents = &pb.UnroutedEvents{Count: unrouted.Count, LastAt: timestamppb.New(unrouted.LastEventAt), LastEventId: unrouted.LastEventID}
	}
	return snapshot
}

func connectionSnapshot(row connectiondao.Connection, usages []connectiondao.Usage) *pb.TeamSpaceConnectionSnapshot {
	var failure *dexflow.Failure
	if len(row.LastFailure) != 0 {
		// The column holds what the server wrote, which is always a Failure.
		if err := json.Unmarshal(row.LastFailure, &failure); err != nil {
			panic("the last_failure of connection " + row.ConnectionID + " is not a Failure: " + err.Error())
		}
	}
	snapshot := &pb.TeamSpaceConnectionSnapshot{
		TeamSpaceId: row.TeamSpaceID, ConnectionId: row.ConnectionID, Name: row.Name, ConnectorId: row.ConnectorID, ModulePath: row.ModulePath,
		VersionLine: row.VersionLine, ManifestVersion: row.ManifestVersion, AuthMethodId: row.AuthMethodID,
		ClientSource: boundary.Enum[pb.ConnectionClientSource](row.ClientSource), SettingsJson: string(row.Settings), Revision: row.Revision,
		CredentialStatus: boundary.Enum[pb.CredentialStatus](row.CredentialStatus), CredentialRevision: row.CredentialRevision,
		Status: boundary.Enum[pb.TeamSpaceConnectionStatus](row.ConnectionStatus), LastFailure: boundary.FailureSnapshot(failure),
		CreatorAccountId: row.CreatorAccountID, UpdatedByAccountId: row.UpdatedByAccountID,
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}
	if row.CredentialIssuedAt.Valid {
		snapshot.CredentialIssuedAt = timestamppb.New(row.CredentialIssuedAt.V)
	}
	if row.CredentialExpiresAt.Valid {
		snapshot.CredentialExpiresAt = timestamppb.New(row.CredentialExpiresAt.V)
	}
	for _, usage := range usages {
		if usage.ConnectionID == row.ConnectionID {
			snapshot.Usages = append(snapshot.Usages, &pb.TeamSpaceConnectionUsage{ProjectId: usage.ProjectID,
				Scope: infra.ProjectScopeOf(usage.Scope)})
		}
	}
	return snapshot
}

// clientSource is the source of a request; UNSPECIFIED is the user's own.
func clientSource(source pb.ConnectionClientSource) connectiondao.ClientSource {
	if source == pb.ConnectionClientSource_CONNECTION_CLIENT_SOURCE_PLATFORM {
		return connectiondao.ClientPlatform
	}
	return connectiondao.ClientUser
}

// jsonFields parses a request's JSON object of field values; empty is nil.
func jsonFields(field, value string) (map[string]json.RawMessage, error) {
	if value == "" {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if len(value) > maxConnectionJSONBytes || json.Unmarshal([]byte(value), &fields) != nil || fields == nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s must be a JSON object", field)
	}
	return fields, nil
}

// jsonStrings parses a request's JSON object of strings; empty is nil.
func jsonStrings(field, value string) (map[string]string, error) {
	if value == "" {
		return nil, nil
	}
	var values map[string]string
	if len(value) > maxConnectionJSONBytes || json.Unmarshal([]byte(value), &values) != nil || values == nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s must be a JSON object of strings", field)
	}
	return values, nil
}
