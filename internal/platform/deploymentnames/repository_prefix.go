// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deploymentnames

// RepositoryPrefix is the first part of the name of a Project's managed
// repository: the lane the server runs in, or the hosted environment. The two
// local lanes share the test environment with AWS test, so the environment
// alone cannot tell their repositories apart. It is a struct that string
// literals cannot convert to: the values below and HostedRepositoryPrefix are
// the only ways to get one, and the server's composition chooses among them.
type RepositoryPrefix struct{ name string }

var (
	// RepositoryPrefixLocalNative names the repositories of Local Native.
	RepositoryPrefixLocalNative = RepositoryPrefix{name: "localnative"}
	// RepositoryPrefixLocalKind names the repositories of Local Kind, also when
	// it runs against GitHub.
	RepositoryPrefixLocalKind = RepositoryPrefix{name: "localkind"}
)

// HostedRepositoryPrefix is the prefix of a hosted environment: its name, so
// that the repositories of AWS test, staging and prod start with test-,
// staging- and prod-.
func HostedRepositoryPrefix(environment Environment) RepositoryPrefix {
	// MustFor panics for the zero Environment, which names nothing.
	_ = MustFor(environment)
	return RepositoryPrefix(environment)
}

// String is the spelling in the repository name.
func (prefix RepositoryPrefix) String() string { return prefix.name }
