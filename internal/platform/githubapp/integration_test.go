// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package githubapp

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestGitHubInstallationScopeIntegration asks GitHub, as the App, for its
// installation and its own description. Missing or invalid credentials fail the
// test.
func TestGitHubInstallationScopeIntegration(t *testing.T) {
	if os.Getenv("SUPERVERSE_TEST_GITHUB_EFFECTS") != "true" {
		t.Fatal("requires configured V2 GitHub App")
	}
	app, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_APP_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub App ID unavailable")
	}
	installation, err := strconv.ParseInt(os.Getenv("SUPERVERSE_TEST_GITHUB_INSTALLATION_ID"), 10, 64)
	if err != nil {
		t.Fatal("GitHub installation ID unavailable")
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("GITHUB_APP_PRIVATE_KEY_BASE64"))
	if err != nil {
		t.Fatal("GitHub key encoding invalid")
	}
	key, err := ParsePrivateKey(raw)
	if err != nil {
		t.Fatal("GitHub key is unavailable")
	}
	client := &App{AppID: app, InstallationID: installation, PrivateKey: key}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var found struct{ ID int64 }
	if err = client.CallAsApp(ctx, http.MethodGet, "/app/installations/"+strconv.FormatInt(client.InstallationID, 10), &found); err != nil {
		t.Fatal(err)
	}
	if found.ID != client.InstallationID {
		t.Fatal("GitHub installation identity mismatch")
	}
	var appInfo struct {
		Slug, HTMLURL string
		Permissions   map[string]string
		Owner         struct{ Login string }
	}
	if err = client.CallAsApp(ctx, http.MethodGet, "/app", &appInfo); err != nil {
		t.Fatal(err)
	}
	t.Logf("app slug=%s owner=%s requested-permissions=%v", appInfo.Slug, appInfo.Owner.Login, appInfo.Permissions)
}
