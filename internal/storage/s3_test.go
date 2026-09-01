package storage_test

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NewS3Client validation: bucket and region required.

func TestNewS3Client_RequiresBucket(t *testing.T) {
	_, err := storage.NewS3Client(&config.StorageConfig{
		S3Region: "us-east-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "s3_bucket")
}

func TestNewS3Client_RequiresRegion(t *testing.T) {
	_, err := storage.NewS3Client(&config.StorageConfig{
		S3Bucket: "my-bucket",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "s3_region")
}

func TestNewS3Client_Succeeds_WithBucketAndRegion(t *testing.T) {
	c, err := storage.NewS3Client(&config.StorageConfig{
		S3Bucket: "b",
		S3Region: "us-east-1",
	})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewS3Client_AcceptsStaticCredentials(t *testing.T) {
	c, err := storage.NewS3Client(&config.StorageConfig{
		S3Bucket: "b",
		S3Region: "us-east-1",
		S3Key:    "AKIA...",
		S3Secret: "secret",
	})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewS3Client_ParsesDigitalOceanSpacesHostname(t *testing.T) {
	c, err := storage.NewS3Client(&config.StorageConfig{
		S3Bucket: "tiqr-agent-storage.sgp1.digitaloceanspaces.com",
		S3Region: "sgp1",
		S3Key:    "key",
		S3Secret: "secret",
	})
	require.NoError(t, err)
	assert.Equal(t,
		"https://tiqr-agent-storage.sgp1.digitaloceanspaces.com/aisensy/foo.pdf",
		c.PublicURL("aisensy/foo.pdf"),
	)
}
