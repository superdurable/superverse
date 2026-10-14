// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectormanifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/superdurable/superversev2/internal/comm"
)

// A release declares an OAuth endpoint, a scope list, a token request setting
// or a JWT bearer claim as a value source: a literal string or list, or where
// the host finds the value. Release metadata writes a literal as a string or
// an array and every other source as an object, as the connectors library's
// schema.ValueSource does. The oauth2 fields of a release accept only a
// literal or {fromSetting}, the forms Dex Web reads too.

// ValueSource is a literal or where a host resolves a value: a credential
// field, a connection setting, a member of the JSON key in a JWT bearer
// grant's keyCredential, or a template whose placeholders name credential
// fields or settings. Values translates a resolved credential, setting or
// key member; the key "" translates a blank value.
type ValueSource struct {
	// Value is a literal string and List a literal list; they are written
	// without an object.
	Value          string            `json:"-"`
	List           []string          `json:"-"`
	FromCredential string            `json:"fromCredential,omitempty"`
	FromSetting    string            `json:"fromSetting,omitempty"`
	FromKey        string            `json:"fromKey,omitempty"`
	Template       string            `json:"template,omitempty"`
	Values         map[string]string `json:"values,omitempty"`
}

// sourceCount counts the sources that are set; a valid source has one.
func (source ValueSource) sourceCount() int {
	count := 0
	for _, isSet := range []bool{source.Value != "", source.List != nil, source.FromCredential != "", source.FromSetting != "", source.FromKey != "", source.Template != ""} {
		if isSet {
			count++
		}
	}
	return count
}

// isEmpty reports a source the release did not declare.
func (source ValueSource) isEmpty() bool { return source.sourceCount() == 0 }

// UnmarshalJSON reads a literal string, a literal array, or an object that
// names exactly one source.
func (source *ValueSource) UnmarshalJSON(contents []byte) error {
	trimmed := bytes.TrimSpace(contents)
	switch {
	case len(trimmed) > 0 && trimmed[0] == '"':
		*source = ValueSource{}
		return json.Unmarshal(trimmed, &source.Value)
	case len(trimmed) > 0 && trimmed[0] == '[':
		*source = ValueSource{List: []string{}}
		return json.Unmarshal(trimmed, &source.List)
	}
	type valueSourceObject ValueSource
	var object valueSourceObject
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&object); err != nil {
		return fmt.Errorf("value source: %w", err)
	}
	if ValueSource(object).sourceCount() != 1 {
		return errors.New("value source: an object must name exactly one source")
	}
	*source = ValueSource(object)
	return nil
}

// MarshalJSON writes a literal as a string, a literal list as an array and
// every other source as an object, as release metadata does.
func (source ValueSource) MarshalJSON() ([]byte, error) {
	switch {
	case source.Value != "":
		return json.Marshal(source.Value)
	case source.List != nil:
		return json.Marshal(source.List)
	}
	type valueSourceObject ValueSource
	return json.Marshal(valueSourceObject(source))
}

// isLiteralOrSetting reports the only forms an oauth2 field may take.
func (source ValueSource) isLiteralOrSetting() bool {
	return source.sourceCount() == 1 && len(source.Values) == 0 && (source.Value != "" || source.List != nil || source.FromSetting != "")
}

// settingError reports a connection setting that a release reads but that
// holds no usable value. It names the setting, never its value.
type settingError struct {
	setting, reason string
}

func (err *settingError) Error() string {
	return fmt.Sprintf("connection setting %q %s", err.setting, err.reason)
}

// ResolvedOAuth2 is a method's OAuth declarations with every value source
// resolved from a connection's settings. The *FromSetting fields report the
// endpoints a setting supplied, which may be plain http where a profile allows
// it; a release's literal endpoints are https.
type ResolvedOAuth2 struct {
	AuthorizationEndpoint, TokenEndpoint                       string
	authorizationEndpointFromSetting, tokenEndpointFromSetting bool
	Scopes, UserScopes                                         []string
	TokenEndpointAuthMethod, TokenRequestEncoding              string
	authorizationParameters                                    map[string]string
	ClientIDCredential, ClientSecretCredential                 string
	credentialMappings                                         []OAuthCredentialMapping
	credentialDerivations                                      []resolvedDerivation
	pkce                                                       bool
	refresh                                                    *oauthRefresh
	// scopesSetting names the setting the scopes come from, or "".
	scopesSetting string
}

// resolvedDerivation is an identity read whose endpoint is resolved.
type resolvedDerivation struct {
	credential, endpoint, source, verifiedBy string
}

