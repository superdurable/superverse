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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
)

const (
	connectorReleaseMetadataName  = "connector-release.json"
	connectorReleaseDigestName    = "connector-release.json.sha256"
	connectorReleaseMetadataLimit = 1 << 20
	// verifiedReleaseCacheSize bounds the verified releases one host keeps.
	verifiedReleaseCacheSize = 128
)

// Module is the exact Connector release the AppManifest declares for
// one connection. The stored connection names only its module path.
type Module struct {
	ConnectorID, ConnectionName, ModulePath, ModuleVersion string
}

// Key is the connection the module serves.
func (module Module) Key() connectorconfig.ConnectionKey {
	return connectorconfig.ConnectionKey{ConnectorID: module.ConnectorID, ConnectionName: module.ConnectionName}
}

// IsPublishedRelease reports whether module names an exact official Connector
// release or release candidate, the only versions configuration resolves.
func (module Module) IsPublishedRelease() bool {
	return connectorReleaseIDPattern.MatchString(module.ConnectorID) && officialConnectorModulePattern.MatchString(module.ModulePath) &&
		publishedConnectorVersionPattern.MatchString(module.ModuleVersion)
}

// ReleaseResolver downloads and verifies official Connector release
// metadata, caching each verified release by its exact tag. A release comes
// from the connectors library and a release candidate from candidateBaseURL,
// where the server publishes the candidates of its contributions.
type ReleaseResolver struct {
	httpClient       *http.Client
	candidateBaseURL string
	mutex            sync.Mutex
	verified         map[string]Release
	// order is the cache keys in insertion order; the oldest is evicted first.
	order []string
}

// NewReleaseResolver downloads releases through httpClient, which the
// caller owns and closes, and release candidates from candidateBaseURL.
func NewReleaseResolver(httpClient *http.Client, candidateBaseURL string) *ReleaseResolver {
	return &ReleaseResolver{httpClient: httpClient, candidateBaseURL: candidateBaseURL, verified: make(map[string]Release)}
}

// Resolve returns the checksum-verified release of module. Studio renders the
// verified manifest itself; no remote UI bundle is downloaded or loaded.
func (resolver *ReleaseResolver) Resolve(ctx context.Context, module Module) (Release, error) {
	if !module.IsPublishedRelease() {
		return Release{}, errors.New("connector module is not an exact official release or release candidate")
	}
	tag := strings.TrimPrefix(module.ModulePath, "github.com/superdurable/dex-connectors-library/") + "/" + module.ModuleVersion
	cacheKey := module.ConnectorID + "/" + tag
	resolver.mutex.Lock()
	release, cached := resolver.verified[cacheKey]
	resolver.mutex.Unlock()
	if cached {
		return release, nil
	}
	release, err := resolver.download(ctx, module, tag)
	if err != nil {
		return Release{}, err
	}
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	if _, cached = resolver.verified[cacheKey]; !cached {
		if len(resolver.order) >= verifiedReleaseCacheSize {
			delete(resolver.verified, resolver.order[0])
			resolver.order = resolver.order[1:]
		}
		resolver.order = append(resolver.order, cacheKey)
	}
	resolver.verified[cacheKey] = release
	return release, nil
}

// download reads tag's release metadata and verifies it against its published
// checksum and the module it must describe.
func (resolver *ReleaseResolver) download(ctx context.Context, module Module, tag string) (Release, error) {
	baseURL := connectorlibrary.OfficialReleaseDownloadURL
	if releaseCandidateVersionPattern.MatchString(module.ModuleVersion) {
		baseURL = resolver.candidateBaseURL
	}
	digestFile, err := resolver.downloadAsset(ctx, baseURL+"/"+tag, connectorReleaseDigestName, 1024)
	if err != nil {
		return Release{}, err
	}
	fields := strings.Fields(string(digestFile))
	if len(fields) != 2 || fields[1] != connectorReleaseMetadataName || len(fields[0]) != sha256.Size*2 {
		return Release{}, errors.New("connector artifact digest is invalid")
	}
	if _, err = hex.DecodeString(fields[0]); err != nil {
		return Release{}, errors.New("connector artifact digest is invalid")
	}
	metadata, err := resolver.downloadAsset(ctx, baseURL+"/"+tag, connectorReleaseMetadataName, connectorReleaseMetadataLimit)
	if err != nil {
		return Release{}, err
	}
	actualDigest := sha256.Sum256(metadata)
	if hex.EncodeToString(actualDigest[:]) != strings.ToLower(fields[0]) {
		return Release{}, errors.New("connector release metadata checksum does not match")
	}
	var release Release
	if err = json.Unmarshal(metadata, &release); err != nil {
		return Release{}, fmt.Errorf("decode connector release metadata: %w", err)
	}
	if release.ConnectorID != module.ConnectorID || release.Manifest.Metadata.Name != module.ConnectorID ||
		release.ModulePath != module.ModulePath || release.Version != module.ModuleVersion || release.Tag != tag {
		return Release{}, errors.New("connector release identity does not match its module")
	}
	manifestDigest, err := hex.DecodeString(release.ManifestSHA256)
	if err != nil || len(manifestDigest) != sha256.Size || release.SourceSHA == "" {
		return Release{}, errors.New("connector release provenance is invalid")
	}
	if err = validateStudioCommands(release.Manifest.Spec.Studio); err != nil {
		return Release{}, err
	}
	return release, nil
}

// downloadAsset reads one asset of the release at releaseURL, at most limit bytes.
func (resolver *ReleaseResolver) downloadAsset(ctx context.Context, releaseURL, asset string, limit int64) ([]byte, error) {
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, releaseURL+"/"+asset, nil))
	response, err := resolver.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download connector artifact: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	contents, err := readResponseBody(response.Body, limit)
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download connector artifact: HTTP %d", response.StatusCode)
	}
	if err != nil {
		return nil, fmt.Errorf("read connector artifact: %w", err)
	}
	return contents, nil
}

// readResponseBody reads at most limit bytes of a provider response body; a
// longer body is an error. The caller closes the body.
func readResponseBody(body io.Reader, limit int64) ([]byte, error) {
	contents, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err == nil && int64(len(contents)) > limit {
		err = errors.New("provider response exceeds its size limit")
	}
	return contents, err
}

// NewReleaseHTTPClient downloads release assets, which GitHub redirects only
// to its content hosts.
func NewReleaseHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: releaseTransport(),
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			host := strings.ToLower(request.URL.Hostname())
			if len(via) >= 5 || host != "github.com" && !strings.HasSuffix(host, ".githubusercontent.com") {
				return errors.New("connector artifact redirect is not allowed")
			}
			return nil
		},
	}
}

// releaseTransport bounds the connections a client keeps to a release host.
func releaseTransport() *http.Transport {
	transport, _ := http.DefaultTransport.(*http.Transport)
	transport = transport.Clone()
	transport.MaxConnsPerHost = 8
	transport.MaxIdleConnsPerHost = 4
	transport.MaxIdleConns = 16
	return transport
}
