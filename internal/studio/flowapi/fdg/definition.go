// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package fdg

import "github.com/superdurable/dex/sdk-go/gen/dexpb"

// V2Definition describes one Flow type's Dex Web v2 contract.
type V2Definition struct {
	IndexedAttributes        []V2IndexedAttribute        `json:"indexedAttributes"`
	Summary                  V2RPCView                   `json:"summary"`
	Display                  V2RPCView                   `json:"display"`
	Actions                  []V2Action                  `json:"actions"`
	Start                    *V2StartDefinition          `json:"start,omitempty"`
	ConnectorTriggerBindings []V2ConnectorTriggerBinding `json:"connectorTriggerBindings,omitempty"`
}

// V2ConnectorTriggerBinding describes one configurable external Trigger binding.
type V2ConnectorTriggerBinding struct {
	ConnectorID          string                     `json:"connectorId"`
	TriggerName          string                     `json:"triggerName"`
	ConnectionName       string                     `json:"connectionName"`
	BindingName          string                     `json:"bindingName"`
	ModulePath           string                     `json:"modulePath"`
	ModuleVersion        string                     `json:"moduleVersion"`
	ConfigurationEnabled bool                       `json:"configurationEnabled"`
	ConfigurationUI      V2ConnectorConfigurationUI `json:"configurationUI"`
}

// V2ConnectorConfigurationUI declares a Flow-owned composition of Connector UI units.
type V2ConnectorConfigurationUI struct {
	Units []V2ConnectorUIUnit `json:"units"`
}

// V2ConnectorUIUnit describes one reusable Connector UI unit instance.
type V2ConnectorUIUnit struct {
	ID          string                 `json:"id"`
	UnitID      string                 `json:"unitId"`
	Label       string                 `json:"label"`
	Description string                 `json:"description,omitempty"`
	Required    bool                   `json:"required"`
	Bindings    []V2ConnectorUIBinding `json:"bindings"`
}

// V2ConnectorUIBinding maps a unit port into the use configuration object.
type V2ConnectorUIBinding struct {
	Port        string `json:"port"`
	JSONPointer string `json:"jsonPointer"`
}

// V2StartDefinition describes the Start Step and its JSON input.
type V2StartDefinition struct {
	StepType string             `json:"stepType"`
	Input    V2StartInputSchema `json:"input"`
	// SkipWaitFor is derived from the validated graph's Start Step phase and is never browser input.
	SkipWaitFor bool `json:"-"`
}

// V2StartInputSchema describes one recursive JSON input value.
type V2StartInputSchema struct {
	Kind        startInputKind          `json:"kind"`
	Nullable    bool                    `json:"nullable,omitempty"`
	Format      string                  `json:"format,omitempty"`
	EnumValues  []v2StartInputEnumValue `json:"enumValues,omitempty"`
	Minimum     string                  `json:"minimum,omitempty"`
	Maximum     string                  `json:"maximum,omitempty"`
	Fields      []v2StartInputField     `json:"fields,omitempty"`
	Items       *V2StartInputSchema     `json:"items,omitempty"`
	Values      *V2StartInputSchema     `json:"values,omitempty"`
	FixedLength *int64                  `json:"fixedLength,omitempty"`
}

// v2StartInputEnumValue describes one source-named enum option.
type v2StartInputEnumValue struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// v2StartInputField describes one JSON object field.
type v2StartInputField struct {
	Name     string             `json:"name"`
	Required bool               `json:"required"`
	Schema   V2StartInputSchema `json:"schema"`
}

// V2IndexedAttribute describes one searchable Attribute.
type V2IndexedAttribute struct {
	AttributeKey string    `json:"attributeKey"`
	IndexKey     string    `json:"indexKey"`
	IndexType    indexType `json:"indexType"`
	ValueType    valueType `json:"valueType"`
	Description  string    `json:"description"`
}

// V2RPCView describes Summary or Display fields.
type V2RPCView struct {
	RPCName string        `json:"rpcName"`
	Fields  []V2ViewField `json:"fields"`
}

// V2ViewField describes one ordered RPC output field.
type V2ViewField struct {
	AttributeKey string    `json:"attributeKey"`
	ValueType    valueType `json:"valueType"`
	Editable     bool      `json:"editable"`
	Description  string    `json:"description"`
	// UISlot is the named position this field takes in a row or drawer. Empty means the detail list.
	UISlot uiSlot `json:"uiSlot,omitempty"`
}

// V2Action describes one operator RPC.
type V2Action struct {
	RPCName            string            `json:"rpcName"`
	Label              string            `json:"label"`
	RequiredPermission string            `json:"requiredPermission"`
	Condition          V2ActionCondition `json:"condition"`
	Input              V2ActionInput     `json:"input"`
}

// V2ActionCondition describes an Action's visibility predicate.
type V2ActionCondition struct {
	AttributeKey string `json:"attributeKey"`
	Operator     string `json:"operator"`
	Values       []any  `json:"values"`
}

// V2ActionInput describes a none or object RPC input.
type V2ActionInput struct {
	Kind   actionInputKind      `json:"kind"`
	Fields []V2ActionInputField `json:"fields"`
}

// IsNone reports whether the Action's RPC takes no input.
func (input V2ActionInput) IsNone() bool { return input.Kind == actionInputKindNone }

// V2ActionInputField describes one ordered Action input.
type V2ActionInputField struct {
	FieldName    string            `json:"fieldName"`
	ValueType    valueType         `json:"valueType"`
	Source       actionInputSource `json:"source"`
	AttributeKey string            `json:"attributeKey,omitempty"`
	Capture      string            `json:"capture,omitempty"`
	Required     bool              `json:"required"`
	Description  string            `json:"description"`
}

