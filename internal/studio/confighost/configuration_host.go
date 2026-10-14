// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package confighost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/studio/flowapi"
	"github.com/superdurable/superversev2/internal/studio/flowapi/fdg"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

// ConfigurationHost serves the configuration of one project scope: its
// AppManifest, application environment, prepared sources and validation, and
// the TeamSpace connections its connection names select, with their forms.
type ConfigurationHost struct {
	project           *projectConfiguration
	connectorReleases *connectormanifest.ReleaseResolver
	shared            SharedConnections
	// releaseHTTPClient is the host's own transport of release downloads;
	// studioCommands copies the shared provider client.
	releaseHTTPClient *http.Client
	studioCommands    *http.Client
	routes            *http.ServeMux
}

// SharedConnections is how a host reaches the TeamSpace connections: their
// selection, their credentials for Studio commands and their usages, and the
// URL of the credential read endpoint that configuration documents give
// applications. Providers is the provider client with the address policy,
// which the composition owns; PlainHTTP admits http URL settings where that
// policy allows private-network endpoints.
type SharedConnections struct {
	Connections *connections.TeamSpaceConnections
	Reader      *connections.CredentialReader
	Usages      *connections.Usages
	ReadURL     string
	Providers   *http.Client
	PlainHTTP   bool
	// FormKey is the root key from which the key of the UI sessions' nonces
	// derives; a secret.
	FormKey []byte
	// ProjectTeamSpace returns the TeamSpace of a Project, whose connections
	// a freeze of its configuration may select.
	ProjectTeamSpace func(ctx context.Context, projectID string) (string, error)
}

// NewConfigurationHost opens the host of scope over its object storage, which
// reads release candidates from candidateBaseURL. It creates and changes no
// stored state.
func NewConfigurationHost(objects connectorconfig.ObjectStore, scope connectorconfig.Scope, candidateBaseURL string, shared SharedConnections) (*ConfigurationHost, error) {
	project, err := newProjectConfiguration(objects, scope)
	if err != nil {
		return nil, err
	}
	releaseHTTPClient := connectormanifest.NewReleaseHTTPClient()
	host := &ConfigurationHost{
		project: project, connectorReleases: connectormanifest.NewReleaseResolver(releaseHTTPClient, candidateBaseURL), shared: shared,
		releaseHTTPClient: releaseHTTPClient, studioCommands: connectormanifest.StudioCommandClient(shared.Providers),
		routes: http.NewServeMux(),
	}
	// The project-configuration routes serve the platform's own Flows; the
	// others serve Studio.
	host.routes.HandleFunc("GET /api/v2/project-configuration", host.handleGetConfiguration)
	host.routes.HandleFunc("PUT /api/v2/project-configuration/app-manifest", host.handlePutManifest)
	host.routes.HandleFunc("PUT /api/v2/project-configuration/sources", host.handlePrepareSource)
	host.routes.HandleFunc("POST /api/v2/project-configuration/validate", host.handleValidate)
	host.routes.HandleFunc("POST /api/v2/project-configuration/requirements", host.handleRequirements)
	host.routes.HandleFunc("POST /api/v2/project-configuration/live-items", host.handleNextPublish)
	host.routes.HandleFunc("POST /api/v2/project-configuration/live-derivations", host.handleNextPublishCopy)
	host.routes.HandleFunc("POST /api/v2/project-configuration/preview-items", host.handlePreviewItems)
	host.routes.HandleFunc("GET /api/v2/application-environment", host.handleGetEnvironment)
	host.routes.HandleFunc("PUT /api/v2/application-environment", host.handlePutEnvironment)
	host.routes.HandleFunc("GET /api/v2/connector-connections", host.handleListConnections)
	host.routes.HandleFunc("PUT /api/v2/connector-selections/{connectorId}/{connectionName}", host.handleSelectConnection)
	host.routes.HandleFunc("PUT /api/v2/connector-trigger-bindings/{connectorId}/{connectionName}/{triggerName}/{bindingName}", host.handlePutTriggerBinding)
	host.routes.HandleFunc("PUT /api/v2/connector-use-configurations/{connectorId}/{connectionName}/{operationId}/{flowType}/{stepType}", host.handlePutUseConfiguration)
	host.routes.HandleFunc("POST /api/v2/connector-native-sessions", host.handleOpenUISession)
	host.routes.HandleFunc("POST /api/v2/connector-ui-sessions/{sessionNonce}/commands/{commandId}", host.handleProviderCommand)
	return host, nil
}

// Close releases the host's idle release connections.
func (host *ConfigurationHost) Close() {
	host.releaseHTTPClient.CloseIdleConnections()
}

// isMutation reports whether a route changes configuration.
func isMutation(method string) bool { return method != http.MethodGet }

