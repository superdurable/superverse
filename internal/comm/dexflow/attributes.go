// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflow

import (
	"errors"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
)

// InitialAttributes collects the encoded Attribute values of one StartFlow.
type InitialAttributes struct {
	values []dex.InitialAttributeDef
}

// Add encodes v as the initial value of a. Encoding fails only for a value the
// Attribute cannot hold, such as invalid UTF-8 or a nil indexed value, which
// is a programming error, so Add panics instead of returning it.
func Add[T any](b *InitialAttributes, a dex.Attribute[T], v T) {
	b.values = append(b.values, comm.Must(dex.InitialAttribute(a, v)))
}

// AddInstance encodes v as the initial value of the instance of m. Encoding
// fails only for a value or key the map cannot hold, a programming error, so
// AddInstance panics instead of returning it.
func AddInstance[T any](b *InitialAttributes, m dex.AttributeMap[T], instance string, v T) {
	b.values = append(b.values, comm.Must(dex.InitialAttributeMapValue(m, instance, v)))
}

// Values returns the collected values for dex.StartFlowOptions.Attributes.
func (b *InitialAttributes) Values() []dex.InitialAttributeDef {
	return b.values
}

// IsAttributeNotFound reports whether err says an Attribute or AttributeMap
// instance holds no value.
func IsAttributeNotFound(err error) bool {
	var missing *dex.AttributeNotFoundError
	return errors.As(err, &missing)
}

// IsAttributeMapNotLoaded reports whether err says the invocation did not load
// the AttributeMap instance it read. The options of a Step or RPC select the
// instances it can read; an instance outside them is neither found nor missing.
func IsAttributeMapNotLoaded(err error) bool {
	var notLoaded *dex.AttributeMapNotLoadedError
	return errors.As(err, &notLoaded)
}

// Optional returns the result of Get on a nil-able Attribute, with a missing
// value read as nil. Initializing or setting such an Attribute to nil stages the
// same null as Delete, which the SDK reads back as a missing value, so nil and
// missing are one state. Any other read failure is a programming error.
func Optional[T any](value *T, err error) *T {
	if IsAttributeNotFound(err) {
		return nil
	}
	return comm.Must(value, err)
}

// OptionalValue returns the result of Get on an Attribute of a value type that
// a Flow deletes when it holds nothing, with a missing value read as nil. Any
// other read failure is a programming error.
func OptionalValue[T any](value T, err error) *T {
	if IsAttributeNotFound(err) {
		return nil
	}
	comm.MustDo(err)
	return &value
}

// ValueOrZero returns the result of Get on an Attribute whose zero value means
// that nothing was recorded yet, with a missing value read as that zero value.
func ValueOrZero[T any](value T, err error) T {
	if IsAttributeNotFound(err) {
		var zero T
		return zero
	}
	return comm.Must(value, err)
}
