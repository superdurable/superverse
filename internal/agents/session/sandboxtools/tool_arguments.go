// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxtools

import (
	"fmt"
	"strings"
)

// The generated schema validators bound strings in runes and decoding already
// yields valid UTF-8. The checks here cover only what a schema cannot express:
// byte bounds, NUL and blank text; the path rules belong to the file helper.

// argumentsError marks a model argument rejection, a known failure the model
// corrects; Runner classifies it.
type argumentsError struct {
	cause error
}

func (err *argumentsError) Error() string { return err.cause.Error() }

func (err *argumentsError) Unwrap() error { return err.cause }

func invalidArguments(err error) error {
	return &argumentsError{cause: err}
}

// validateText bounds the bytes of a text argument and rejects NUL. The
// generated decoder already enforced the schema's minimum length, which counts
// characters rather than bytes.
func validateText(name, value string, maximumBytes int) error {
	if len(value) > maximumBytes {
		return invalidArguments(fmt.Errorf("%s must contain at most %d bytes", name, maximumBytes))
	}
	if strings.ContainsRune(value, '\x00') {
		return invalidArguments(fmt.Errorf("%s must not contain NUL", name))
	}
	return nil
}

func validateNonBlankText(name, value string, maximumBytes int) error {
	if strings.TrimSpace(value) == "" {
		return invalidArguments(fmt.Errorf("%s must not be blank", name))
	}
	return validateText(name, value, maximumBytes)
}

// validateEach applies check to every item of a list argument.
func validateEach(name string, values []string, maximumBytes int, check func(name, value string, maximumBytes int) error) error {
	for index, value := range values {
		if err := check(fmt.Sprintf("%s[%d]", name, index), value, maximumBytes); err != nil {
			return err
		}
	}
	return nil
}
