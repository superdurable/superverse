// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagent

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

type downloads struct {
	root, minimumVersion string
}

// load reads the one disk/wire schema. A missing local build has an empty
// manifest; malformed manifests and other filesystem failures stay unavailable.
func (d *downloads) load(root *os.Root) (bridgeapi.DownloadsManifest, error) {
	manifest := bridgeapi.DownloadsManifest{MinimumBridgeVersion: d.minimumVersion, Artifacts: []bridgeapi.Artifact{}}
	file, err := root.Open("manifest.json")
	if errors.Is(err, os.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return manifest, err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return manifest, errors.New("download manifest unreadable or too large")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil || decoder.Decode(new(any)) != io.EOF || len(manifest.Artifacts) > 5 {
		return manifest, errors.New("invalid download manifest")
	}
	seen := make(map[string]bool, 5)
	for _, artifact := range manifest.Artifacts {
		digest, err := hex.DecodeString(artifact.SHA256)
		if artifact.Validate() != nil || !downloadPath(artifact.Path) || seen[artifact.Path] || err != nil || len(digest) != 32 {
			return manifest, errors.New("invalid download artifact")
		}
		seen[artifact.Path] = true
	}
	manifest.MinimumBridgeVersion = d.minimumVersion
	if manifest.Artifacts == nil {
		manifest.Artifacts = []bridgeapi.Artifact{}
	}
	return manifest, nil
}

func (e *Endpoint) serveDownload(w http.ResponseWriter, r *http.Request) {
	d := e.downloads
	name := strings.TrimPrefix(r.URL.Path, "/remote-agent/v1/downloads/")
	manifestRequest := r.URL.Path == bridgeapi.PathDownloadsManifest
	if !manifestRequest && !downloadPath(name) {
		e.writeError(w, r, refusal(bridgeapi.ErrorNotFound))
		return
	}
	root, err := os.OpenRoot(d.root)
	if errors.Is(err, os.ErrNotExist) {
		if manifestRequest {
			writeJSON(w, http.StatusOK, bridgeapi.DownloadsManifest{MinimumBridgeVersion: d.minimumVersion, Artifacts: []bridgeapi.Artifact{}})
		} else {
			e.writeError(w, r, refusal(bridgeapi.ErrorNotFound))
		}
		return
	}
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	defer func() { _ = root.Close() }()
	manifest, err := d.load(root)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	if manifestRequest {
		writeJSON(w, http.StatusOK, manifest)
		return
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Path != name {
			continue
		}
		file, err := root.Open(artifact.Path)
		if errors.Is(err, os.ErrNotExist) {
			e.writeError(w, r, refusal(bridgeapi.ErrorNotFound))
			return
		}
		if err != nil {
			e.writeError(w, r, err)
			return
		}
		defer func() { _ = file.Close() }()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			e.writeError(w, r, errors.New("download artifact is not a regular file"))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+artifact.Path+`"`)
		w.Header().Set("ETag", `"`+artifact.SHA256+`"`)
		http.ServeContent(w, r, artifact.Path, info.ModTime(), file)
		return
	}
	e.writeError(w, r, refusal(bridgeapi.ErrorNotFound))
}

func downloadPath(name string) bool {
	switch name {
	case "superverse-bridge-darwin-arm64", "superverse-bridge-darwin-amd64", "superverse-bridge-linux-arm64", "superverse-bridge-linux-amd64", "superverse-orca-plugin.tgz":
		return true
	default:
		return false
	}
}
