// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package build

// FailureCategory is the closed set of trusted hosted-runner failure reasons.
// Its zero value means a provider with no category (the local builder).
type FailureCategory uint8

type FailureClass string

const (
	FailureClassApplication FailureClass = "application"
	FailureClassPlatform    FailureClass = "platform"
)

const (
	FailureUnsupportedBuildTools FailureCategory = iota + 1
	FailureInvalidFlowDefinition
	FailureDependencyDownload
	FailureImageBuild
	FailureResultPublication
	FailureHostPreparation
	FailureUnclassifiedPlatform
)

type failureCategoryData struct {
	name  string
	class FailureClass
}

var failureCategories = map[FailureCategory]failureCategoryData{
	FailureUnsupportedBuildTools: {"unsupported-build-tools", FailureClassPlatform},
	FailureInvalidFlowDefinition: {"invalid-flow-definition", FailureClassApplication},
	FailureDependencyDownload:    {"dependency-download-failed", FailureClassPlatform},
	FailureImageBuild:            {"image-build-failed", FailureClassApplication},
	FailureResultPublication:     {"result-publication-failed", FailureClassPlatform},
	FailureHostPreparation:       {"host-preparation-failed", FailureClassPlatform},
	FailureUnclassifiedPlatform:  {"unclassified-platform-failure", FailureClassPlatform},
}

func init() {
	names := map[string]bool{}
	for category := FailureUnsupportedBuildTools; category <= FailureUnclassifiedPlatform; category++ {
		data, ok := failureCategories[category]
		if !ok || data.name == "" || names[data.name] || (data.class != FailureClassApplication && data.class != FailureClassPlatform) {
			panic("invalid hosted failure category catalog")
		}
		names[data.name] = true
	}
	if len(FailureCategoryNames()) != len(failureCategories) {
		panic("runner stage categories do not cover the hosted failure catalog")
	}
}

func (c FailureCategory) Name() string        { return failureCategories[c].name }
func (c FailureCategory) Class() FailureClass { return failureCategories[c].class }

func ParseFailureCategory(name string) (FailureCategory, bool) {
	for category, data := range failureCategories {
		if data.name == name {
			return category, true
		}
	}
	return 0, false
}

// FailureCategoryNames supplies the runner's stage keys without declaring
// their wire values a second time in Python.
func FailureCategoryNames() map[string]string {
	return map[string]string{
		"unsupportedBuildTools": FailureUnsupportedBuildTools.Name(),
		"invalidFlowDefinition": FailureInvalidFlowDefinition.Name(),
		"dependencyDownload":    FailureDependencyDownload.Name(),
		"imageBuild":            FailureImageBuild.Name(),
		"resultPublication":     FailureResultPublication.Name(),
		"hostPreparation":       FailureHostPreparation.Name(),
		"unclassifiedPlatform":  FailureUnclassifiedPlatform.Name(),
	}
}

// FailureCategoryContract includes owner classes in the executor identity.
func FailureCategoryContract() map[string]FailureClass {
	contract := make(map[string]FailureClass, len(failureCategories))
	for _, data := range failureCategories {
		contract[data.name] = data.class
	}
	return contract
}
