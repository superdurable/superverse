// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost"
)

// The internal routes of the configuration host that the platform's Flows
// call.
const (
	configurationRoute = "/api/v2/project-configuration"
	manifestRoute      = "/api/v2/project-configuration/app-manifest"
	sourcesRoute       = "/api/v2/project-configuration/sources"
	validateRoute      = "/api/v2/project-configuration/validate"
	requirementsRoute  = "/api/v2/project-configuration/requirements"
)

const (
	// maxPooledHosts bounds the scopes with an open configuration host.
	maxPooledHosts = 64
	// pooledHostIdle is how long an unused host stays open.
	pooledHostIdle = 20 * time.Minute
	// maxHostRequests bounds the concurrent requests of one host.
	maxHostRequests = 8
	// hostRequestTimeout bounds one internal configuration request.
	hostRequestTimeout = 30 * time.Second
	// maxHostErrorBytes bounds the host error document an error carries.
	maxHostErrorBytes = 1 << 10
)

// pooledHost is one scope's configuration host with its requests in flight.
type pooledHost struct {
	host   *confighost.ConfigurationHost
	used   time.Time
	active int
}

// invokeHost sends one internal request to the scope's configuration host and
// decodes its document into output. A sourceCommit addresses that commit's
// prepared source, and "" the scope's head.
func (c *Configuration) invokeHost(ctx context.Context, scope infra.ScopeRef, sourceCommit, method, route string, input, output any) error {
	return c.invokeHostAs(ctx, scope, confighost.ConfigurationAdmission{Internal: true, SourceCommit: sourceCommit}, method, route, input, output)
}

// invokeHostAs is invokeHost with an internal admission that may also name
// the Project's TeamSpace.
func (c *Configuration) invokeHostAs(ctx context.Context, scope infra.ScopeRef, admission confighost.ConfigurationAdmission, method, route string, input, output any) error {
	ctx, cancel := context.WithTimeout(ctx, hostRequestTimeout)
	defer cancel()
	host, release, err := c.acquireHost(ctx, scope)
	if err != nil {
		return err
	}
	defer release()
	var body []byte
	if input != nil {
		if body, err = objectstore.EncodeJSON(input); err != nil {
			return fmt.Errorf("encode the configuration host %s %s request: %w", method, route, err)
		}
	}
	document, err := host.Invoke(ctx, admission, method, route, body)
	if err != nil {
		return fmt.Errorf("configuration host request failed: %w", err)
	}
	if document.StatusCode == http.StatusOK {
		comm.MustDo(json.Unmarshal(document.JSON, output))
		return nil
	}
	// The host's error documents carry safe codes and causes; they never hold
	// configuration values or credentials.
	detail := strings.TrimSpace(comm.TruncateUTF8(string(document.JSON), maxHostErrorBytes))
	if document.StatusCode != http.StatusOK && hostErrorCode(document.JSON) == "FLOW_DEFINITION_INVALID" {
		// The host answers rejected Flow Definitions with the 503 of an
		// unavailable definition source; only its code tells them apart.
		return fmt.Errorf("%w: %s", ErrInvalidDefinitions, detail)
	}
	switch document.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", errNotFound, detail)
	case http.StatusConflict:
		if hostErrorCode(document.JSON) == confighost.PreviewRevisionChangedCode {
			return fmt.Errorf("%w: %w: %s", ErrRevisionChanged, ErrConflict, detail)
		}
		return fmt.Errorf("%w: %s", ErrConflict, detail)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", infra.ErrAccessDenied, detail)
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrNotReady, detail)
	default:
		return fmt.Errorf("configuration host %s %s answered status %d: %s", method, route, document.StatusCode, detail)
	}
}

// hostErrorCode is the safe code of a host error document, or "" for a
// document without one.
func hostErrorCode(document []byte) string {
	var answer struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(document, &answer) != nil {
		return ""
	}
	return answer.Code
}

// acquireHost returns the configuration host of scope and the release
// function the caller must call. Hosts hold only disposable provider
// sessions: configuration, OAuth attempts and exchange receipts stay in the
// scope's versioned storage, so closing a host loses nothing.
func (c *Configuration) acquireHost(ctx context.Context, scope infra.ScopeRef) (*confighost.ConfigurationHost, func(), error) {
	if err := c.Storage.VerifyProjectStorage(ctx, scope.ProjectID, scope.Storage); err != nil {
		return nil, nil, err
	}
	scopeKey := scope.ProjectID + "/" + scope.Scope.Kind.String() + "/"
	key := scopeKey + scope.Storage.Boundary.UID
	c.hostsMu.Lock()
	defer c.hostsMu.Unlock()
	if c.closed {
		return nil, nil, ErrNotReady
	}
	if c.hosts == nil {
		// The composition root builds Configuration as a struct literal.
		c.hosts = make(map[string]*pooledHost)
	}
	now := time.Now()
	for other, pooled := range c.hosts {
		// A host of the scope's replaced namespace can no longer serve it.
		if pooled.active == 0 && (now.Sub(pooled.used) > pooledHostIdle || strings.HasPrefix(other, scopeKey) && other != key) {
			pooled.host.Close()
			delete(c.hosts, other)
		}
	}
	pooled := c.hosts[key]
	if pooled == nil {
		if len(c.hosts) >= maxPooledHosts && !c.closeLeastRecentlyUsedHost() {
			return nil, nil, errors.New("configuration session capacity is busy")
		}
		objects, err := c.Storage.ConfigurationObjects(ctx, scope)
		if err != nil {
			return nil, nil, err
		}
		host, err := confighost.NewConfigurationHost(objects, connectorconfig.Scope{ProjectID: scope.ProjectID, Kind: scope.Scope.Kind.String()}, c.ReleaseCandidates, c.Shared)
		if err != nil {
			return nil, nil, err
		}
		pooled = &pooledHost{host: host}
		c.hosts[key] = pooled
	}
	if pooled.active >= maxHostRequests {
		return nil, nil, errors.New("configuration operation capacity is busy")
	}
	pooled.used, pooled.active = now, pooled.active+1
	return pooled.host, func() {
		c.hostsMu.Lock()
		defer c.hostsMu.Unlock()
		pooled.active--
		pooled.used = time.Now()
		if c.closed && pooled.active == 0 {
			pooled.host.Close()
			delete(c.hosts, key)
		}
	}, nil
}

// closeLeastRecentlyUsedHost closes the idle host used least recently. It
// reports false when every host has a request in flight.
func (c *Configuration) closeLeastRecentlyUsedHost() bool {
	oldest := ""
	for key, pooled := range c.hosts {
		if pooled.active == 0 && (oldest == "" || pooled.used.Before(c.hosts[oldest].used)) {
			oldest = key
		}
	}
	if oldest == "" {
		return false
	}
	c.hosts[oldest].host.Close()
	delete(c.hosts, oldest)
	return true
}

// Close closes idle hosts now and every other host when its last request
// ends. Afterwards no request acquires a host.
func (c *Configuration) Close() {
	c.hostsMu.Lock()
	defer c.hostsMu.Unlock()
	c.closed = true
	for key, pooled := range c.hosts {
		if pooled.active == 0 {
			pooled.host.Close()
			delete(c.hosts, key)
		}
	}
}
