// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package localgit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/platform/repohost"
)

// apiPrefix starts the read-only JSON interface of the administrator. A name
// cannot start with an underscore, so it never meets a repository path.
const apiPrefix = "/_api/v1/"

// serveAPI serves the checks of the end-to-end tests:
//
//	GET /_api/v1/repositories/<owner>/<name>                         the repository
//	GET /_api/v1/repositories/<owner>/<name>/branch?name=<branch>    the commit a branch points to
//	GET /_api/v1/repositories/<owner>/<name>/file?ref=<ref>&path=<p> a file at a commit or branch
//
// It reads and changes nothing, and it requires the administrator's token as a
// Bearer credential.
func (h *Host) serveAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	claims, err := h.signer.verify(token, time.Now())
	if scheme != "Bearer" || err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "the administrator token is required", http.StatusUnauthorized)
		return
	}
	if claims.Scope != scopeAdministrator {
		http.Error(w, "the token is not an administrator token", http.StatusForbidden)
		return
	}
	segments := strings.Split(strings.TrimPrefix(r.URL.Path, apiPrefix), "/")
	if len(segments) < 3 || len(segments) > 4 || segments[0] != "repositories" {
		http.NotFound(w, r)
		return
	}
	fullName := segments[1] + "/" + segments[2]
	var answer any
	switch {
	case len(segments) == 3:
		answer, err = h.repositoryAnswer(fullName)
	case segments[3] == "branch":
		answer, err = h.branchAnswer(fullName, r.URL.Query().Get("name"))
	case segments[3] == "file":
		answer, err = h.fileAnswer(r.Context(), fullName, r.URL.Query().Get("ref"), r.URL.Query().Get("path"))
	default:
		http.NotFound(w, r)
		return
	}
	switch {
	case errors.Is(err, errInvalidBranch):
		http.Error(w, errInvalidBranch.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, repohost.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case err != nil:
		h.fail(w, r, err)
		return
	}
	body, err := json.Marshal(answer)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (h *Host) repositoryAnswer(fullName string) (any, error) {
	found, err := h.store.find(fullName)
	if err != nil {
		return nil, err
	}
	return struct {
		ID            int64  `json:"id"`
		FullName      string `json:"fullName"`
		CloneURL      string `json:"cloneUrl"`
		DefaultBranch string `json:"defaultBranch"`
		Description   string `json:"description"`
		Private       bool   `json:"private"`
		Archived      bool   `json:"archived"`
	}{found.ID, found.FullName, h.cloneURL(found.FullName), defaultBranch, found.Description, found.Private, found.Archived}, nil
}

func (h *Host) branchAnswer(fullName, branch string) (any, error) {
	head, err := h.branchHead(fullName, branch)
	if err != nil {
		return nil, err
	}
	return struct {
		Name string `json:"name"`
		SHA  string `json:"sha"`
	}{branch, head}, nil
}

func (h *Host) fileAnswer(ctx context.Context, fullName, ref, path string) (any, error) {
	contents, err := h.ReadFile(ctx, fullName, ref, path)
	if err != nil {
		return nil, err
	}
	return struct {
		Path    string `json:"path"`
		Ref     string `json:"ref"`
		Content string `json:"content"`
	}{path, ref, base64.StdEncoding.EncodeToString(contents)}, nil
}
