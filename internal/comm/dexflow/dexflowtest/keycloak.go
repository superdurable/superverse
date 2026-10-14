// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflowtest

import (
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// keycloak is the lane's OAuth provider of the connection tests: the realm
// superverse-e2e with its confidential client superverse-e2e and its user
// superverse-e2e, whose secrets the lane keeps in keycloak-credentials.json.
type keycloak struct {
	// Origin is where the browser, the test and, on Native, the server reach
	// the realm; it is the tokens' issuer.
	Origin string
	// StudioOrigin is the origin whose callback the client admits as its
	// redirect URI: the lane's studio.publicOrigin.
	StudioOrigin                              string
	ClientSecret, userPassword, adminPassword string
}

// KeycloakRealmForTest is the realm of the connection tests.
const KeycloakRealmForTest = "/realms/superverse-e2e"

// KeycloakForTest reads the lane's Keycloak: SUPERVERSE_TEST_KEYCLOAK_ORIGIN
// names its origin, and the lane's private state its secrets.
func KeycloakForTest(t testing.TB) keycloak {
	t.Helper()
	origin := Setting(t, "SUPERVERSE_TEST_KEYCLOAK_ORIGIN")
	var state, profile string
	switch LocalLaneForTest(t) {
	case LaneKindForTest:
		state, profile = laneStateRoot(t), filepath.Join(repositoryRoot(t), "cmd", "superverse-server", "config", "local-kind.yaml")
	case laneNative:
		instance := nativeInstance(t)
		state = nativeStateDir(t, instance)
		profile = filepath.Join(state, "runs", nativeRunID(t, state), "native-run.yaml")
	}
	raw, err := os.ReadFile(filepath.Join(state, "keycloak-credentials.json")) //nolint:gosec // the lane's own private state
	if err != nil {
		t.Fatalf("the lane keeps no Keycloak credentials: %v", err)
	}
	var secrets struct{ ClientSecret, UserPassword, AdminPassword string }
	if err = json.Unmarshal(raw, &secrets); err != nil || secrets.ClientSecret == "" || secrets.UserPassword == "" {
		t.Fatal("the lane's Keycloak credentials do not parse")
	}
	layer, err := os.ReadFile(profile) //nolint:gosec // the lane's own configuration layer
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Studio struct {
			PublicOrigin string `yaml:"publicOrigin"`
		} `yaml:"studio"`
	}
	if err = yaml.Unmarshal(layer, &document); err != nil || document.Studio.PublicOrigin == "" {
		t.Fatalf("the lane's configuration names no studio.publicOrigin: %v", err)
	}
	return keycloak{Origin: strings.TrimSuffix(origin, "/"), StudioOrigin: document.Studio.PublicOrigin,
		ClientSecret: secrets.ClientSecret, userPassword: secrets.UserPassword, adminPassword: secrets.AdminPassword}
}

// nativeRunID is the retained run of a Native instance's state directory.
func nativeRunID(t testing.TB, state string) string {
	t.Helper()
	owner, err := os.ReadFile(filepath.Join(state, "owner.json")) //nolint:gosec // the instance's own state file
	if err != nil {
		t.Fatal(err)
	}
	var retained struct {
		RunID string `json:"runId"`
	}
	if err = json.Unmarshal(owner, &retained); err != nil || retained.RunID == "" {
		t.Fatal("the Native instance has no retained run")
	}
	return retained.RunID
}

var loginAction = regexp.MustCompile(`<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"`)

// AuthorizeForTest consents as the realm's user at authorizationURL, as a
// browser does, and returns the redirect to the callback with its state and
// code, which it does not follow. No credential is logged.
func (k keycloak) AuthorizeForTest(t testing.TB, authorizationURL string) *url.URL {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(authorizationURL) //nolint:noctx // a bounded test request
	if err != nil {
		t.Fatalf("open the authorization page: %v", err)
	}
	page, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	match := loginAction.FindSubmatch(page)
	if err != nil || response.StatusCode != http.StatusOK || match == nil {
		t.Fatalf("the authorization page answered %d without a login form", response.StatusCode)
	}
	// Keycloak marks its cookies Secure; a browser sends them to http://localhost, which it trusts.
	target, err := url.Parse(html.UnescapeString(string(match[1])))
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range jar.Cookies(&url.URL{Scheme: "https", Host: target.Host, Path: target.Path}) {
		jar.SetCookies(&url.URL{Scheme: "http", Host: target.Host, Path: "/"}, []*http.Cookie{{Name: cookie.Name, Value: cookie.Value, Path: "/"}})
	}
	response, err = client.PostForm(target.String(), url.Values{"username": {"superverse-e2e"}, "password": {k.userPassword}, "credentialId": {""}}) //nolint:noctx // a bounded test request
	if err != nil {
		t.Fatalf("sign in to Keycloak: %v", err)
	}
	_ = response.Body.Close()
	location, err := response.Location()
	if errors.Is(err, http.ErrNoLocation) || err != nil || response.StatusCode != http.StatusFound {
		t.Fatalf("the sign-in answered %d without a redirect", response.StatusCode)
	}
	return location
}

// LogoutUserForTest ends every Keycloak session of the realm's user, which
// invalidates its refresh tokens, through the admin API.
func (k keycloak) LogoutUserForTest(t testing.TB) {
	t.Helper()
	response, err := http.PostForm(k.Origin+"/realms/master/protocol/openid-connect/token", //nolint:noctx // a bounded test request
		url.Values{"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {"admin"}, "password": {k.adminPassword}})
	if err != nil {
		t.Fatal(err)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	err = json.NewDecoder(response.Body).Decode(&token)
	_ = response.Body.Close()
	if err != nil || token.AccessToken == "" {
		t.Fatalf("the Keycloak admin token answered %d", response.StatusCode)
	}
	admin := func(method, path string) *http.Response {
		request, err := http.NewRequest(method, k.Origin+"/admin"+KeycloakRealmForTest+path, nil) //nolint:noctx // a bounded test request
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token.AccessToken)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	users := admin(http.MethodGet, "/users?exact=true&username=superverse-e2e")
	var found []struct {
		ID string `json:"id"`
	}
	err = json.NewDecoder(users.Body).Decode(&found)
	_ = users.Body.Close()
	if err != nil || len(found) != 1 {
		t.Fatalf("the Keycloak user lookup answered %d", users.StatusCode)
	}
	logout := admin(http.MethodPost, "/users/"+found[0].ID+"/logout")
	_ = logout.Body.Close()
	if logout.StatusCode != http.StatusNoContent {
		t.Fatalf("the Keycloak logout answered %d", logout.StatusCode)
	}
}
