package tiqrleads

import (
	"encoding/json"
	"strings"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

// HasCollectableAnswers reports whether the session has at least one customer
// answer beyond the automatic WhatsApp profile name.
func HasCollectableAnswers(session *models.ChatbotSession) bool {
	if session == nil || session.SessionData == nil {
		return false
	}
	if asString(session.SessionData[sessionLeadSaved]) == "1" {
		return false
	}
	for _, item := range answersFromSessionData(session.SessionData) {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		name := strings.TrimSpace(asString(row["criterion_name"]))
		kind := asString(row["kind"])
		if kind == answerKindField && strings.EqualFold(name, "Name") {
			continue
		}
		if strings.TrimSpace(asString(row["answer"])) != "" {
			return true
		}
	}
	return false
}

// ExpiryCreateLeadParams builds create_lead params for a timed-out partial lead.
// ok is false when there is nothing to save.
func ExpiryCreateLeadParams(session *models.ChatbotSession) (map[string]string, bool) {
	if !HasCollectableAnswers(session) {
		return nil, false
	}
	answers := answersFromSessionData(session.SessionData)
	payloadAnswers, _ := json.Marshal(answers)
	criteriaSnap, _ := json.Marshal(session.SessionData[sessionCriteria])
	name := strings.TrimSpace(asString(session.SessionData["lead_f_name"]))
	if name == "" {
		name = asString(session.SessionData["contact_name"])
	}
	evalMode := asString(session.SessionData[sessionEvalMode])
	if evalMode == "" {
		evalMode = evalRules
	}
	return map[string]string{
		"session_id":        session.ID.String(),
		"phone_number":      session.PhoneNumber,
		"name":              name,
		"evaluation_mode":   evalMode,
		"completion_action": completionSessionExpired,
		"qualified":         "false",
		"answers_json":      string(payloadAnswers),
		"criteria_snapshot": string(criteriaSnap),
	}, true
}

// MarkLeadSavedOnSession sets the once-per-session save flag after a successful post.
func MarkLeadSavedOnSession(session *models.ChatbotSession) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData[sessionLeadSaved] = "1"
	score, _ := scoreFromSessionData(session.SessionData)
	session.SessionData[sessionScore] = score
	// Expiry always stores Unclassified on the backend; keep session mirror in sync.
	session.SessionData[sessionCategory] = "unclassified"
	session.SessionData[sessionQualified] = false
}

// IsLeadsFlowSession is true when the session is running TiQR Leads.
func IsLeadsFlowSession(session *models.ChatbotSession) bool {
	return codedflow.SessionKey(session) == FlowKey
}
