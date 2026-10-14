// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package configitems

import (
	"bytes"
	"cmp"
	"encoding/json"
	"maps"
	"slices"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
)

// A configuration item is one setting of a configuration document that Setup
// shows on its own: a connection name's selection, a trigger binding, a Step
// setting or an environment variable. Items are compared by key; a secret
// only by its reference.

// Kind is the kind of a configuration item.
type Kind string

const (
	KindConnection     Kind = "CONNECTION"
	KindTriggerBinding Kind = "TRIGGER_BINDING"
	KindStepSetting    Kind = "STEP_SETTING"
	KindEnvironment    Kind = "ENVIRONMENT"
)

// State is how an item compares: a Preview head's item with the applied
// version, and an item of Live's next publish by where its value comes from.
type State string

const (
	Unchanged State = "UNCHANGED"
	Changed   State = "CHANGED"
	Added     State = "ADDED"
	Removed   State = "REMOVED"
	// FollowsPreview is an item the next-publish layer does not hold.
	FollowsPreview State = "FOLLOWS_PREVIEW"
	// Own is an item the next-publish layer holds, or one that can never
	// follow Preview.
	Own State = "OWN"
	// NoLongerUsed is an item of current Live that the source no longer
	// declares.
	NoLongerUsed State = "NO_LONGER_USED"
)

// Warning is a pitfall an item's value runs into.
type Warning string

const (
	// ChannelShared is a Slack trigger binding whose channel another Project's
	// Live listens to through the same TeamSpace connection.
	ChannelShared Warning = "CHANNEL_SHARED"
	// ConnectionDeleted is a connection name whose selected TeamSpace
	// connection is missing or no longer ACTIVE.
	ConnectionDeleted Warning = "CONNECTION_DELETED"
)

// Key names one item. Only the names of its kind are set.
type Key struct {
	Kind            Kind   `json:"kind"`
	ConnectorID     string `json:"connectorId,omitempty"`
	ConnectionName  string `json:"connectionName,omitempty"`
	TriggerName     string `json:"triggerName,omitempty"`
	BindingName     string `json:"bindingName,omitempty"`
	OperationID     string `json:"operationId,omitempty"`
	FlowType        string `json:"flowType,omitempty"`
	StepType        string `json:"stepType,omitempty"`
	EnvironmentName string `json:"environmentName,omitempty"`
}

// ConnectionKey is the key of a connection name's selection.
func ConnectionKey(connectorID, connectionName string) Key {
	return Key{Kind: KindConnection, ConnectorID: connectorID, ConnectionName: connectionName}
}

// Item is one item with its state. The value fields never hold a secret.
type Item struct {
	Key
	State              State     `json:"state"`
	DiffersFromLive    bool      `json:"differsFromLive"`
	DiffersFromPreview bool      `json:"differsFromPreview"`
	Warnings           []Warning `json:"warnings,omitempty"`
	WarningProjectName string    `json:"warningProjectName,omitempty"`
	// FollowRefusal is why a Live item can never follow Preview.
	FollowRefusal appmanifest.FollowRefusal `json:"followRefusal,omitzero"`
	// PreviewValueJSON is the non-secret value of Preview's applied version.
	PreviewValueJSON json.RawMessage `json:"previewValueJson,omitempty"`
	// LiveValueJSON is current Live's non-secret value.
	LiveValueJSON json.RawMessage `json:"liveValueJson,omitempty"`
}

// entry is one item's entry of a document: what it compares by and how it
// is written into a document.
type entry struct {
	value       []byte
	secret      bool
	connection  *connectorconfig.ConnectionConfiguration
	trigger     *connectorconfig.TriggerConfiguration
	operation   *connectorconfig.OperationConfiguration
	environment *connectorconfig.EnvironmentValue
}

func (e entry) equal(other entry) bool { return bytes.Equal(e.value, other.value) }

// display is the entry's value as Setup shows it, or nil for a secret. A
// connection shows the TeamSpace connection it selects.
func (e entry) display() json.RawMessage {
	switch {
	case e.secret:
		return nil
	case e.connection != nil:
		return comm.Must(json.Marshal(map[string]string{"teamSpaceConnectionId": e.connection.SharedConnectionID}))
	case e.environment != nil:
		return comm.Must(json.Marshal(map[string]*string{"value": e.environment.Value}))
	}
	return e.value
}

