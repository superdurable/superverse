// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package identity

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Account provides the AWS clients and ownership configuration for hosted cloud operations.
type Account struct {
	AccountID              string
	Region                 string
	ClusterName            string
	BucketPrefix           string
	KMSKeyARN              string
	PermissionsBoundaryARN string
	S3                     *s3.Client
	IAM                    *iam.Client
	EKS                    *eks.Client
}

func CloudDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 20*time.Second)
}
