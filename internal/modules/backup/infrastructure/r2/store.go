// Package r2 implements the S3-compatible subset needed by database backups.
package r2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/dujiao-next/internal/modules/backup/contract"
	"github.com/dujiao-next/internal/modules/backup/domain"
)

const (
	partSize    int64 = 8 * 1024 * 1024
	maxPartSize int64 = 5 * 1024 * 1024 * 1024
	maxParts    int64 = 10000
)

type store struct {
	client *s3.Client
	bucket string
}

var _ contract.ObjectStore = (*store)(nil)

func New(cfg domain.Config) (contract.ObjectStore, error) {
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") ||
		strings.TrimSpace(cfg.Bucket) == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, domain.ErrInvalidConfig
	}
	client := s3.NewFromConfig(aws.Config{
		Region:      "auto",
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		// R2 does not implement every optional AWS checksum/trailer extension.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(strings.TrimRight(cfg.Endpoint, "/"))
		options.UsePathStyle = true
	})
	return &store{client: client, bucket: cfg.Bucket}, nil
}

func (s *store) Upload(ctx context.Context, key string, file *os.File, sha256 string) error {
	if file == nil {
		return errors.New("backup upload requires a file")
	}
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat backup file: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return errors.New("backup upload requires a seekable regular file")
	}
	metadata := map[string]string{"sha256": sha256}
	if stat.Size() <= partSize {
		_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(s.bucket), Key: aws.String(key),
			Body: io.NewSectionReader(file, 0, stat.Size()), ContentLength: aws.Int64(stat.Size()),
			ContentType: aws.String("application/octet-stream"), Metadata: metadata,
		})
		return err
	}
	// Read each part from the same seekable file. One request at a time keeps
	// memory and network concurrency bounded; no full-file or part-sized buffer.
	size := max(partSize, (stat.Size()+maxParts-1)/maxParts)
	if size > maxPartSize {
		return errors.New("backup exceeds the multipart object size limit")
	}
	created, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key),
		ContentType: aws.String("application/octet-stream"), Metadata: metadata,
	})
	if err != nil {
		return err
	}
	if created.UploadId == nil || *created.UploadId == "" {
		return errors.New("object storage returned an empty multipart upload ID")
	}
	completed := false
	defer func() {
		if !completed {
			// Cancellation must still release the remote multipart upload.
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_, _ = s.client.AbortMultipartUpload(cleanupCtx, &s3.AbortMultipartUploadInput{
				Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: created.UploadId,
			})
		}
	}()
	parts := make([]types.CompletedPart, 0, (stat.Size()+size-1)/size)
	for offset, number := int64(0), int32(1); offset < stat.Size(); offset, number = offset+size, number+1 {
		length := min(size, stat.Size()-offset)
		part, err := s.client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: created.UploadId,
			PartNumber: aws.Int32(number), Body: io.NewSectionReader(file, offset, length), ContentLength: aws.Int64(length),
		})
		if err != nil {
			return err
		}
		parts = append(parts, types.CompletedPart{ETag: part.ETag, PartNumber: aws.Int32(number)})
	}
	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	completed = err == nil
	return err
}

func (s *store) Head(ctx context.Context, key string) (domain.ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		var responseErr interface{ HTTPStatusCode() int }
		var apiErr smithy.APIError
		if (errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusNotFound) ||
			(errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey")) {
			return domain.ObjectInfo{}, domain.ErrNotFound
		}
		return domain.ObjectInfo{}, err
	}
	return domain.ObjectInfo{Size: aws.ToInt64(out.ContentLength), SHA256: out.Metadata["sha256"]}, nil
}

func (s *store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *store) Presign(ctx context.Context, key, filename string, ttl time.Duration) (string, error) {
	if ttl < time.Second || ttl > 7*24*time.Hour {
		return "", errors.New("download link lifetime must be between one second and seven days")
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	out, err := s3.NewPresignClient(s.client).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ResponseContentDisposition: aws.String(disposition),
	}, func(options *s3.PresignOptions) { options.Expires = ttl })
	if err != nil {
		return "", err
	}
	return out.URL, nil
}
