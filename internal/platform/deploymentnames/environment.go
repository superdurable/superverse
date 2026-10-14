// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deploymentnames

import "fmt"

// Environment is a deployment environment, the key of one catalog entry. It is
// a struct that string literals cannot convert to: the environments below are
// its only values, and ParseEnvironment turns the name an operator wrote into
// one. The zero Environment names nothing and has no catalog entry.
type Environment struct{ name string }

// The deployment environments. Their names are the keys of environments.json,
// the values of the configuration's environment key and the spelling that
// Terraform, Helm and the infra scripts select an entry by.
var (
	// EnvironmentTest is the hosted test environment, which Local Kind and
	// Local Native also run as.
	EnvironmentTest = Environment{name: "test"}
	// EnvironmentStaging is the hosted staging environment.
	EnvironmentStaging = Environment{name: "staging"}
	// EnvironmentProd is the production environment.
	EnvironmentProd = Environment{name: "prod"}
)

// environments are all the declared environments. The catalog holds exactly
// their entries, which its initialization checks.
var environments = []Environment{EnvironmentTest, EnvironmentStaging, EnvironmentProd}

// String is the name of the environment: its catalog key and the spelling that
// receipts, tags and resource names carry.
func (environment Environment) String() string { return environment.name }

// MarshalText writes the name, so that a JSON document that holds an
// Environment keeps the spelling it always had.
func (environment Environment) MarshalText() ([]byte, error) {
	return []byte(environment.name), nil
}

// UnmarshalText reads the name of a declared environment.
func (environment *Environment) UnmarshalText(text []byte) error {
	parsed, err := ParseEnvironment(string(text))
	if err != nil {
		return err
	}
	*environment = parsed
	return nil
}

// ParseEnvironment returns the environment an operator named, as the
// configuration's environment key or a command line flag does. An unknown name
// is an *unknownEnvironmentError.
func ParseEnvironment(name string) (Environment, error) {
	for _, environment := range environments {
		if environment.name == name {
			return environment, nil
		}
	}
	return Environment{}, &unknownEnvironmentError{name: name}
}

// unknownEnvironmentError is a name that is no deployment environment, such as
// an operator's typo or the zero Environment's empty name.
type unknownEnvironmentError struct{ name string }

func (err *unknownEnvironmentError) Error() string {
	return fmt.Sprintf("unsupported deployment environment %q", err.name)
}