// ResolveOAuth2 resolves oauth's value sources from settings, the values of a
// connection's configuration fields, with the rules of Dex Web: a setting
// takes its saved value or, when missing or null, its field's default; an
// endpoint from a setting is an absolute http or https URL without user
// information, a fragment or spaces, and a literal endpoint is https; scopes
// from a setting are a non-empty list without blank or spaced entries; a token
// request option must be one of its values.
func ResolveOAuth2(oauth ManifestOAuth2, fields []ManifestField, settings map[string]json.RawMessage) (ResolvedOAuth2, error) {
	resolver := settingResolver{fields: fields, settings: settings}
	resolved := ResolvedOAuth2{
		UserScopes: oauth.UserScopes, authorizationParameters: oauth.AuthorizationParameters, ClientIDCredential: oauth.ClientIDCredential,
		ClientSecretCredential: oauth.ClientSecretCredential, credentialMappings: oauth.CredentialMappings, pkce: oauth.PKCE, refresh: oauth.Refresh,
		scopesSetting: oauth.Scopes.FromSetting, TokenRequestEncoding: oauth.TokenRequestEncoding,
		authorizationEndpointFromSetting: oauth.AuthorizationEndpoint.FromSetting != "", tokenEndpointFromSetting: oauth.TokenEndpoint.FromSetting != "",
	}
	var err error
	if resolved.AuthorizationEndpoint, err = resolver.endpoint(oauth.AuthorizationEndpoint); err != nil {
		return ResolvedOAuth2{}, err
	}
	if resolved.TokenEndpoint, err = resolver.endpoint(oauth.TokenEndpoint); err != nil {
		return ResolvedOAuth2{}, err
	}
	if resolved.Scopes, err = resolver.scopes(oauth.Scopes); err != nil {
		return ResolvedOAuth2{}, err
	}
	resolved.TokenEndpointAuthMethod = clientSecretPostAuthMethod
	if oauth.TokenEndpointAuthMethod != nil {
		if resolved.TokenEndpointAuthMethod, err = resolver.option(*oauth.TokenEndpointAuthMethod, clientSecretPostAuthMethod, clientSecretBasicAuthMethod); err != nil {
			return ResolvedOAuth2{}, err
		}
	}
	for _, derivation := range oauth.CredentialDerivations {
		endpoint, err := resolver.endpoint(derivation.Endpoint)
		if err != nil {
			return ResolvedOAuth2{}, err
		}
		resolved.credentialDerivations = append(resolved.credentialDerivations, resolvedDerivation{
			credential: derivation.Credential, endpoint: endpoint, source: derivation.Source, verifiedBy: derivation.VerifiedBy,
		})
	}
	return resolved, nil
}

// settingResolver reads the values of a connection's settings.
type settingResolver struct {
	fields   []ManifestField
	settings map[string]json.RawMessage
}

// value is a setting's saved JSON, or its field's default when the value is
// missing or null; nil when it has neither.
func (resolver settingResolver) value(name string) json.RawMessage {
	if raw := resolver.settings[name]; len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return raw
	}
	for _, field := range resolver.fields {
		if field.Name == name && field.Default != nil {
			if encoded, err := json.Marshal(field.Default); err == nil {
				return encoded
			}
		}
	}
	return nil
}

func (resolver settingResolver) text(name string) (string, error) {
	var value string
	if raw := resolver.value(name); raw != nil && json.Unmarshal(raw, &value) != nil {
		return "", &settingError{setting: name, reason: "must be a string"}
	}
	return value, nil
}

func (resolver settingResolver) endpoint(source ValueSource) (string, error) {
	if source.FromSetting == "" {
		return source.Value, nil
	}
	value, err := resolver.text(source.FromSetting)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", &settingError{setting: source.FromSetting, reason: "is missing or blank"}
	}
	if err = validSettingEndpoint(value); err != nil {
		return "", &settingError{setting: source.FromSetting, reason: err.Error()}
	}
	return value, nil
}

func (resolver settingResolver) scopes(source ValueSource) ([]string, error) {
	if source.FromSetting == "" {
		return source.List, nil
	}
	var values []string
	if raw := resolver.value(source.FromSetting); raw != nil && json.Unmarshal(raw, &values) != nil {
		return nil, &settingError{setting: source.FromSetting, reason: "must be a list of strings"}
	}
	if len(values) == 0 {
		return nil, &settingError{setting: source.FromSetting, reason: "is missing or blank"}
	}
	for _, scope := range values {
		if scope == "" || strings.ContainsFunc(scope, unicode.IsSpace) {
			return nil, &settingError{setting: source.FromSetting, reason: "must list scopes that are not blank and contain no spaces"}
		}
	}
	return values, nil
}

// option resolves a token request option. A blank value takes the field's
// default, then the first supported value.
func (resolver settingResolver) option(source ValueSource, supported ...string) (string, error) {
	value := source.Value
	if source.FromSetting != "" {
		var err error
		if value, err = resolver.text(source.FromSetting); err != nil {
			return "", err
		}
	}
	if value == "" {
		return supported[0], nil
	}
	if !slices.Contains(supported, value) {
		return "", &settingError{setting: source.FromSetting, reason: "must be " + strings.Join(supported, " or ")}
	}
	return value, nil
}

