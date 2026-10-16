// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectormanifest

import "regexp"

// Release is the checksum-verified metadata of one official Connector release.
type Release struct {
	ConnectorID    string          `json:"connectorId"`
	Manifest       ReleaseManifest `json:"manifest"`
	ModulePath     string          `json:"modulePath"`
	Version        string          `json:"version"`
	Tag            string          `json:"tag"`
	SourceSHA      string          `json:"sourceSha"`
	ManifestSHA256 string          `json:"manifestSha256"`
	// AuthMethods lists the credential fields of each method that a host
	// keeps from applications; a release published before the list has none.
	AuthMethods []releaseAuthMethod `json:"authMethods"`
}

// releaseAuthMethod is one method's entry of a release's metadata. As the
// library's schema.Auth.AuthMethods defines it, a manifest without declared
// methods has one entry, spec.auth itself, with the empty ID, which is the ID
// ManifestAuth.Method gives that method. HostKeptCredentials is nil when the
// field is absent and empty when the method keeps nothing.
type releaseAuthMethod struct {
	ID                  string    `json:"id"`
	HostKeptCredentials *[]string `json:"hostKeptCredentials"`
}

// HostKeptCredentials names the credential fields of the method methodID, a
// resolved method's ID, that a host keeps from the applications reading a
// shared connection, exactly as the release lists them; the host never
// derives them itself. declared is false for a release without the entry or
// its field, which predates shared connections and must not back one.
func (release Release) HostKeptCredentials(methodID string) (kept []string, declared bool) {
	for _, method := range release.AuthMethods {
		if method.ID == methodID && method.HostKeptCredentials != nil {
			return *method.HostKeptCredentials, true
		}
	}
	return nil, false
}

// StudioCommand is the read-only Studio command commandID the release
// declares.
func (release Release) StudioCommand(commandID string) (ManifestStudioCommand, bool) {
	if release.Manifest.Spec.Studio != nil {
		for _, command := range release.Manifest.Spec.Studio.Commands {
			if command.ID == commandID {
				return command, true
			}
		}
	}
	return ManifestStudioCommand{}, false
}

// ReleaseManifest is a release's Connector manifest. Studio renders
// its connection forms from these fields.
type ReleaseManifest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	} `json:"metadata"`
	Spec struct {
		Provider      string            `json:"provider"`
		Triggers      []ManifestTrigger `json:"triggers,omitempty"`
		Configuration ManifestFieldSet  `json:"configuration"`
		Auth          ManifestAuth      `json:"auth"`
		Studio        *ManifestStudio   `json:"studio,omitempty"`
	} `json:"spec"`
}

// Trigger returns the release's trigger name.
func (manifest ReleaseManifest) Trigger(name string) (ManifestTrigger, bool) {
	for _, trigger := range manifest.Spec.Triggers {
		if trigger.Name == name {
			return trigger, true
		}
	}
	return ManifestTrigger{}, false
}

// ManifestFieldSet declares the fields of a configuration, an
// authentication method's configuration or a trigger binding.
type ManifestFieldSet struct {
	Fields []ManifestField `json:"fields"`
}

// manifestFieldType is the type of a field a release declares. It decides the JSON
// value the field holds.
type manifestFieldType string

const (
	fieldTypeString       manifestFieldType = "string"
	fieldTypeURL          manifestFieldType = "url"
	fieldTypeDuration     manifestFieldType = "duration"
	fieldTypeEnum         manifestFieldType = "enum"
	fieldTypeSecretString manifestFieldType = "secretString"
	fieldTypeInteger      manifestFieldType = "integer"
	fieldTypeBoolean      manifestFieldType = "boolean"
	fieldTypeStringList   manifestFieldType = "stringList"
	fieldTypeStringMap    manifestFieldType = "stringMap"
)

// holdsString reports whether the values of a field of this type are JSON
// strings.
func (fieldType manifestFieldType) holdsString() bool {
	switch fieldType {
	case fieldTypeString, fieldTypeURL, fieldTypeDuration, fieldTypeEnum, fieldTypeSecretString:
		return true
	default:
		return false
	}
}

