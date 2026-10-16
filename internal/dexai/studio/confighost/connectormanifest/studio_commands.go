// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectormanifest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
)

// providerFixedHeaderValueLimit bounds a fixed header value a Studio command sends.
const providerFixedHeaderValueLimit = 256

// validateStudioCommands admits a release's Studio commands once, when the
// release is downloaded: each is a uniquely named, read-only GET with a
// capability, its parameters fill exactly one URL placeholder or a query
// parameter that no fixed query value holds, and its credential and fixed
// headers name no header Dex owns.
func validateStudioCommands(studio *ManifestStudio) error {
	if studio == nil {
		return nil
	}
	commandIDs := make(map[string]bool, len(studio.Commands))
	for _, command := range studio.Commands {
		if command.ID == "" || commandIDs[command.ID] || command.Capability == "" || command.Request.Method != http.MethodGet {
			return fmt.Errorf("connector Studio command %q must be a uniquely named GET with a capability", command.ID)
		}
		commandIDs[command.ID] = true
		if err := validateStudioRequestParameters(command.Request); err != nil {
			return fmt.Errorf("connector Studio command %q parameters are invalid: %w", command.ID, err)
		}
		if err := validateStudioRequestHeaders(command.Request); err != nil {
			return fmt.Errorf("connector Studio command %q request headers are invalid: %w", command.ID, err)
		}
	}
	return nil
}

// validateStudioRequestParameters admits a request's parameters. A path
// parameter replaces its placeholder once, so two of them cannot share one.
func validateStudioRequestParameters(request manifestStudioHTTPRequest) error {
	pathTargets := make(map[string]bool, len(request.Parameters))
	for _, parameter := range request.Parameters {
		switch parameter.Location {
		case "path":
			if pathTargets[parameter.Target] || strings.Count(request.URL, "{"+parameter.Target+"}") != 1 {
				return fmt.Errorf("path parameter %q must fill exactly one URL placeholder", parameter.Name)
			}
			pathTargets[parameter.Target] = true
		case "query":
			if _, fixed := request.FixedQuery[parameter.Target]; fixed {
				return fmt.Errorf("query parameter %q cannot replace a fixed query value", parameter.Name)
			}
		default:
			return fmt.Errorf("parameter %q location must be path or query", parameter.Name)
		}
	}
	return nil
}

// Dex owns credentials, routing, framing and browser security headers.
var (
	forbiddenProviderHeaderNames = map[string]bool{
		"accept": true, "authorization": true, "connection": true, "content-length": true, "cookie": true,
		"forwarded": true, "host": true, "keep-alive": true, "origin": true, "referer": true,
		"set-cookie": true, "te": true, "trailer": true, "transfer-encoding": true, "upgrade": true,
		"x-http-method": true, "x-http-method-override": true, "x-method-override": true,
	}
	forbiddenProviderHeaderPrefixes = []string{"proxy-", "x-forwarded-"}
	// providerHeaderNamePattern matches RFC 7230 token characters.
	providerHeaderNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
)

func validateStudioRequestHeaders(request manifestStudioHTTPRequest) error {
	credentialHeader := ""
	switch request.Credential.Scheme {
	case "bearer":
		if request.Credential.Header != "" {
			return errors.New("bearer credential cannot name a header")
		}
	case "header":
		if !isAllowedProviderHeaderName(request.Credential.Header) {
			return fmt.Errorf("credential header name %q is not allowed", request.Credential.Header)
		}
		credentialHeader = foldHeaderName(request.Credential.Header)
	default:
		return errors.New("credential scheme must be bearer or header")
	}
	fixedHeaders := make(map[string]bool, len(request.FixedHeaders))
	for name, value := range request.FixedHeaders {
		folded := foldHeaderName(name)
		if !isAllowedProviderHeaderName(name) || folded == credentialHeader || fixedHeaders[folded] {
			return fmt.Errorf("fixed header name %q is not allowed", name)
		}
		fixedHeaders[folded] = true
		if value == "" || len(value) > providerFixedHeaderValueLimit || value[0] == ' ' || value[len(value)-1] == ' ' || !printableASCII(value) {
			return fmt.Errorf("fixed header %q value is invalid", name)
		}
	}
	return nil
}

func isAllowedProviderHeaderName(name string) bool {
	if !providerHeaderNamePattern.MatchString(name) {
		return false
	}
	folded := foldHeaderName(name)
	if forbiddenProviderHeaderNames[folded] {
		return false
	}
	for _, prefix := range forbiddenProviderHeaderPrefixes {
		if strings.HasPrefix(folded, prefix) {
			return false
		}
	}
	return true
}

