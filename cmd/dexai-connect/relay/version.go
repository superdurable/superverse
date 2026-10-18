// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package relay

import (
	"fmt"
	"strconv"
	"strings"
)

// VersionPolicy is the shared minimum-and-newest-verified rule for local
// providers. A newer version may connect, with a visible compatibility warning.
type VersionPolicy struct {
	name   string
	min    [3]int
	newest [3]int
}

func NewVersionPolicy(name, minimum, newestVerified string) VersionPolicy {
	min, err := parseVersion(minimum)
	if err != nil {
		panic(err)
	}
	newest, err := parseVersion(newestVerified)
	if err != nil || compareVersion(newest, min) < 0 {
		panic("invalid newest verified version for " + name)
	}
	return VersionPolicy{name: name, min: min, newest: newest}
}

// Check refuses versions below the floor and returns one warning for a newer
// unverified version. Malformed versions cannot establish compatibility.
func (policy VersionPolicy) Check(version string) (string, error) {
	parsed, err := parseVersion(version)
	if err != nil {
		return "", fmt.Errorf("%s version %q is not a supported semantic version: %w", policy.name, version, err)
	}
	if compareVersion(parsed, policy.min) < 0 {
		return "", fmt.Errorf("%s %s is below dexai's minimum supported version %s; update %s", policy.name, version, formatVersion(policy.min), policy.name)
	}
	if compareVersion(parsed, policy.newest) > 0 {
		return fmt.Sprintf("Warning: %s %s is newer than dexai's newest verified version %s; verify provider compatibility", policy.name, version, formatVersion(policy.newest)), nil
	}
	return "", nil
}

func parseVersion(version string) ([3]int, error) {
	var result [3]int
	parts := strings.Split(version, ".")
	if len(parts) != len(result) {
		return result, fmt.Errorf("expected major.minor.patch")
	}
	for i, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return result, fmt.Errorf("expected numeric major.minor.patch")
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return result, err
		}
		result[i] = value
	}
	return result, nil
}

func compareVersion(a, b [3]int) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func formatVersion(version [3]int) string {
	return fmt.Sprintf("%d.%d.%d", version[0], version[1], version[2])
}
