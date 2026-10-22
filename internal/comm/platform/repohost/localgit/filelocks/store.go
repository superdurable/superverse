// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package filelocks owns cross-process locks and temporary-file lifetimes for a local Git directory.
package filelocks

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	retryInterval = 20 * time.Millisecond
)

// Store owns one live writer's temporary files and the directory's advisory locks.
type Store struct {
	root, owner string
	lifetime    *os.File
}

// New claims a unique owner and removes only abandoned writers' temporary files.
func New(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, ".locks"), 0o755); err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	s := &Store{root: root, owner: fmt.Sprintf("%x", id)}
	startup, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStartup()
	releaseStartup, err := s.Lock(startup, "owners")
	if err != nil {
		return nil, err
	}
	defer releaseStartup()

	f, err := os.OpenFile(filepath.Join(root, ".locks", "owner-"+s.owner), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	s.lifetime = f
	if err = s.cleanAbandoned(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the owner's lifetime lock after its operations have stopped.
func (s *Store) Close() error {
	// Temporary files have already been removed by their operation owner.
	return s.lifetime.Close()
}

// Lock waits for one operation's lock, or the caller's cancellation. Lock files remain
// in place so an unlink cannot split one lock identity into two live inodes.
func (s *Store) Lock(ctx context.Context, key string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(s.root, ".locks", key), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, err
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// TempPrefix associates a temporary operation with this live owner.
func (s *Store) TempPrefix(prefix string) string { return prefix + s.owner + "--" }

func (s *Store) cleanAbandoned() error {
	for _, prefix := range []string{".creating-", ".receiving-"} {
		paths, err := filepath.Glob(filepath.Join(s.root, prefix+"*"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			owner, _, owned := strings.Cut(strings.TrimPrefix(filepath.Base(path), prefix), "--")
			// The release reset removes old unowned files; startup never guesses their owner.
			if !owned || len(owner) != 32 || strings.Trim(owner, "0123456789abcdef") != "" {
				continue
			}
			f, openErr := os.OpenFile(filepath.Join(s.root, ".locks", "owner-"+owner), os.O_CREATE|os.O_RDWR, 0o644)
			if openErr != nil {
				return openErr
			}
			lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if lockErr == nil {
				err = os.RemoveAll(path)
			} else if !errors.Is(lockErr, syscall.EWOULDBLOCK) && !errors.Is(lockErr, syscall.EAGAIN) {
				err = lockErr
			}
			_ = f.Close()
			if err != nil {
				return err
			}
		}
	}
	// Owner IDs are never reused; startup registration is serialized by the owners lock.
	owners, err := filepath.Glob(filepath.Join(s.root, ".locks", "owner-*"))
	if err != nil {
		return err
	}
	for _, path := range owners {
		if filepath.Base(path) == "owner-"+s.owner {
			continue
		}
		f, openErr := os.OpenFile(path, os.O_RDWR, 0)
		if errors.Is(openErr, os.ErrNotExist) {
			continue
		}
		if openErr != nil {
			return openErr
		}
		lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if lockErr == nil {
			err = os.Remove(path)
		} else if !errors.Is(lockErr, syscall.EWOULDBLOCK) && !errors.Is(lockErr, syscall.EAGAIN) {
			err = lockErr
		}
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
