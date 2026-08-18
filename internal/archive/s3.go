package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

type S3Object struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag,omitempty"`
}

type s3Store struct {
	client *s3.Client
	bucket string
}

func newS3Store(cfg config.ArchiveS3Config) (*s3Store, error) {
	if strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.Bucket) == "" ||
		strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, errors.New("region, bucket, access_key_id, and secret_access_key are required")
	}
	awsCfg := aws.Config{
		Region: cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "",
		),
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.ForcePathStyle
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(strings.TrimRight(cfg.Endpoint, "/"))
		}
	})
	return &s3Store{client: client, bucket: cfg.Bucket}, nil
}

func (s *s3Store) test(ctx context.Context, prefix string) error {
	key := strings.Trim(strings.TrimSpace(prefix), "/") + "/.prismcat-probe-" + uuid.NewString()
	key = strings.TrimLeft(key, "/")
	data := []byte("prismcat-s3-probe")
	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: bytes.NewReader(data),
		ContentType:    aws.String("application/octet-stream"),
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(sum[:])),
		Metadata:       map[string]string{"sha256": shaHex},
	})
	if err != nil {
		return fmt.Errorf("write S3 probe: %w", err)
	}
	defer func() {
		_, _ = s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	}()
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return fmt.Errorf("inspect S3 probe: %w", err)
	}
	if aws.ToInt64(head.ContentLength) != int64(len(data)) || !strings.EqualFold(head.Metadata["sha256"], shaHex) {
		return errors.New("S3 probe metadata mismatch")
	}
	got, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return fmt.Errorf("read S3 probe: %w", err)
	}
	body, readErr := io.ReadAll(got.Body)
	closeErr := got.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read S3 probe body: %w", readErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if !bytes.Equal(body, data) {
		return errors.New("S3 probe content mismatch")
	}
	return nil
}

func (s *s3Store) upload(ctx context.Context, key, path, shaHex, contentType string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	uploader := manager.NewUploader(s.client, func(u *manager.Uploader) {
		u.PartSize = 8 << 20
		u.Concurrency = 2
	})
	out, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: f,
		ContentType:       aws.String(contentType),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		Metadata:          map[string]string{"sha256": shaHex},
	})
	if err != nil {
		return 0, fmt.Errorf("multipart upload: %w", err)
	}
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return 0, fmt.Errorf("verify uploaded object: %w", err)
	}
	if head.ContentLength == nil || *head.ContentLength != info.Size() {
		return 0, fmt.Errorf("uploaded object length mismatch: got %v, want %d", head.ContentLength, info.Size())
	}
	if !strings.EqualFold(head.Metadata["sha256"], shaHex) {
		return 0, errors.New("uploaded object SHA-256 metadata mismatch")
	}
	serverChecksum := aws.ToString(head.ChecksumSHA256)
	if serverChecksum != "" && aws.ToString(out.ChecksumSHA256) != "" {
		if serverChecksum != aws.ToString(out.ChecksumSHA256) {
			return 0, errors.New("uploaded object server checksum mismatch")
		}
	} else if err := s.verifyObjectHash(ctx, key, shaHex); err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (s *s3Store) uploadBytes(ctx context.Context, key string, data []byte, contentType string) error {
	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])
	tmp, err := os.CreateTemp("", "prismcat-sidecar-*")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_, err = s.upload(ctx, key, path, shaHex, contentType)
	return err
}

func (s *s3Store) verifyObjectHash(ctx context.Context, key, wantHex string) error {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("download object for checksum verification: %w", err)
	}
	defer result.Body.Close()
	h := sha256.New()
	if _, err := io.Copy(h, result.Body); err != nil {
		return fmt.Errorf("hash downloaded object: %w", err)
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantHex) {
		return errors.New("downloaded object SHA-256 mismatch")
	}
	return nil
}

func (s *s3Store) list(ctx context.Context, prefix string) ([]S3Object, error) {
	packages := make(map[string]S3Object)
	committed := make(map[string]bool)
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket), Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Contents {
			key := aws.ToString(item.Key)
			lower := strings.ToLower(key)
			if strings.HasSuffix(lower, ".tar.zst.manifest.json") {
				committed[strings.TrimSuffix(key, ".manifest.json")] = true
			} else if strings.HasSuffix(lower, ".tar.zst") {
				packages[key] = S3Object{Key: key, Size: aws.ToInt64(item.Size), LastModified: aws.ToTime(item.LastModified), ETag: strings.Trim(aws.ToString(item.ETag), "\"")}
			}
		}
	}
	var out []S3Object
	for key, item := range packages {
		if committed[key] {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key > out[j].Key })
	return out, nil
}

func (s *s3Store) size(ctx context.Context, key string) (int64, error) {
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return 0, err
	}
	if result.ContentLength == nil || *result.ContentLength < 0 {
		return 0, errors.New("S3 object has invalid content length")
	}
	return *result.ContentLength, nil
}

func (s *s3Store) download(ctx context.Context, key, path string, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("download limit must be positive")
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return 0, err
	}
	defer result.Body.Close()
	if result.ContentLength != nil && *result.ContentLength > limit {
		return 0, fmt.Errorf("S3 object exceeds download limit: %d > %d", *result.ContentLength, limit)
	}
	return writeLimitedDownload(path, result.Body, limit)
}

func writeLimitedDownload(path string, src io.Reader, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("download limit must be positive")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return 0, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(path)
		}
	}()
	written, copyErr := io.Copy(f, io.LimitReader(src, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if written > limit {
		return written, fmt.Errorf("S3 object exceeds download limit: read more than %d bytes", limit)
	}
	if closeErr != nil {
		return written, closeErr
	}
	ok = true
	return written, nil
}

func (s *s3Store) readBytes(ctx context.Context, key string, limit int64) ([]byte, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	return io.ReadAll(io.LimitReader(result.Body, limit+1))
}
