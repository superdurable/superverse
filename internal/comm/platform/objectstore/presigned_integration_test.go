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
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// TestPresignedTransfersIntegration drives a browser's presigned upload and
// download against the real versioned S3/MinIO API: the exact signed request
// succeeds, another length, digest, media type or a second PUT is refused by
// storage, a CORS preflight from a Studio origin is answered, the head read
// and VerifyUpload report the stored upload, the presigned GET downloads that
// version as a download or inline, and CopyVersion copies it once however
// often it is asked and reports a deleted source version as ErrNotFound.
func TestPresignedTransfersIntegration(t *testing.T) {
	endpoint, bucket := os.Getenv("SUPERVERSE_S3_ENDPOINT"), os.Getenv("SUPERVERSE_ARTIFACT_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Fatal("requires a versioned artifact bucket: SUPERVERSE_S3_ENDPOINT and SUPERVERSE_ARTIFACT_BUCKET")
	}
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatal("integration requires explicit loopback S3 endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pathStyle := func(o *s3.Options) { o.BaseEndpoint = &endpoint; o.UsePathStyle = true }
	store, err := NewStore(s3.NewFromConfig(cfg, pathStyle), bucket, "", "sv2-test", true)
	if err != nil {
		t.Fatal(err)
	}
	// The browser reaches the same loopback MinIO, as on both local lanes.
	store = store.WithPresigner(s3.NewPresignClient(s3.NewFromConfig(cfg, pathStyle)))
	id := flowid.RandomID(12)
	prefix := "integration-" + id + "/"
	defer func() {
		for attempt := 0; attempt < 8; attempt++ {
			if done, cleanErr := store.DeletePrefix(context.Background(), prefix); cleanErr != nil || done {
				return
			}
		}
	}()

	body := []byte("an uploaded order process, version " + id)
	digest := Digest(body)
	upload := Upload{Key: prefix + "original/" + strings.TrimPrefix(digest, "sha256:"), MediaType: "text/markdown", Size: int64(len(body)), Digest: digest}
	put, err := store.PresignPut(ctx, upload, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if put.method != http.MethodPut || strings.Contains(put.String(), "X-Amz-Signature") {
		t.Fatalf("the presigned PUT is %s and prints %q", put.method, put.String())
	}
	for _, header := range []string{"content-type", "if-none-match", "x-amz-checksum-sha256", "x-amz-meta-sha256"} {
		if put.Headers[header] == "" {
			t.Fatalf("the presigned PUT does not sign %s: %v", header, put.Headers)
		}
	}
	if _, err = store.head(ctx, upload.Key); !errors.Is(err, ErrNotFound) {
		t.Fatal("the head of a key that holds nothing is not ErrNotFound:", err)
	}

	// A CORS preflight from a Studio origin, which a browser sends before the PUT.
	preflight, err := http.NewRequestWithContext(ctx, http.MethodOptions, put.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	preflight.Header.Set("Origin", "http://localhost:21000")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPut)
	preflight.Header.Set("Access-Control-Request-Headers", "content-type,if-none-match,x-amz-checksum-sha256,x-amz-meta-sha256")
	answer := send(t, preflight)
	if answer.status/100 != 2 || answer.header.Get("Access-Control-Allow-Origin") == "" || !strings.Contains(strings.ToLower(answer.header.Get("Access-Control-Allow-Methods")), "put") {
		t.Fatalf("the CORS preflight answered %d with %v", answer.status, answer.header)
	}

	longer := append(append([]byte{}, body...), '!')
	other := append([]byte{}, body...)
	other[0] = 'A'
	for name, attempt := range map[string]struct {
		body    []byte
		headers map[string]string
	}{
		"another length":     {longer, put.Headers},
		"another digest":     {other, put.Headers},
		"another media type": {body, with(put.Headers, "content-type", "text/html")},
		"no checksum header": {body, with(put.Headers, "x-amz-checksum-sha256", "")},
	} {
		if refused := send(t, browserPut(ctx, t, put.URL, attempt.headers, attempt.body)); refused.status/100 == 2 {
			t.Fatalf("storage accepted a PUT with %s: %d", name, refused.status)
		}
	}
	if accepted := send(t, browserPut(ctx, t, put.URL, put.Headers, body)); accepted.status != http.StatusOK {
		t.Fatalf("the exact presigned PUT answered %d: %s", accepted.status, accepted.body)
	}
	if again := send(t, browserPut(ctx, t, put.URL, put.Headers, body)); again.status != http.StatusPreconditionFailed {
		t.Fatalf("a second PUT of the key answered %d, not 412", again.status)
	}

	ref, err := store.VerifyUpload(ctx, upload)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.head(ctx, upload.Key)
	if err != nil || head.ref != ref || head.size != upload.Size || ref.Digest != digest || ref.MediaType != upload.MediaType {
		t.Fatalf("the head read %+v and the verified upload %+v differ from the upload: %v", head, ref, err)
	}
	declaredOther := upload
	declaredOther.Size++
	if _, err = store.VerifyUpload(ctx, declaredOther); !errors.Is(err, ErrUploadMismatch) {
		t.Fatal("a declared length that differs from storage is not ErrUploadMismatch:", err)
	}
	if read, readErr := store.Get(ctx, ref); readErr != nil || !bytes.Equal(read, body) {
		t.Fatal("the uploaded version does not read back verified", readErr)
	}

	get, err := store.PresignGet(ctx, ref, "Order process.md", false, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	download, err := http.NewRequestWithContext(ctx, http.MethodGet, get.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	download.Header.Set("Origin", "http://localhost:21000")
	downloaded := send(t, download)
	if downloaded.status != http.StatusOK || !bytes.Equal(downloaded.body, body) ||
		downloaded.header.Get("Content-Disposition") != `attachment; filename="Order process.md"; filename*=UTF-8''Order%20process.md` ||
		downloaded.header.Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("the presigned GET answered %d with %v", downloaded.status, downloaded.header)
	}
	inline, err := store.PresignGet(ctx, ref, "Order process.md", true, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := http.NewRequestWithContext(ctx, http.MethodGet, inline.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if answer := send(t, shown); answer.status != http.StatusOK || !strings.HasPrefix(answer.header.Get("Content-Disposition"), `inline; filename="Order process.md"`) {
		t.Fatalf("the inline presigned GET answered %d with %v", answer.status, answer.header)
	}

	copied, err := store.CopyVersion(ctx, ref, prefix+"copies/original")
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.CopyVersion(ctx, ref, prefix+"copies/original")
	if err != nil || again != copied || copied.Digest != ref.Digest || copied.MediaType != ref.MediaType || copied.Key != store.FullKey(prefix+"copies/original") {
		t.Fatalf("a repeated copy answered %+v, not the first copy %+v: %v", again, copied, err)
	}
	if read, readErr := store.Get(ctx, copied); readErr != nil || !bytes.Equal(read, body) {
		t.Fatal("the copy does not read back verified", readErr)
	}
	conflicting, err := store.Put(ctx, prefix+"copies/other", "text/markdown", []byte("other content"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CopyVersion(ctx, ref, prefix+"copies/other"); !errors.Is(err, errImmutableConflict) {
		t.Fatal("a copy onto other content is not an immutable conflict:", err)
	}
	stored, err := store.Put(ctx, prefix+"rendition/1", MediaTypeRendition, []byte(`{"type":"text","text":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if renditionCopy, copyErr := store.CopyVersion(ctx, stored, prefix+"copies/rendition"); copyErr != nil || renditionCopy.Digest != stored.Digest {
		t.Fatal("a copy of an object the store wrote failed", copyErr)
	}
	if err = store.Delete(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CopyVersion(ctx, stored, prefix+"copies/gone"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a copy of a deleted version is not ErrNotFound:", err)
	}
	t.Logf("verified presigned upload %s version %s, its refusals, download and copy %s; kept %s", ref.Key, ref.Version, copied.Version, conflicting.Key)
}

// response is what storage answered a browser request.
type response struct {
	status int
	header http.Header
	body   []byte
}

func send(t *testing.T, request *http.Request) response {
	t.Helper()
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = answer.Body.Close() }()
	body, err := io.ReadAll(answer.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{status: answer.StatusCode, header: answer.Header, body: body}
}

// browserPut is the PUT a browser sends with headers; it sets Content-Length
// from the body itself.
func browserPut(ctx context.Context, t *testing.T, url string, headers map[string]string, body []byte) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		if value != "" {
			request.Header.Set(name, value)
		}
	}
	return request
}

// with is headers with name set to value, or removed when value is empty.
func with(headers map[string]string, name, value string) map[string]string {
	changed := map[string]string{}
	for key, existing := range headers {
		changed[key] = existing
	}
	changed[name] = value
	return changed
}