// validSettingEndpoint admits an endpoint a setting supplies: an absolute
// http or https URL with a host and without user information, a fragment or
// spaces. The address policy of the HTTP client decides where it may connect.
func validSettingEndpoint(value string) error {
	target, err := url.Parse(value)
	if err != nil {
		return errors.New("is not a URL")
	}
	if target.Scheme != "https" && target.Scheme != "http" || target.Hostname() == "" || target.User != nil ||
		strings.Contains(value, "#") || strings.ContainsFunc(value, unicode.IsSpace) {
		return errors.New("must be an absolute HTTP or HTTPS URL with a host, without user information or a fragment")
	}
	return nil
}

// GrantValues are what a minting grant's value sources read: the method's
// stored credentials, the connection's settings and the JSON key of its
// keyCredential.
type GrantValues struct {
	credentials map[string]json.RawMessage
	settings    settingResolver
	key         map[string]any
}

// NewGrantValues reads credentials, settings with their fields' defaults and,
// when keyCredential names a credential, the JSON key it holds.
func NewGrantValues(credentials map[string]json.RawMessage, fields []ManifestField, settings map[string]json.RawMessage, keyCredential string) (GrantValues, error) {
	values := GrantValues{credentials: credentials, settings: settingResolver{fields: fields, settings: settings}}
	if keyCredential == "" {
		return values, nil
	}
	var key string
	if err := json.Unmarshal(credentials[keyCredential], &key); err != nil || key == "" {
		return values, fmt.Errorf("credential %q holds no JSON key", keyCredential)
	}
	if err := json.Unmarshal([]byte(key), &values.key); err != nil {
		return values, fmt.Errorf("credential %q is not a JSON key object", keyCredential)
	}
	return values, nil
}

// templatePlaceholder matches one {name} of a template.
var templatePlaceholder = regexp.MustCompile(`\{([^{}]*)\}`)

// Resolve returns the value source names. A template inserts each credential
// field or setting path-escaped; a translation table must translate the
// value.
func (values GrantValues) Resolve(source ValueSource) (string, error) {
	var value string
	switch {
	case source.Value != "":
		return source.Value, nil
	case source.Template != "":
		var failure error
		resolved := templatePlaceholder.ReplaceAllStringFunc(source.Template, func(placeholder string) string {
			name := strings.Trim(placeholder, "{}")
			inserted, err := values.named(name)
			if err != nil || inserted == "" {
				failure = errors.Join(failure, fmt.Errorf("template value %q is missing", name))
			}
			return url.PathEscape(inserted)
		})
		return resolved, failure
	case source.FromCredential != "":
		value = values.credential(source.FromCredential)
	case source.FromSetting != "":
		text, err := values.settings.text(source.FromSetting)
		if err != nil {
			return "", err
		}
		value = text
	case source.FromKey != "":
		text, isText := values.key[source.FromKey].(string)
		if !isText {
			return "", fmt.Errorf("key member %q is not a string", source.FromKey)
		}
		value = text
	default:
		return "", errors.New("the value source names nothing")
	}
	if source.Values == nil {
		return value, nil
	}
	translated, found := source.Values[value]
	if !found {
		return "", errors.New("the value source has no translation of the resolved value")
	}
	return translated, nil
}

// named is a credential field's text, else a setting's.
func (values GrantValues) named(name string) (string, error) {
	if value := values.credential(name); value != "" {
		return value, nil
	}
	return values.settings.text(name)
}

func (values GrantValues) credential(name string) string {
	var value string
	_ = json.Unmarshal(values.credentials[name], &value)
	return value
}

// CredentialSettings returns the settings that a method's grants read: every
// fromSetting source and every template placeholder of its OAuth, client
// credentials and service account declarations, such as a token endpoint or
// the scopes. Changing one changes where or how the credential is used.
func CredentialSettings(method ManifestAuthMethod) []string {
	var names []string
	var walk func(value any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, member := range typed {
				switch text, isText := member.(string); {
				case key == "fromSetting" && isText:
					names = append(names, text)
				case key == "template" && isText:
					for _, match := range templatePlaceholder.FindAllStringSubmatch(text, -1) {
						names = append(names, match[1])
					}
				default:
					walk(member)
				}
			}
		case []any:
			for _, member := range typed {
				walk(member)
			}
		}
	}
	for _, declaration := range []any{method.OAuth2, method.ClientCredentials, method.ServiceAccount} {
		var decoded any
		comm.MustDo(json.Unmarshal(comm.Must(json.Marshal(declaration)), &decoded))
		walk(decoded)
	}
	slices.Sort(names)
	return slices.Compact(names)
}
