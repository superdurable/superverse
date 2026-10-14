// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	projectconfig "github.com/superdurable/superversev2/internal/projects/config"
)

// liveLogReadBytes bounds the log lines one read keeps.
const liveLogReadBytes = 256 << 10

// ReadApplication reads the application Deployment that the Live deployment
// recorded, still with its accepted UID.
func (l *LiveServing) ReadApplication(ctx context.Context, deployment deployments.Snapshot) (deployments.LiveApplication, error) {
	application := deployment.Providers.Application
	namespace := l.ProjectNamespace(deployment.ProjectID)
	controller, _, err := l.API.Call(ctx, http.MethodGet, "/apis/apps/v1/namespaces/"+namespace+"/deployments/"+application.Name, nil)
	if err != nil {
		return nil, fmt.Errorf("read the Live application controller: %w", err)
	}
	if controller == nil || kubeapi.UID(controller) != application.UID {
		return nil, deployments.ErrApplicationChanged
	}
	return &liveApplication{api: l.API, namespace: namespace, controller: controller}, nil
}

// liveApplication is the application Deployment that ReadApplication read.
type liveApplication struct {
	api        *kubeapi.Client
	namespace  string
	controller kubeapi.Object
}

// Environment reads the Secrets that the application container takes its
// environment from, the workload grant among them, and returns every value long
// enough to be redacted.
func (a *liveApplication) Environment(ctx context.Context) ([]string, error) {
	secrets := map[string]kubeapi.Object{}
	var values []string
	for _, container := range podContainers(a.controller) {
		for _, item := range kubeapi.Items(container, "env") {
			entry, _ := item.(map[string]any)
			reference := kubeapi.Child(kubeapi.Child(entry, "valueFrom"), "secretKeyRef")
			name := kubeapi.String(reference, "name")
			if name == "" {
				continue
			}
			if _, read := secrets[name]; !read {
				secret, _, err := a.api.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+a.namespace+"/secrets/"+name, nil)
				if err != nil {
					return nil, fmt.Errorf("read the Live application environment: %w", err)
				}
				secrets[name] = secret
			}
			value, _ := base64.StdEncoding.DecodeString(kubeapi.String(kubeapi.Child(secrets[name], "data"), kubeapi.String(reference, "key")))
			if len(value) >= projectconfig.MinimumRedactedValueBytes {
				values = append(values, string(value))
			}
		}
	}
	return values, nil
}

func podContainers(controller kubeapi.Object) []kubeapi.Object {
	var containers []kubeapi.Object
	for _, item := range kubeapi.Items(kubeapi.Child(kubeapi.Child(kubeapi.Child(controller, "spec"), "template"), "spec"), "containers") {
		container, _ := item.(map[string]any)
		containers = append(containers, container)
	}
	return containers
}

// Log reads the last lines of the application container's log in the
// controller's newest Pod, whole lines only and not yet redacted. A container
// that has not started has no log; its state says why.
func (a *liveApplication) Log(ctx context.Context, tailLines int) (deployments.ApplicationLog, error) {
	matchLabels := kubeapi.Child(kubeapi.Child(kubeapi.Child(a.controller, "spec"), "selector"), "matchLabels")
	selector := make([]string, 0, len(matchLabels))
	for _, key := range slices.Sorted(maps.Keys(matchLabels)) {
		selector = append(selector, key+"="+kubeapi.String(matchLabels, key))
	}
	pods, _, err := a.api.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+a.namespace+"/pods?labelSelector="+url.QueryEscape(strings.Join(selector, ",")), nil)
	if err != nil {
		return deployments.ApplicationLog{}, fmt.Errorf("list the Live application Pods: %w", err)
	}
	var pod kubeapi.Object
	for _, item := range kubeapi.Items(pods, "items") {
		candidate, _ := item.(map[string]any)
		if pod == nil || kubeapi.String(kubeapi.Child(candidate, "metadata"), "creationTimestamp") > kubeapi.String(kubeapi.Child(pod, "metadata"), "creationTimestamp") {
			pod = candidate
		}
	}
	if pod == nil {
		return deployments.ApplicationLog{State: "no application Pod exists"}, nil
	}
	container := kubeapi.String(podContainers(a.controller)[0], "name")
	log := deployments.ApplicationLog{State: "starting"}
	for _, item := range kubeapi.Items(kubeapi.Child(pod, "status"), "containerStatuses") {
		status, _ := item.(map[string]any)
		if kubeapi.String(status, "name") != container {
			continue
		}
		restarts, _ := status["restartCount"].(float64)
		log.Restarts, log.State = int64(restarts), containerState(kubeapi.Child(status, "state"))
		if terminated := kubeapi.Child(kubeapi.Child(status, "lastState"), "terminated"); terminated != nil {
			log.LastTermination = containerState(kubeapi.Child(status, "lastState"))
		}
	}
	_, name, _ := kubeapi.Ref(pod)
	query := url.Values{"container": {container}, "tailLines": {strconv.Itoa(tailLines)}, "limitBytes": {strconv.Itoa(liveLogReadBytes)}}
	text, _, err := a.api.ReadText(ctx, "/api/v1/namespaces/"+a.namespace+"/pods/"+name+"/log?"+query.Encode(), liveLogReadBytes)
	var waiting *kubeapi.StatusError
	if errors.As(err, &waiting) && waiting.Status == http.StatusBadRequest {
		// The container has not started yet.
		return log, nil
	}
	if err != nil {
		return deployments.ApplicationLog{}, fmt.Errorf("read the Live application log: %w", err)
	}
	if len(text) == liveLogReadBytes {
		// The read stopped inside a line, which may hold a value redaction
		// would not recognize in part.
		text = text[:strings.LastIndexByte(text, '\n')+1]
	}
	log.Log = text
	return log, nil
}

// containerState describes a container state: running, or waiting or
// terminated with its reason and exit code.
func containerState(state kubeapi.Object) string {
	for _, phase := range []string{"running", "waiting", "terminated"} {
		detail := kubeapi.Child(state, phase)
		if detail == nil {
			continue
		}
		description := phase
		if reason := kubeapi.String(detail, "reason"); reason != "" {
			description += ": " + reason
		}
		if code, exited := detail["exitCode"].(float64); exited {
			description += fmt.Sprintf(" (exit code %d)", int(code))
		}
		return description
	}
	return "unknown"
}
