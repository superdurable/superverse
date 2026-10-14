// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowid

import "fmt"

// teamSpaceIDLength is the length of every TeamSpace ID the server assigns.
const teamSpaceIDLength = 8

// TeamSpaceFlowID addresses the TeamSpaceFlow of a TeamSpace, for example
// teamspace-k3v9q2ma.
func TeamSpaceFlowID(teamSpaceID string) string {
	return "teamspace-" + teamSpaceID
}

// ValidateTeamSpaceID checks a caller-supplied TeamSpace ID.
func ValidateTeamSpaceID(teamSpaceID string) error {
	if len(teamSpaceID) != teamSpaceIDLength || !IsLowerAlphanumeric(teamSpaceID) {
		return fmt.Errorf("%w: TeamSpace ID must contain %d lowercase letters or digits", ErrInvalidIdentity, teamSpaceIDLength)
	}
	return nil
}
