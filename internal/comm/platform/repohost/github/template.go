// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/githubapp"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"golang.org/x/sync/errgroup"
)

type gitObject struct {
	SHA  string `json:"sha"`
	Type string `json:"type"`
}

// InitializeTemplate commits the application template embedded in this server
// build as the first commit of an empty managed repository. The revision the
// Setup Flow accepted must be this exact template: an older revision fails
// instead of silently initializing different content. Git object identities
// make every retry queryable and immutable.
func (c *Client) InitializeTemplate(ctx context.Context, target repohost.Repository, revision string, template *repohost.Template, createdAt time.Time) (string, error) {
	if createdAt.Unix() <= 0 {
		return "", errors.New("project creation time must be after the Unix epoch")
	}
	if revision != template.Revision {
		return "", fmt.Errorf("this server does not serve the accepted application template revision %s", revision)
	}
	request := func(method, suffix string, input, output any) error {
		return c.repositoryRequest(ctx, method, target.FullName, suffix, input, output)
	}
	// GitHub cannot create refs in an empty repository; bootstrap via Contents.
	marker := []byte("superverse-v2-template-bootstrap:" + target.FullName + ":" + revision + "\n")
	head, err := c.head(ctx, target.FullName, "main")
	if err != nil {
		var empty *githubapp.HTTPError
		if !errors.As(err, &empty) || (empty.Status != http.StatusNotFound && empty.Status != http.StatusConflict) {
			return "", err
		}
		createErr := c.repositoryRequest(ctx, http.MethodPut, target.FullName, "/contents/.superverse-bootstrap", map[string]any{"message": "Prepare the app template", "branch": "main", "content": base64.StdEncoding.EncodeToString(marker)}, nil)
		if head, err = c.head(ctx, target.FullName, "main"); err != nil {
			return "", errors.Join(createErr, err)
		}
	}
	var existing struct {
		Tree    gitObject   `json:"tree"`
		Parents []gitObject `json:"parents"`
	}
	if err = request(http.MethodGet, "/git/commits/"+head, nil, &existing); err != nil {
		return "", err
	}
	// A lost branch-update response reconciles here, before any blob upload.
	if existing.Tree.SHA == template.TreeID {
		return head, nil
	}
	var bootstrap struct {
		Tree      []repohost.TreeEntry `json:"tree"`
		Truncated bool                 `json:"truncated"`
	}
	if err = request(http.MethodGet, "/git/trees/"+existing.Tree.SHA+"?recursive=1", nil, &bootstrap); err != nil {
		return "", err
	}
	if len(existing.Parents) != 0 || bootstrap.Truncated || len(bootstrap.Tree) != 1 || bootstrap.Tree[0].Path != ".superverse-bootstrap" || bootstrap.Tree[0].SHA != repohost.BlobID(marker) {
		return "", errors.New("managed repository changed before template initialization")
	}
	if err = c.uploadTemplateBlobs(ctx, target.FullName, template.Files); err != nil {
		return "", err
	}
	entries := make([]repohost.TreeEntry, 0, len(template.Files))
	for _, file := range template.Files {
		entries = append(entries, file.TreeEntry())
	}
	var tree gitObject
	if err = request(http.MethodPost, "/git/trees", map[string]any{"tree": entries}, &tree); err != nil {
		return "", err
	}
	if tree.SHA != template.TreeID {
		return "", fmt.Errorf("uploaded template tree has identity %s instead of %s", tree.SHA, template.TreeID)
	}
	// The owning Project persists this time before setup starts, so retries
	// create the same object even before a branch update succeeds.
	templateAuthor := map[string]string{"name": "Platform agent", "email": "bot@superdurable.io", "date": createdAt.UTC().Format(time.RFC3339)}
	var commit gitObject
	if err = request(http.MethodPost, "/git/commits", map[string]any{"message": "Start the project from the app template", "tree": tree.SHA, "parents": []string{head}, "committer": templateAuthor, "author": templateAuthor}, &commit); err != nil {
		return "", err
	}
	updateErr := c.repositoryRequest(ctx, http.MethodPatch, target.FullName, "/git/refs/heads/main", map[string]any{"sha": commit.SHA, "force": false}, nil)
	if head, err = c.head(ctx, target.FullName, "main"); err != nil {
		return "", errors.Join(updateErr, err)
	}
	if head != commit.SHA {
		return "", errors.New("managed branch changed during initialization")
	}
	return head, nil
}

// uploadTemplateBlobs uploads every template file as a Git blob, eight at a
// time; the first failure cancels the remaining uploads.
func (c *Client) uploadTemplateBlobs(ctx context.Context, fullName string, files []repohost.TemplateFile) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for _, file := range files {
		if groupCtx.Err() != nil {
			break
		}
		group.Go(func() error {
			var created gitObject
			if err := c.repositoryRequest(groupCtx, http.MethodPost, fullName, "/git/blobs", map[string]string{"content": base64.StdEncoding.EncodeToString(file.Contents), "encoding": "base64"}, &created); err != nil {
				return err
			}
			if created.SHA != file.BlobID {
				return fmt.Errorf("uploaded template blob %s has identity %s instead of %s", file.Path, created.SHA, file.BlobID)
			}
			return nil
		})
	}
	return group.Wait()
}
