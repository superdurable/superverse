// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package platform

import (
	"crypto/x509"
	"errors"
	"net"
	"strings"
)

// ErrEndpointGone identifies a definite missing API or invalid endpoint address.
// It says nothing about revocation of a credential at the original platform.
var ErrEndpointGone = errors.New("the endpoint no longer serves this connection")

func endpointAddressGone(err error, hostname string) bool {
	matches := func(name string) bool {
		return strings.EqualFold(strings.TrimSuffix(name, "."), strings.TrimSuffix(hostname, "."))
	}
	var dns *net.DNSError
	if errors.As(err, &dns) && dns.IsNotFound && matches(dns.Name) {
		return true
	}
	var certificate x509.HostnameError
	return errors.As(err, &certificate) && matches(certificate.Host)
}
