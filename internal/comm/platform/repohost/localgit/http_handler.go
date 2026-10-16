// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package localgit

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost/localgit/releases"
)

// gitUsername is the user name of a repository credential, as GitHub's
// installation tokens use it.
const gitUsername = "x-access-token"

// agentName is advertised as the agent of the local Git host.
const agentName = "superverse-localgit/1"

// operationTimeout bounds the reading of one request and the writing of its answer.
const operationTimeout = 15 * time.Minute

// gitOperation is a Git operation a repository serves, below /<owner>/<name>.git/.
// The two services are also the values of a ref discovery's service parameter.
type gitOperation string

const (
	operationRefs        gitOperation = "info/refs"
	operationUploadPack  gitOperation = "git-upload-pack"
	operationReceivePack gitOperation = "git-receive-pack"
)

// ServeHTTP serves the Git smart HTTP protocol of every repository and the
// administrator's read-only JSON interface.
func (h *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	if strings.HasPrefix(r.URL.Path, apiPrefix) {
		h.serveAPI(w, r)
		return
	}
	if releases.IsDownloadPath(r.URL.Path) {
		h.releases.ServeHTTP(w, r)
		return
	}
	h.serveGit(w, r)
}

// serveGit authenticates a Git request, resolves its repository and serves the
// operation. Only the smart protocol exists: the dumb one is not found.
func (h *Host) serveGit(w http.ResponseWriter, r *http.Request) {
	name, operation, ok := parseGitPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// A repository that is not private, the copy of the public connectors
	// library, serves its reads without a credential; every write needs one.
	found, findErr := h.store.find(repositoryOwner + "/" + name)
	isAnonymousRead := findErr == nil && !found.Private && isRead(r, operation) && r.Header.Get("Authorization") == ""
	if !isAnonymousRead {
		claims, err := h.authenticateGit(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Superverse local Git"`)
			http.Error(w, "authentication required: "+err.Error(), http.StatusUnauthorized)
			return
		}
		if errors.Is(findErr, repohost.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if findErr != nil {
			h.fail(w, r, findErr)
			return
		}
		if claims.Scope != scopeRepository || claims.RepositoryID != found.ID {
			http.Error(w, "the credential does not cover this repository", http.StatusForbidden)
			return
		}
	}
	// A clone or a push takes as long as it takes, but a stalled client must not hold a connection, or a
	// repository's write lock, forever. The server bounds only the request header.
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(operationTimeout)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline)
	switch {
	case operation == operationRefs && r.Method == http.MethodGet:
		h.advertise(w, r, found)
	case operation == operationUploadPack && r.Method == http.MethodPost:
		h.uploadPack(w, r, found)
	case operation == operationReceivePack && r.Method == http.MethodPost:
		h.receivePack(w, r, found)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// isRead reports whether the request reads a repository: the ref discovery of
// a fetch, or the fetch itself.
func isRead(r *http.Request, operation gitOperation) bool {
	switch operation {
	case operationRefs:
		return r.URL.Query().Get("service") == string(operationUploadPack)
	case operationUploadPack:
		return true
	default:
		return false
	}
}

// parseGitPath splits /<owner>/<name>.git/<operation> into the repository name and
// the operation. A name outside the repository alphabet is not a path.
func parseGitPath(path string) (name string, operation gitOperation, ok bool) {
	rest, found := strings.CutPrefix(path, "/"+repositoryOwner+"/")
	if !found {
		return "", "", false
	}
	name, rest, found = strings.Cut(rest, repositorySuffix+"/")
	if !found || !namePattern.MatchString(name) {
		return "", "", false
	}
	switch operation = gitOperation(rest); operation {
	case operationRefs, operationUploadPack, operationReceivePack:
		return name, operation, true
	default:
		return "", "", false
	}
}

// authenticateGit verifies the repository credential of a Git request: HTTP Basic
// authentication with the user name x-access-token and the token as password.
func (h *Host) authenticateGit(r *http.Request) (tokenClaims, error) {
	username, token, ok := r.BasicAuth()
	if !ok || username != gitUsername {
		return tokenClaims{}, errors.New("a Basic credential of user " + gitUsername + " is required")
	}
	return h.signer.verify(token, time.Now())
}

// fail reports an internal failure without its detail, which can name paths, and
// logs it.
func (h *Host) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "local Git host request failed", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Any("error", err))
	http.Error(w, "internal error", http.StatusInternalServerError)
}
