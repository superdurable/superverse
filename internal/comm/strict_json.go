// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package comm

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// MediaTypeJSON is the media type of JSON, which stored objects and uploaded
// files declare alike.
const MediaTypeJSON = "application/json"

// DecodeStrictJSON decodes exactly one JSON value of data into target. It
// rejects unknown struct fields and trailing data, so a stored record or a
// request that does not match its type fails instead of losing fields.
func DecodeStrictJSON(data []byte, target any) error {
	return decodeStrictJSON(data, target, false)
}

// DecodeStrictJSONNumbers is DecodeStrictJSON that keeps numbers as
// json.Number, for values that travel on as JSON bytes and whose integers an
// intermediate float64 would round.
func DecodeStrictJSONNumbers(data []byte, target any) error {
	return decodeStrictJSON(data, target, true)
}

func decodeStrictJSON(data []byte, target any, useNumber bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if useNumber {
		decoder.UseNumber()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after a JSON value")
	}
	return nil
}
