// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowid

import (
	"regexp"
	"strings"
)

var (
	commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256HexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// IsCommitSHA reports whether value is a full lowercase Git commit SHA.
func IsCommitSHA(value string) bool {
	return commitSHAPattern.MatchString(value)
}

// IsSHA256Hex reports whether value is a lowercase hex SHA-256 sum.
func IsSHA256Hex(value string) bool {
	return sha256HexPattern.MatchString(value)
}

// IsSHA256Digest reports whether value is a sha256:<hex> content digest.
func IsSHA256Digest(value string) bool {
	sum, found := strings.CutPrefix(value, "sha256:")
	return found && IsSHA256Hex(sum)
}
