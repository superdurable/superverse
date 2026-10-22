// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxd

// WorkspacePath is a clean slash-separated path relative to the workspace
// root selected by platform callers. Model file paths go through superverse-files.
type WorkspacePath string
