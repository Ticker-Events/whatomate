package handlers

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirestoreMediaURLUsesSpacesHost(t *testing.T) {
	client, err := storage.NewS3Client(&config.StorageConfig{
		S3Bucket: "tiqr-agent-storage.sgp1.digitaloceanspaces.com",
		S3Region: "sgp1",
	})
	require.NoError(t, err)

	got := firestoreMediaURL(client, "images/4c844ca5-0e76-4826-9ee3-1213fd218a88.jpg")
	assert.Equal(t, "https://tiqr-agent-storage.sgp1.digitaloceanspaces.com/images/4c844ca5-0e76-4826-9ee3-1213fd218a88.jpg", got)
}

func TestFirestoreMediaURLLeavesAbsoluteAndLocal(t *testing.T) {
	assert.Equal(t, "https://cdn.example/a.jpg", firestoreMediaURL(nil, "https://cdn.example/a.jpg"))
	assert.Equal(t, "images/a.jpg", firestoreMediaURL(nil, "images/a.jpg"))
	assert.Equal(t, "", firestoreMediaURL(nil, "  "))
}
