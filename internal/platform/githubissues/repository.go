// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package githubissues

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/platform/githubapp"
)

// Repository is the GitHub repository where the platform files its incidents,
// reached as one installation of the GitHub App, which needs the Issues
// permission there. Bootstrap builds it from the validated configuration and
// restricts the App's token to TokenPermissions.
type Repository struct {
	App *githubapp.App
	// FullName is owner/name.
	FullName string
}

// TokenPermissions are the permissions of the installation token that files
// issues: the repository's issues and metadata, and nothing else the App may
// hold there.
func TokenPermissions() map[string]string {
	return map[string]string{"issues": "write", "metadata": "read"}
}

// issueResponse is the part of a GitHub issue that EnsureIssue reads. The
// issues of a repository list its pull requests too, which carry PullRequest.
type issueResponse struct {
	Number      int64           `json:"number"`
	HTMLURL     string          `json:"html_url"`
	PullRequest *struct{}       `json:"pull_request"`
	Labels      []labelResponse `json:"labels"`
}

type labelResponse struct {
	Name string `json:"name"`
}

// carries reports whether the issue has label; GitHub compares label names
// without case.
func (issue issueResponse) carries(label string) bool {
	return slices.ContainsFunc(issue.Labels, func(candidate labelResponse) bool { return strings.EqualFold(candidate.Name, label) })
}

// EnsureIssue returns the open or closed issue that carries issue.Label and
// opens one only when none does: it always queries the label before it writes,
// so a call repeated after an error, a lost answer included, never opens a
// second issue once the first carries the label. Two calls for one label at
// the same moment, in one process or in several, may each open one: GitHub
// offers no create-if-absent. GitHub silently drops a label the caller may not
// set, so an opened issue may lack it (Filed.Labeled); the caller then commits
// the issue's number before it attaches the label with LabelIssue, since a
// repeated EnsureIssue would not find the unlabeled issue and would open
// another.
func (r *Repository) EnsureIssue(ctx context.Context, issue Issue) (Filed, error) {
	if found, isFound, err := r.find(ctx, issue.Label); err != nil || isFound {
		return found, err
	}
	var created issueResponse
	if err := r.call(ctx, http.MethodPost, "/issues", map[string]any{"title": issue.Title, "body": issue.Body, "labels": []string{issue.Label}}, &created); err != nil {
		return Filed{}, fmt.Errorf("open the issue of %s: %w", issue.Label, err)
	}
	return Filed{Number: created.Number, URL: created.HTMLURL, Opened: true, Labeled: created.carries(issue.Label)}, nil
}

// LabelIssue adds label to issue number. GitHub adds a label the issue
// already carries without change, so a repeated call is safe.
func (r *Repository) LabelIssue(ctx context.Context, number int64, label string) error {
	path := "/issues/" + strconv.FormatInt(number, 10) + "/labels"
	if err := r.call(ctx, http.MethodPost, path, map[string]any{"labels": []string{label}}, nil); err != nil {
		return fmt.Errorf("label issue #%d with %s: %w", number, label, err)
	}
	return nil
}

// find returns the newest issue that carries label, open or closed; false
// when none does.
func (r *Repository) find(ctx context.Context, label string) (Filed, bool, error) {
	var listed []issueResponse
	if err := r.call(ctx, http.MethodGet, "/issues?state=all&per_page=10&labels="+url.QueryEscape(label), nil, &listed); err != nil {
		return Filed{}, false, fmt.Errorf("find the issue of %s: %w", label, err)
	}
	for _, candidate := range listed {
		if candidate.PullRequest == nil && candidate.carries(label) {
			return Filed{Number: candidate.Number, URL: candidate.HTMLURL, Labeled: true}, true, nil
		}
	}
	return Filed{}, false, nil
}

// call sends one request to an endpoint of the repository as the installation
// and classifies its error.
func (r *Repository) call(ctx context.Context, method, suffix string, input, output any) error {
	return classify(r.App.Call(ctx, method, "/repos/"+r.FullName+suffix, input, output))
}

// classify gives an error of the GitHub App the type callers classify, as the
// GitHub repository host does, and keeps the GitHub error behind it: a lost
// answer is ErrOutcomeUnknown, a 4xx other than 408 and 429 is ErrRejected,
// and any other error, such as a server error, stays untyped and may succeed
// when asked again.
func classify(err error) error {
	var response *githubapp.HTTPError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, githubapp.ErrOutcomeUnknown):
		return fmt.Errorf("%w: %w", errOutcomeUnknown, err)
	case !errors.As(err, &response):
		return err
	case response.Status >= http.StatusBadRequest && response.Status < http.StatusInternalServerError &&
		response.Status != http.StatusRequestTimeout && response.Status != http.StatusTooManyRequests:
		return fmt.Errorf("%w: %w", ErrRejected, err)
	default:
		return err
	}
}
