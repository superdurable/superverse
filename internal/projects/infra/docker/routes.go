// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// Routes are the public routes of the projects' applications, which one
// gateway serves for Preview and Live alike. Each route is a file in the
// lane's state directory with the owner and the generation that hold it:
// Publish writes one under a lock by compare-and-swap, and Withdraw removes
// one only for its owner. The gateway loads them from the server's loopback
// through its HTTP provider (ServeHTTP), which resolves the port each target
// container publishes now; Loaded confirms through the gateway's API that it
// serves a route, and Probe asks the gateway for a route's host.
type Routes struct {
	infrastructure *Infrastructure
	directory      string
}

// RouteOwner is who holds a public route: the project's Preview, served on
// one Sandbox instance, or one Live deployment.
type RouteOwner struct {
	Scope              string `json:"scope"`
	ProjectID          string `json:"projectId"`
	WorkspaceRequestID string `json:"workspaceRequestId,omitempty"`
	DeploymentID       string `json:"deploymentId,omitempty"`
}

// Route is one public route: the host it serves, its holder and the port of
// the container it forwards to, by the container's ID.
type Route struct {
	Host       string     `json:"host"`
	Owner      RouteOwner `json:"owner"`
	Generation uint64     `json:"generation"`
	Container  string     `json:"container"`
	Port       int        `json:"port"`
}

// GatewayEntryPoint is the gateway's entry point that serves every route.
const GatewayEntryPoint = "web"

// routeHost admits the host names PublicRoutes derive, which name the files.
var routeHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

// Publish writes route unless the host holds a route that admit refuses. A
// route of the same holder and generation is published already. A refused
// write is infra.ErrOwnershipConflict: another holder or a newer generation
// holds the host.
func (r *Routes) Publish(ctx context.Context, route Route, admit func(current Route) bool) error {
	if !routeHost.MatchString(route.Host) {
		return fmt.Errorf("invalid route host %q", route.Host)
	}
	unlock, err := r.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	current, exists, err := r.read(route.Host)
	if err != nil {
		return err
	}
	if exists {
		if current == route {
			return nil
		}
		if !admit(current) {
			return fmt.Errorf("%w: the route of %s is held by %s %s generation %d", infra.ErrOwnershipConflict, route.Host, current.Owner.Scope, current.Owner.ProjectID, current.Generation)
		}
	}
	draft, err := os.CreateTemp(r.directory, ".route-")
	if err != nil {
		return fmt.Errorf("write the route of %s: %w", route.Host, err)
	}
	defer func() { _ = os.Remove(draft.Name()) }()
	_, writeErr := draft.Write(comm.Must(json.Marshal(route)))
	if err = errors.Join(writeErr, draft.Sync(), draft.Close()); err != nil {
		return fmt.Errorf("write the route of %s: %w", route.Host, err)
	}
	if err = os.Rename(draft.Name(), r.path(route.Host)); err != nil {
		return fmt.Errorf("place the route of %s: %w", route.Host, err)
	}
	return nil
}

// Withdraw removes the route of host when owned reports that it is the
// caller's, and returns the route the host still holds, nil when it holds
// none.
func (r *Routes) Withdraw(ctx context.Context, host string, owned func(current Route) bool) (*Route, error) {
	unlock, err := r.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, exists, err := r.read(host)
	if err != nil || !exists {
		return nil, err
	}
	if !owned(current) {
		return &current, nil
	}
	if err = os.Remove(r.path(host)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &current, fmt.Errorf("remove the route of %s: %w", host, err)
	}
	return nil, nil
}

// Read returns the route of host; exists is false when it holds none.
func (r *Routes) Read(host string) (Route, bool, error) { return r.read(host) }

func (r *Routes) path(host string) string { return filepath.Join(r.directory, host+".json") }

func (r *Routes) read(host string) (Route, bool, error) {
	data, err := os.ReadFile(r.path(host))
	if errors.Is(err, fs.ErrNotExist) {
		return Route{}, false, nil
	}
	if err != nil {
		return Route{}, false, fmt.Errorf("read the route of %s: %w", host, err)
	}
	var route Route
	if err = json.Unmarshal(data, &route); err != nil || route.Host != host {
		return Route{}, false, fmt.Errorf("the route file of %s is invalid", host)
	}
	return route, true, nil
}

