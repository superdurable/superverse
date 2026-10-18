// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package objectstore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// maxPresignValidity bounds how long a presigned URL, a bearer capability,
// stays valid.
const maxPresignValidity = time.Hour

// ErrUploadMismatch reports an uploaded object whose length, digest or media
// type differ from its declared upload, which storage refuses itself, so it is
// a defect alarm.
var ErrUploadMismatch = errors.New("uploaded object differs from its declared upload")

// PresignAPI is the part of the S3 presign client the store uses. It signs for
// the storage endpoint that browsers reach (storage.browserEndpoint).
type PresignAPI interface {
	PresignPutObject(context.Context, *s3.PutObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
	PresignGetObject(context.Context, *s3.GetObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// WithPresigner is the store that also presigns browser transfers with
// presigner. Only the platform's store has one; the presigning methods of a
// store without it panic.
func (s *Store) WithPresigner(presigner PresignAPI) *Store {
	presigned := *s
	presigned.presigner = presigner
	return &presigned
}

// Upload is the one object a browser may create with a presigned PUT: its key
// below the store's prefix, its media type, its exact length and its SHA-256
// digest in the Digest format, which storage verifies against the body.
type Upload struct {
	Key       string
	MediaType MediaType
	Size      int64
	Digest    string
}

// valid admits an upload that storage can verify.
func (u Upload) valid() error {
	if !validKey(u.Key) || u.Size < 1 || u.Size > MaxObjectBytes || !flowid.IsSHA256Digest(u.Digest) || u.MediaType == "" || strings.ContainsAny(u.MediaType, "\r\n") {
		return fmt.Errorf("%w: key %q, media type %q, %d bytes and digest %q are not an upload", errInvalidObject, u.Key, u.MediaType, u.Size, u.Digest)
	}
	return nil
}

// PresignedRequest is a presigned URL and the headers its request must carry
// exactly; the browser adds Host and Content-Length itself, and both are
// signed. It is a bearer capability until Expires: it goes into a response
// only, never into Flow state, logs or history, so it prints without its URL.
type PresignedRequest struct {
	method  string
	URL     string
	Headers map[string]string
	Expires time.Time
}

// String names the request without its capability.
func (r PresignedRequest) String() string {
	return "presigned " + r.method + " until " + r.Expires.UTC().Format(time.RFC3339)
}

// LogValue keeps the URL out of structured logs.
func (r PresignedRequest) LogValue() slog.Value { return slog.StringValue(r.String()) }

// PresignPut presigns the browser's PUT of upload, valid for validity. It
// signs Content-Length, Content-Type, x-amz-checksum-sha256, x-amz-meta-sha256
// (the digest the store's reads verify), If-None-Match: * and, in hosted
// storage, the KMS headers, so storage itself refuses a body of another length
// or digest, another media type and any second write of the key.
func (s *Store) PresignPut(ctx context.Context, upload Upload, validity time.Duration) (PresignedRequest, error) {
	if err := upload.valid(); err != nil {
		return PresignedRequest{}, err
	}
	sum := digestBytes(upload.Digest)
	fullKey := s.prefix + upload.Key
	input := &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &fullKey, ContentLength: aws.Int64(upload.Size), ContentType: aws.String(upload.MediaType),
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(sum)), Metadata: map[string]string{digestMetadata: hex.EncodeToString(sum)},
		IfNoneMatch: aws.String("*"),
	}
	if s.keyID != "" {
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		input.SSEKMSKeyId = &s.keyID
	}
	expires, err := s.validity(validity)
	if err != nil {
		return PresignedRequest{}, err
	}
	signed, err := s.presigner.PresignPutObject(ctx, input, s3.WithPresignExpires(validity))
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign the upload of %s: %w", fullKey, err)
	}
	return presigned(signed, expires), nil
}

// PresignGet presigns the browser's GET of the exact version of ref, valid for
// validity, which storage answers as a download named fileName or, inline, as
// content the page shows, such as an image's thumbnail, under that name.
func (s *Store) PresignGet(ctx context.Context, ref ObjectRef, fileName string, inline bool, validity time.Duration) (PresignedRequest, error) {
	if err := s.validRef(ref); err != nil {
		return PresignedRequest{}, err
	}
	if fileName == "" || len(fileName) > 255 {
		return PresignedRequest{}, fmt.Errorf("%w: download name of %d bytes is not 1 to 255", errInvalidObject, len(fileName))
	}
	expires, err := s.validity(validity)
	if err != nil {
		return PresignedRequest{}, err
	}
	signed, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket, Key: &ref.Key, VersionId: &ref.Version, ResponseContentDisposition: aws.String(contentDisposition(fileName, inline)),
	}, s3.WithPresignExpires(validity))
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign the download of %s version %s: %w", ref.Key, ref.Version, err)
	}
	return presigned(signed, expires), nil
}

