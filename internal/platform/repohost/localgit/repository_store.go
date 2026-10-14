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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/superdurable/superversev2/internal/platform/repohost"
)

// The layout of the directory the host owns:
//
//	<dir>/repository-sequence                   the last repository ID handed out
//	<dir>/superverse-local/<name>.git/          one bare repository
//	<dir>/superverse-local/<name>.git/superverse-repository.json   its record
//	<dir>/.creating-*                           a repository being created
//	<dir>/.receiving-*                          the pack of a push being received
const (
	// repositoryOwner owns every repository of the local host.
	repositoryOwner = "superverse-local"
	// installationID is the fixed identity of the credentials of the local host.
	installationID int64 = 1
	// defaultBranch is the branch every repository starts on.
	defaultBranch = "main"

	recordFile       = "superverse-repository.json"
	sequenceFile     = "repository-sequence"
	repositorySuffix = ".git"
	creatingPrefix   = ".creating-"
	receivingPrefix  = ".receiving-"
)

// namePattern admits the characters of a GitHub repository name, none of which
// is a path separator, and no name that starts with a dot.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)

// record is the persisted state of one repository, kept in the repository's own
// directory. The host is the authority for it: nothing else stores it.
type record struct {
	ID          int64     `json:"id"`
	FullName    string    `json:"fullName"`
	Description string    `json:"description"`
	Private     bool      `json:"private"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"createdAt"`
}

// repository is the reference to the stored repository that a request works on.
type repository struct {
	record
	path string
}

// store owns the directory of bare repositories. Creation and the ID sequence
// are serialized in this process; the lanes that share the directory never run
// at the same time.
type store struct {
	dir string

	// creating serializes the creation of repositories and their IDs.
	creating sync.Mutex
	// mu guards locks, the write lock of each repository by ID: a channel with
	// one slot, so that a caller can stop waiting for it.
	mu    sync.Mutex
	locks map[int64]chan struct{}
}

func newStore(dir string) (*store, error) {
	if err := os.MkdirAll(filepath.Join(dir, repositoryOwner), 0o755); err != nil { //nolint:gosec // G301: the lanes share this directory with the server Pod's user
		return nil, fmt.Errorf("create the repository directory: %w", err)
	}
	// A crash between creating and publishing a repository leaves its directory
	// behind, and a crash during a push the pack it was receiving.
	for _, prefix := range []string{creatingPrefix, receivingPrefix} {
		stale, err := filepath.Glob(filepath.Join(dir, prefix+"*"))
		if err != nil {
			return nil, err
		}
		for _, path := range stale {
			if err = os.RemoveAll(path); err != nil {
				return nil, fmt.Errorf("remove an unfinished repository or push: %w", err)
			}
		}
	}
	return &store{dir: dir, locks: map[int64]chan struct{}{}}, nil
}

// splitFullName validates owner/name. Only repositoryOwner owns repositories.
func splitFullName(fullName string) (name string, ok bool) {
	owner, name, found := strings.Cut(fullName, "/")
	return name, found && owner == repositoryOwner && namePattern.MatchString(name)
}

func (s *store) path(name string) string {
	return filepath.Join(s.dir, repositoryOwner, name+repositorySuffix)
}

// find reads the repository named owner/name, or repohost.ErrNotFound.
func (s *store) find(fullName string) (repository, error) {
	name, ok := splitFullName(fullName)
	if !ok {
		return repository{}, fmt.Errorf("repository %q: %w", fullName, repohost.ErrNotFound)
	}
	path := s.path(name)
	data, err := os.ReadFile(filepath.Join(path, recordFile)) //nolint:gosec // G304: the path is built from a validated repository name
	if errors.Is(err, fs.ErrNotExist) {
		return repository{}, fmt.Errorf("repository %q: %w", fullName, repohost.ErrNotFound)
	}
	if err != nil {
		return repository{}, fmt.Errorf("read the record of repository %q: %w", fullName, err)
	}
	var stored record
	if err = json.Unmarshal(data, &stored); err != nil {
		return repository{}, fmt.Errorf("decode the record of repository %q: %w", fullName, err)
	}
	return repository{record: stored, path: path}, nil
}

