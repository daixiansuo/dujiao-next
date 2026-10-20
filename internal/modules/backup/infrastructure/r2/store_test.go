package r2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dujiao-next/internal/modules/backup/domain"
)

func newTestStore(t *testing.T, handler http.HandlerFunc) (*store, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	objectStore, err := New(domain.Config{Endpoint: server.URL, Bucket: "backups", AccessKeyID: "test-access", SecretAccessKey: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return objectStore.(*store), server
}

func assertSigned(t *testing.T, r *http.Request) {
	t.Helper()
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "AWS4-HMAC-SHA256 Credential=test-access/") || !strings.Contains(authorization, "/auto/s3/aws4_request") || !strings.Contains(authorization, "Signature=") {
		t.Error("request is missing its region-auto SigV4 signature")
	}
	if r.Header.Get("X-Amz-Date") == "" || r.Header.Get("X-Amz-Content-Sha256") == "" {
		t.Error("request is missing signing headers")
	}
	if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") || strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") || r.Header.Get("X-Amz-Trailer") != "" || len(r.TransferEncoding) != 0 {
		t.Error("R2-incompatible streaming signature or checksum trailer")
	}
	for name := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-amz-checksum-") || strings.EqualFold(name, "X-Amz-Sdk-Checksum-Algorithm") {
			t.Error("optional AWS checksum extension was sent")
		}
	}
}