type ManifestField struct {
	UniqueItems bool                     `json:"uniqueItems,omitempty"`
	Name        string                   `json:"name"`
	Type        manifestFieldType        `json:"type"`
	Description string                   `json:"description"`
	Required    bool                     `json:"required"`
	Default     any                      `json:"default,omitempty"`
	Enum        []string                 `json:"enum,omitempty"`
	StudioUnit  *manifestFieldStudioUnit `json:"studioUnit,omitempty"`
}

type manifestFieldStudioUnit struct {
	Unit string `json:"unit"`
	Port string `json:"port"`
}

// ManifestTrigger is a trigger the release publishes. Its binding configuration
// is the Flow's: the FDG composes Connector UI units for each binding.
type ManifestTrigger struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ManifestAuth declares either one authentication method in place or
// several alternative methods. Every connection selects exactly one: a declared
// method ID, or the empty ID for a release without methods.
type ManifestAuth struct {
	Type              string                  `json:"type,omitempty"`
	ConnectionKind    string                  `json:"connectionKind,omitempty"`
	Fields            []ManifestField         `json:"fields,omitempty"`
	Guide             *authorizationGuide     `json:"guide,omitempty"`
	OAuth2            *ManifestOAuth2         `json:"oauth2,omitempty"`
	ClientCredentials *ClientCredentialsGrant `json:"clientCredentials,omitempty"`
	ServiceAccount    *ServiceAccountGrants   `json:"serviceAccount,omitempty"`
	DefaultMethod     string                  `json:"defaultMethod,omitempty"`
	MethodLabel       string                  `json:"methodLabel,omitempty"`
	Methods           []ManifestAuthMethod    `json:"methods,omitempty"`
}

// AuthTypeOAuth2 is the authentication type whose credentials an OAuth
// authorization fills.
const AuthTypeOAuth2 = "oauth2"

// The token response members the platform reads, as OAuth names them.
const (
	TokenMemberAccessToken  = "access_token"
	TokenMemberRefreshToken = "refresh_token"
)

type ManifestAuthMethod struct {
	ID                string                  `json:"id"`
	DisplayName       string                  `json:"displayName"`
	Description       string                  `json:"description"`
	Recommended       bool                    `json:"recommended,omitempty"`
	Type              string                  `json:"type"`
	ConnectionKind    string                  `json:"connectionKind"`
	Fields            []ManifestField         `json:"fields"`
	Configuration     *ManifestFieldSet       `json:"configuration,omitempty"`
	Guide             *authorizationGuide     `json:"guide,omitempty"`
	OAuth2            *ManifestOAuth2         `json:"oauth2,omitempty"`
	ClientCredentials *ClientCredentialsGrant `json:"clientCredentials,omitempty"`
	ServiceAccount    *ServiceAccountGrants   `json:"serviceAccount,omitempty"`
}

type authorizationGuide struct {
	StartURL string   `json:"startURL"`
	Steps    []string `json:"steps"`
}

// Method returns the declared method methodID selects. A release with one
// method in place accepts only the empty ID; otherwise empty selects the
// default method.
func (auth ManifestAuth) Method(methodID string) (ManifestAuthMethod, bool) {
	if len(auth.Methods) == 0 {
		if methodID != "" {
			return ManifestAuthMethod{}, false
		}
		return ManifestAuthMethod{
			Type: auth.Type, ConnectionKind: auth.ConnectionKind, Fields: auth.Fields, Guide: auth.Guide, OAuth2: auth.OAuth2,
			ClientCredentials: auth.ClientCredentials, ServiceAccount: auth.ServiceAccount,
		}, true
	}
	if methodID == "" {
		methodID = auth.DefaultMethod
	}
	for _, method := range auth.Methods {
		if method.ID == methodID {
			return method, true
		}
	}
	return ManifestAuthMethod{}, false
}

// ConfigurationFields returns the connection's configuration fields with
// those of its selected method.
func ConfigurationFields(manifest ReleaseManifest, method ManifestAuthMethod) []ManifestField {
	fields := append([]ManifestField{}, manifest.Spec.Configuration.Fields...)
	if method.Configuration != nil {
		fields = append(fields, method.Configuration.Fields...)
	}
	return fields
}

