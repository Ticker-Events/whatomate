package tiqrecommerce

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

var (
	simpleEmailRE  = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	relativeTimeRE = regexp.MustCompile(`(?i)^\s*(?:in|after)\s+(\d+)\s*(m|min|mins|minute|minutes|h|hr|hrs|hour|hours)\s*$`)
	asapTimeRE     = regexp.MustCompile(`(?i)^\s*(asap|earliest|soonest|now|as soon as possible)\s*$`)
	absoluteTimeRE = regexp.MustCompile(`(?i)^\s*(today|tomorrow)?\s*(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*$`)
)

func promptCaptureField(field map[string]any) string {
	label := asString(field["label"])
	if help := asString(field["help_text"]); help != "" {
		label += "\n" + help
	}
	if options, ok := field["options"].([]any); ok && len(options) > 0 {
		values := make([]string, 0, len(options))
		for _, option := range options {
			values = append(values, fmt.Sprint(option))
		}
		label += "\nOptions: " + strings.Join(values, ", ")
	} else if options, ok := field["options"].([]string); ok && len(options) > 0 {
		label += "\nOptions: " + strings.Join(options, ", ")
	}
	if CaptureFieldAcceptsAttachment(field) {
		if imageCaptureField(field) {
			label += "\nPlease send a photo."
		} else {
			label += "\nPlease attach the requested file."
		}
	}
	return label
}

const (
	capturePendingKey      = "commerce_capture_pending"
	inboundCaptureMediaKey = "_inbound_capture_media"
	captureMediaNotesKey   = "media_shared"
)

// CaptureMediaLink is the handoff and order-notes URL for a saved WhatsApp message.
func CaptureMediaLink(messageID string) string {
	id := strings.TrimSpace(messageID)
	if id == "" {
		return ""
	}
	return "/api/media/" + id
}

