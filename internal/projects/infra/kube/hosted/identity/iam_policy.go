// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package identity

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// policyLanguageVersion is the version of the IAM policy language that every
// policy document of this package declares.
const policyLanguageVersion = "2012-10-17"

// policyDocument is an IAM policy document. Its fields, like those of a
// statement, are declared in the alphabetical order of their JSON keys, which
// is the order the documents have always been marshalled in.
type policyDocument struct {
	Statement []policyStatement `json:"Statement"`
	Version   string            `json:"Version"`
}

// policyStatement is one statement of a policy document. IAM accepts an Action
// or a Resource as a string or as a list of strings and a Principal as a string
// or as an object. A statement holds the form its document has always been
// written in, because policyMatches compares a document with the stored one by
// value, and a list of one value is not that value.
type policyStatement struct {
	Action    any             `json:"Action,omitempty"`
	Condition policyCondition `json:"Condition,omitempty"`
	Effect    policyEffect    `json:"Effect"`
	Principal any             `json:"Principal,omitempty"`
	Resource  any             `json:"Resource,omitempty"`
	// Sid names the grant generation of a Preview grant's statement.
	Sid string `json:"Sid,omitempty"`
}

// policyEffect is whether a statement allows or denies its actions.
type policyEffect string

const (
	effectAllow policyEffect = "Allow"
	effectDeny  policyEffect = "Deny"
)

// policyAction is an IAM action that a statement of this package allows or
// denies.
type policyAction string

const (
	actionAny                   policyAction = "*"
	actionKMSDecrypt            policyAction = "kms:Decrypt"
	actionKMSGenerateDataKey    policyAction = "kms:GenerateDataKey"
	actionS3Any                 policyAction = "s3:*"
	actionS3DeleteObject        policyAction = "s3:DeleteObject"
	actionS3DeleteObjectVersion policyAction = "s3:DeleteObjectVersion"
	actionS3GetBucketVersioning policyAction = "s3:GetBucketVersioning"
	actionS3GetObject           policyAction = "s3:GetObject"
	actionS3GetObjectVersion    policyAction = "s3:GetObjectVersion"
	actionS3ListBucket          policyAction = "s3:ListBucket"
	actionS3ListBucketVersions  policyAction = "s3:ListBucketVersions"
	actionS3PutObject           policyAction = "s3:PutObject"
	actionSTSAssumeRole         policyAction = "sts:AssumeRole"
	actionSTSTagSession         policyAction = "sts:TagSession"
)

// The wildcards of a statement's Principal and Resource, which match every
// principal and every resource. The wildcard of an Action is actionAny.
const (
	anyPrincipal = "*"
	anyResource  = "*"
)

// conditionOperator compares the request's value of a condition key with the
// values the statement lists for it.
type conditionOperator string

const (
	operatorBool            conditionOperator = "Bool"
	operatorNull            conditionOperator = "Null"
	operatorStringEquals    conditionOperator = "StringEquals"
	operatorStringLike      conditionOperator = "StringLike"
	operatorStringNotEquals conditionOperator = "StringNotEquals"
)

// policyCondition is the Condition of a statement: for each operator, the
// values of each condition key that make it hold.
type policyCondition map[conditionOperator]map[string]any

// policyJSON marshals the policy document that holds the statements.
func policyJSON(statements ...policyStatement) string {
	return string(comm.Must(json.Marshal(policyDocument{Statement: statements, Version: policyLanguageVersion})))
}

// denyAllPolicy is the document of a revoked role: it denies every action on
// every resource.
var denyAllPolicy = policyJSON(policyStatement{Effect: effectDeny, Action: actionAny, Resource: anyResource})

// inlinePolicyLimit is IAM's limit on the inline policies of one role: their
// documents together hold at most 10,240 characters, and IAM does not count
// white space (IAM and AWS STS quotas, "Role policy size"). A write past it
// fails with LimitExceeded.
const inlinePolicyLimit = 10240

// policySize is the size IAM counts for a policy document: its characters
// other than white space.
func policySize(document string) int {
	return utf8.RuneCountInString(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, document))
}

// checkScopePolicySize refuses the scope policy document of a role when it
// and reserved, the size of the role's other inline policies, exceed
// inlinePolicyLimit. Only the secret keys of a grant make its policy grow.
func checkScopePolicySize(role, document string, reserved int) error {
	if size := policySize(document) + reserved; size > inlinePolicyLimit {
		return fmt.Errorf("%w: the %s policy of IAM role %s needs %d of the %d characters IAM allows for the inline policies of a role, so the grant names too many secret keys", infra.ErrGrantTooLarge, scopePolicyName, role, size, inlinePolicyLimit)
	}
	return nil
}

// kmsThroughS3 is the statement that lets a role use the project KMS key only
// through S3, and only when the S3 encryption context names the bucket or one
// of the objects. An object written under the bucket's S3 Bucket Key carries
// the bucket ARN as its context; one written before the bucket had a Bucket
// Key keeps its own ARN.
func (h *Account) kmsThroughS3(bucket string, objects ...string) policyStatement {
	return policyStatement{
		Effect:   effectAllow,
		Action:   []policyAction{actionKMSDecrypt, actionKMSGenerateDataKey},
		Resource: h.KMSKeyARN,
		Condition: policyCondition{
			operatorStringEquals: {"kms:ViaService": "s3." + h.Region + ".amazonaws.com"},
			operatorStringLike:   {"kms:EncryptionContext:aws:s3:arn": slices.Concat(objects, []string{bucket})},
		},
	}
}

// bucketPolicy is the policy of a project's bucket: it denies plain HTTP and
// every upload that selects another encryption or another key than the
// project's.
func (h *Account) BucketPolicy(bucket string) string {
	arn := "arn:aws:s3:::" + bucket
	objects := arn + "/*"
	return policyJSON(
		policyStatement{Effect: effectDeny, Principal: anyPrincipal, Action: actionS3Any, Resource: []string{arn, objects}, Condition: policyCondition{operatorBool: {"aws:SecureTransport": "false"}}},
		denyUploadHeader(objects, "s3:x-amz-server-side-encryption", string(types.ServerSideEncryptionAwsKms)),
		denyUploadHeader(objects, "s3:x-amz-server-side-encryption-aws-kms-key-id", h.KMSKeyARN),
	)
}

// denyUploadHeader is the statement that denies an upload to objects whose
// request sets header to any value but allowed. A request without the header
// is not denied: the bucket default applies to it.
func denyUploadHeader(objects, header, allowed string) policyStatement {
	// A Null condition that is false holds when the request carries the key.
	const carriesKey = "false"
	return policyStatement{
		Effect:    effectDeny,
		Principal: anyPrincipal,
		Action:    actionS3PutObject,
		Resource:  objects,
		Condition: policyCondition{
			operatorStringNotEquals: {header: allowed},
			operatorNull:            {header: carriesKey},
		},
	}
}
