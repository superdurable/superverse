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
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/hosted/identity"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/hosted/storage"
	"go.temporal.io/cloud-sdk/cloudclient"
)

// Config contains only fixed operator inputs. No request selects an AWS
// account, cluster, KMS key, bucket prefix or Temporal account.
type Config struct {
	AccountID, Region, ClusterName                    string
	Environment                                       deploymentnames.Environment
	BucketPrefix, KMSKeyARN, PermissionsBoundaryARN   string
	TemporalAccountID, TemporalRegion, TemporalAPIKey string
	TemporalRetentionDays                             int32
	// TemporalKeyRenewWindow is the remaining lifetime below which a
	// project's API key is rotated.
	TemporalKeyRenewWindow time.Duration
}

// CloudAccount is the project cloud account: S3, IAM, EKS Pod Identity and
// Temporal Cloud clients bound to the validated configuration. It implements
// kube.Backend: it provisions a project's Live scope and grants Live
// generations and Session Previews access to its storage.
type CloudAccount struct {
	identity *identity.Account
	storage  *storage.Account
	config   Config
	temporal *cloudclient.Client
}

// New validates that the configuration names exactly this environment's
// account, cluster, storage, permissions boundary and Temporal
// Cloud account.
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
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(in.TemporalAccountID) || in.TemporalRegion != "aws-"+in.Region || strings.TrimSpace(in.TemporalAPIKey) == "" || in.TemporalRetentionDays < 1 || in.TemporalRetentionDays > 90 || in.TemporalKeyRenewWindow < 2*24*time.Hour || in.TemporalKeyRenewWindow > 60*24*time.Hour {
		return nil, errors.New("hosted project Temporal Cloud configuration is incomplete")
	}
	temporal, err := cloudclient.New(cloudclient.Options{APIKey: in.TemporalAPIKey, DisableRetry: true})
	if err != nil {
		return nil, err
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
// cloud name of the project, with a role's identity suffix or the Temporal
// account of its namespace, and its bucket. The hashes are UUID prefixes, so
// they hold a hyphen.
func (h *CloudAccount) RedactionPatterns(k *kube.Infrastructure, project string) []infra.Redaction {
	return []infra.Redaction{
		{Name: regexp.MustCompile(regexp.QuoteMeta(identity.CloudNamePrefix(k, project)) + `[0-9a-f-]{12}(?:-[0-9a-f-]{12})?(?:\.` + regexp.QuoteMeta(h.config.TemporalAccountID) + `)?`), Placeholder: "<cloud-resource>"},
		{Name: regexp.MustCompile(regexp.QuoteMeta(h.config.BucketPrefix) + `-[0-9a-f-]{16}`), Placeholder: "<bucket>"},
	}
}