// Invoke serves one fixed route that the caller mapped from a named,
// authenticated operation. It returns an error only when the stored source
// cannot serve the request; every other outcome is a document.
func (host *ConfigurationHost) Invoke(ctx context.Context, admission ConfigurationAdmission, method, path string, input []byte) (flowapi.Document, error) {
	// The commit names object keys of the prepared source and its draft.
	if admission.SourceCommit != "" && !flowid.IsCommitSHA(admission.SourceCommit) {
		return flowapi.CodedErrorDocument(http.StatusBadRequest, "RELEASE_CONFIGURATION_INVALID", "Configuration commit context is invalid"), nil
	}
	// Only the Live scope has a next-publish layer, and a layer request names the source it is checked against.
	if admission.NextPublish != nil && (host.project.scope.Kind != infra.ScopeLive.String() || admission.SourceCommit == "" ||
		admission.NextPublish.Source != nil && admission.NextPublish.Source.source.Manifest.SourceCommit != admission.SourceCommit) {
		return flowapi.CodedErrorDocument(http.StatusBadRequest, "PROJECT_CONFIGURATION_INVALID", "Configuration next-publish context is invalid"), nil
	}
	admitted := host.project.admit(admission)
	ctx = context.WithValue(ctx, admissionKey{}, admitted)
	if !admission.Internal && isMutation(method) {
		// An actor's form was built from one prepared source. The source is
		// immutable, so its bundle digest is the definition revision every graph
		// of this request reads; handlers parse graphs only when they need them.
		if admission.SourceCommit == "" {
			return flowapi.Document{}, errors.New("an actor mutation requires an exact prepared source")
		}
		source, err := host.project.readPreparedSource(ctx, admitted)
		if err != nil {
			return flowapi.Document{}, err
		}
		if admission.DefinitionRevision == "" || appmanifest.FlowDefinitionDigest(source.Definitions) != admission.DefinitionRevision {
			return flowapi.CodedErrorDocument(http.StatusConflict, "FLOW_DEFINITION_REVISION_CONFLICT", "Configuration source changed; reopen the form"), nil
		}
	}
	request := comm.Must(http.NewRequestWithContext(ctx, method, "http://configuration.internal"+path, bytes.NewReader(input)))
	recorder := &documentRecorder{header: make(http.Header), status: http.StatusOK}
	host.routes.ServeHTTP(recorder, request)
	if recorder.overflowed {
		return flowapi.Document{}, errors.New("configuration response exceeds its limit")
	}
	return flowapi.Document{StatusCode: recorder.status, JSON: recorder.body.Bytes()}, nil
}

// documentRecorder records one handler response, bounded so that a handler
// cannot build an unbounded document in memory.
type documentRecorder struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
	overflowed  bool
}

const maxDocumentBytes = 32 << 20

func (recorder *documentRecorder) Header() http.Header { return recorder.header }

func (recorder *documentRecorder) WriteHeader(status int) {
	if !recorder.wroteHeader {
		recorder.status, recorder.wroteHeader = status, true
	}
}

func (recorder *documentRecorder) Write(data []byte) (int, error) {
	recorder.wroteHeader = true
	if recorder.body.Len()+len(data) > maxDocumentBytes {
		recorder.overflowed = true
		return 0, errors.New("configuration response exceeds its limit")
	}
	return recorder.body.Write(data)
}

// ConfigurationAdmission is what the owning resource service resolved for one
// request after it authorized the account. It is never decoded from browser input.
type ConfigurationAdmission struct {
	// Internal admits the platform's own manifest, source and validation calls.
	Internal bool
	// ActorID is the authenticated account of a Studio request.
	ActorID string
	// TeamSpaceID is the TeamSpace of the scope's Project, whose connections
	// its connection names may select.
	TeamSpaceID string
	// SourceCommit selects a prepared source; every actor request names one,
	// and forms are bound to it.
	SourceCommit string
	// DefinitionRevision, AppManifestRevision and ConfigurationRevision are
	// what the actor's form was read at.
	DefinitionRevision                         string
	AppManifestRevision, ConfigurationRevision uint64
	// NextPublish addresses the Live scope's next-publish layer, checked
	// against SourceCommit's prepared source, with what it resolves from.
	NextPublish *NextPublish
}

// configurationRequest is one admitted request: its admission and the
// configuration it reads and edits, which the admission fixes for the whole
// request.
type configurationRequest struct {
	ConfigurationAdmission
	// drafts reads and writes the configuration the request edits: Live's
	// next-publish layer, the frozen copy of its Live commit, otherwise the
	// scope's canonical head.
	drafts *connectorconfig.ConfigurationStore
	// liveDraft reports that drafts is a Live commit's frozen copy.
	liveDraft bool
	// nextPublish is the admission's NextPublish when drafts is the layer.
	nextPublish *NextPublish
	// source caches the request's prepared source, which is immutable.
	source *preparedSource
}

type admissionKey struct{}

// requestOf returns the request Invoke admitted; every route is reached
// through it.
func requestOf(request *http.Request) *configurationRequest {
	admitted, found := request.Context().Value(admissionKey{}).(*configurationRequest)
	if !found {
		panic("configuration request was not admitted by Invoke")
	}
	return admitted
}

