package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const maxImageBytes int64 = 10 << 20

type ObjectStore struct {
	client *minio.Client
	bucket string
}

func NewObjectStore(cfg config.ObjectStorageConfig) (*ObjectStore, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" || strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("web object storage endpoint and bucket are required")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("web object storage access_key and secret_key are required")
	}
	lookup := minio.BucketLookupAuto
	if cfg.ForcePathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       cfg.UseSSL,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}
	return &ObjectStore{client: client, bucket: cfg.Bucket}, nil
}

func (s *ObjectStore) EnsureBucket(ctx context.Context, region string) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check object storage bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		return fmt.Errorf("create object storage bucket: %w", err)
	}
	return nil
}

func (s *ObjectStore) Put(ctx context.Context, key, mimeType string, data []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: mimeType})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

func (s *ObjectStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		return nil, err
	}
	return object, nil
}

func (s *ObjectStore) DataURL(ctx context.Context, key, mimeType, _ string) (string, error) {
	reader, err := s.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxImageBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxImageBytes {
		return "", fmt.Errorf("stored image exceeds %d bytes", maxImageBytes)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (s *ObjectStore) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