// ManifestOAuth2 declares a method's authorization code exchange and, with
// the same token endpoint, client and mappings, its refresh. Its endpoints
// and scopes are a literal or a connection setting; ResolveOAuth2 resolves
// them.
type ManifestOAuth2 struct {
	AuthorizationEndpoint   ValueSource                 `json:"authorizationEndpoint"`
	TokenEndpoint           ValueSource                 `json:"tokenEndpoint"`
	TokenRequestEncoding    string                      `json:"tokenRequestEncoding,omitempty"`
	TokenEndpointAuthMethod *ValueSource                `json:"tokenEndpointAuthMethod,omitempty"`
	AuthorizationParameters map[string]string           `json:"authorizationParameters,omitempty"`
	ClientIDCredential      string                      `json:"clientIDCredential,omitempty"`
	ClientSecretCredential  string                      `json:"clientSecretCredential,omitempty"`
	Scopes                  ValueSource                 `json:"scopes"`
	UserScopes              []string                    `json:"userScopes,omitempty"`
	CredentialMappings      []OAuthCredentialMapping    `json:"credentialMappings,omitempty"`
	CredentialDerivations   []oauthCredentialDerivation `json:"credentialDerivations,omitempty"`
	PKCE                    bool                        `json:"pkce"`
	Refresh                 *oauthRefresh               `json:"refresh,omitempty"`
}

// OAuthCredentialMapping copies a token response value into a credential.
type OAuthCredentialMapping struct {
	Credential string `json:"credential"`
	Source     string `json:"source"`
}

// oauthCredentialDerivation reads a credential from an identity
// endpoint with the granted token.
type oauthCredentialDerivation struct {
	Credential string      `json:"credential"`
	Endpoint   ValueSource `json:"endpoint"`
	Source     string      `json:"source"`
	VerifiedBy string      `json:"verifiedBy,omitempty"`
}

type ManifestStudio struct {
	Setup    manifestStudioSetup     `json:"setup"`
	Commands []ManifestStudioCommand `json:"commands,omitempty"`
	Units    []manifestStudioUnit    `json:"units,omitempty"`
}

type manifestStudioSetup struct {
	Entrypoint          string   `json:"entrypoint"`
	HostAPIRange        string   `json:"hostApiRange"`
	BackendCapabilities []string `json:"backendCapabilities"`
}

// ManifestStudioCommand is a read-only provider request a UI session
// may run with the connection's credential.
type ManifestStudioCommand struct {
	ID         string                    `json:"id"`
	Capability string                    `json:"capability"`
	Request    manifestStudioHTTPRequest `json:"request"`
}

type manifestStudioHTTPRequest struct {
	Method       string                    `json:"method"`
	URL          string                    `json:"url"`
	Credential   manifestStudioCredential  `json:"credential"`
	FixedQuery   map[string]string         `json:"fixedQuery,omitempty"`
	FixedHeaders map[string]string         `json:"fixedHeaders,omitempty"`
	Parameters   []manifestStudioParameter `json:"parameters,omitempty"`
}

type manifestStudioCredential struct {
	Field  string `json:"field"`
	Scheme string `json:"scheme"`
	Header string `json:"header,omitempty"`
}

type manifestStudioParameter struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	Target   string `json:"target"`
}

type manifestStudioUnit struct {
	ID                  string               `json:"id"`
	Description         string               `json:"description"`
	BackendCapabilities []string             `json:"backendCapabilities,omitempty"`
	Inputs              []manifestStudioPort `json:"inputs,omitempty"`
	Outputs             []manifestStudioPort `json:"outputs"`
}

type manifestStudioPort struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Configuration addresses connections by name and resolves only published
// Connector versions, a release or a release candidate, so it admits fewer
// manifests than source admission does.
var (
	officialConnectorModulePattern   = regexp.MustCompile(`^github\.com/superdurable/dex-connectors-library/connectors/[a-z0-9][a-z0-9-]*(?:/[a-z0-9][a-z0-9-]*)*$`)
	publishedConnectorVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[1-9][0-9]*)?$`)
	connectorReleaseIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	releaseCandidateVersionPattern   = regexp.MustCompile(`-rc\.[1-9][0-9]*$`)
)
