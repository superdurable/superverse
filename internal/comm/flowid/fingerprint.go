// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowid

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/superdurable/superversev2/internal/comm"
)

// Fingerprint hashes safe typed input for Start RequestIDs and command
// fingerprints; callers must exclude secrets and observation timestamps.
// Callers pass plain structs, so a marshal failure is a programming error.
func Fingerprint(input any) string {
	digest := sha256.Sum256(comm.Must(json.Marshal(input)))
	return hex.EncodeToString(digest[:])
}
