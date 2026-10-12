package codedflow

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	// ArchiveKey holds the preview-shaped session log on SessionData.
	ArchiveKey = "coded_session_archive"
	// archiveByteBudget caps Postgres session_data size for the archive.
	archiveByteBudget = 180_000
)

// SessionArchive is the versioned log Copy Log / Import session log share.
type SessionArchive struct {
	Version        int                  `json:"version"`
	FlowKey        string               `json:"flow_key"`
	SessionID      string               `json:"session_id"`
	ContactID      string               `json:"contact_id"`
	OrganizationID string               `json:"organization_id"`
	StartedAt      string               `json:"started_at"`
	EndedAt        string               `json:"ended_at,omitempty"`
	Status         string               `json:"status"`
	Turns          []SessionArchiveTurn `json:"turns"`
}

// SessionArchiveTurn is one inbound + outbound preview-shaped turn.
type SessionArchiveTurn struct {
	At       string                `json:"at"`
	Inbound  map[string]any        `json:"inbound,omitempty"`
	Messages []CodedPreviewMessage `json:"messages"`
	AICalls  []CodedPreviewAICall  `json:"ai_calls,omitempty"`
	APICalls []CodedPreviewAPICall `json:"api_calls,omitempty"`
	Context  map[string]any        `json:"context,omitempty"`
	Step     string                `json:"step,omitempty"`
	Status   string                `json:"status,omitempty"`
}

// LoadSessionArchive reads the archive from session_data, or nil.
func LoadSessionArchive(session *models.ChatbotSession) *SessionArchive {
	if session == nil || session.SessionData == nil {
		return nil
	}
	raw, ok := session.SessionData[ArchiveKey]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var archive SessionArchive
	if err := json.Unmarshal(b, &archive); err != nil {
		return nil
	}
	if archive.Version == 0 {
		archive.Version = 1
	}
	return &archive
}

// SaveSessionArchive writes archive onto session_data (size-capped).
func SaveSessionArchive(session *models.ChatbotSession, archive *SessionArchive) {
	if session == nil || archive == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	trimArchiveToBudget(archive)
	session.SessionData[ArchiveKey] = archiveToMap(archive)
}

func archiveToMap(archive *SessionArchive) map[string]any {
	b, err := json.Marshal(archive)
	if err != nil {
		return map[string]any{"version": 1, "turns": []any{}}
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{"version": 1, "turns": []any{}}
	}
	return out
}

func trimArchiveToBudget(archive *SessionArchive) {
	if archive == nil {
		return
	}
	for len(archive.Turns) > 1 {
		b, err := json.Marshal(archive)
		if err != nil || len(b) <= archiveByteBudget {
			return
		}
		archive.Turns = archive.Turns[1:]
	}
}

