// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package attachments

import (
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// AttachmentFlowBootstrap assembles AttachmentFlow with the server's object
// store, which presigns the browser's uploads, the replica's one extractor,
// whose slot every preparation of the replica waits for, the TeamSpace
// client that holds the staging quota and the attachment settings, and builds
// its client. It holds no durable state and serves no request.
type AttachmentFlowBootstrap struct {
	flow *AttachmentFlow
}

// NewAttachmentFlowBootstrap assembles the Flow; teamSpaces is bound to the
// Dex client later, as the server binds every client.
func NewAttachmentFlowBootstrap(objects *objectstore.Store, extractor *fileextract.Extractor, teamSpaces *teamspaces.TeamSpaceFlowClient, settings Settings) *AttachmentFlowBootstrap {
	return &AttachmentFlowBootstrap{flow: &AttachmentFlow{objects: objects, extractor: extractor, teamSpaces: teamSpaces, settings: settings}}
}

// Flow returns the shared definition registered by the server bootstrap.
func (bootstrap *AttachmentFlowBootstrap) Flow() *AttachmentFlow { return bootstrap.flow }

// Client binds the Flow client to the shared Dex client.
func (bootstrap *AttachmentFlowBootstrap) Client(sdk *dex.Client) *AttachmentFlowClient {
	return &AttachmentFlowClient{dex: sdk, flow: bootstrap.flow}
}
