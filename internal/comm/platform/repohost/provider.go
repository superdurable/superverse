// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package repohost

import (
	"fmt"
	"strings"
)

// Provider is an implementation of Host that the server configuration selects,
// the value of its repohost key. It is a struct that string literals cannot
// convert to: the providers below are its only values, and ParseProvider turns
// the name an operator wrote into one. The zero Provider names nothing.
type Provider struct{ name string }

var (
	// ProviderGitHub is GitHub, the Git host of every hosted environment.
	ProviderGitHub = Provider{name: "github"}
	// ProviderLocalGit is the local Git host inside the server, which only the
	// local lanes run.
	ProviderLocalGit = Provider{name: "localgit"}
)

// providers are all the declared providers.
var providers = []Provider{ProviderGitHub, ProviderLocalGit}

// String is the name of the provider: its configuration value and the spelling
// that profiles and receipts carry.
func (provider Provider) String() string { return provider.name }

// others names the providers other than this one, the hosts that a repository
// bound under another installation belongs to.
func (provider Provider) others() string {
	var names []string
	for _, other := range providers {
		if other != provider {
			names = append(names, other.name)
		}
	}
	return strings.Join(names, " or ")
}

// ParseProvider returns the provider an operator named. An unknown name is an
// error.
func ParseProvider(name string) (Provider, error) {
	for _, provider := range providers {
		if provider.name == name {
			return provider, nil
		}
	}
	return Provider{}, fmt.Errorf("unsupported repository host %q", name)
}