// validity admits a presign duration and returns when the URL expires at the
// latest. A store without a presigner is a composition defect.
func (s *Store) validity(validity time.Duration) (time.Time, error) {
	if s.presigner == nil {
		panic("objectstore: the store has no presigner")
	}
	if validity <= 0 || validity > maxPresignValidity {
		return time.Time{}, fmt.Errorf("%w: presign validity %s is not within (0, %s]", errInvalidObject, validity, maxPresignValidity)
	}
	return time.Now().Add(validity), nil
}

// presigned is the request a browser sends: the signed headers but Host and
// Content-Length, which it sets itself.
func presigned(signed *v4.PresignedHTTPRequest, expires time.Time) PresignedRequest {
	headers := map[string]string{}
	for name, values := range signed.SignedHeader {
		if canonical := http.CanonicalHeaderKey(name); canonical != "Host" && canonical != "Content-Length" {
			headers[strings.ToLower(name)] = strings.Join(values, ",")
		}
	}
	return PresignedRequest{method: signed.Method, URL: signed.URL, Headers: headers, Expires: expires}
}

// contentDisposition is the Content-Disposition of a download, or of inline
// content, named fileName (RFC 6266): an ASCII fallback and the exact name in
// UTF-8 (RFC 8187).
func contentDisposition(fileName string, inline bool) string {
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == '%' {
			return '_'
		}
		return r
	}, fileName)
	var encoded strings.Builder
	for _, b := range []byte(fileName) {
		if b < 0x80 && (b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("!#$&+-.^_`|~", b) >= 0) {
			encoded.WriteByte(b)
		} else {
			fmt.Fprintf(&encoded, "%%%02X", b)
		}
	}
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	return disposition + `; filename="` + fallback + `"; filename*=UTF-8''` + encoded.String()
}

// objectHead is the current version of a key as storage verified it: its exact
// reference, whose digest is the SHA-256 checksum storage computed from the
// body it received, and its length.
type objectHead struct {
	ref  ObjectRef
	size int64
}

// head reads the head of the current version of key, below the store's prefix,
// with its checksum. A key that holds no object is ErrNotFound; an object
// without a full-object SHA-256 checksum, or whose recorded digest metadata
// differs from it, is invalid.
func (s *Store) head(ctx context.Context, key string) (objectHead, error) {
	if !validKey(key) {
		return objectHead{}, fmt.Errorf("%w: key %q is not readable", errInvalidObject, key)
	}
	fullKey := s.prefix + key
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &fullKey, ChecksumMode: types.ChecksumModeEnabled})
	if isMissing(err) {
		return objectHead{}, fmt.Errorf("%w: %s: %w", ErrNotFound, fullKey, err)
	}
	if err != nil {
		return objectHead{}, fmt.Errorf("read the head of %s: %w", fullKey, err)
	}
	sum, err := base64.StdEncoding.DecodeString(aws.ToString(result.ChecksumSHA256))
	if err != nil || len(sum) != 32 || (result.ChecksumType != "" && result.ChecksumType != types.ChecksumTypeFullObject) {
		return objectHead{}, fmt.Errorf("%w: %s has no full-object SHA-256 checksum (%q, %q)", errInvalidObject, fullKey, aws.ToString(result.ChecksumSHA256), result.ChecksumType)
	}
	digest := "sha256:" + hex.EncodeToString(sum)
	if recorded := result.Metadata[digestMetadata]; recorded != "" && "sha256:"+recorded != digest {
		return objectHead{}, fmt.Errorf("%w: %s records digest %s but its checksum is %s", errInvalidObject, fullKey, recorded, digest)
	}
	version := aws.ToString(result.VersionId)
	if !IsVersion(version) {
		return objectHead{}, fmt.Errorf("%w: %s has version %q", errUnversioned, fullKey, version)
	}
	return objectHead{ref: ObjectRef{Key: fullKey, Version: version, Digest: digest, MediaType: aws.ToString(result.ContentType)}, size: aws.ToInt64(result.ContentLength)}, nil
}

// VerifyUpload reads the head of upload's key and accepts it only with the
// declared length, SHA-256 checksum and media type and a version: the
// reference its owner records. A key that holds no object is ErrNotFound and
// another object ErrUploadMismatch.
func (s *Store) VerifyUpload(ctx context.Context, upload Upload) (ObjectRef, error) {
	if err := upload.valid(); err != nil {
		return ObjectRef{}, err
	}
	head, err := s.head(ctx, upload.Key)
	if err != nil {
		return ObjectRef{}, err
	}
	if head.size != upload.Size || head.ref.Digest != upload.Digest || head.ref.MediaType != upload.MediaType {
		return ObjectRef{}, fmt.Errorf("%w: %s version %s holds %d bytes of %s with type %q, not the declared %d bytes of %s with type %q", ErrUploadMismatch,
			head.ref.Key, head.ref.Version, head.size, head.ref.Digest, head.ref.MediaType, upload.Size, upload.Digest, upload.MediaType)
	}
	return head.ref, nil
}

// digestBytes is the 32 bytes of a valid digest.
func digestBytes(digest string) []byte {
	sum, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	if err != nil {
		panic("objectstore: invalid digest " + digest)
	}
	return sum
}
