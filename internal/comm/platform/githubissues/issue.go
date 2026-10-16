// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package githubissues files each platform incident once as a GitHub issue,
// found or opened by the label of its signature, as one installation of the
// GitHub App.
package githubissues

import (
	"context"
	"errors"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
)

// Issue is the issue of one platform incident: Label identifies it, and Title
// and Body open it. The caller writes only identifiers and classification into
// Title and Body, never a credential, an argument or a conversation.
type Issue struct {
	Label string
	Title string
	Body  string
}

// Filed is the issue of an incident: its number, its page, whether this call
// opened it and whether it carries the incident's label. A found issue always
// carries it; an opened one lacks it when GitHub dropped it.
type Filed struct {
	Number  int64
	URL     string
	Opened  bool
	Labeled bool
}

// IncidentLabel identifies the issue of the incidents with one signature in
// one environment: superverse-incident:<env>:<sig16>, where <sig16> is the
// first 16 hex characters of the signature, a SHA-256 digest. The label stays
// within GitHub's 50 characters and holds no comma, which separates the labels
// of a query.
func IncidentLabel(environment deploymentnames.Environment, signature string) string {
	return "superverse-incident:" + environment.String() + ":" + signature[:16]
}

var (
	// ErrNotConfigured is the answer of NotConfigured: the server names no
	// issue repository, so an incident is only logged.
	ErrNotConfigured = errors.New("no platform issue repository is configured")
	// errOutcomeUnknown marks a request whose answer was lost, so it may have
	// taken effect. EnsureIssue reads before it writes, so asking again is safe.
	errOutcomeUnknown = errors.New("GitHub issue request outcome is uncertain")
	// ErrRejected marks a request GitHub refused in a way that asking again
	// cannot change: a missing permission or installation, a repository that
	// does not exist or an invalid request.
	ErrRejected = errors.New("GitHub refused the issue request")
)

// NotConfigured stands for a server without an issue repository: it files
// nothing.
type NotConfigured struct{}

// EnsureIssue files nothing and answers ErrNotConfigured.
func (NotConfigured) EnsureIssue(context.Context, Issue) (Filed, error) {
	return Filed{}, ErrNotConfigured
}

// LabelIssue labels nothing and answers ErrNotConfigured.
func (NotConfigured) LabelIssue(context.Context, int64, string) error {
	return ErrNotConfigured
}
