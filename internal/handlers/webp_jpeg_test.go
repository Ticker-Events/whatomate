package handlers

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2x2 lossy WebP generated with Pillow (RGB red).
const tinyWebPBase64 = "UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAUAmJaACdLoB+AADsAD+8ut//NgVzXPv9//S4P0uD9Lg/9KQAAA="

func tinyWebP(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(tinyWebPBase64)
	require.NoError(t, err)
	require.True(t, isWebPBytes(data))
	return data
}

func TestIsWebPPathOrMIME(t *testing.T) {
	t.Parallel()
	assert.True(t, isWebPPathOrMIME("image/webp", ""))
	assert.True(t, isWebPPathOrMIME("", "https://cdn.example.com/a.webp"))
	assert.True(t, isWebPPathOrMIME("", "https://cdn.example.com/a.WEBP?x=1"))
	assert.False(t, isWebPPathOrMIME("image/jpeg", "https://cdn.example.com/a.jpg"))
	assert.False(t, isWebPPathOrMIME("", "https://cdn.example.com/a.jpg"))
}

func TestWebPToJPEG(t *testing.T) {
	t.Parallel()
	jpegData, err := webpToJPEG(tinyWebP(t))
	require.NoError(t, err)
	require.Greater(t, len(jpegData), 2)
	assert.Equal(t, []byte{0xff, 0xd8}, jpegData[:2], "JPEG SOI marker")
}

func TestEnsureJPEGForWhatsApp(t *testing.T) {
	t.Parallel()

	webp := tinyWebP(t)
	out, mime, name, err := ensureJPEGForWhatsApp(webp, "image/webp", "product.webp")
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", mime)
	assert.Equal(t, "product.jpg", name)
	assert.Equal(t, []byte{0xff, 0xd8}, out[:2])

	png := []byte("\x89PNG\r\n\x1a\n")
	out, mime, name, err = ensureJPEGForWhatsApp(png, "image/png", "a.png")
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	assert.Equal(t, "a.png", name)
	assert.Equal(t, png, out)
}

func TestLooksLikeWebPFromBytes(t *testing.T) {
	t.Parallel()
	assert.True(t, looksLikeWebP("application/octet-stream", "file.bin", tinyWebP(t)))
}
