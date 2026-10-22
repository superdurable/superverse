// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform"
)

const MaxObjectBytes = 32 << 20

// digestMetadata is the user metadata key of every object's SHA-256 digest in
// hex, x-amz-meta-sha256 on the wire, which writes and copies carry.
const digestMetadata = "sha256"

var errImmutableConflict = errors.New("immutable object identity has different content")
var errInvalidObject = errors.New("object version or digest is invalid")

// ErrNotFound reports a key that holds no object.
var ErrNotFound = errors.New("object does not exist")

// S3API is the part of the S3 client the store uses.
type S3API interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
}

// MediaType is the media type of a stored object, which ObjectRef records as
// a plain string. The platform's own objects have the media types below; an
// uploaded file keeps the one its format declares (internal/comm/fileextract).
type MediaType = string

// The media types of the objects the platform writes.
const (
	MediaTypeJSON MediaType = comm.MediaTypeJSON
	MediaTypeYAML MediaType = "application/yaml"
	MediaTypeZip  MediaType = "application/zip"
	// MediaTypeGzip is a gzip stream, such as the compressed JSON Lines of a run export part.
	MediaTypeGzip MediaType = "application/gzip"
	// MediaTypeRendition is a rendition: the exact JSON of one Claude content
	// block, which a model request carries byte for byte.
	MediaTypeRendition MediaType = "application/vnd.superverse.content-block+json"
)

// ObjectRef is persisted in Flow state and build records as plain JSON under
// these keys; changing one breaks every stored reference.
type ObjectRef struct {
	Key       string `json:"Key"`
	Version   string `json:"Version"`
	Digest    string `json:"Digest"`
	MediaType string `json:"MediaType"`
}

type Store struct {
	client                S3API
	bucket, keyID, prefix string
	// presigner signs the browser's transfers; only the platform store has one.
	presigner PresignAPI
}

// NewStore confines a store to one platform environment prefix; every caller
// adds its project identity. Hosted storage requires the KMS key.
func NewStore(client S3API, bucket, keyID, prefix string, isLocalKind bool) (*Store, error) {
	if bucket == "" || (!isLocalKind && keyID == "") || !validKey(prefix) {
		return nil, errors.New("object store bucket, scoped prefix and hosted KMS key are required")
	}
	return &Store{client: client, bucket: bucket, keyID: keyID, prefix: prefix + "/"}, nil
}

// EncodeJSON encodes value without HTML escaping and without a trailing
// newline. Embedded Flow Definition graphs (json.RawMessage) keep the exact
// bytes that their definition revisions digest; escaping would silently change
// those bytes and break every digest computed over them.
func EncodeJSON(value any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte("\n")), nil
}

func (s *Store) PutJSON(ctx context.Context, key string, value any) (ObjectRef, error) {
	encoded, err := EncodeJSON(value)
	if err != nil {
		return ObjectRef{}, fmt.Errorf("encode object %s: %w", key, err)
	}
	return s.Put(ctx, key, MediaTypeJSON, encoded)
}

func (s *Store) GetJSON(ctx context.Context, ref ObjectRef, value any) error {
	data, err := s.Get(ctx, ref)
	if err != nil {
		return err
	}
	return decodeJSON(data, ref, value)
}

// GetCurrentJSON decodes the current version of key, which an immutable Put
// wrote once, into value and returns its exact reference. A key that holds no
// object is ErrNotFound.
func (s *Store) GetCurrentJSON(ctx context.Context, key string, value any) (ObjectRef, error) {
	if !validKey(key) {
		return ObjectRef{}, fmt.Errorf("%w: key %q is not readable", errInvalidObject, key)
	}
	fullKey := s.prefix + key
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &fullKey})
	if isMissing(err) {
		return ObjectRef{}, fmt.Errorf("%w: %s: %w", ErrNotFound, fullKey, err)
	}
	if err != nil {
		return ObjectRef{}, fmt.Errorf("read %s: %w", fullKey, err)
	}
	data, err := readObject(result, fullKey)
	if err != nil {
		return ObjectRef{}, err
	}
	ref := ObjectRef{Key: fullKey, Version: aws.ToString(result.VersionId), Digest: Digest(data), MediaType: aws.ToString(result.ContentType)}
	if !IsVersion(ref.Version) {
		return ObjectRef{}, fmt.Errorf("%w: %s has version %q", errUnversioned, fullKey, ref.Version)
	}
	return ref, decodeJSON(data, ref, value)
}

// decodeJSON decodes data, the one JSON value of the object ref, into value.
func decodeJSON(data []byte, ref ObjectRef, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: decode %s version %s: %w", errInvalidObject, ref.Key, ref.Version, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %s version %s has data after its JSON value", errInvalidObject, ref.Key, ref.Version)
	}
	return nil
}

