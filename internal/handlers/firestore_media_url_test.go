package handlers

import (
	"testing"

	"github.com/google/uuid"
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

func TestDisplayMediaURLPrefersSpacesHost(t *testing.T) {
	client, err := storage.NewS3Client(&config.StorageConfig{
		S3Bucket: "tiqr-agent-storage.sgp1.digitaloceanspaces.com",
		S3Region: "sgp1",
	})
	require.NoError(t, err)

	got := displayMediaURL(client, "images/4c844ca5-0e76-4826-9ee3-1213fd218a88.jpg", "msg-1")
	assert.Equal(t, "https://tiqr-agent-storage.sgp1.digitaloceanspaces.com/images/4c844ca5-0e76-4826-9ee3-1213fd218a88.jpg", got)
}

func TestDisplayMediaURLFallsBackToAPIMedia(t *testing.T) {
	assert.Equal(t, "/api/media/msg-1", displayMediaURL(nil, "images/a.jpg", "msg-1"))
	assert.Equal(t, "https://cdn.example/a.jpg", displayMediaURL(nil, "https://cdn.example/a.jpg", "msg-1"))
	assert.Equal(t, "", displayMediaURL(nil, "  ", ""))
}

func TestAttachmentsJSONUsesDisplayMediaURL(t *testing.T) {
	client, err := storage.NewS3Client(&config.StorageConfig{
		S3Bucket: "tiqr-agent-storage.sgp1.digitaloceanspaces.com",
		S3Region: "sgp1",
	})
	require.NoError(t, err)

	id := uuid.MustParse("4c844ca5-0e76-4826-9ee3-1213fd218a88")
	out := attachmentsJSON(client, []AIAttachment{{
		MessageID: id, MediaURL: "images/ref.jpg", MIMEType: "image/jpeg", Filename: "ref.jpg",
	}})
	require.Len(t, out, 1)
	item, ok := out[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "images/ref.jpg", item["media_url"])
	assert.Equal(t, "https://tiqr-agent-storage.sgp1.digitaloceanspaces.com/images/ref.jpg", item["url"])

	fallback := attachmentsJSON(nil, []AIAttachment{{
		MessageID: id, MediaURL: "images/ref.jpg", MIMEType: "image/jpeg",
	}})
	item, ok = fallback[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "/api/media/"+id.String(), item["url"])
}
