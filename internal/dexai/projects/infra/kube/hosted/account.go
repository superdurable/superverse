// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package hosted

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/hosted/identity"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/hosted/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config contains only fixed operator inputs. No request selects an AWS
// account, cluster, KMS key, bucket prefix or Temporal cluster.
type Config struct {
	AccountID, Region, ClusterName                  string
	Environment                                     deploymentnames.Environment
	BucketPrefix, KMSKeyARN, PermissionsBoundaryARN string
	TemporalAddress                                 string
	TemporalRetentionDays                           int32
}

// CloudAccount is the project cloud account: S3, IAM, EKS Pod Identity and
// self-hosted Temporal clients bound to the validated configuration. It implements
// kube.Backend: it provisions a project's Live scope and grants Live
// generations and Session Previews access to its storage.
type CloudAccount struct {
	identity *identity.Account
	storage  *storage.Account
	config   Config
	temporal *grpc.ClientConn
}

// New validates that the configuration names exactly this environment's
// account, cluster, storage, permissions boundary and self-hosted Temporal cluster.
func New(config aws.Config, in Config) (*CloudAccount, error) {
	names, err := deploymentnames.For(in.Environment)
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^[0-9]{12}$`).MatchString(in.AccountID) || config.Region != in.Region || in.Region == "" || in.ClusterName != names.Cluster {
		return nil, errors.New("hosted project provider requires the exact V2 account, environment, region and cluster")
	}
	if in.BucketPrefix != names.WorkloadPrefix+"-"+in.Environment.String()+"-"+in.AccountID+"-projects" || !strings.HasPrefix(in.KMSKeyARN, "arn:aws:kms:"+in.Region+":"+in.AccountID+":key/") || in.PermissionsBoundaryARN != "arn:aws:iam::"+in.AccountID+":policy/"+names.Cluster+"-project-boundary" {
		return nil, errors.New("hosted project storage and permissions boundary identities are required")
	}
	host, port, err := net.SplitHostPort(in.TemporalAddress)
	if err != nil || strings.TrimSpace(host) == "" || port != "7233" || in.TemporalRetentionDays < 1 || in.TemporalRetentionDays > 90 {
		return nil, errors.New("hosted project Temporal address must use port 7233 and retention must be 1 to 90 days")
	}
	temporal, err := grpc.NewClient("passthrough:///"+in.TemporalAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy())
	if err != nil {
		return nil, fmt.Errorf("create Project Temporal connection: %w", err)
	}

	s3Client, iamClient, eksClient := s3.NewFromConfig(config), iam.NewFromConfig(config), eks.NewFromConfig(config)
	identityAccount := &identity.Account{
		AccountID: in.AccountID, Region: in.Region, ClusterName: in.ClusterName,
		KMSKeyARN: in.KMSKeyARN, PermissionsBoundaryARN: in.PermissionsBoundaryARN, BucketPrefix: in.BucketPrefix,
		S3: s3Client, IAM: iamClient, EKS: eksClient,
	}
	return &CloudAccount{identity: identityAccount, storage: &storage.Account{Account: identityAccount}, config: in, temporal: temporal}, nil
}

func (h *CloudAccount) Close() error { return h.temporal.Close() }

// RedactionPatterns name the project's cloud resources for infra.Redact: every
// cloud name of the project, with a role's identity suffix, and its bucket.
// The hashes are UUID prefixes, so
// they hold a hyphen.
func (h *CloudAccount) RedactionPatterns(k *kube.Infrastructure, project string) []infra.Redaction {
	return []infra.Redaction{
		{Name: regexp.MustCompile(regexp.QuoteMeta(identity.CloudNamePrefix(k, project)) + `[0-9a-f-]{12}(?:-[0-9a-f-]{12})?`), Placeholder: "<cloud-resource>"},
		{Name: regexp.MustCompile(regexp.QuoteMeta(h.config.BucketPrefix) + `-[0-9a-f-]{16}`), Placeholder: "<bucket>"},
	}
}
