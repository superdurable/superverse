// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package repohost is the Git hosting service of the platform's repositories:
// the interface every host implements, the values it exchanges and the errors
// it reports. An implementation lives in a sub-package and internal/bootstrap
// chooses it; callers depend on this package alone.
package repohost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Host creates, reads and archives the repositories of Projects and hands out
// the credentials of the Git processes that clone and push them. Every method
// reports the outcome of a lost response as ErrOutcomeUnknown, so a caller
// reads before it repeats a write.
type Host interface {
	// Provider is the implementation, as the repohost setting names it.
	Provider() Provider
	// InstallationID names who owns the credentials of this host. A repository
	// binding records it, and CheckInstallation compares it before a binding's
	// repository is used.
	InstallationID() int64

	// ManagedRepository returns the private repository of a Project's setup,
	// creating it on first use under the name ManagedName gives it. A repository
	// the setup already created, also by an earlier attempt, is returned as it
	// is; an archived one is an error.
	ManagedRepository(ctx context.Context, setup SetupIdentity) (Repository, error)
	// FindManagedRepository returns the setup's repository by its deterministic
	// name, or nil when there is none. It creates nothing, so cleanup can use it
	// for a setup that recorded no repository.
	FindManagedRepository(ctx context.Context, setup SetupIdentity) (*Repository, error)
	// ArchiveManaged archives the repository fullName, the one the Project's
	// binding or its setup recorded, only when this setup's ownership marker owns
	// it. A repository that no longer exists needs no archive. An archived
	// repository refuses pushes and stays readable.
	ArchiveManaged(ctx context.Context, projectID, setupID, fullName string) error
	// ReadRepository returns the repository named owner/name.
	ReadRepository(ctx context.Context, fullName string) (Repository, error)
	// InitializeTemplate commits the application template of this server build as
	// the first commit of an empty managed repository and returns that commit.
	// The revision must be the template this server serves. A repeated call
	// returns the same commit.
	InitializeTemplate(ctx context.Context, target Repository, revision string) (string, error)

	// ReadFile reads one repository-relative file at an exact commit.
	ReadFile(ctx context.Context, fullName, commit, path string) ([]byte, error)
	// ListCommits lists the recent commits of a branch, newest first, whoever
	// authored them.
	ListCommits(ctx context.Context, fullName, branch string) ([]Commit, error)
	// ValidateReleaseCommit accepts any commit reachable from branch and returns
	// ErrCommitUnreachable for another one.
	ValidateReleaseCommit(ctx context.Context, fullName, branch, commit string) error
	// DescribeCommit returns the version of a commit that the caller already
	// checked is on main: its number and its subject. A commit's answer never
	// changes, so callers freeze it with their record.
	DescribeCommit(ctx context.Context, fullName, commit string) (CommitVersion, error)

	// RepositoryCredential mints a credential restricted to the contents of one
	// repository, for the Git processes and builds of that repository.
	RepositoryCredential(ctx context.Context, repositoryID int64) (Credential, error)
}

// Repository is the part of a hosted repository that the platform reads.
type Repository struct {
	// ID is the host's identity of the repository. A repository binding is keyed
	// by it, so a host never reuses one.
	ID       int64
	FullName string
	// CloneURL is where the Sandbox clones the repository from.
	CloneURL      string
	DefaultBranch string
	// Description carries the ownership marker of a managed repository.
	Description string
	Private     bool
	Archived    bool
}

// Commit is one entry of a branch's history.
type Commit struct {
	SHA         string
	Message     string
	CommittedAt time.Time
}

// CommitVersion is a commit as Studio shows it, "V<Number> · <Subject>":
// Number counts the commit's ancestors, the root commit V0, and Subject is the
// first line of its message.
type CommitVersion struct {
	Number  uint64
	Subject string
}

// maximumSubjectRunes bounds a CommitVersion's subject.
const maximumSubjectRunes = 72

// NewCommitVersion is the version of a commit with ancestors commits in its
// history, itself included, and message: the subject is the message's first
// line, cut at 72 code points with "…".
func NewCommitVersion(ancestors uint64, message string) CommitVersion {
	subject, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	subject = strings.TrimSpace(subject)
	if runes := []rune(subject); len(runes) > maximumSubjectRunes {
		subject = string(runes[:maximumSubjectRunes-1]) + "…"
	}
	return CommitVersion{Number: max(ancestors, 1) - 1, Subject: subject}
}

// Credential is a repository-scoped access token and its expiry.
type Credential struct {
	Token     string
	ExpiresAt time.Time
}

// The errors every host reports, so that failure classification does not depend
// on the host. A host wraps its own detail around them.
var (
	// ErrNotFound reports a repository, commit or file that does not exist or
	// that the host's credentials cannot read.
	ErrNotFound = errors.New("repository content is not found or not readable")
	// ErrCommitUnreachable reports a release commit that is not on the Project
	// branch.
	ErrCommitUnreachable = errors.New("release commit is not reachable from the project branch")
	// ErrOutcomeUnknown reports a request whose effect could not be observed, such
	// as a lost or unreadable response. It never authorizes a replay without
	// observing the owning operation.
	ErrOutcomeUnknown = errors.New("repository host request outcome is uncertain")
	// ErrPermissionDenied reports that the host refused the platform's credentials.
	ErrPermissionDenied = errors.New("repository host denied access")
	// ErrRejected reports a request that the host refused for another reason of
	// its own: it will not succeed unchanged.
	ErrRejected = errors.New("repository host rejected the request")
	// ErrOtherHost reports a repository that a binding recorded under another
	// repository host than the server's. The server's host cannot reach it and
	// would read it as absent, so a caller refuses instead of taking that
	// absence for an archived or missing repository.
	ErrOtherHost = errors.New("repository belongs to another repository host")
)

// CheckInstallation returns ErrOtherHost when a repository binding recorded an
// installation other than host's, with a message that names the repository
// host to run the server with. A repository of host itself passes, also one
// that no longer exists.
func CheckInstallation(host Host, installationID int64) error {
	if installationID == host.InstallationID() {
		return nil
	}
	return fmt.Errorf("%w: it was bound under installation %d, not under installation %d of this server's %s host; run the server with the %s repository host",
		ErrOtherHost, installationID, host.InstallationID(), host.Provider(), host.Provider().others())
}
