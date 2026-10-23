package codedflow

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	// ArchiveKey holds the session-archive marker on SessionData (no turns).
	ArchiveKey = "coded_session_archive"
	// LocalArchiveTurnsKey holds turns on SessionData when Firestore is disabled
	// (local/dev debug). Production keeps turns in Firestore only.
	LocalArchiveTurnsKey = "_archive_turns"
)

// SessionArchive is the versioned log metadata. Full turns live in Firestore;
// session_data keeps only this marker (turn_count, ids, status).
type SessionArchive struct {
	Version        int                  `json:"version"`
	FlowKey        string               `json:"flow_key"`
	SessionID      string               `json:"session_id"`
	ContactID      string               `json:"contact_id"`
	OrganizationID string               `json:"organization_id"`
	StartedAt      string               `json:"started_at"`
	EndedAt        string               `json:"ended_at,omitempty"`
	Status         string               `json:"status"`
	TurnCount      int                  `json:"turn_count"`
	// Turns is legacy-only (old Postgres blobs). Not written to session_data.
	Turns []SessionArchiveTurn `json:"turns,omitempty"`
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

// CommittedArchiveTurn is one turn ready for Firestore, with its stable index.
type CommittedArchiveTurn struct {
	Index int
	Turn  SessionArchiveTurn
	Meta  *SessionArchive
}

// LoadSessionArchive reads the archive marker from session_data, or nil.
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
	if archive.TurnCount == 0 && len(archive.Turns) > 0 {
		archive.TurnCount = len(archive.Turns)
	}
	return &archive
}

// HasSessionArchive reports whether the marker (or legacy turns) indicates an archive.
func HasSessionArchive(archive *SessionArchive) bool {
	if archive == nil {
		return false
	}
	if archive.TurnCount > 0 {
		return true
	}
	return len(archive.Turns) > 0
}

// SaveSessionArchive writes the metadata marker onto session_data (no turns).
func SaveSessionArchive(session *models.ChatbotSession, archive *SessionArchive) {
	if session == nil || archive == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData[ArchiveKey] = archiveMarkerToMap(archive)
}

func archiveMarkerToMap(archive *SessionArchive) map[string]any {
	if archive == nil {
		return map[string]any{"version": 1, "turn_count": 0}
	}
	out := map[string]any{
		"version":         archive.Version,
		"flow_key":        archive.FlowKey,
		"session_id":      archive.SessionID,
		"contact_id":      archive.ContactID,
		"organization_id": archive.OrganizationID,
		"started_at":      archive.StartedAt,
		"status":          archive.Status,
		"turn_count":      archive.TurnCount,
	}
	if archive.EndedAt != "" {
		out["ended_at"] = archive.EndedAt
	}
	return out
}

// EnsureSessionArchive seeds metadata for a live session archive.
func EnsureSessionArchive(session *models.ChatbotSession, flowKey string) *SessionArchive {
	if session == nil {
		return &SessionArchive{Version: 1}
	}
	archive := LoadSessionArchive(session)
	if archive == nil {
		archive = &SessionArchive{Version: 1}
	}
	// Drop any legacy turns loaded from old blobs; Firestore holds the log.
	archive.Turns = nil
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

// CommitArchiveTurn builds one full turn, bumps the marker turn_count, and
// returns that turn with its stable index. Turns are not stored on session_data.
func CommitArchiveTurn(
	session *models.ChatbotSession,
	flowKey string,
	sink *PreviewSink,
	inbound map[string]any,
) *CommittedArchiveTurn {
	if session == nil || sink == nil {
		return nil
	}
	sink.FlushPendingAIDebug()
	archive := EnsureSessionArchive(session, flowKey)
	messages := append([]CodedPreviewMessage(nil), sink.Messages...)
	for i := range messages {
		messages[i].Context = SanitizeArchiveContext(messages[i].Context)
	}
	index := archive.TurnCount
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
	archive.TurnCount = index + 1
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
	return &CommittedArchiveTurn{Index: index, Turn: turn, Meta: archive}
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
			key == LocalArchiveTurnsKey ||
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

// MarkArchiveEnded sets ended_at/status on the in-session archive marker.
func MarkArchiveEnded(session *models.ChatbotSession) *SessionArchive {
	archive := LoadSessionArchive(session)
	if archive == nil || session == nil {
		return archive
	}
	archive.Turns = nil
	archive.Status = string(session.Status)
	if session.CompletedAt != nil {
		archive.EndedAt = session.CompletedAt.UTC().Format(time.RFC3339)
	} else if archive.EndedAt == "" {
		archive.EndedAt = time.Now().UTC().Format(time.RFC3339)
	}
	SaveSessionArchive(session, archive)
	return archive
}

// TurnToMap converts a turn to a Firestore-safe map.
func TurnToMap(turn SessionArchiveTurn) (map[string]any, error) {
	b, err := json.Marshal(turn)
	if err != nil {
		return nil, err
	}
	row := map[string]any{}
	if err := json.Unmarshal(b, &row); err != nil {
		return nil, err
	}
	return row, nil
}

// TurnFromMap converts a Firestore turn document back to SessionArchiveTurn.
// Extra keys such as "index" are ignored.
func TurnFromMap(row map[string]any) (SessionArchiveTurn, error) {
	var turn SessionArchiveTurn
	if len(row) == 0 {
		return turn, nil
	}
	b, err := json.Marshal(row)
	if err != nil {
		return turn, err
	}
	if err := json.Unmarshal(b, &turn); err != nil {
		return turn, err
	}
	return turn, nil
}

// TurnsFromMaps converts Firestore turn documents to SessionArchiveTurn values.
func TurnsFromMaps(rows []map[string]any) ([]SessionArchiveTurn, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]SessionArchiveTurn, 0, len(rows))
	for _, row := range rows {
		turn, err := TurnFromMap(row)
		if err != nil {
			return nil, err
		}
		out = append(out, turn)
	}
	return out, nil
}

// AppendLocalArchiveTurn stores a turn on SessionData for debug when Firestore is off.
func AppendLocalArchiveTurn(session *models.ChatbotSession, turn SessionArchiveTurn) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	row, err := TurnToMap(turn)
	if err != nil {
		return
	}
	existing, _ := session.SessionData[LocalArchiveTurnsKey].([]any)
	session.SessionData[LocalArchiveTurnsKey] = append(existing, row)
}

// LoadLocalArchiveTurns reads turns stored under LocalArchiveTurnsKey.
func LoadLocalArchiveTurns(session *models.ChatbotSession) []SessionArchiveTurn {
	if session == nil || session.SessionData == nil {
		return nil
	}
	raw, ok := session.SessionData[LocalArchiveTurnsKey].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	turns, err := TurnsFromMaps(rows)
	if err != nil {
		return nil
	}
	return turns
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
		url := strings.TrimSpace(media.DisplayURL)
		if url == "" {
			url = media.MediaURL
		}
		inbound["media"] = map[string]any{
			"url":        url,
			"mime":       media.MIMEType,
			"filename":   media.Filename,
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