// documentItems returns the items of document by key. A connection compares
// by the TeamSpace connection it selects: a freeze copies the connection's
// settings, so they are not the selection's. Settings compare as canonical
// JSON.
func documentItems(document connectorconfig.Configuration) map[Key]entry {
	items := map[Key]entry{}
	for _, connection := range document.Connections {
		value := comm.Must(json.Marshal([]string{connection.SharedConnectionID, connection.ModulePath, connection.AuthMethodID}))
		items[ConnectionKey(connection.ConnectorID, connection.ConnectionName)] = entry{value: value, connection: &connection}
	}
	for _, binding := range document.TriggerBindings {
		key := Key{Kind: KindTriggerBinding, ConnectorID: binding.ConnectorID, ConnectionName: binding.ConnectionName, TriggerName: binding.TriggerName, BindingName: binding.BindingName}
		items[key] = entry{value: canonicalJSON(binding.Configuration), trigger: &binding}
	}
	for _, operation := range document.OperationConfigurations {
		key := Key{
			Kind: KindStepSetting, ConnectorID: operation.ConnectorID, ConnectionName: operation.ConnectionName,
			OperationID: operation.OperationID, FlowType: operation.FlowType, StepType: operation.StepType,
		}
		items[key] = entry{value: canonicalJSON(operation.Configuration), operation: &operation}
	}
	for name, environment := range document.Environment {
		// A secret compares by its reference only.
		items[Key{Kind: KindEnvironment, EnvironmentName: name}] = entry{
			value: comm.Must(json.Marshal(environment)), secret: environment.SecretRef != nil, environment: &environment,
		}
	}
	return items
}

// canonicalJSON re-encodes value with sorted object keys, so equal settings
// compare equal whatever their stored order.
func canonicalJSON(value json.RawMessage) []byte {
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return value
	}
	return comm.Must(json.Marshal(decoded))
}

// itemsDocument writes items into a document with broker as its credential
// broker, in key order.
func itemsDocument(items map[Key]entry, broker *connectorconfig.CredentialBrokerConfiguration) connectorconfig.Configuration {
	document := connectorconfig.Configuration{
		Connections: []connectorconfig.ConnectionConfiguration{}, TriggerBindings: []connectorconfig.TriggerConfiguration{},
		OperationConfigurations: []connectorconfig.OperationConfiguration{}, Environment: map[string]connectorconfig.EnvironmentValue{},
		CredentialBroker: broker,
	}
	for _, key := range sortedKeys(items) {
		item := items[key]
		switch {
		case item.connection != nil:
			document.Connections = append(document.Connections, *item.connection)
		case item.trigger != nil:
			document.TriggerBindings = append(document.TriggerBindings, *item.trigger)
		case item.operation != nil:
			document.OperationConfigurations = append(document.OperationConfigurations, *item.operation)
		case item.environment != nil:
			document.Environment[key.EnvironmentName] = *item.environment
		}
	}
	return document
}

// SameItems reports whether two documents hold the same items with the same
// values and the same credential broker.
func SameItems(first, second connectorconfig.Configuration) bool {
	firstItems, secondItems := documentItems(first), documentItems(second)
	brokers := (first.CredentialBroker == nil) == (second.CredentialBroker == nil) &&
		(first.CredentialBroker == nil || *first.CredentialBroker == *second.CredentialBroker)
	return brokers && len(firstItems) == len(secondItems) && !slices.ContainsFunc(sortedKeys(firstItems), func(key Key) bool {
		other, found := secondItems[key]
		return !found || !other.equal(firstItems[key])
	})
}

// Without is document without the items of keys. A connection whose trigger
// bindings or Step settings stay keeps an anchor in their place.
func Without(document connectorconfig.Configuration, keys ...Key) connectorconfig.Configuration {
	original, items := documentItems(document), documentItems(document)
	for _, key := range keys {
		delete(items, key)
	}
	return withIdentity(itemsDocument(anchored(items, original), document.CredentialBroker), document)
}

// A Live next-publish layer holds a trigger binding or Step setting of Live's
// own while their connection name follows Preview, but a configuration
// document holds no binding or setting without its connection's entry. The
// layer then holds an anchor: the connection's entry without a selected
// TeamSpace connection, which is no value of Live's own.

// isAnchor reports whether a connection entry is an anchor.
func isAnchor(connection *connectorconfig.ConnectionConfiguration) bool {
	return connection != nil && connection.SharedConnectionID == ""
}