// Put writes data once under key. It reads before writing, writes only when the
// key is absent, and after an uncertain write response accepts only the exact
// content it meant to write.
func (s *Store) Put(ctx context.Context, key, mediaType string, data []byte) (ObjectRef, error) {
	if !validKey(key) || len(data) == 0 || len(data) > MaxObjectBytes || mediaType == "" {
		return ObjectRef{}, fmt.Errorf("%w: key %q, media type %q and %d bytes are not a writable object", errInvalidObject, key, mediaType, len(data))
	}
	fullKey := s.prefix + key
	digest := Digest(data)
	existing, err := s.inspect(ctx, fullKey, mediaType, digest)
	if err == nil {
		return existing, nil
	}
	if !isMissing(err) {
		return ObjectRef{}, err
	}
	input := &s3.PutObjectInput{Bucket: &s.bucket, Key: &fullKey, Body: bytes.NewReader(data), ContentType: &mediaType, IfNoneMatch: aws.String("*"), Metadata: map[string]string{digestMetadata: strings.TrimPrefix(digest, "sha256:")}}
	if s.keyID != "" {
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		input.SSEKMSKeyId = &s.keyID
	}
	result, err := s.client.PutObject(ctx, input)
	if err != nil {
		if observed, readErr := s.inspect(ctx, fullKey, mediaType, digest); readErr == nil {
			return observed, nil
		} else if errors.Is(readErr, errImmutableConflict) {
			return ObjectRef{}, readErr
		}
		return ObjectRef{}, fmt.Errorf("immutable write of %s remains unconfirmed: %w", fullKey, err)
	}
	version := aws.ToString(result.VersionId)
	if !IsVersion(version) {
		return ObjectRef{}, fmt.Errorf("%w: the write of %s returned version %q", errUnversioned, fullKey, version)
	}
	return ObjectRef{Key: fullKey, Version: version, Digest: digest, MediaType: mediaType}, nil
}

// Get reads the exact version of ref and accepts it only with its recorded
// digest and media type.
func (s *Store) Get(ctx context.Context, ref ObjectRef) ([]byte, error) {
	if err := s.validRef(ref); err != nil {
		return nil, err
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &ref.Key, VersionId: &ref.Version})
	if err != nil {
		return nil, fmt.Errorf("read %s version %s: %w", ref.Key, ref.Version, err)
	}
	data, err := readObject(result, ref.Key)
	if err != nil {
		return nil, err
	}
	switch observed := aws.ToString(result.VersionId); {
	case observed != ref.Version:
		return nil, fmt.Errorf("%w: reading %s version %s returned version %q", errInvalidObject, ref.Key, ref.Version, observed)
	case Digest(data) != ref.Digest:
		return nil, fmt.Errorf("%w: %s version %s has digest %s, not the recorded %s", errInvalidObject, ref.Key, ref.Version, Digest(data), ref.Digest)
	case aws.ToString(result.ContentType) != ref.MediaType:
		return nil, fmt.Errorf("%w: %s version %s has media type %q, not the recorded %q", errInvalidObject, ref.Key, ref.Version, aws.ToString(result.ContentType), ref.MediaType)
	}
	return data, nil
}

// Delete removes the exact version of ref.
func (s *Store) Delete(ctx context.Context, ref ObjectRef) error {
	if err := s.validRef(ref); err != nil {
		return err
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &ref.Key, VersionId: &ref.Version}); err != nil {
		return fmt.Errorf("delete %s version %s: %w", ref.Key, ref.Version, err)
	}
	return nil
}

// CopyVersion copies the exact version of source to key, below the store's
// prefix, inside storage: the bytes never leave it, and the copy keeps the
// source's metadata and so its digest. It reads before it writes: a key that
// already holds source's digest and media type is the copy, which a retry
// after an uncertain response answers, and a key holding other content is an
// immutable conflict. A source version that no longer exists, and has no copy
// at key, is ErrNotFound.
func (s *Store) CopyVersion(ctx context.Context, source ObjectRef, key string) (ObjectRef, error) {
	if err := s.validRef(source); err != nil {
		return ObjectRef{}, err
	}
	if !validKey(key) {
		return ObjectRef{}, fmt.Errorf("%w: key %q is not writable", errInvalidObject, key)
	}
	fullKey := s.prefix + key
	if existing, err := s.inspectCopy(ctx, fullKey, source); !errors.Is(err, ErrNotFound) {
		return existing, err
	}
	copySource := (&url.URL{Path: s.bucket + "/" + source.Key}).EscapedPath() + "?versionId=" + url.QueryEscape(source.Version)
	input := &s3.CopyObjectInput{Bucket: &s.bucket, Key: &fullKey, CopySource: &copySource, MetadataDirective: types.MetadataDirectiveCopy}
	if s.keyID != "" {
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		input.SSEKMSKeyId = &s.keyID
	}
	result, err := s.client.CopyObject(ctx, input)
	if err != nil {
		if observed, readErr := s.inspectCopy(ctx, fullKey, source); readErr == nil {
			return observed, nil
		} else if errors.Is(readErr, errImmutableConflict) {
			return ObjectRef{}, readErr
		}
		if isMissing(err) {
			return ObjectRef{}, fmt.Errorf("%w: %s version %s: %w", ErrNotFound, source.Key, source.Version, err)
		}
		return ObjectRef{}, fmt.Errorf("copy of %s version %s to %s remains unconfirmed: %w", source.Key, source.Version, fullKey, err)
	}
	version := aws.ToString(result.VersionId)
	if !IsVersion(version) {
		return ObjectRef{}, fmt.Errorf("%w: the copy to %s returned version %q", errUnversioned, fullKey, version)
	}
	return ObjectRef{Key: fullKey, Version: version, Digest: source.Digest, MediaType: source.MediaType}, nil
}

