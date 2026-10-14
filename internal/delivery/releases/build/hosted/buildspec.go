// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package hosted

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/base64"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cb "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
)

//go:embed runner.py
var hostedRunner []byte

// startInput is the one StartBuild call of an admitted attempt. Its inputs are
// trusted constants, so encoding them cannot fail.
func (b *Builder) startInput(in build.BuildRequest, profile build.Profile, source objectstore.ObjectRef) *codebuild.StartBuildInput {
	encode := base64.StdEncoding.EncodeToString
	config := map[string]any{"executorDigest": b.executorDigest(), "request": in, "projectName": b.config.ProjectName, "sourceVersion": source.Version, "repositoryURI": b.config.RepositoryURI, "imageTag": b.tag(in), "region": b.config.Region, "accountId": b.config.AccountID, "bucket": b.config.Bucket, "resultKey": b.attemptPrefix(in) + "result.json", "kmsKeyARN": b.config.KMSKeyARN, "toolImage": b.config.ToolImage, "hostImage": b.config.HostImage, "profile": map[string]string{"manifest": encode(profile.Manifest), "dockerfile": encode(profile.Dockerfile)}}
	data := comm.Must(json.Marshal(config))
	// JSON is a YAML subset. No repository buildspec, shell expansion from app
	// inputs, artifacts hook or secondary source can execute in the trusted host.
	// CodeBuild limits inline buildspecs to 25,600 characters. Compress only the
	// trusted runner transport; its exact source bytes still own executorDigest.
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_ = comm.Must(writer.Write(hostedRunner))
	comm.MustDo(writer.Close())
	restore := "python3 -I -c 'import base64,gzip,pathlib; pathlib.Path(\"/tmp/sv2-hosted-build.py\").write_bytes(gzip.decompress(base64.b64decode(\"" + encode(compressed.Bytes()) + "\")))'"
	spec := comm.Must(json.Marshal(map[string]any{"version": 0.2, "phases": map[string]any{"install": map[string]any{"commands": []string{"python3 -I /usr/local/lib/sv2/start-docker.py"}}, "build": map[string]any{"commands": []string{restore, "python3 -I /tmp/sv2-hosted-build.py"}}}}))
	if len(spec) > 25600 {
		panic("the embedded hosted runner exceeds the CodeBuild inline buildspec limit")
	}
	variable := func(key, value string) cb.EnvironmentVariable {
		return cb.EnvironmentVariable{Name: aws.String(key), Value: aws.String(value), Type: cb.EnvironmentVariableTypePlaintext}
	}
	return &codebuild.StartBuildInput{
		ProjectName:                      &b.config.ProjectName,
		ImageOverride:                    &b.config.HostImage,
		EnvironmentTypeOverride:          cb.EnvironmentTypeArmContainer,
		ComputeTypeOverride:              cb.ComputeTypeBuildGeneral1Large,
		ImagePullCredentialsTypeOverride: cb.ImagePullCredentialsTypeServiceRole,
		AutoRetryLimitOverride:           aws.Int32(0),
		DebugSessionEnabled:              aws.Bool(false),
		PrivilegedModeOverride:           aws.Bool(true),
		CacheOverride:                    &cb.ProjectCache{Type: cb.CacheTypeNoCache},
		SecondarySourcesOverride:         []cb.ProjectSource{},
		SecondaryArtifactsOverride:       []cb.ProjectArtifacts{},
		IdempotencyToken:                 aws.String(b.token(in)),
		SourceTypeOverride:               cb.SourceTypeS3,
		SourceLocationOverride:           aws.String(b.config.Bucket + "/" + source.Key),
		SourceVersion:                    &source.Version,
		BuildspecOverride:                aws.String(string(spec)),
		ArtifactsOverride:                &cb.ProjectArtifacts{Type: cb.ArtifactsTypeNoArtifacts},
		EnvironmentVariablesOverride: []cb.EnvironmentVariable{
			variable("SV2_BUILD_IDENTITY", b.token(in)),
			variable("SV2_PROFILE_DIGEST", in.ProfileDigest),
			variable("SV2_EXECUTOR_DIGEST", b.executorDigest()),
			variable("SV2_BUILD_CONFIG", encode(data)),
		},
		TimeoutInMinutesOverride:       aws.Int32(45),
		QueuedTimeoutInMinutesOverride: aws.Int32(5),
	}
}
