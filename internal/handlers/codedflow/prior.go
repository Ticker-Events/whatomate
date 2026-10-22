package codedflow

import (
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	codedPreviousStepKey = "_coded_previous_step"
	codedMenuChoicesKey  = "_coded_menu_choices"
)

// PreviousStep is the immediately previous interactive question and its choices.
type PreviousStep struct {
	Question   string
	Choices    map[string]string // id -> title
	AcceptedID string
}

// RememberMenuChoices stores the welcome-menu ids for the rest of the session
// so later steps can jump to Buy products, Check order status, or Talk to staff.
func (c *Conv) RememberMenuChoices(choices map[string]string) {
	if c == nil || c.session() == nil || len(choices) == 0 {
		return
	}
	out := make(map[string]any, len(choices))
	for id, title := range choices {
		id = strings.TrimSpace(id)
		title = strings.TrimSpace(title)
		if id == "" || title == "" {
			continue
		}
		out[id] = title
	}
	if len(out) == 0 {
		return
	}
	c.session().SessionData[codedMenuChoicesKey] = out
}

// rememberPreviousStep saves the question just answered so the next ask can
// detect a change of mind.
func (c *Conv) rememberPreviousStep(cfg map[string]any, choice Choice) {
	if c == nil || c.session() == nil || cfg == nil {
		return
	}
	id := strings.TrimSpace(choice.ID)
	if id == "" {
		return
	}
	choices := choiceMapFromConfig(c, cfg)
	if len(choices) == 0 {
		return
	}
	title := strings.TrimSpace(choice.Title)
	if title == "" {
		title = choices[id]
	}
	if title != "" {
		choices[id] = title
	}
	stored := make(map[string]any, len(choices))
	for cid, ctitle := range choices {
		stored[cid] = ctitle
	}
	c.session().SessionData[codedPreviousStepKey] = map[string]any{
		"question":    asString(cfg["body"]),
		"choices":     stored,
		"accepted_id": id,
	}
}

func choiceMapFromConfig(c *Conv, cfg map[string]any) map[string]string {
	out := map[string]string{}
	if c == nil || c.app == nil || cfg == nil {
		return out
	}
	buttons, err := c.app.ButtonsForNode(cfg, c.session().SessionData)
	if err != nil {
		return out
	}
	for _, button := range buttons {
		id := fieldString(button, "id")
		if id == "" {
			id = fieldString(button, "id_2")
		}
		if id == "" {
			continue
		}
		out[id] = choiceLabel(button)
	}
	return out
}

func previousStepFromSession(data models.JSONB) PreviousStep {
	if data == nil {
		return PreviousStep{}
	}
	raw, ok := asStringMap(data[codedPreviousStepKey])
	if !ok || raw == nil {
		return PreviousStep{}
	}
	choices := map[string]string{}
	if stored, ok := asStringMap(raw["choices"]); ok {
		for id, title := range stored {
			id = strings.TrimSpace(id)
			t := strings.TrimSpace(asString(title))
			if id == "" || t == "" {
				continue
			}
			choices[id] = t
		}
	}
	return PreviousStep{
		Question:   strings.TrimSpace(asString(raw["question"])),
		Choices:    choices,
		AcceptedID: strings.TrimSpace(asString(raw["accepted_id"])),
	}
}

func menuChoicesFromSession(data models.JSONB) map[string]string {
	if data == nil {
		return nil
	}
	raw, ok := asStringMap(data[codedMenuChoicesKey])
	if !ok || raw == nil {
		return nil
	}
	out := make(map[string]string, len(raw))
	for id, title := range raw {
		id = strings.TrimSpace(id)
		t := strings.TrimSpace(asString(title))
		if id == "" || t == "" {
			continue
		}
		out[id] = t
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func choiceTitle(choices map[string]string, id string) string {
	if choices == nil {
		return ""
	}
	return strings.TrimSpace(choices[strings.TrimSpace(id)])
}