func TestUploadHeadDeleteAndPresign(t *testing.T) {
	body := []byte("custom PostgreSQL archive data")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var uploaded, deleted bool
	s, server := newTestStore(t, func(w http.ResponseWriter, r *http.Request) {
		assertSigned(t, r)
		if r.URL.Path == "/backups/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/backups/forbidden" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path != "/backups/daily/备份.dump" {
			t.Errorf("expected path style bucket and key, got %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPut:
			got, err := io.ReadAll(r.Body)
			if err != nil || string(got) != string(body) || r.ContentLength != int64(len(body)) {
				t.Error("upload did not stream the complete file")
			}
			if r.Header.Get("X-Amz-Meta-Sha256") != digest || r.Header.Get("Content-Type") != "application/octet-stream" || !strings.Contains(r.Header.Get("Authorization"), "x-amz-meta-sha256") {
				t.Error("missing signed SHA metadata or content type")
			}
			uploaded = true
			w.Header().Set("ETag", `"single"`)
		case http.MethodHead:
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.Header().Set("X-Amz-Meta-Sha256", digest)
		case http.MethodDelete:
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	file, err := os.CreateTemp(t.TempDir(), "archive")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(body); err != nil {
		t.Fatal(err)
	}
	// Upload must read from zero even when the caller's file offset is at EOF.
	if err := s.Upload(context.Background(), "daily/备份.dump", file, digest); err != nil {
		t.Fatal(err)
	}
	info, err := s.Head(context.Background(), "daily/备份.dump")
	if err != nil || info.Size != int64(len(body)) || info.SHA256 != digest {
		t.Fatalf("wrong object info: %+v, %v", info, err)
	}
	if _, err := s.Head(context.Background(), "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("404 not mapped to ErrNotFound: %v", err)
	}
	if _, err := s.Head(context.Background(), "forbidden"); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatal("403 incorrectly mapped to not found")
	}
	if err := s.Delete(context.Background(), "daily/备份.dump"); err != nil || !uploaded || !deleted {
		t.Fatalf("upload/delete failed: %v", err)
	}
	link, err := s.Presign(context.Background(), "daily/备份.dump", "数据库备份.dump", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	mediaType, params, err := mime.ParseMediaType(q.Get("response-content-disposition"))
	if err != nil || mediaType != "attachment" || params["filename"] != "数据库备份.dump" {
		t.Fatal("download filename was not encoded correctly")
	}
	if !strings.HasPrefix(link, server.URL+"/backups/") || q.Get("X-Amz-Expires") != "900" || len(q.Get("X-Amz-Signature")) != 64 || !strings.Contains(q.Get("X-Amz-Credential"), "/auto/s3/aws4_request") || strings.Contains(link, "test-secret") {
		t.Fatal("invalid presigned URL")
	}
	for _, ttl := range []time.Duration{0, 500 * time.Millisecond, -time.Second, 8 * 24 * time.Hour} {
		if _, err := s.Presign(context.Background(), "key", "name", ttl); err == nil {
			t.Fatal("invalid presign lifetime accepted")
		}
	}
}

func TestMultipartUploadAndAbortOnCancellation(t *testing.T) {
	for _, cancelUpload := range []bool{false, true} {
		t.Run(fmt.Sprint("cancel=", cancelUpload), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var initiated, completed, aborted bool
			var partNumbers []string
			var uploadedSize int64
			s, _ := newTestStore(t, func(w http.ResponseWriter, r *http.Request) {
				assertSigned(t, r)
				q := r.URL.Query()
				switch {
				case r.Method == http.MethodPost && q.Has("uploads"):
					initiated = true
					if r.Header.Get("X-Amz-Meta-Sha256") != "file-sha" {
						t.Error("multipart SHA metadata missing")
					}
					fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>test-upload</UploadId></InitiateMultipartUploadResult>`)
				case r.Method == http.MethodPut && q.Get("uploadId") == "test-upload":
					if cancelUpload {
						cancel()
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					partNumbers = append(partNumbers, q.Get("partNumber"))
					n, err := io.Copy(io.Discard, r.Body)
					if err != nil || n != r.ContentLength || n > partSize {
						t.Error("incorrect multipart stream length")
					}
					uploadedSize += n
					w.Header().Set("ETag", `"part-`+q.Get("partNumber")+`"`)
				case r.Method == http.MethodPost && q.Get("uploadId") == "test-upload":
					var body struct {
						Parts []struct {
							ETag       string
							PartNumber int
						} `xml:"Part"`
					}
					if err := xml.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Parts) != 2 || body.Parts[0].PartNumber != 1 || body.Parts[1].ETag != `"part-2"` {
						t.Error("incorrect multipart completion manifest")
					}
					completed = true
					fmt.Fprint(w, `<CompleteMultipartUploadResult><ETag>"complete"</ETag></CompleteMultipartUploadResult>`)
				case r.Method == http.MethodDelete && q.Get("uploadId") == "test-upload":
					aborted = true
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected multipart request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusBadRequest)
				}
			})
			file, err := os.Create(filepath.Join(t.TempDir(), "archive"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := file.Truncate(partSize + 17); err != nil {
				t.Fatal(err)
			}
			err = s.Upload(ctx, "archive.dump", file, "file-sha")
			if cancelUpload {
				if err == nil || !initiated || !aborted || completed {
					t.Fatalf("cancelled upload was not aborted: %v", err)
				}
			} else if err != nil || !initiated || !completed || aborted || strings.Join(partNumbers, ",") != "1,2" || uploadedSize != partSize+17 {
				t.Fatalf("multipart upload failed: %v", err)
			}
		})
	}
}

func TestInvalidConfigAndFile(t *testing.T) {
	base := domain.Config{Endpoint: "https://example.r2.cloudflarestorage.com", Bucket: "backups", AccessKeyID: "key", SecretAccessKey: "secret"}
	for _, endpoint := range []string{"", "ftp://localhost", "http:///missing", "https://user:secret@example.com", "https://example.com?query=1", "https://example.com/path"} {
		cfg := base
		cfg.Endpoint = endpoint
		if _, err := New(cfg); !errors.Is(err, domain.ErrInvalidConfig) {
			t.Fatal("invalid endpoint accepted")
		}
	}
	s, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(context.Background(), "key", nil, "sha"); err == nil {
		t.Fatal("nil file accepted")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if err := s.Upload(context.Background(), "key", reader, "sha"); err == nil {
		t.Fatal("non-seekable file accepted")
	}
}
