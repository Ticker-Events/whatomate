package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/shridarpatil/whatomate/internal/config"
)

// S3Client provides upload and presigned URL operations for call recordings
// and publicly fetchable media (e.g. AiSensy outbound media links).
type S3Client struct {
	client     *s3.Client
	bucket     string
	publicBase string // e.g. https://bucket.sgp1.digitaloceanspaces.com
}

// NewS3Client creates a new S3 client from the application's StorageConfig.
// Accepts either a bare bucket name or a DigitalOcean Spaces hostname
// (e.g. "tiqr-agent-storage.sgp1.digitaloceanspaces.com").
func NewS3Client(cfg *config.StorageConfig) (*S3Client, error) {
	if cfg.S3Bucket == "" || cfg.S3Region == "" {
		return nil, fmt.Errorf("s3_bucket and s3_region are required")
	}

	bucket, region, endpoint, publicBase := normalizeS3Bucket(cfg.S3Bucket, cfg.S3Region)

	opts := s3.Options{
		Region: region,
	}

	if endpoint != "" {
		opts.BaseEndpoint = aws.String(endpoint)
		opts.UsePathStyle = true
	}

	if cfg.S3Key != "" && cfg.S3Secret != "" {
		opts.Credentials = credentials.NewStaticCredentialsProvider(cfg.S3Key, cfg.S3Secret, "")
	}

	client := s3.New(opts)
	return &S3Client{client: client, bucket: bucket, publicBase: publicBase}, nil
}

// normalizeS3Bucket parses a bare bucket name or a Spaces virtual-host style
// hostname into bucket, region, API endpoint, and public base URL.
func normalizeS3Bucket(raw, region string) (bucket, outRegion, endpoint, publicBase string) {
	bucket = raw
	outRegion = region

	const spacesSuffix = ".digitaloceanspaces.com"
	if strings.HasSuffix(raw, spacesSuffix) {
		// "tiqr-agent-storage.sgp1.digitaloceanspaces.com"
		host := strings.TrimSuffix(raw, spacesSuffix) // "tiqr-agent-storage.sgp1"
		if i := strings.LastIndex(host, "."); i > 0 {
			bucket = host[:i]
			if outRegion == "" {
				outRegion = host[i+1:]
			}
		} else {
			bucket = host
		}
		if outRegion == "" {
			outRegion = region
		}
		endpoint = fmt.Sprintf("https://%s.digitaloceanspaces.com", outRegion)
		publicBase = fmt.Sprintf("https://%s.%s.digitaloceanspaces.com", bucket, outRegion)
		return bucket, outRegion, endpoint, publicBase
	}

	return bucket, outRegion, "", ""
}

// Upload uploads a file to S3 at the given key.
func (s *S3Client) Upload(ctx context.Context, key string, body io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	return err
}

// GetPresignedURL returns a time-limited download URL for the given S3 key.
func (s *S3Client) GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	presigner := s3.NewPresignClient(s.client)
	req, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// PublicURL returns a stable public HTTPS URL for the key when a Spaces/CDN
// public base is known. Empty when the bucket is not publicly addressable.
func (s *S3Client) PublicURL(key string) string {
	if s.publicBase == "" {
		return ""
	}
	return strings.TrimRight(s.publicBase, "/") + "/" + strings.TrimLeft(key, "/")
}

// UploadAndPresign uploads data and returns a time-limited HTTPS URL that
// third parties (e.g. AiSensy) can fetch without credentials.
func (s *S3Client) UploadAndPresign(ctx context.Context, key string, body io.Reader, contentType string, expiry time.Duration) (string, error) {
	if err := s.Upload(ctx, key, body, contentType); err != nil {
		return "", err
	}
	return s.GetPresignedURL(ctx, key, expiry)
}
