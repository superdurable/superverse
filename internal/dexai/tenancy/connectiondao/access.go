// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectiondao

import (
	"context"
	"database/sql"
	"fmt"
)

// TeamSpaceAccess is what the read model says about an account in a
// TeamSpace: the TeamSpace's status, the account's TeamSpace role, empty for a
// non-member, and whether it holds one of the builder roles in a Project of
// the TeamSpace.
type TeamSpaceAccess struct {
	Status, Role string
	BuildsAny    bool
}

// Access reads an account's TeamSpace role and whether it builds any of the
// TeamSpace's Projects outside the excluded Project status. A TeamSpace the
// read model does not hold, or holds in the excluded status, is ErrNotFound.
func (d *ConnectionDAO) Access(ctx context.Context, teamSpaceID, accountID string, builderRoles []string, excludedTeamSpaceStatus, excludedProjectStatus string) (TeamSpaceAccess, error) {
	var teamSpaceStatus string
	var role sql.Null[string]
	var builds bool
	err := d.db.QueryRowContext(ctx, `SELECT s.team_space_status, m.role, m.role IS NOT NULL AND EXISTS (SELECT FROM projects p
			JOIN project_members pm ON pm.project_id = p.project_id AND pm.account_id = $2 AND pm.role = ANY($3)
			WHERE p.team_space_id = s.team_space_id AND p.project_status IS NOT NULL AND p.project_status <> $5)
		FROM team_spaces s
		LEFT JOIN team_space_members m ON m.team_space_id = s.team_space_id AND m.account_id = $2
		WHERE s.team_space_id = $1 AND s.team_space_status IS NOT NULL AND s.team_space_status <> $4`,
		teamSpaceID, accountID, nonNil(builderRoles), excludedTeamSpaceStatus, excludedProjectStatus).Scan(&teamSpaceStatus, &role, &builds)
	if err = noRows(err); err != nil {
		if isSentinel(err) {
			return TeamSpaceAccess{}, err
		}
		return TeamSpaceAccess{}, fmt.Errorf("read the TeamSpace access: %w", err)
	}
	return TeamSpaceAccess{Status: teamSpaceStatus, Role: role.V, BuildsAny: builds}, nil
}

// BuildsEveryUser reports whether the account holds one of the builder roles
// in every Project, outside the excluded status, that uses the connection in
// scope: one NOT EXISTS over the usages joined to project_members. A usage
// whose Project the read model does not hold yet counts as a Project the
// account does not build.
func (d *ConnectionDAO) BuildsEveryUser(ctx context.Context, teamSpaceID, connectionID, scope, accountID string, builderRoles []string, excludedProjectStatus string) (bool, error) {
	var builds bool
	err := d.db.QueryRowContext(ctx, `SELECT NOT EXISTS (SELECT FROM team_space_connection_usages u
			LEFT JOIN projects p ON p.project_id = u.project_id
			WHERE u.team_space_id = $1 AND u.connection_id = $2 AND u.scope = $3
				AND p.project_status IS DISTINCT FROM $6
				AND NOT EXISTS (SELECT FROM project_members m WHERE m.project_id = u.project_id AND m.account_id = $4 AND m.role = ANY($5)))`,
		teamSpaceID, connectionID, scope, accountID, nonNil(builderRoles), excludedProjectStatus).Scan(&builds)
	if err != nil {
		return false, fmt.Errorf("read the builders of the connection's Projects: %w", err)
	}
	return builds, nil
}
