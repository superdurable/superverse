// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// dex-debug-tunnel connects the pinned plaintext Dex CLI to platform TLS/gRPC.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// tunnelEnvironments are the hosted environments whose platform Dex has a
// debug ingress.
var tunnelEnvironments = []deploymentnames.Environment{deploymentnames.EnvironmentTest, deploymentnames.EnvironmentStaging}

// tunnelEnvironmentNames lists the names of tunnelEnvironments.
func tunnelEnvironmentNames(separator string) string {
	names := make([]string, len(tunnelEnvironments))
	for i, environment := range tunnelEnvironments {
		names[i] = environment.String()
	}
	return strings.Join(names, separator)
}

func run() error {
	environmentName := flag.String("environment", deploymentnames.EnvironmentTest.String(), "hosted platform: "+tunnelEnvironmentNames(" or "))
	port := flag.Int("port", 28814, "localhost plaintext HTTP/2 port")
	flag.Parse()
	environment, err := deploymentnames.ParseEnvironment(*environmentName)
	if flag.NArg() != 0 || err != nil || !slices.Contains(tunnelEnvironments, environment) || *port < 1024 || *port > 65535 {
		return errors.New("use --environment " + tunnelEnvironmentNames("|") + " and --port 1024..65535")
	}
	target := &url.URL{Scheme: "https", Host: "dex-server." + deploymentnames.MustFor(environment).DNSDomain}
	protocols := new(http.Protocols)
	protocols.SetHTTP2(true)
	transport := &http.Transport{
		Protocols:             protocols,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxConnsPerHost:       2,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       60 * time.Second,
		DisableCompression:    true,
	}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(request *httputil.ProxyRequest) {
			// SetURL rewrites HTTP/2 :authority as well as the URL; a TLS-only
			// byte relay incorrectly sends localhost to the ALB hostname rule.
			request.SetURL(target)
		},
		ErrorHandler: func(response http.ResponseWriter, _ *http.Request, _ error) {
			// Never log protobufs, credentials, headers or Flow state.
			http.Error(response, "Platform debug upstream unavailable", http.StatusBadGateway)
		},
	}
	requests := make(chan struct{}, 32)
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case requests <- struct{}{}:
			defer func() { <-requests }()
			proxy.ServeHTTP(response, request)
		default:
			http.Error(response, "Concurrent debug request limit reached", http.StatusServiceUnavailable)
		}
	})
	localProtocols := new(http.Protocols)
	localProtocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Handler: handler, Protocols: localProtocols,
		HTTP2:             &http.HTTP2Config{MaxConcurrentStreams: 32},
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
		ErrorLog: log.New(io.Discard, "", 0),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return fmt.Errorf("listen on owned localhost port: %w", err)
	}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	fmt.Printf("%s -> %s:443 (verified TLS, HTTP/2, fixed authority)\n", listener.Addr(), target.Host)
	select {
	case err := <-finished:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve debug proxy: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			if closeErr := server.Close(); closeErr != nil {
				return errors.Join(err, closeErr)
			}
		}
		if err := <-finished; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
