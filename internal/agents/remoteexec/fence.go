// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteexec

// Fence is the immutable authority that admitted a model or pending tool
// batch. Zero is the legacy platform epoch/generation; it is never remote.
type Fence struct {
	Epoch      uint64 `json:"epoch"`
	Generation uint64 `json:"generation"`
}

// Admitted captures an Executor without connection metadata or credentials.
func (executor Executor) Admitted() Fence {
	return Fence{Epoch: executor.Epoch, Generation: executor.Generation}
}

// Matches compares source admission, never an identity rebuilt after an effect.
func (admission Fence) Matches(executor Executor) bool {
	return admission.Epoch == executor.Epoch && admission.Generation == executor.Generation
}