// requireInternal admits only the platform's internal calls.
func requireInternal(response http.ResponseWriter, request *http.Request) (*configurationRequest, bool) {
	admission := requestOf(request)
	if !admission.Internal {
		writeCodedError(response, http.StatusForbidden, "PROJECT_CONFIGURATION_FORBIDDEN", "Internal project configuration authorization is required")
		return nil, false
	}
	return admission, true
}

// requireReader admits internal calls and authenticated actors.
func requireReader(response http.ResponseWriter, request *http.Request) (*configurationRequest, bool) {
	admission := requestOf(request)
	if !admission.Internal && admission.ActorID == "" {
		writeCodedError(response, http.StatusForbidden, "PROJECT_CONFIGURATION_FORBIDDEN", "Authenticated project actor is required")
		return nil, false
	}
	return admission, true
}

// requireActor admits only authenticated actors and returns the admission.
func requireActor(response http.ResponseWriter, request *http.Request) (*configurationRequest, bool) {
	admission := requestOf(request)
	if admission.ActorID == "" {
		writeCodedError(response, http.StatusForbidden, "PROJECT_CONFIGURATION_FORBIDDEN", "Authenticated project actor is required")
		return nil, false
	}
	return admission, true
}

// admitConnection admits an actor's request for one connection that the
// request's prepared source declares, and returns its declaration. The actor's
// form must have been read at the source's AppManifest revision.
func (host *ConfigurationHost) admitConnection(response http.ResponseWriter, request *http.Request, key connectorconfig.ConnectionKey) (*configurationRequest, projectConnectorDeclaration, bool) {
	admission, admitted := requireActor(response, request)
	if !admitted {
		return nil, projectConnectorDeclaration{}, false
	}
	manifest, err := host.project.readManifest(request.Context(), admission)
	if err != nil {
		writeStoreError(response, err)
		return nil, projectConnectorDeclaration{}, false
	}
	if manifest.Revision != admission.AppManifestRevision {
		writeStoreError(response, connectorconfig.ErrConflict)
		return nil, projectConnectorDeclaration{}, false
	}
	for _, declaration := range manifest.Manifest.Connectors {
		if declaration.ConnectorID == key.ConnectorID && declaration.ConnectionName == key.ConnectionName {
			return admission, declaration, true
		}
	}
	writeCodedError(response, http.StatusNotFound, "CONNECTOR_CONNECTION_UNSUPPORTED", "Connection is not declared by the AppManifest")
	return nil, projectConnectorDeclaration{}, false
}

// pathConnection is the connection a route's path names.
func pathConnection(request *http.Request) connectorconfig.ConnectionKey {
	return connectorconfig.ConnectionKey{ConnectorID: request.PathValue("connectorId"), ConnectionName: request.PathValue("connectionName")}
}

// decodeBody decodes a request body of at most limit bytes strictly.
func decodeBody(response http.ResponseWriter, request *http.Request, limit int64, target any) error {
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, limit))
	if err != nil {
		return err
	}
	return comm.DecodeStrictJSON(body, target)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	writeDocument(response, flowapi.JSONDocument(status, value))
}

func writeCodedError(response http.ResponseWriter, status int, code, message string) {
	writeDocument(response, flowapi.CodedErrorDocument(status, code, message))
}

func writeDocument(response http.ResponseWriter, document flowapi.Document) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(document.StatusCode)
	// The recorder reports a failed write to Invoke.
	_, _ = response.Write(document.JSON)
}

// writeStoreError reports a failed read or write of stored configuration or
// credentials. An unexpected failure keeps the object store's error, which
// names the object key but never its contents.
func writeStoreError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, connectorconfig.ErrConflict):
		writeCodedError(response, http.StatusConflict, "PROJECT_CONFIGURATION_REVISION_CONFLICT", "Project configuration changed; reload before continuing")
	case errors.Is(err, connectorconfig.ErrObjectNotFound):
		writeCodedError(response, http.StatusNotFound, "PROJECT_CONFIGURATION_NOT_FOUND", "Project configuration has not been prepared")
	default:
		writeCodedError(response, http.StatusServiceUnavailable, "PROJECT_CONFIGURATION_UNAVAILABLE", "Project configuration is unavailable: "+err.Error())
	}
}

// writeDefinitionError reports Flow Definitions that are invalid or could not be read.
func writeDefinitionError(response http.ResponseWriter, err error) {
	if errors.Is(err, fdg.ErrInvalidBundle) {
		writeCodedError(response, http.StatusServiceUnavailable, "FLOW_DEFINITION_INVALID", "Flow Definition source is invalid: "+err.Error())
		return
	}
	writeCodedError(response, http.StatusServiceUnavailable, "FLOW_DEFINITION_SOURCE_UNAVAILABLE", "Flow Definition source is unavailable: "+err.Error())
}

// writeReleaseUnavailable reports a Connector release that could not be
// resolved and verified, with the download or verification failure.
func writeReleaseUnavailable(response http.ResponseWriter, err error) {
	writeCodedError(response, http.StatusBadGateway, "CONNECTOR_RELEASE_UNAVAILABLE", "Verified connector release is unavailable: "+err.Error())
}
