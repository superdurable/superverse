// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mockmodel

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// followReplay walks a conversation and applies the mock model's cheap
// stand-in for the API's thinking binding: every assistant message must hold
// exactly the blocks this server returned for the reply it stood at, key order
// and whitespace aside. A
// client that edits, drops or reorders a stored block is rejected, as the real
// API rejects a thinking block that no longer matches its conversation. It
// returns the selection in force after the last message.
func followReplay(messages []requestMessage) (selection, error) {
	return walk(messages, func(message requestMessage, sel selection) error {
		expected, err := sel.nextReply()
		if err != nil {
			return err
		}
		if expected.failure != nil || !sameBlocks(message.Content, expected.blocks) {
			return fmt.Errorf("mock replay mismatch at %s", sel.at)
		}
		return nil
	})
}

// sameBlocks compares replayed blocks with the blocks a step produces.
func sameBlocks(replayed []json.RawMessage, expected []block) bool {
	if len(replayed) != len(expected) {
		return false
	}
	for index, raw := range replayed {
		var got, want any
		if json.Unmarshal(raw, &got) != nil || json.Unmarshal(expected[index].json(), &want) != nil || !reflect.DeepEqual(got, want) {
			return false
		}
	}
	return true
}
