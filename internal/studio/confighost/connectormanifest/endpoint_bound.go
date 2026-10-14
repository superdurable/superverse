// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectormanifest

// A connector is endpoint-bound when one of its connections is tied to one
// address: its provider signs the inbound webhooks it delivers with a secret
// that belongs to one endpoint, so a connection cannot serve Preview's and
// Live's addresses at once, and Live's next publish never follows Preview's
// connection of it. The platform keeps this list until the connectors library
// declares it in the manifest.

// endpointBinding is one endpoint-bound connector and why it is one.
type endpointBinding struct {
	connectorID string
	reason      string
}

// endpointBoundConnectors is the closed set of endpoint-bound connectors, by
// connector ID, each with its reason in a sentence.
var endpointBoundConnectors = []endpointBinding{
	{"stripe", "Stripe signs each webhook endpoint's events with that endpoint's own signing secret, so a connection receives events at one address."},
	{"typeform", "Typeform signs a form's webhook with the secret registered for its one URL, so a connection receives responses at one address."},
	{"helpscout", "Help Scout signs a webhook with the secret registered for its one URL, so a connection receives conversation events at one address."},
	{"calendly", "Calendly signs a webhook subscription with the signing key of its one callback URL, so a connection receives invitee events at one address."},
}

// EndpointBoundReason returns why connections of connectorID are tied to one
// address, and false for a connector whose connections can serve both scopes.
func EndpointBoundReason(connectorID string) (string, bool) {
	for _, binding := range endpointBoundConnectors {
		if binding.connectorID == connectorID {
			return binding.reason, true
		}
	}
	return "", false
}