// inspectCopy reads the head of the current version of key and accepts it only
// as a copy of source: the digest its metadata records and the media type. Only
// the store writes that metadata, and every read verifies the digest anyway.
func (s *Store) inspectCopy(ctx context.Context, key string, source ObjectRef) (ObjectRef, error) {
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if isMissing(err) {
		return ObjectRef{}, fmt.Errorf("%w: %s: %w", ErrNotFound, key, err)
	}
	if err != nil {
		return ObjectRef{}, fmt.Errorf("read the head of %s: %w", key, err)
	}
	if digest := "sha256:" + result.Metadata[digestMetadata]; digest != source.Digest || aws.ToString(result.ContentType) != source.MediaType {
		return ObjectRef{}, fmt.Errorf("%w: %s holds %s of type %q, not a copy of %s version %s", errImmutableConflict, key, digest, aws.ToString(result.ContentType), source.Key, source.Version)
	}
	version := aws.ToString(result.VersionId)
	if !IsVersion(version) {
		return ObjectRef{}, fmt.Errorf("%w: %s has version %q", errUnversioned, key, version)
	}
	return ObjectRef{Key: key, Version: version, Digest: source.Digest, MediaType: source.MediaType}, nil
}

// inspect reads the current version of key and accepts it only with the exact
// digest and media type.
func (s *Store) inspect(ctx context.Context, key, mediaType, digest string) (ObjectRef, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return ObjectRef{}, fmt.Errorf("read %s: %w", key, err)
	}
	data, err := readObject(result, key)
	if err != nil {
		return ObjectRef{}, err
	}
	if Digest(data) != digest || aws.ToString(result.ContentType) != mediaType {
		return ObjectRef{}, fmt.Errorf("%w: %s holds %s of type %q", errImmutableConflict, key, Digest(data), aws.ToString(result.ContentType))
	}
	version := aws.ToString(result.VersionId)
	if !IsVersion(version) {
		return ObjectRef{}, fmt.Errorf("%w: %s has version %q", errUnversioned, key, version)
	}
	return ObjectRef{Key: key, Version: version, Digest: digest, MediaType: mediaType}, nil
}

var errUnversioned = errors.New("object store bucket must have versioning enabled")

// validRef admits a persisted reference only inside this store's prefix.
func (s *Store) validRef(ref ObjectRef) error {
	if !strings.HasPrefix(ref.Key, s.prefix) || !validKey(ref.Key) || !IsVersion(ref.Version) || !flowid.IsSHA256Digest(ref.Digest) || ref.MediaType == "" {
		return fmt.Errorf("%w: reference to %q version %q is not an exact object of %s", errInvalidObject, ref.Key, ref.Version, s.prefix)
	}
	return nil
}

func validKey(key string) bool {
	return key != "" && len(key) <= 1024 && path.Clean(key) == key && !strings.HasPrefix(key, "/") && !strings.ContainsAny(key, "\\\x00\r\n") && key != "." && key != ".." && !strings.HasPrefix(key, "../")
}

// IsVersion reports whether version names a version of an object: S3 answers
// "null" for an object written while the bucket was unversioned.
func IsVersion(version string) bool { return version != "" && version != "null" }

// readObject reads the body of the object at key, which the S3 client always
// returns with a successful response.
func readObject(result *s3.GetObjectOutput, key string) ([]byte, error) {
	defer func() { _ = result.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(result.Body, MaxObjectBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read the body of %s: %w", key, err)
	}
	if len(data) == 0 || len(data) > MaxObjectBytes {
		return nil, fmt.Errorf("%w: %s holds %d bytes, outside 1..%d", errInvalidObject, key, len(data), MaxObjectBytes)
	}
	return data, nil
}

func isMissing(err error) bool {
	return platform.HasAWSErrorCode(err, "NoSuchKey", "NoSuchVersion", "NotFound")
}

// Digest is the content identity every ObjectRef carries.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