// foldHeaderName compares header names the way CGI-style servers do, which
// merge "_" and "-".
func foldHeaderName(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "_", "-")
}

// printableASCII reports whether value holds only printable ASCII, as every
// header value a Studio command sends must.
func printableASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

// StudioCommandClient runs Studio commands through the provider client and
// its address policy, following a provider redirect only to the same HTTPS
// host; every redirect dials through the policy again.
func StudioCommandClient(providers *http.Client) *http.Client {
	commands := *providers
	commands.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 || request.URL.Scheme != "https" || !strings.EqualFold(request.URL.Hostname(), via[0].URL.Hostname()) {
			return errors.New("connector provider redirect is not allowed")
		}
		return nil
	}
	return &commands
}

// studioCommandResponseLimit bounds a Studio command's provider response.
const studioCommandResponseLimit = 4 << 20

var providerParameterPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// RunStudioCommand sends a Studio command's GET request with the stored
// credential through client and returns the provider's JSON object. Neither the request nor
// the response may expose the credential; the provider client's address
// policy decides where it may connect.
func RunStudioCommand(ctx context.Context, client *http.Client, command ManifestStudioCommand, parameters map[string]string, credentials map[string]json.RawMessage) (map[string]any, error) {
	target, err := providerCommandURL(command.Request, parameters)
	if err != nil {
		return nil, err
	}
	var storedCredential string
	if err = json.Unmarshal(credentials[command.Request.Credential.Field], &storedCredential); err != nil {
		return nil, errors.New("connector provider credential is unavailable")
	}
	// HTTP trims surrounding spaces on the wire; reflection checks must match
	// the value sent.
	credential := strings.Trim(storedCredential, " ")
	if credential == "" || !printableASCII(credential) {
		return nil, errors.New("connector provider credential is not a valid header value")
	}
	for _, value := range command.Request.FixedHeaders {
		if strings.Contains(value, credential) {
			return nil, errors.New("connector provider fixed header contains credential material")
		}
	}
	providerRequest := comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil))
	for name, value := range command.Request.FixedHeaders {
		providerRequest.Header.Set(name, value)
	}
	if command.Request.Credential.Scheme == "bearer" {
		providerRequest.Header.Set("Authorization", "Bearer "+credential)
	} else {
		providerRequest.Header.Set(command.Request.Credential.Header, credential)
	}
	providerRequest.Header.Set("Accept", "application/json")
	value, err := providerJSONObject(client, providerRequest, studioCommandResponseLimit, successStatus)
	if err != nil {
		return nil, fmt.Errorf("connector provider: %w", err)
	}
	if containsSecret(value, credential) {
		return nil, errors.New("connector provider response contains credential material")
	}
	return value, nil
}

// providerCommandURL fills a command's declared path and query parameters
// into its URL; parameters the command does not declare are rejected. The
// release download validated the declaration itself.
func providerCommandURL(declaration manifestStudioHTTPRequest, parameters map[string]string) (*url.URL, error) {
	targetText := declaration.URL
	queryParameters := map[string]string{}
	declared := make(map[string]bool, len(declaration.Parameters))
	for _, parameter := range declaration.Parameters {
		declared[parameter.Name] = true
		value := parameters[parameter.Name]
		if parameter.Location == "query" {
			if value != "" {
				queryParameters[parameter.Target] = value
			}
			continue
		}
		if value == "" || !providerParameterPattern.MatchString(value) {
			return nil, errors.New("connector provider path parameter is invalid")
		}
		targetText = strings.Replace(targetText, "{"+parameter.Target+"}", url.PathEscape(value), 1)
	}
	for name := range parameters {
		if !declared[name] {
			return nil, errors.New("connector provider parameter is not declared")
		}
	}
	target, err := url.Parse(targetText)
	if err != nil || target.Host == "" || target.User != nil || target.Fragment != "" || target.RawQuery != "" || strings.ContainsAny(target.Path, "{}") {
		return nil, errors.New("connector provider URL is invalid")
	}
	query := target.Query()
	for name, value := range declaration.FixedQuery {
		query.Set(name, value)
	}
	for name, value := range queryParameters {
		query.Set(name, value)
	}
	target.RawQuery = query.Encode()
	return target, nil
}

func containsSecret(value any, secret string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, secret)
	case []any:
		for _, item := range typed {
			if containsSecret(item, secret) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if strings.Contains(key, secret) || containsSecret(item, secret) {
				return true
			}
		}
	}
	return false
}