// Anchor is the anchor of a connection name: its declared module, provider
// and method without a selected TeamSpace connection.
func Anchor(connectorID, connectionName, modulePath, provider, authMethodID string) connectorconfig.ConnectionConfiguration {
	return connectorconfig.ConnectionConfiguration{
		ConnectorID: connectorID, ConnectionName: connectionName, ModulePath: modulePath, Provider: provider, AuthMethodID: authMethodID, Configuration: json.RawMessage(`{}`),
	}
}

// ownItems is the items of a layer that are Live's own: every item but the anchors.
func ownItems(layer connectorconfig.Configuration) map[Key]entry {
	items := documentItems(layer)
	for key, item := range items {
		if isAnchor(item.connection) {
			delete(items, key)
		}
	}
	return items
}

// anchored is items with an anchor for each connection name that a binding or
// Step setting of items needs and items lack, made from the name's entry in
// one of sources, and without the anchors nothing needs.
func anchored(items map[Key]entry, sources ...map[Key]entry) map[Key]entry {
	needed := map[Key]bool{}
	for key, item := range items {
		if item.trigger != nil || item.operation != nil {
			needed[ConnectionKey(key.ConnectorID, key.ConnectionName)] = true
		}
	}
	for key, item := range items {
		if isAnchor(item.connection) && !needed[key] {
			delete(items, key)
		}
	}
	for key := range needed {
		if _, present := items[key]; present {
			continue
		}
		for _, source := range sources {
			if from, found := source[key]; found && from.connection != nil {
				anchor := Anchor(key.ConnectorID, key.ConnectionName, from.connection.ModulePath, from.connection.Provider, from.connection.AuthMethodID)
				items[key] = entry{value: comm.Must(json.Marshal([]string{"", anchor.ModulePath, anchor.AuthMethodID})), connection: &anchor}
				break
			}
		}
	}
	return items
}

// withIdentity gives document the schema, scope and revision of the stored
// document it replaces.
func withIdentity(document, stored connectorconfig.Configuration) connectorconfig.Configuration {
	document.SchemaVersion, document.Scope, document.Revision = stored.SchemaVersion, stored.Scope, stored.Revision
	return document
}

func sortedKeys(items map[Key]entry) []Key {
	return slices.SortedFunc(maps.Keys(items), compareKeys)
}

// compareKeys orders keys by kind, then by their names.
func compareKeys(first, second Key) int {
	return cmp.Or(
		cmp.Compare(first.Kind, second.Kind), cmp.Compare(first.ConnectorID, second.ConnectorID), cmp.Compare(first.ConnectionName, second.ConnectionName),
		cmp.Compare(first.TriggerName, second.TriggerName), cmp.Compare(first.BindingName, second.BindingName), cmp.Compare(first.OperationID, second.OperationID),
		cmp.Compare(first.FlowType, second.FlowType), cmp.Compare(first.StepType, second.StepType), cmp.Compare(first.EnvironmentName, second.EnvironmentName),
	)
}

// PreviewItems compares head, as a freeze would write it, with the applied
// version: an item of both is UNCHANGED or CHANGED, one only the head holds
// ADDED and one only the applied version holds REMOVED. Before the first
// Apply every item of the head is ADDED. A connection also compares by its
// settings, which a freeze copies from the TeamSpace connection, and one of
// deleted, whose TeamSpace connection is gone, is CHANGED and warns.
func PreviewItems(head, applied connectorconfig.Configuration, deleted []Key) []Item {
	heads, applieds := previewDocumentItems(head), previewDocumentItems(applied)
	union := maps.Clone(heads)
	maps.Copy(union, applieds)
	items := make([]Item, 0, len(union))
	for _, key := range sortedKeys(union) {
		current, inHead := heads[key]
		previous, inApplied := applieds[key]
		state := Unchanged
		switch {
		case !inApplied:
			state = Added
		case !inHead:
			state = Removed
		case !current.equal(previous):
			state = Changed
		}
		item := Item{Key: key, State: state}
		if slices.Contains(deleted, key) {
			item.Warnings = []Warning{ConnectionDeleted}
			if item.State == Unchanged {
				item.State = Changed
			}
		}
		items = append(items, item)
	}
	return items
}

func previewDocumentItems(document connectorconfig.Configuration) map[Key]entry {
	items := documentItems(document)
	for key, item := range items {
		if item.connection != nil {
			item.value = comm.Must(json.Marshal([]json.RawMessage{item.value, canonicalJSON(item.connection.Configuration)}))
			items[key] = item
		}
	}
	return items
}
