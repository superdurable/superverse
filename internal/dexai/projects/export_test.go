// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

// ChildCallPending exposes the rule with which StopAndCleanChildren keeps a
// child pending to the deletion scenario, which starts its Sessions through the
// Session Service and therefore lives in the external test package.
var ChildCallPending = childCallPending

// FailureTeamSpaceAdmissionForTest is the failure code of a Project its
// TeamSpace refused, for the scenarios of the external test package.
const FailureTeamSpaceAdmissionForTest = failureTeamSpaceAdmission
