// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package configitems

import (
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
)

// Live's next publish is resolved from three documents: the sparse
// next-publish layer, which holds only what Live chose for itself; the
// version Preview applied, without its secrets; and the version current Live
// runs. Every resolution is in memory; only the layer and a version's frozen
// copy are stored.

// Declared is an item a source declares, with why it can never follow
// Preview, the zero refusal when it can.
type Declared struct {
	Key
	Refusal appmanifest.FollowRefusal
}

// Seed is the layer of a Project that has none yet, computed from what runs:
// of current Live's items, every secret, every connection of an
// endpoint-bound connector and every item whose value differs from
// Preview's applied one (or that Preview has not applied) becomes Live's
// own; the others follow Preview. It keeps current Live's credential broker,
// so running Live changes in nothing.
func Seed(current connectorconfig.Configuration, preview *connectorconfig.Configuration, endpointBound func(connectorID string) bool) connectorconfig.Configuration {
	previews := map[Key]entry{}
	if preview != nil {
		previews = documentItems(*preview)
	}
	onLive, own := documentItems(current), map[Key]entry{}
	for key, live := range onLive {
		previewed, inPreview := previews[key]
		if live.secret || key.Kind == KindConnection && endpointBound(key.ConnectorID) || !inPreview || !live.equal(previewed) {
			own[key] = live
		}
	}
	return itemsDocument(anchored(own, onLive), current.CredentialBroker)
}

// Resolve resolves every declared item of the next publish: Live's own value
// from layer, else Preview's applied value, else, when the applied version
// lacks the item, current Live's value; an item without any is a gap, which
// readiness reports. An item that cannot follow takes only Live's own value.
// It returns the resolved document, with the layer's credential broker or
// else current Live's, and the items, followed by current Live's items the
// source no longer declares. Preview's connection entries are Preview's
// selections, which a frozen copy selects again for the Live scope.
func Resolve(layer, current connectorconfig.Configuration, preview *connectorconfig.Configuration, declared []Declared) (connectorconfig.Configuration, []Item) {
	layered, onLive := ownItems(layer), documentItems(current)
	previews := map[Key]entry{}
	if preview != nil {
		previews = documentItems(*preview)
	}
	resolved := map[Key]entry{}
	items := make([]Item, 0, len(declared))
	declaredKeys := map[Key]bool{}
	for _, declaration := range declared {
		key := declaration.Key
		declaredKeys[key] = true
		own, inLayer := layered[key]
		live, isLive := onLive[key]
		previewed, inPreview := previews[key]
		inPreview = inPreview && !previewed.secret
		item := Item{Key: key, State: FollowsPreview, FollowRefusal: declaration.Refusal}
		var value entry
		present := true
		switch {
		case inLayer:
			item.State, value = Own, own
		case !declaration.Refusal.IsZero():
			item.State, present = Own, false
		case inPreview:
			value = previewed
		case isLive:
			value = live
		default:
			present = false
		}
		if present {
			resolved[key] = value
		}
		item.DiffersFromLive = present != isLive || present && !value.equal(live)
		item.DiffersFromPreview = preview != nil && (present != inPreview || present && !value.equal(previewed))
		if isLive {
			item.LiveValueJSON = live.display()
		}
		if inPreview {
			item.PreviewValueJSON = previewed.display()
		}
		items = append(items, item)
	}
	for _, key := range sortedKeys(onLive) {
		if !declaredKeys[key] {
			items = append(items, Item{Key: key, State: NoLongerUsed, DiffersFromLive: true, LiveValueJSON: onLive[key].display()})
		}
	}
	broker := layer.CredentialBroker
	if broker == nil {
		broker = current.CredentialBroker
	}
	return itemsDocument(resolved, broker), items
}

// Selected reports, for each connection item of document, the TeamSpace
// connection it selects, by key.
func Selected(document connectorconfig.Configuration) map[Key]string {
	selected := map[Key]string{}
	for _, connection := range document.Connections {
		if connection.SharedConnectionID != "" {
			selected[ConnectionKey(connection.ConnectorID, connection.ConnectionName)] = connection.SharedConnectionID
		}
	}
	return selected
}

// Holds reports whether a layer holds Live's own value of key.
func Holds(layer connectorconfig.Configuration, key Key) bool {
	_, found := ownItems(layer)[key]
	return found
}
