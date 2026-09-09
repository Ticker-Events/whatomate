package handlers

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/tickermcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testAIImageApp(t *testing.T, data []byte, mimeType string) (*App, AIAttachment) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "images"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "images", "reference.jpg"), data, 0o600))
	app := &App{Config: &config.Config{}}
	app.Config.Storage.LocalPath = root
	return app, AIAttachment{
		MessageID: uuid.New(), MediaURL: "images/reference.jpg", MIMEType: mimeType,
		CaptureKey: "reference_images",
	}
}

func testJPEGBytes() []byte {
	return []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01}
}

func TestProviderNativeImageParts(t *testing.T) {
	imageBytes := testJPEGBytes()
	app, attachment := testAIImageApp(t, imageBytes, "image/jpeg")
	encoded := base64.StdEncoding.EncodeToString(imageBytes)

	openAI, ok := app.openAIContent("reference", []AIAttachment{attachment}).([]map[string]any)
	require.True(t, ok)
	require.Len(t, openAI, 2)
	assert.Equal(t, "data:image/jpeg;base64,"+encoded, openAI[1]["image_url"].(map[string]any)["url"])

	anthropic := app.anthropicContent("reference", []AIAttachment{attachment}).([]map[string]any)
	source := anthropic[0]["source"].(map[string]any)
	assert.Equal(t, "base64", source["type"])
	assert.Equal(t, "image/jpeg", source["media_type"])
	assert.Equal(t, encoded, source["data"])

	gemini := app.geminiParts("reference", []AIAttachment{attachment})
	inline := gemini[1]["inlineData"].(map[string]any)
	assert.Equal(t, "image/jpeg", inline["mimeType"])
	assert.Equal(t, encoded, inline["data"])
}

func TestCaptionlessCommerceMediaFallbackText(t *testing.T) {
	assert.Equal(t, "[Image attached]", commerceMediaFallbackText("image", ""))
	assert.Equal(t, "[Document attached: design.pdf]", commerceMediaFallbackText("document", "design.pdf"))
	assert.Empty(t, commerceMediaFallbackText("video", "clip.mp4"))
}

func TestAIImageValidationRejectsMIMEAndSize(t *testing.T) {
	app, attachment := testAIImageApp(t, []byte("pdf"), "application/pdf")
	_, err := app.loadAIImage(attachment)
	require.ErrorContains(t, err, "unsupported")

	app, attachment = testAIImageApp(t, make([]byte, maxAIImageBytes+1), "image/png")
	_, err = app.loadAIImage(attachment)
	require.ErrorContains(t, err, "between")

	attachment.MediaURL = "../outside.png"
	_, err = app.loadAIImage(attachment)
	require.ErrorContains(t, err, "invalid media path")

	app, attachment = testAIImageApp(t, []byte("%PDF-1.7"), "image/jpeg")
	_, err = app.loadAIImage(attachment)
	require.ErrorContains(t, err, "not a supported image")

	app, attachment = testAIImageApp(t, testJPEGBytes(), "image/png")
	_, err = app.loadAIImage(attachment)
	require.ErrorContains(t, err, "does not match declared")
}

func TestRequiredCaptureMissingIncludesMedia(t *testing.T) {
	fields := []tickermcp.CaptureField{
		{Key: "flavour", Required: true},
		{Key: "reference_images", Type: "image", Required: true},
		{Key: "writing", Required: false},
	}
	captured := models.JSONB{"flavour": "Chocolate", "reference_images": models.JSONBArray{}}
	assert.Equal(t, []string{"reference_images"}, requiredCaptureMissing(fields, captured))
	captured["reference_images"] = attachmentsJSON([]AIAttachment{{MessageID: uuid.New(), MediaURL: "images/a.jpg", MIMEType: "image/jpeg"}})
	assert.Empty(t, requiredCaptureMissing(fields, captured))
}