// lock holds the lock of the route files until the returned function runs,
// waiting for it at most until ctx ends.
func (r *Routes) lock(ctx context.Context) (func(), error) {
	file, err := os.OpenFile(filepath.Join(r.directory, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the route lock: %w", err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // a file descriptor
		if err == nil {
			return func() { _ = file.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = file.Close()
			return nil, fmt.Errorf("take the route lock: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// routes reads every route file.
func (r *Routes) routes() ([]Route, error) {
	entries, err := os.ReadDir(r.directory)
	if err != nil {
		return nil, fmt.Errorf("list the routes: %w", err)
	}
	var routes []Route
	for _, entry := range entries {
		host, isRoute := strings.CutSuffix(entry.Name(), ".json")
		if !isRoute || strings.HasPrefix(host, ".") {
			continue
		}
		route, exists, err := r.read(host)
		if err != nil {
			return nil, err
		}
		if exists {
			routes = append(routes, route)
		}
	}
	return routes, nil
}

// target is the URL at which the gateway reaches the route's container: the
// port the container publishes now, on the host. The container must still
// belong to the route's project.
func (r *Routes) target(ctx context.Context, route Route) (url, notServing string, err error) {
	container, err := r.infrastructure.api.InspectContainer(ctx, route.Container)
	if err != nil {
		return "", "", err
	}
	if container == nil || container.ID != route.Container || container.Config.Labels[deploymentnames.ProjectLabel] != route.Owner.ProjectID {
		return "", "the target container of " + route.Host + " is absent", nil
	}
	port, published := container.LoopbackPort(route.Port)
	if !published {
		return "", "the route's container is " + container.State.Status + " and publishes no port " + strconv.Itoa(route.Port) + " yet", nil
	}
	return "http://" + net.JoinHostPort(infra.HostFromContainers, port), "", nil
}

// routeName is the name of a route's router and service in the gateway.
func routeName(host string) string { return strings.ReplaceAll(host, ".", "-") }

// ServeHTTP answers the gateway's HTTP provider with the dynamic
// configuration of every route whose container runs and publishes its port
// now. A route whose container does not is left out until it does.
func (r *Routes) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	routes, err := r.routes()
	if err != nil {
		http.Error(w, "the routes are unreadable", http.StatusInternalServerError)
		return
	}
	// The gateway keeps its previous configuration when the provider answers an
	// empty one, so a sentinel router that matches no host keeps it non-empty
	// and a withdrawn last route goes away.
	routers := map[string]any{sentinel: map[string]any{"entryPoints": []string{GatewayEntryPoint}, "rule": routeRule(sentinel + ".invalid"), "service": sentinel}}
	services := map[string]any{sentinel: map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]string{"url": "http://127.0.0.1:9"}}}}}
	for _, route := range routes {
		url, notServing, err := r.target(ctx, route)
		if err != nil {
			// The gateway keeps its last configuration while the engine does not answer.
			http.Error(w, "the engine does not answer", http.StatusServiceUnavailable)
			return
		}
		if notServing != "" {
			continue
		}
		name := routeName(route.Host)
		routers[name] = map[string]any{"entryPoints": []string{GatewayEntryPoint}, "rule": routeRule(route.Host), "service": name}
		services[name] = map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]string{"url": url}}, "passHostHeader": true}}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(comm.Must(json.Marshal(map[string]any{"http": map[string]any{"routers": routers, "services": services}})))
}

func routeRule(host string) string { return "Host(`" + host + "`)" }

// sentinel names the router that keeps the provider's configuration
// non-empty; no route host can take its name, which has no dot.
const sentinel = "superverse-routes"

// gatewayClient reaches the gateway and its API, never following a redirect.
var gatewayClient = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// Loaded reports whether the gateway serves route now: its API lists the
// route's router for the route's host and the service that forwards to the
// port the route's container publishes now. Until it does, observed says what
// the gateway serves instead.
func (r *Routes) Loaded(ctx context.Context, route Route) (loaded bool, observed string, err error) {
	url, notServing, err := r.target(ctx, route)
	if err != nil || notServing != "" {
		return false, notServing, err
	}
	name := routeName(route.Host)
	var router struct {
		Rule, Service, Status string
	}
	if found, err := r.gatewayAPI(ctx, "/api/http/routers/"+name+"@http", &router); err != nil || !found {
		return false, "the gateway has not loaded the router yet", err
	}
	if router.Rule != routeRule(route.Host) || router.Status != "enabled" || strings.TrimSuffix(router.Service, "@http") != name {
		return false, "the gateway serves the router " + router.Rule + " as " + router.Status, nil
	}
	var service struct {
		LoadBalancer struct {
			Servers []struct {
				URL string `json:"url"`
			} `json:"servers"`
		} `json:"loadBalancer"`
	}
	if found, err := r.gatewayAPI(ctx, "/api/http/services/"+name+"@http", &service); err != nil || !found {
		return false, "the gateway has not loaded the service yet", err
	}
	servers := service.LoadBalancer.Servers
	if len(servers) != 1 || servers[0].URL != url {
		return false, "the gateway forwards to another target", nil
	}
	return true, "", nil
}

// gatewayAPI reads one document of the gateway's API; found is false while
// the gateway does not have it.
func (r *Routes) gatewayAPI(ctx context.Context, path string, document any) (bool, error) {
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.infrastructure.config.GatewayAPI, "/")+path, nil))
	response, err := gatewayClient.Do(request)
	if err != nil {
		return false, fmt.Errorf("ask the gateway API: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("the gateway API answered %s", response.Status)
	}
	return true, json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(document)
}

// Probe asks the gateway for path on the route of host, as a browser does,
// and returns the status it answers, without following a redirect.
func (r *Routes) Probe(ctx context.Context, host, path string) (status int, unanswered string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request := comm.Must(http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.infrastructure.config.GatewayURL, "/")+path, nil))
	request.Host = host
	response, failure := gatewayClient.Do(request)
	if failure != nil {
		return 0, failure.Error()
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode, ""
}
