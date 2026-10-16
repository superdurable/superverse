// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import (
	"log/slog"
	"net/http"
)

// NewClaudeClientForTest is newClaudeClient for the tests.
func NewClaudeClientForTest(httpClient *http.Client, config ClaudeClientConfig, logger *slog.Logger) (*claudeClient, error) {
	return newClaudeClient(httpClient, config, logger)
}