// FromAttribute reports whether the field takes its value from an Attribute of
// the Run instead of from the user.
func (field V2ActionInputField) FromAttribute() bool {
	return field.Source == actionInputSourceAttribute
}

// The closed sets of the contract, which the exported types above use for their
// fields. A set whose values carry data declares it in a table beside the
// constants.

// valueType is a value type of the contract: the analyzer's name for the Go
// type of an Attribute, a view field or an Action input.
type valueType string

const (
	valueTypeString       valueType = "string"
	valueTypeStringArray  valueType = "string-array"
	valueTypeInt64        valueType = "int64"
	valueTypeDouble       valueType = "double"
	valueTypeBool         valueType = "bool"
	valueTypeDatetime     valueType = "datetime"
	valueTypeJSON         valueType = "json"
	valueTypeObject       valueType = "object"
	valueTypeArray        valueType = "array"
	valueTypeAttributeMap valueType = "attribute-map"
)

// IsNumber reports whether values of the type are numbers.
func (t valueType) IsNumber() bool { return t == valueTypeInt64 || t == valueTypeDouble }

// IsBool reports whether values of the type are booleans.
func (t valueType) IsBool() bool { return t == valueTypeBool }

// v2ValueTypeEditable lists every value type with whether a Display field or
// an Action input of that type can be edited.
var v2ValueTypeEditable = map[valueType]bool{
	valueTypeString:       true,
	valueTypeInt64:        true,
	valueTypeDouble:       true,
	valueTypeBool:         true,
	valueTypeDatetime:     true,
	valueTypeStringArray:  false,
	valueTypeJSON:         false,
	valueTypeObject:       false,
	valueTypeArray:        false,
	valueTypeAttributeMap: false,
}

// indexType is how Dex indexes a searchable Attribute.
type indexType string

const (
	indexTypeKeyword      indexType = "keyword"
	indexTypeFulltext     indexType = "fulltext"
	indexTypeKeywordArray indexType = "keyword-array"
	indexTypeInt          indexType = "int"
	indexTypeDouble       indexType = "double"
	indexTypeBool         indexType = "bool"
	indexTypeDatetime     indexType = "datetime"
)

// v2IndexTypes lists every index type with the only value type it indexes and
// the Dex index type that stores it.
var v2IndexTypes = map[indexType]struct {
	indexes valueType
	dex     dexpb.IndexType
}{
	indexTypeKeyword:      {valueTypeString, dexpb.IndexType_INDEX_TYPE_KEYWORD},
	indexTypeFulltext:     {valueTypeString, dexpb.IndexType_INDEX_TYPE_TEXT},
	indexTypeKeywordArray: {valueTypeStringArray, dexpb.IndexType_INDEX_TYPE_KEYWORD_ARRAY},
	indexTypeInt:          {valueTypeInt64, dexpb.IndexType_INDEX_TYPE_INT},
	indexTypeDouble:       {valueTypeDouble, dexpb.IndexType_INDEX_TYPE_DOUBLE},
	indexTypeBool:         {valueTypeBool, dexpb.IndexType_INDEX_TYPE_BOOL},
	indexTypeDatetime:     {valueTypeDatetime, dexpb.IndexType_INDEX_TYPE_DATETIME},
}

// startInputKind is the kind of a Start input schema: a JSON Schema type, or
// map for an object of free keys whose values share one schema.
type startInputKind string

const (
	startInputKindNull    startInputKind = "null"
	startInputKindString  startInputKind = "string"
	startInputKindInteger startInputKind = "integer"
	startInputKindNumber  startInputKind = "number"
	startInputKindBoolean startInputKind = "boolean"
	startInputKindObject  startInputKind = "object"
	startInputKindArray   startInputKind = "array"
	startInputKindMap     startInputKind = "map"
)

// startInputFormatDateTime is the only format of a string Start input. Empty
// declares none.
const startInputFormatDateTime = "date-time"

// actionInputKind is the kind of an Action RPC's input.
type actionInputKind string

const (
	actionInputKindNone   actionInputKind = "none"
	actionInputKindObject actionInputKind = "object"
)

// actionInputSource is where an Action input field takes its value from: the
// user, or an Attribute of the Run.
type actionInputSource string

const (
	actionInputSourceUser      actionInputSource = "user"
	actionInputSourceAttribute actionInputSource = "attribute"
)

// actionInputCaptureQRCode is the only browser control that captures a
// user-provided string. Empty declares none.
const actionInputCaptureQRCode = "qr-code"

// actionConditionOperatorIn is the only way an Action's condition compares its
// Attribute with the condition values.
const actionConditionOperatorIn = "in"

// uiSlot is the named position a view field takes in a Flow row or drawer.
type uiSlot string

const (
	uiSlotTitle          uiSlot = "title"
	uiSlotSubtitle       uiSlot = "subtitle"
	uiSlotStatus         uiSlot = "status"
	uiSlotRecommendation uiSlot = "recommendation"
	uiSlotReason         uiSlot = "reason"
)

// v2ViewUISlots lists every UI slot with whether only one field per view may
// claim it.
var v2ViewUISlots = map[uiSlot]bool{
	uiSlotTitle:          true,
	uiSlotSubtitle:       true,
	uiSlotStatus:         true,
	uiSlotRecommendation: true,
	uiSlotReason:         false,
}