// EnsureSessionArchive seeds metadata for a live session archive.
func EnsureSessionArchive(session *models.ChatbotSession, flowKey string) *SessionArchive {
	if session == nil {
		return &SessionArchive{Version: 1, Turns: []SessionArchiveTurn{}}
	}
	archive := LoadSessionArchive(session)
	if archive == nil {
		archive = &SessionArchive{
			Version: 1,
			Turns:   []SessionArchiveTurn{},
		}
	}
	archive.Version = 1
	if flowKey != "" {
		archive.FlowKey = flowKey
	} else if key := SessionKey(session); key != "" {
		archive.FlowKey = key
	}
	archive.SessionID = session.ID.String()
	archive.ContactID = session.ContactID.String()
	archive.OrganizationID = session.OrganizationID.String()
	if archive.StartedAt == "" {
		if !session.StartedAt.IsZero() {
			archive.StartedAt = session.StartedAt.UTC().Format(time.RFC3339)
		} else {
			archive.StartedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}
	archive.Status = string(session.Status)
	return archive
}

// CommitArchiveTurn appends one turn from a live archive sink.
func CommitArchiveTurn(
	session *models.ChatbotSession,
	flowKey string,
	sink *PreviewSink,
	inbound map[string]any,
) *SessionArchive {
	if session == nil || sink == nil {
		return LoadSessionArchive(session)
	}
	sink.FlushPendingAIDebug()
	archive := EnsureSessionArchive(session, flowKey)
	messages := append([]CodedPreviewMessage(nil), sink.Messages...)
	for i := range messages {
		messages[i].Context = SanitizeArchiveContext(messages[i].Context)
	}
	turn := SessionArchiveTurn{
		At:       time.Now().UTC().Format(time.RFC3339),
		Inbound:  sanitizeInbound(inbound),
		Messages: messages,
		AICalls:  append([]CodedPreviewAICall(nil), sink.AICalls...),
		APICalls: append([]CodedPreviewAPICall(nil), sink.APICalls...),
		Context:  SanitizeArchiveContext(PreviewSessionContext(session)),
		Step:     strings.TrimSpace(session.CurrentStep),
		Status:   string(session.Status),
	}
	archive.Turns = append(archive.Turns, turn)
	archive.Status = string(session.Status)
	if session.Status == models.SessionStatusCompleted ||
		session.Status == models.SessionStatusCancelled ||
		session.Status == models.SessionStatusTimeout {
		if session.CompletedAt != nil {
			archive.EndedAt = session.CompletedAt.UTC().Format(time.RFC3339)
		} else {
			archive.EndedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}
	SaveSessionArchive(session, archive)
	return archive
}

func sanitizeInbound(inbound map[string]any) map[string]any {
	if len(inbound) == 0 {
		return nil
	}
	out := make(map[string]any, len(inbound))
	for key, value := range inbound {
		if value == nil || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SanitizeArchiveContext drops oversized / private keys from session context.
func SanitizeArchiveContext(ctx map[string]any) map[string]any {
	if len(ctx) == 0 {
		return nil
	}
	out := make(map[string]any, len(ctx))
	for key, value := range ctx {
		lower := strings.ToLower(key)
		if key == ArchiveKey ||
			strings.Contains(lower, "instruction") ||
			strings.Contains(lower, "secret") ||
			strings.Contains(lower, "token") ||
			strings.Contains(lower, "api_key") ||
			strings.Contains(lower, "password") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MarkArchiveEnded sets ended_at/status on the in-session archive.
func MarkArchiveEnded(session *models.ChatbotSession) *SessionArchive {
	archive := LoadSessionArchive(session)
	if archive == nil || session == nil {
		return archive
	}
	archive.Status = string(session.Status)
	if session.CompletedAt != nil {
		archive.EndedAt = session.CompletedAt.UTC().Format(time.RFC3339)
	} else if archive.EndedAt == "" {
		archive.EndedAt = time.Now().UTC().Format(time.RFC3339)
	}
	SaveSessionArchive(session, archive)
	return archive
}

// ArchiveInboundFromChat builds the inbound map for a turn.
func ArchiveInboundFromChat(chat Chat) map[string]any {
	if chat == nil {
		return nil
	}
	inbound := map[string]any{}
	if text := strings.TrimSpace(chat.UserInput()); text != "" {
		inbound["text"] = text
	}
	if id := strings.TrimSpace(chat.ButtonID()); id != "" {
		inbound["button_id"] = id
	}
	media := chat.InboundMedia()
	if media.MediaURL != "" || media.MIMEType != "" || media.Filename != "" {
		inbound["media"] = map[string]any{
			"url":       media.MediaURL,
			"mime":      media.MIMEType,
			"filename":  media.Filename,
			"message_id": media.MessageID,
		}
	}
	if data := chat.FlowResponseData(); len(data) > 0 {
		inbound["flow_response"] = data
	}
	return inbound
}

// SessionEnded reports whether the session is in a terminal status.
func SessionEnded(session *models.ChatbotSession) bool {
	if session == nil {
		return false
	}
	switch session.Status {
	case models.SessionStatusCompleted, models.SessionStatusCancelled, models.SessionStatusTimeout:
		return true
	default:
		return false
	}
}
