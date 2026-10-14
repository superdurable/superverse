// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package build

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/superdurable/superversev2/internal/platform/objectstore"
)

// The bp1 recipe is the imported go-react-v1 profile; the reviewed hosted bp2
// recipe reuses its Dockerfile with a Docker-configuration manifest, and the
// local bp3 recipe is bp1 with the module redirect of the local lanes.
var (
	//go:embed build-profiles/go-react-v1/profile.json
	publishedProfileManifest []byte
	//go:embed build-profiles/go-react-v1/Dockerfile
	publishedProfileDockerfile []byte
	//go:embed hosted/profile-bp2.json
	hostedProfileManifest []byte
	//go:embed build-profiles/go-react-v1/local/profile.json
	localProfileManifest []byte
	//go:embed build-profiles/go-react-v1/local/Dockerfile
	localProfileDockerfile []byte
)

// Profile is one immutable build recipe. Digest covers its manifest and
// Dockerfile bytes, so an accepted build always rebuilds with the same recipe.
type Profile struct {
	Version, ID, Digest  string
	Manifest, Dockerfile []byte
}

// Profiles holds the registered recipes and the version new builds select.
type Profiles struct {
	profiles       map[string]Profile
	defaultVersion string
}

func NewProfiles() *Profiles {
	published := mustParseProfile("bp1", publishedProfileManifest, publishedProfileDockerfile)
	return &Profiles{profiles: map[string]Profile{published.Version: published}, defaultVersion: published.Version}
}

// SelectHostedDefault selects the reviewed Docker configuration recipe for new
// hosted admissions. The bp1 recipe remains registered for its accepted records.
func (p *Profiles) SelectHostedDefault() {
	hosted := mustParseProfile("bp2", hostedProfileManifest, publishedProfileDockerfile)
	p.profiles[hosted.Version] = hosted
	p.defaultVersion = hosted.Version
}

// SelectLocalDefault selects the recipe of the local builder for new local
// admissions, which builds a release candidate that only the local Git host's
// copy of the connectors library holds. The bp1 recipe remains registered for
// its accepted records.
func (p *Profiles) SelectLocalDefault() {
	local := mustParseProfile("bp3", localProfileManifest, localProfileDockerfile)
	p.profiles[local.Version] = local
	p.defaultVersion = local.Version
}

// Default returns the composition-selected recipe that new builds use.
func (p *Profiles) Default() Profile { return p.profiles[p.defaultVersion] }

// Get returns the registered recipe an accepted build pinned.
func (p *Profiles) Get(version string) (Profile, error) {
	profile, ok := p.profiles[version]
	if !ok {
		return Profile{}, errors.New("unsupported immutable build profile version")
	}
	return profile, nil
}

var digestPinnedImage = regexp.MustCompile(`^[^\s@]+@sha256:[a-f0-9]{64}$`)

// mustParseProfile validates an embedded recipe; a failure is a broken build.
func mustParseProfile(version string, manifest, dockerfile []byte) Profile {
	var body struct {
		SchemaVersion              int               `json:"schemaVersion"`
		ID                         string            `json:"id"`
		BuildArgs                  map[string]string `json:"buildArgs"`
		Version                    string            `json:"version"`
		DockerConfigurationVersion int               `json:"dockerConfigurationVersion"`
	}
	if json.Unmarshal(manifest, &body) != nil || len(dockerfile) == 0 || body.SchemaVersion != 3 || body.ID != "go-react-v1" || len(body.BuildArgs) != 3 {
		panic("invalid embedded release build profile " + version)
	}
	if version == "bp2" && (body.Version != version || body.DockerConfigurationVersion != 2) {
		panic("invalid embedded hosted Docker configuration profile")
	}
	if version == "bp3" && (body.Version != version || body.DockerConfigurationVersion != 0) {
		panic("invalid embedded local build profile")
	}
	for _, key := range []string{"GO_BUILD_IMAGE", "NODE_BUILD_IMAGE", "STATIC_BASE_IMAGE"} {
		if !digestPinnedImage.MatchString(body.BuildArgs[key]) {
			panic("embedded build profile images must be digest pinned")
		}
	}
	digest := strings.TrimPrefix(objectstore.Digest(bytes.Join([][]byte{manifest, dockerfile}, []byte{0})), "sha256:")
	return Profile{Version: version, ID: body.ID, Digest: digest, Manifest: manifest, Dockerfile: dockerfile}
}
