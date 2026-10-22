// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"context"
	"errors"
	"log/slog"

	"github.com/superdurable/dex/sdk-go/dex"
	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
)

// ComposeWorkerForTest reuses the complete production assembly with an isolated push Worker.
// The caller supplies the lane's effective configuration and secret environment, and owns Start and Stop.
func ComposeWorkerForTest(ctx context.Context, settings serverconfig.Config, options dex.WorkerOptions, serverBinary string) (*dex.Worker, func() error, error) {
	config, err := loadConfig(ctx, slog.Default(), settings)
	if err != nil {
		return nil, nil, err
	}
	var owned resources
	if config.localGit != nil {
		owned.add(config.localGit.Close)
	}
	config.extractor = serverBinary
	config.dexWorker = options
	config.dexWorker.FlowServiceAddress = settings.Dex.ServerAddress
	server, err := compose(ctx, config, slog.Default(), &owned)
	if err != nil {
		return nil, nil, errors.Join(err, owned.release())
	}
	return server.worker, owned.release, nil
}