// StashInboundCaptureMedia keeps this turn's photo on the session until the coded flow reads it.
func StashInboundCaptureMedia(session *models.ChatbotSession, messageID, mediaURL, mimeType, filename string) {
	if session == nil || strings.TrimSpace(mediaURL) == "" || strings.TrimSpace(messageID) == "" {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData[inboundCaptureMediaKey] = map[string]any{
		"message_id": messageID,
		"media_url":  mediaURL,
		"mime_type":  mimeType,
		"filename":   filename,
	}
}

// TakeInboundCaptureMedia removes and returns the stashed photo so it is not persisted.
func TakeInboundCaptureMedia(session *models.ChatbotSession) codedflow.InboundMedia {
	if session == nil || session.SessionData == nil {
		return codedflow.InboundMedia{}
	}
	raw, ok := asStringMap(session.SessionData[inboundCaptureMediaKey])
	delete(session.SessionData, inboundCaptureMediaKey)
	if !ok {
		return codedflow.InboundMedia{}
	}
	return codedflow.InboundMedia{
		MessageID: strings.TrimSpace(asString(raw["message_id"])),
		MediaURL:  strings.TrimSpace(asString(raw["media_url"])),
		MIMEType:  strings.TrimSpace(asString(raw["mime_type"])),
		Filename:  strings.TrimSpace(asString(raw["filename"])),
	}
}

// SessionExpectsCaptureAttachment reports a coded-flow step waiting for a photo or file.
func SessionExpectsCaptureAttachment(session *models.ChatbotSession) bool {
	if session == nil || session.SessionData == nil {
		return false
	}
	pending, ok := asStringMap(session.SessionData[capturePendingKey])
	if !ok {
		return false
	}
	if strings.TrimSpace(asString(pending["step"])) != strings.TrimSpace(session.CurrentStep) {
		return false
	}
	return CaptureFieldAcceptsAttachment(pending)
}

// RecordCaptureMediaNote stores the image link in commerce notes for checkout and handoff.
func RecordCaptureMediaNote(session *models.ChatbotSession, field map[string]any, link string) {
	link = strings.TrimSpace(link)
	if session == nil || link == "" {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	notes := jsonMapFromSession(session, "commerce_notes")
	label := strings.TrimSpace(asString(field["label"]))
	if label == "" {
		label = strings.TrimSpace(asString(field["key"]))
	}
	if label == "" {
		label = "Image"
	}
	line := label + ": " + link
	existing := strings.TrimSpace(asString(notes[captureMediaNotesKey]))
	switch {
	case existing == "":
		notes[captureMediaNotesKey] = line
	case !strings.Contains(existing, link):
		notes[captureMediaNotesKey] = existing + "\n" + line
	}
	if key := strings.TrimSpace(asString(field["key"])); key != "" {
		notes[key] = link
	}
	session.SessionData["commerce_notes"] = map[string]any(notes)
}

// SessionHandoffNotes is the readable notes text copied into handoff context.
func SessionHandoffNotes(session *models.ChatbotSession) string {
	if session == nil {
		return ""
	}
	return joinHandoffNoteParts(jsonMapFromSession(session, "commerce_notes"))
}

func joinHandoffNoteParts(notes models.JSONB) string {
	if len(notes) == 0 {
		return ""
	}
	primary := strings.TrimSpace(asString(notes["order_notes"]))
	if primary == "" {
		primary = strings.TrimSpace(asString(notes["customer_notes"]))
	}
	media := strings.TrimSpace(asString(notes[captureMediaNotesKey]))
	if media == "" || strings.Contains(primary, media) {
		return primary
	}
	if primary == "" {
		return media
	}
	return primary + "\n" + media
}

// MediaReferencesFromCaptured lists image links stored on captured field values.
func MediaReferencesFromCaptured(captured models.JSONB) []any {
	if len(captured) == 0 {
		return nil
	}
	keys := make([]string, 0, len(captured))
	for key := range captured {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]any, 0)
	for _, key := range keys {
		out = append(out, mediaRefsFromValue(key, captured[key])...)
	}
	return out
}

// MergeMediaReferences appends captured image links that are not already listed.
func MergeMediaReferences(existing []any, captured models.JSONB) []any {
	seen := map[string]bool{}
	out := make([]any, 0, len(existing))
	for _, raw := range existing {
		item, ok := asStringMap(raw)
		if !ok {
			continue
		}
		if key := mediaRefIdentity(item); key != "" {
			seen[key] = true
		}
		out = append(out, item)
	}
	for _, raw := range MediaReferencesFromCaptured(captured) {
		item, ok := asStringMap(raw)
		if !ok {
			continue
		}
		key := mediaRefIdentity(item)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func mediaRefIdentity(item map[string]any) string {
	if id := strings.TrimSpace(asString(item["message_id"])); id != "" {
		return id
	}
	return strings.TrimSpace(asString(item["url"]))
}

func mediaRefsFromValue(captureKey string, value any) []any {
	switch typed := value.(type) {
	case map[string]any:
		if item := mediaRefItem(captureKey, typed); item != nil {
			return []any{item}
		}
	case []any:
		out := make([]any, 0, len(typed))
		for _, entry := range typed {
			item, ok := asStringMap(entry)
			if !ok {
				continue
			}
			if ref := mediaRefItem(captureKey, item); ref != nil {
				out = append(out, ref)
			}
		}
		return out
	}
	return nil
}

func mediaRefItem(captureKey string, item map[string]any) map[string]any {
	messageID := strings.TrimSpace(asString(item["message_id"]))
	url := strings.TrimSpace(asString(item["url"]))
	if url == "" {
		url = CaptureMediaLink(messageID)
	}
	if url == "" {
		return nil
	}
	key := strings.TrimSpace(asString(item["capture_key"]))
	if key == "" {
		key = captureKey
	}
	return map[string]any{
		"message_id":  messageID,
		"type":        strings.TrimSpace(asString(item["mime_type"])),
		"filename":    strings.TrimSpace(asString(item["filename"])),
		"capture_key": key,
		"url":         url,
	}
}

func imageCaptureField(field map[string]any) bool {
	fieldType := strings.ToLower(asString(field["type"]))
	if fieldType == "image" || fieldType == "images" {
		return true
	}
	key := strings.ToLower(asString(field["key"]))
	return strings.Contains(key, "reference") && strings.Contains(key, "image")
}

// CaptureFieldAcceptsAttachment reports collection fields answered with a WhatsApp photo or file.
func CaptureFieldAcceptsAttachment(field map[string]any) bool {
	fieldType := strings.ToLower(asString(field["type"]))
	switch fieldType {
	case "image", "images", "file", "document", "media", "attachment":
		return true
	}
	key := strings.ToLower(asString(field["key"]))
	return strings.Contains(key, "reference") && strings.Contains(key, "image")
}

// CaptureAttachmentRetry is the reply when a photo or file field did not receive media.
func CaptureAttachmentRetry(field map[string]any) string {
	if imageCaptureField(field) {
		return "Please send the photo again."
	}
	return "Please attach the requested file again."
}

func validCaptureValue(field map[string]any, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if asString(field["type"]) == "number" {
		_, err := strconv.ParseFloat(value, 64)
		return err == nil
	}
	var options []string
	switch raw := field["options"].(type) {
	case []string:
		options = raw
	case []any:
		for _, option := range raw {
			options = append(options, fmt.Sprint(option))
		}
	}
	if len(options) > 0 {
		values := []string{value}
		if asString(field["type"]) == "multi_select" {
			values = strings.Split(value, ",")
		} else if asString(field["type"]) != "single_select" {
			return true
		}
		for _, candidate := range values {
			found := false
			for _, option := range options {
				if strings.EqualFold(strings.TrimSpace(option), strings.TrimSpace(candidate)) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return true
}

func normalizedCaptureValue(field map[string]any, value string) any {
	if asString(field["type"]) != "multi_select" {
		return strings.TrimSpace(value)
	}
	raw := strings.Split(value, ",")
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func checkoutSlotLabel(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return codedflow.TruncateRunes(value, 20)
	}
	return parsed.Format("02 Jan 3:04 PM")
}

func parseFulfillmentTimeText(text string, now time.Time, loc *time.Location, earliestAt string) (time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, errors.New("empty time")
	}
	if asapTimeRE.MatchString(text) {
		if earliestAt != "" {
			if parsed, err := time.Parse(time.RFC3339, earliestAt); err == nil {
				return parsed.In(loc), nil
			}
		}
		return now.Add(time.Minute).Truncate(time.Minute), nil
	}
	if m := relativeTimeRE.FindStringSubmatch(text); len(m) == 3 {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return time.Time{}, errors.New("invalid relative amount")
		}
		unit := strings.ToLower(m[2])
		switch unit {
		case "h", "hr", "hrs", "hour", "hours":
			return now.Add(time.Duration(n) * time.Hour).Truncate(time.Minute), nil
		default:
			return now.Add(time.Duration(n) * time.Minute).Truncate(time.Minute), nil
		}
	}
	if m := absoluteTimeRE.FindStringSubmatch(text); len(m) == 5 {
		dayOffset := 0
		switch strings.ToLower(m[1]) {
		case "tomorrow":
			dayOffset = 1
		}
		hour, err := strconv.Atoi(m[2])
		if err != nil {
			return time.Time{}, err
		}
		minute := 0
		if m[3] != "" {
			minute, err = strconv.Atoi(m[3])
			if err != nil || minute > 59 {
				return time.Time{}, errors.New("invalid minutes")
			}
		}
		ampm := strings.ToLower(m[4])
		if ampm == "pm" || ampm == "am" {
			if hour < 1 || hour > 12 {
				return time.Time{}, errors.New("invalid hour")
			}
			if ampm == "pm" && hour < 12 {
				hour += 12
			}
			if ampm == "am" && hour == 12 {
				hour = 0
			}
		} else if hour > 23 {
			return time.Time{}, errors.New("invalid hour")
		}
		base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, dayOffset)
		candidate := time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, loc)
		// Bare clock times that already passed today roll to tomorrow.
		if dayOffset == 0 && m[1] == "" && !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate, nil
	}
	return time.Time{}, errors.New("unrecognized time")
}
