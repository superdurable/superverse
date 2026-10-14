// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// The directories below the state directory.
const (
	credentialsDirectory = "credentials"
	routesDirectory      = "routes"
	grantsDirectory      = "grants"
	rootCredentialFile   = "storage-root.json"
)

// credential is a key pair the server generates for a project's storage: the
// root key pair of the project's MinIO, or the key pair of one workload
// grant's MinIO service account. Kubernetes keeps it in an immutable Secret;
// here it is a 0600 file that is created exclusively, never rewritten, and
// carries the owner it was created for, so a lost answer never loses the key
// and a file of another owner is never used. It reaches a container only as
// an environment variable at creation and never Flow state, a log or a
// receipt.
type credential struct {
	Owner  credentialOwner `json:"owner"`
	Access string          `json:"accessKeyId"`
	Secret string          `json:"secretAccessKey"`
}

// credentialOwner is who a credential was created for: the project's
// storage, or one workload grant generation, both bound to the project
// network that held the storage. Scope marks a grant of the project's
// Preview, which the project's release removes; a Live one has none.
type credentialOwner struct {
	ProjectID  string `json:"projectId"`
	Component  string `json:"component"`
	NetworkID  string `json:"networkId"`
	Scope      string `json:"scope,omitempty"`
	WorkloadID string `json:"workloadId,omitempty"`
	Generation uint64 `json:"generation,omitempty"`
}

// ProjectCredentials is the directory of the credentials the server generated
// for a project, which DestroyScope removes.
func (k *Infrastructure) ProjectCredentials(project string) string {
	return filepath.Join(k.config.StateDir, credentialsDirectory, project)
}

// rootCredentialPath is the file of the root key pair of a project's MinIO.
func (k *Infrastructure) rootCredentialPath(project string) string {
	return filepath.Join(k.ProjectCredentials(project), rootCredentialFile)
}

// grantCredentialPath is the file of one workload grant generation's key pair.
func (k *Infrastructure) grantCredentialPath(project, grant string) string {
	return filepath.Join(k.ProjectCredentials(project), grantsDirectory, grant+".json")
}

func rootOwner(project, networkID string) credentialOwner {
	return credentialOwner{ProjectID: project, Component: infra.ComponentStorage.String(), NetworkID: networkID}
}

func grantOwner(s infra.Workload) credentialOwner {
	owner := credentialOwner{ProjectID: s.ProjectID, Component: infra.ComponentApplication.String(), NetworkID: s.Storage.Boundary.UID,
		WorkloadID: s.WorkloadID, Generation: s.Generation}
	if s.Preview() {
		owner.Scope = infra.ScopePreview.String()
	}
	return owner
}

// keyLengths are the lengths of a key pair's access and secret keys.
type keyLengths struct{ access, secret int }

var (
	// rootKeys are the MinIO root user's.
	rootKeys = keyLengths{access: 24, secret: 64}
	// grantKeys are a MinIO service account's, whose secret key MinIO admits
	// from 8 to 40 characters.
	grantKeys = keyLengths{access: 20, secret: 32}
)

// ensureCredential returns the credential at path, creating it for owner with
// a new key pair of lengths when it does not exist. The complete file is
// linked into place, which fails when a file exists, so the first creation
// wins and a concurrent one reads its key pair.
func ensureCredential(path string, owner credentialOwner, lengths keyLengths) (credential, error) {
	found, exists, err := readCredential(path, owner)
	if err != nil || exists {
		return found, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return credential{}, fmt.Errorf("prepare the credential directory: %w", err)
	}
	created := credential{Owner: owner, Access: infra.RandomSecret()[:lengths.access], Secret: infra.RandomSecret()[:lengths.secret]}
	draft, err := os.CreateTemp(filepath.Dir(path), ".credential-")
	if err != nil {
		return credential{}, fmt.Errorf("create a credential: %w", err)
	}
	defer func() { _ = os.Remove(draft.Name()) }()
	_, writeErr := draft.Write(comm.Must(json.Marshal(created))) //nolint:gosec // the owner-only credential file holds the key by design
	if err = errors.Join(writeErr, draft.Sync(), draft.Close()); err != nil {
		return credential{}, fmt.Errorf("write a credential: %w", err)
	}
	if err = os.Link(draft.Name(), path); err != nil && !errors.Is(err, fs.ErrExist) {
		return credential{}, fmt.Errorf("place a credential: %w", err)
	}
	found, exists, err = readCredential(path, owner)
	if err == nil && !exists {
		err = errors.New("a created credential is absent")
	}
	return found, err
}

// readCredential reads the credential at path, which must be a private
// regular file of owner; exists is false when there is none.
func readCredential(path string, owner credentialOwner) (credential, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return credential{}, false, nil
	}
	if err != nil {
		return credential{}, false, fmt.Errorf("read a credential: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return credential{}, false, errors.New("a credential file is not a private regular file")
	}
	data, err := os.ReadFile(path) //nolint:gosec // a path below the private state directory, from validated names
	if err != nil {
		return credential{}, false, fmt.Errorf("read a credential: %w", err)
	}
	var found credential
	if err = json.Unmarshal(data, &found); err != nil || found.Access == "" || found.Secret == "" {
		return credential{}, false, errors.New("a credential file is invalid")
	}
	if found.Owner != owner {
		return credential{}, false, fmt.Errorf("%w: a credential of another owner", infra.ErrOwnershipConflict)
	}
	return found, true, nil
}

// removeCredential removes the credential at path when it is owner's; an
// absent one is removed.
func removeCredential(path string, owner credentialOwner) error {
	if _, exists, err := readCredential(path, owner); err != nil || !exists {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove a credential: %w", err)
	}
	return nil
}

// previewGrantFiles are the grant credential files whose owner is the
// project's Preview, by path, which the project's release removes with the
// grants.
func (k *Infrastructure) previewGrantFiles(project string) (map[string]credentialOwner, error) {
	directory := filepath.Join(k.ProjectCredentials(project), grantsDirectory)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list the grant credentials: %w", err)
	}
	files := map[string]credentialOwner{}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(path) //nolint:gosec // a file below the project's private credential directory
		if err != nil {
			return nil, fmt.Errorf("read a grant credential: %w", err)
		}
		var found credential
		if json.Unmarshal(data, &found) == nil && found.Owner.ProjectID == project && found.Owner.Scope == infra.ScopePreview.String() {
			files[path] = found.Owner
		}
	}
	return files, nil
}

// CredentialValues are the key pairs the server generated for a project, for
// the redaction of what the Agent reads; they never leave the server.
func (k *Infrastructure) CredentialValues(project string) ([]string, error) {
	var values []string
	err := filepath.WalkDir(k.ProjectCredentials(project), func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // a file below the project's private credential directory
		if err != nil {
			return err
		}
		var found credential
		if json.Unmarshal(data, &found) == nil && found.Owner.ProjectID == project {
			values = append(values, found.Access, found.Secret)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the project's credentials: %w", err)
	}
	return values, nil
}