// lock waits for the write lock of one repository, which serializes its
// pushes, its archival and its template initialization, and returns the
// release. A caller whose ctx ends first gets ctx's error and holds nothing,
// so a cleanup round is not stuck behind a long push.
func (s *store) lock(ctx context.Context, id int64) (release func(), err error) {
	s.mu.Lock()
	slot := s.locks[id]
	if slot == nil {
		slot = make(chan struct{}, 1)
		s.locks[id] = slot
	}
	s.mu.Unlock()
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// receive copies the pack of a push into a file of the directory before the
// push takes its repository's lock, because reading the network is the slow
// part of a push. The caller closes and removes the file.
func (s *store) receive(pack io.Reader) (*os.File, error) {
	file, err := os.CreateTemp(s.dir, receivingPrefix+"*")
	if err != nil {
		return nil, err
	}
	if _, err = io.Copy(file, pack); err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}

// create makes the bare repository fullName on its default branch, with the
// next repository ID, and publishes it with one rename so that a crash leaves
// either no repository or a complete one. A repository that is not private
// serves its reads without a credential. The caller holds the creation lock
// and has seen that the repository does not exist.
func (s *store) create(name, description string, private bool) (repository, error) {
	id, err := s.nextID()
	if err != nil {
		return repository{}, err
	}
	temporary, err := os.MkdirTemp(s.dir, creatingPrefix)
	if err != nil {
		return repository{}, fmt.Errorf("create a repository directory: %w", err)
	}
	created := repository{record: record{ID: id, FullName: repositoryOwner + "/" + name, Description: description, Private: private, CreatedAt: time.Now().UTC()}, path: s.path(name)}
	if err = initBare(temporary); err == nil {
		err = created.save(temporary)
	}
	if err == nil {
		err = os.Rename(temporary, created.path)
	}
	if err != nil {
		_ = os.RemoveAll(temporary)
		return repository{}, fmt.Errorf("create repository %s: %w", created.FullName, err)
	}
	return created, nil
}

func initBare(path string) error {
	storage := filesystem.NewStorage(osfs.New(path), cache.NewObjectLRUDefault())
	_, err := git.InitWithOptions(storage, nil, git.InitOptions{DefaultBranch: "refs/heads/" + defaultBranch})
	return err
}

// save writes the record of a repository into its directory atomically.
func (r repository) save(directory string) error {
	data, err := json.Marshal(r.record)
	if err != nil {
		return err
	}
	return writeFileAtomic(directory, filepath.Join(directory, recordFile), data)
}

// reference reads a branch or tag from its loose file, the form in which the
// host writes references, or else from the packed references. A loose file that
// is empty, or does not hold an object ID, is corruption and an error, never a
// missing reference: a push must not create a reference over it.
func (r repository) reference(references storer.ReferenceStorer, name plumbing.ReferenceName) (plumbing.Hash, bool, error) {
	data, err := os.ReadFile(filepath.Join(r.path, filepath.FromSlash(name.String()))) //nolint:gosec // G304: a validated reference name inside the repository
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.EISDIR) {
		packed, packedErr := references.Reference(name)
		if errors.Is(packedErr, plumbing.ErrReferenceNotFound) {
			return plumbing.ZeroHash, false, nil
		}
		if packedErr != nil {
			return plumbing.ZeroHash, false, fmt.Errorf("read reference %s of repository %s: %w", name, r.FullName, packedErr)
		}
		return packed.Hash(), true, nil
	}
	if err != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("read reference %s of repository %s: %w", name, r.FullName, err)
	}
	line := strings.TrimSpace(string(data))
	if !plumbing.IsHash(line) {
		return plumbing.ZeroHash, false, fmt.Errorf("reference %s of repository %s is corrupt: %q", name, r.FullName, line)
	}
	return plumbing.NewHash(line), true, nil
}

// setReference points a branch or tag at hash through writeFileAtomic, staged
// in the repository's own directory, outside refs/, so that a crash never
// leaves an empty reference or a stray file among the references. The caller
// holds the repository's lock, which makes the reference's read and write one
// compare-and-swap, and validated the name.
func (r repository) setReference(name plumbing.ReferenceName, hash plumbing.Hash) error {
	path := filepath.Join(r.path, filepath.FromSlash(name.String()))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: the lanes share this directory with the server Pod's user
		return err
	}
	return writeFileAtomic(r.path, path, []byte(hash.String()+"\n"))
}

// nextID hands out the next repository ID: one more than the last one handed out,
// which the sequence file keeps. A repository directory is never deleted, but a
// sequence file that is missing is rebuilt from the repositories, so an ID is
// never handed out twice.
func (s *store) nextID() (int64, error) {
	path := filepath.Join(s.dir, sequenceFile)
	last, err := s.readSequence(path)
	if err != nil {
		return 0, err
	}
	last = max(last, s.highestID())
	next := last + 1
	if err = writeFileAtomic(s.dir, path, []byte(strconv.FormatInt(next, 10))); err != nil {
		return 0, fmt.Errorf("record repository ID %d: %w", next, err)
	}
	return next, nil
}

func (s *store) readSequence(path string) (int64, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the fixed sequence file of the repository directory
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read the repository ID sequence: %w", err)
	}
	last, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || last < 0 {
		return 0, fmt.Errorf("the repository ID sequence is corrupt: %q", data)
	}
	return last, nil
}

// highestID is the largest ID of the repositories on disk, or 0.
func (s *store) highestID() int64 {
	entries, err := os.ReadDir(filepath.Join(s.dir, repositoryOwner))
	if err != nil {
		return 0
	}
	var highest int64
	for _, entry := range entries {
		name, found := strings.CutSuffix(entry.Name(), repositorySuffix)
		if !found {
			continue
		}
		if existing, findErr := s.find(repositoryOwner + "/" + name); findErr == nil {
			highest = max(highest, existing.ID)
		}
	}
	return highest
}

// writeFileAtomic replaces path with data so that a reader sees the old file or
// the new one, never a partial one, and a crash leaves one of them, never an
// empty file: the data is written to a temporary file in staging, a directory
// of the same file system that no reader lists, and synced to the disk before a
// rename publishes it.
func writeFileAtomic(staging, path string, data []byte) error {
	temporary, err := os.CreateTemp(staging, ".write-*")
	if err != nil {
		return err
	}
	_, err = temporary.Write(data)
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(temporary.Name(), 0o644) //nolint:gosec // G302: the Native and Kind servers read what the other wrote
	}
	if err == nil {
		err = os.Rename(temporary.Name(), path)
	}
	if err != nil {
		_ = os.Remove(temporary.Name())
	}
	return err
}

// largeObjectBytes is the size above which a read streams an object from disk
// instead of loading it, so a read can check an object's size first.
const largeObjectBytes = 1 << 20

// open opens the bare repository of r. The caller closes the storage.
func (r repository) open() (*git.Repository, *filesystem.Storage, error) {
	storage := filesystem.NewStorageWithOptions(osfs.New(r.path), cache.NewObjectLRUDefault(), filesystem.Options{LargeObjectThreshold: largeObjectBytes})
	opened, err := git.Open(storage, nil)
	if err != nil {
		_ = storage.Close()
		return nil, nil, fmt.Errorf("open repository %s: %w", r.FullName, err)
	}
	return opened, storage, nil
}
