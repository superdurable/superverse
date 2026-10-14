// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package accounts

import (
	"crypto/rand"
	"crypto/subtle"

	"github.com/superdurable/superversev2/internal/comm"
	"golang.org/x/crypto/argon2"
)

// passwordRecord is an account's Argon2id record, stored as the accounts row's
// password_record. It contains no plaintext and no public API returns it.
type passwordRecord struct {
	Algorithm   string `json:"algorithm"`
	Version     int    `json:"version"`
	Memory      uint32 `json:"memory"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	Salt        []byte `json:"salt"`
	Hash        []byte `json:"hash"`
}

// The Argon2id parameters of every record this server writes and accepts.
const (
	passwordMemory      = 64 * 1024
	passwordIterations  = 3
	passwordParallelism = 2
	passwordSaltLength  = 16
	passwordHashLength  = 32
)

func hashPassword(password string) passwordRecord {
	salt := make([]byte, passwordSaltLength)
	// crypto/rand.Read never returns an error.
	comm.Must(rand.Read(salt))
	return passwordRecord{
		Algorithm: "argon2id", Version: argon2.Version, Memory: passwordMemory, Iterations: passwordIterations, Parallelism: passwordParallelism,
		Salt: salt, Hash: argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemory, passwordParallelism, passwordHashLength),
	}
}

func verifyPassword(password string, r passwordRecord) error {
	// The record is persisted state: bound its parameters before Argon2id
	// allocates the memory it names.
	if !validPasswordRecord(r) {
		return ErrInvalidCredential
	}
	hash := argon2.IDKey([]byte(password), r.Salt, r.Iterations, r.Memory, r.Parallelism, passwordHashLength)
	if subtle.ConstantTimeCompare(hash, r.Hash) != 1 {
		return ErrInvalidCredential
	}
	return nil
}

func validPasswordRecord(r passwordRecord) bool {
	return r.Algorithm == "argon2id" && r.Version == argon2.Version && r.Memory == passwordMemory && r.Iterations == passwordIterations &&
		r.Parallelism == passwordParallelism && len(r.Salt) == passwordSaltLength && len(r.Hash) == passwordHashLength
}
