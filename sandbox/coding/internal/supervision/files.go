// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package supervision

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// TryLock takes the exclusive lock of path without waiting, creating the file
// and its directory when absent. The returned file holds the lock until it is
// closed or the process exits; its descriptor is close-on-exec, so no child
// inherits the lock unless it is passed on explicitly.
func TryLock(path string) (*os.File, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // fixed lock files of the supervisors' own directories
	if err != nil {
		return nil, false, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return file, true, nil
}

// WriteJSON replaces path atomically with value: it writes a temporary file
// beside it and renames it, so a reader sees the old or the new record whole,
// and concurrent writers never share a temporary file.
func WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.new")
	if err != nil {
		return err
	}
	_, err = temporary.Write(append(data, '\n'))
	if err = errors.Join(err, temporary.Close()); err == nil {
		err = os.Rename(temporary.Name(), path)
	}
	if err != nil {
		_ = os.Remove(temporary.Name())
	}
	return err
}

// ReadJSON decodes the record at path.
func ReadJSON(path string, value any) error {
	data, err := os.ReadFile(path) //nolint:gosec // fixed records of the supervisors' own directories
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
