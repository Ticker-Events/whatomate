package handlers

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
)

const maxAIImageBytes = 5 * 1024 * 1024

var supportedAIImageMIMEs = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// AIConversationMessage preserves the persisted message and media identity while
// provider adapters translate it to their native content-part format.
type AIConversationMessage struct {
	Role        string
	Text        string
	MessageID   *uuid.UUID
	Attachments []AIAttachment
}

type AIAttachment struct {
	MessageID  uuid.UUID `json:"message_id"`
	MediaURL   string    `json:"media_url"`
	MIMEType   string    `json:"mime_type"`
	Filename   string    `json:"filename,omitempty"`
	CaptureKey string    `json:"capture_key,omitempty"`
}

func attachmentFromMessage(msg *models.Message, captureKey string) []AIAttachment {
	if msg == nil || strings.TrimSpace(msg.MediaURL) == "" {
		return nil
	}
	return []AIAttachment{{
		MessageID:  msg.ID,
		MediaURL:   msg.MediaURL,
		MIMEType:   strings.ToLower(strings.TrimSpace(msg.MediaMimeType)),
		Filename:   msg.MediaFilename,
		CaptureKey: captureKey,
	}}
}

func attachmentsJSON(items []AIAttachment) models.JSONBArray {
	out := make(models.JSONBArray, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"message_id": item.MessageID.String(), "media_url": item.MediaURL,
			"mime_type": item.MIMEType, "filename": item.Filename, "capture_key": item.CaptureKey,
		})
	}
	return out
}

func attachmentsFromJSON(items models.JSONBArray) []AIAttachment {
	out := make([]AIAttachment, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, err := uuid.Parse(asString(item["message_id"]))
		if err != nil {
			continue
		}
		out = append(out, AIAttachment{
			MessageID: id, MediaURL: asString(item["media_url"]),
			MIMEType: asString(item["mime_type"]), Filename: asString(item["filename"]),
			CaptureKey: asString(item["capture_key"]),
		})
	}
	return out
}

func (a *App) currentAIAttachments(session *models.ChatbotSession, text string) []AIAttachment {
	if a == nil || a.DB == nil || session == nil {
		return nil
	}
	var message models.ChatbotSessionMessage
	err := a.DB.Where("session_id = ? AND direction = ? AND message = ?", session.ID, models.DirectionIncoming, text).
		Order("created_at DESC").First(&message).Error
	if err != nil {
		return nil
	}
	return attachmentsFromJSON(message.Attachments)
}

func (a *App) loadAIImage(attachment AIAttachment) ([]byte, error) {
	mimeType := strings.ToLower(strings.TrimSpace(attachment.MIMEType))
	if !supportedAIImageMIMEs[mimeType] {
		return nil, fmt.Errorf("unsupported AI image MIME type %q", mimeType)
	}
	relative := filepath.Clean(strings.TrimPrefix(attachment.MediaURL, "/"))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("invalid media path")
	}
	base, err := filepath.Abs(a.getMediaStoragePath())
	if err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(base, relative))
	if err != nil || (path != base && !strings.HasPrefix(path, base+string(filepath.Separator))) {
		return nil, errors.New("media path escapes storage root")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open controlled media: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() > maxAIImageBytes {
		return nil, fmt.Errorf("AI image size must be between 1 and %d bytes", maxAIImageBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAIImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAIImageBytes {
		return nil, fmt.Errorf("AI image exceeds %d bytes", maxAIImageBytes)
	}
	detectedMIME := normalizeImageMIME(http.DetectContentType(data))
	if detectedMIME == "" {
		return nil, errors.New("AI image content is not a supported image type")
	}
	if detectedMIME != mimeType {
		return nil, fmt.Errorf("AI image content type %q does not match declared MIME type %q", detectedMIME, mimeType)
	}
	return data, nil
}

func (a *App) openAIContent(text string, attachments []AIAttachment) any {
	parts := make([]map[string]any, 0, len(attachments)+1)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]any{"type": "text", "text": text})
	}
	for _, attachment := range attachments {
		data, err := a.loadAIImage(attachment)
		if err != nil {
			a.Log.Warn("AI image omitted", "message_id", attachment.MessageID, "error", err)
			continue
		}
		parts = append(parts, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": "data:" + attachment.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(data)},
		})
	}
	if len(parts) == 0 {
		return "[Attachment unavailable]"
	}
	if len(parts) == 1 && parts[0]["type"] == "text" {
		return text
	}
	return parts
}

func (a *App) anthropicContent(text string, attachments []AIAttachment) any {
	parts := make([]map[string]any, 0, len(attachments)+1)
	for _, attachment := range attachments {
		data, err := a.loadAIImage(attachment)
		if err != nil {
			a.Log.Warn("AI image omitted", "message_id", attachment.MessageID, "error", err)
			continue
		}
		parts = append(parts, map[string]any{"type": "image", "source": map[string]any{
			"type": "base64", "media_type": attachment.MIMEType, "data": base64.StdEncoding.EncodeToString(data),
		}})
	}
	if strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]any{"type": "text", "text": text})
	}
	if len(parts) == 0 {
		return "[Attachment unavailable]"
	}
	return parts
}

func (a *App) geminiParts(text string, attachments []AIAttachment) []map[string]any {
	parts := make([]map[string]any, 0, len(attachments)+1)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]any{"text": text})
	}
	for _, attachment := range attachments {
		data, err := a.loadAIImage(attachment)
		if err != nil {
			a.Log.Warn("AI image omitted", "message_id", attachment.MessageID, "error", err)
			continue
		}
		parts = append(parts, map[string]any{"inlineData": map[string]any{
			"mimeType": attachment.MIMEType, "data": base64.StdEncoding.EncodeToString(data),
		}})
	}
	if len(parts) == 0 {
		parts = append(parts, map[string]any{"text": "[Attachment unavailable]"})
	}
	return parts
}
