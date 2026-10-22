// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package infra

import "maps"

// ProjectDexConfig is the Dex server configuration of a project's Live Dex:
// the API, interpreter and matching, Flow blobs in the project's bucket and
// Streams in memory. temporal is the interpreter's Temporal connection, whose
// hostPort the runtime also gives the container as TEMPORAL_HOST_PORT, and
// storage holds the provider fields of the blob storage a0. A zero
// historyRetentionDays keeps the blob history without a bound.
func ProjectDexConfig(temporal, storage map[string]any, historyRetentionDays int32) map[string]any {
	blobs := map[string]any{"status": "active", "storageId": "a0", "storageType": "s3", "cleanupStrategy": map[string]any{"cleanupStrategyType": "afterAllRunsDeleted", "cleanupFrequencyInDays": 1}}
	maps.Copy(blobs, storage)
	blobStore := map[string]any{"enabled": true, "asyncStepInputSnapshotsEnabled": true, "thresholdInBytes": 100, "objectIdLength": 10, "supportedStorages": []any{blobs}}
	if historyRetentionDays != 0 {
		blobStore["historyRetentionInDays"] = historyRetentionDays
	}
	return map[string]any{
		"log":         map[string]any{"stdout": true, "level": "info"},
		"api":         map[string]any{"port": DexPort},
		"interpreter": map[string]any{"attributeIndexesManagedExternally": false, "temporal": temporal},
		"blobStore":   blobStore,
		"streamStore": map[string]any{"backend": "memory"},
		"matching":    map[string]any{"maxConcurrentTasksCap": 512, "maxStreams": 2048},
		"web":         map[string]any{"startFlowWorkerTargetPull": true},
	}
}

// LocalDexTemporal is the Temporal connection of a project Dex whose Temporal
// start-dev runs in the project's scope.
func LocalDexTemporal(access TemporalAccess) map[string]any {
	return map[string]any{"hostPort": access.HostPort, "namespace": access.Namespace}
}

// LocalDexStorage holds the blob storage fields of a project Dex on the MinIO
// of its own scope, which it reaches with the root key pair.
func LocalDexStorage(bucket, access, secret string) map[string]any {
	return map[string]any{"s3Endpoint": "http://" + ScopeAddress(ComponentStorage, StoragePort), "s3Bucket": bucket, "s3Region": LocalStorageRegion, "s3AccessKey": access, "s3SecretKey": secret}
}
