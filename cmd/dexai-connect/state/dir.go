// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package state keeps the Bridge's local state: a 0700 directory of 0600
// files, written atomically, with a single-instance lock for serve.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// StateDirEnvironment overrides the default state directory.
const StateDirEnvironment = "DEXAI_CONNECT_STATE_DIR"

// MaxBindings bounds the bindings one serve process holds.
const MaxBindings = 8

// maxSocketPathBytes is the shortest Unix socket path limit (macOS).
const maxSocketPathBytes = 103

const (
	lockFile     = "serve.lock"
	mutationLock = "bindings.lock"
	// stagingPrefix names a binding being assembled; it is published by a
	// rename and never listed.
	stagingPrefix = ".staging-"
	socketFile    = "bridge.sock"
	bindingsDir   = "bindings"
	directoryMod  = 0o700
	fileMode      = 0o600
)

// ErrLocked is returned by Lock while another serve holds the directory.
var ErrLocked = errors.New("another dexai-connect serve is running with this state directory")

// Dir is the Bridge's state directory.
type Dir struct {
	path string
	lock *os.File
}

// DefaultPath is $DEXAI_CONNECT_STATE_DIR, or dexai-connect under the
// user's configuration directory.
func DefaultPath() (string, error) {
	if path := os.Getenv(StateDirEnvironment); path != "" {
		return path, nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the user configuration directory: %w", err)
	}
	return filepath.Join(config, "dexai-connect"), nil
}

// Open creates the directory if needed and tightens its mode to 0700.
func Open(path string) (*Dir, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve the state directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absolute, bindingsDir), directoryMod); err != nil {
		return nil, fmt.Errorf("create the state directory: %w", err)
	}
	for _, dir := range []string{absolute, filepath.Join(absolute, bindingsDir)} {
		if err := os.Chmod(dir, directoryMod); err != nil {
			return nil, fmt.Errorf("restrict the state directory: %w", err)
		}
	}
	return &Dir{path: absolute}, nil
}

// Path is the directory's absolute path.
func (dir *Dir) Path() string { return dir.path }

// Lock takes the single-instance lock, or returns ErrLocked. The lock ends
// with the process or Unlock.
func (dir *Dir) Lock() error {
	file, err := os.OpenFile(filepath.Join(dir.path, lockFile), os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return fmt.Errorf("open the lock file: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrLocked
		}
		return fmt.Errorf("lock the state directory: %w", err)
	}
	dir.lock = file
	return nil
}

// Unlock releases the lock taken by Lock.
func (dir *Dir) Unlock() {
	if dir.lock != nil {
		_ = dir.lock.Close()
		dir.lock = nil
	}
}

// Running reports whether a serve process holds the lock.
func (dir *Dir) Running() (bool, error) {
	probe := &Dir{path: dir.path}
	switch err := probe.Lock(); {
	case errors.Is(err, ErrLocked):
		return true, nil
	case err != nil:
		return false, err
	default:
		probe.Unlock()
		return false, nil
	}
}

// SocketPath is the local socket between mcp and serve.
func (dir *Dir) SocketPath() (string, error) {
	path := filepath.Join(dir.path, socketFile)
	if len(path) > maxSocketPathBytes {
		return "", fmt.Errorf("socket path %q is longer than %d bytes; choose a shorter state directory with --state-dir or %s",
			path, maxSocketPathBytes, StateDirEnvironment)
	}
	return path, nil
}

// Bindings loads every binding, ordered by connection ID.
func (dir *Dir) Bindings() ([]*Binding, error) {
	entries, err := os.ReadDir(filepath.Join(dir.path, bindingsDir))
	if err != nil {
		return nil, fmt.Errorf("list bindings: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	bindings := make([]*Binding, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		binding, err := loadBinding(filepath.Join(dir.path, bindingsDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

// AddBinding stores a new binding with its connection token. It assembles
// the binding in a staging directory and publishes it with one rename, under
// a lock that serializes every AddBinding of the state directory, so a crash
// leaves no partial binding and two connects cannot both bind one Orca
// session or pass the bound.
func (dir *Dir) AddBinding(record BindingRecord, token string) (*Binding, error) {
	if record.ConnectionID == "" || filepath.Base(record.ConnectionID) != record.ConnectionID || record.ConnectionID[0] == '.' {
		return nil, fmt.Errorf("connection ID %q cannot name a binding", record.ConnectionID)
	}
	unlock, err := LockFile(filepath.Join(dir.path, mutationLock))
	if err != nil {
		return nil, err
	}
	defer unlock()
	root := filepath.Join(dir.path, bindingsDir)
	// Staging directories left by a crash belong to no one while the lock
	// is held.
	leftovers, err := filepath.Glob(filepath.Join(root, stagingPrefix+"*"))
	if err != nil {
		return nil, fmt.Errorf("list staged bindings: %w", err)
	}
	for _, leftover := range leftovers {
		if err := os.RemoveAll(leftover); err != nil {
			return nil, fmt.Errorf("remove a staged binding: %w", err)
		}
	}
	existing, err := dir.Bindings()
	if err != nil {
		return nil, err
	}
	if len(existing) >= MaxBindings {
		return nil, fmt.Errorf("this Bridge already holds %d bindings; disconnect one first", MaxBindings)
	}
	for _, binding := range existing {
		if binding.Record.OrcaSessionID == record.OrcaSessionID && record.OrcaSessionID != "" {
			return nil, fmt.Errorf("the Orca session is already bound to Project %s", binding.Record.ProjectName)
		}
	}
	staging, err := os.MkdirTemp(root, stagingPrefix)
	if err != nil {
		return nil, fmt.Errorf("stage the binding: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := os.Mkdir(filepath.Join(staging, reportsDir), directoryMod); err != nil {
		return nil, fmt.Errorf("create the report store: %w", err)
	}
	if err := writeFile(filepath.Join(staging, tokenFile), []byte(token)); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(staging, recordFile), record); err != nil {
		return nil, err
	}
	path := filepath.Join(root, record.ConnectionID)
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("binding %s already exists", record.ConnectionID)
	}
	if err := os.Rename(staging, path); err != nil {
		return nil, fmt.Errorf("publish the binding: %w", err)
	}
	return loadBinding(path)
}

// writeFile replaces path atomically with a 0600 file.
func writeFile(path string, data []byte) error {
	return ReplaceFile(path, data, fileMode)
}

// ReplaceFile replaces path atomically: a synced temporary file with mode in
// the same directory, renamed over it.
func ReplaceFile(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync %s: %w", filepath.Base(path), err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

// LockFile takes an exclusive flock on path, creating it, and waits while
// another process holds it. The returned function releases it.
func LockFile(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, fileMode) //nolint:gosec // a lock file the Bridge names
	if err != nil {
		return nil, fmt.Errorf("open the lock file %s: %w", filepath.Base(path), err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock %s: %w", filepath.Base(path), err)
	}
	return func() { _ = file.Close() }, nil
}
