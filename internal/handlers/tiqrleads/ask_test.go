package tiqrleads

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLooksLikeQuestion(t *testing.T) {
	assert.True(t, looksLikeQuestion("What is your refund policy?"))
	assert.True(t, looksLikeQuestion("how much does it cost"))
	assert.True(t, looksLikeQuestion("Do you deliver?"))
	assert.False(t, looksLikeQuestion("John"))
	assert.False(t, looksLikeQuestion("yes"))
	assert.False(t, looksLikeQuestion("a@b.com"))
	assert.False(t, looksLikeQuestion("Need delivery in Kochi tomorrow"))
}

func TestShouldDivertFreeTextExactFAQWithoutQuestionMark(t *testing.T) {
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			"faqs": []any{
				map[string]any{
					"id":       "1",
					"question": "Need delivery in Kochi tomorrow",
					"answer":   "Yes, we deliver in Kochi.",
				},
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: &codedflow.PreviewSink{}})
	assert.True(t, shouldDivertFreeText(c, "Need delivery in Kochi tomorrow"))
}

func TestStepAlreadyAnswered(t *testing.T) {
	assert.False(t, stepAlreadyAnswered(nil, answerKindCriterion, "Budget"))

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindCriterion,
					"criterion_name": "Budget",
					"answer":         "Yes",
				},
				map[string]any{
					"kind":           answerKindField,
					"criterion_name": "Email",
					"answer":         "a@b.com",
				},
				map[string]any{
					"kind":           answerKindCriterion,
					"criterion_name": "Education",
					"answer":         "",
				},
			},
		},
	}
	c := codedflow.NewConv(nil, &handoffTestChat{session: session})
	assert.True(t, stepAlreadyAnswered(c, answerKindCriterion, "Budget"))
	assert.True(t, stepAlreadyAnswered(c, answerKindCriterion, "budget"))
	assert.False(t, stepAlreadyAnswered(c, answerKindCriterion, "Need"))
	assert.True(t, stepAlreadyAnswered(c, answerKindField, "Email"))
	assert.False(t, stepAlreadyAnswered(c, answerKindField, "Phone"))
	assert.False(t, stepAlreadyAnswered(c, answerKindCriterion, "Education"), "empty answers must be re-asked")
}

func TestConsumeNamedCallDoesNotStealOtherStep(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	c := codedflow.NewConv(nil, &handoffTestChat{session: session})
	c.AppendCall(map[string]any{
		"name":  "lead_q_47",
		"ok":    true,
		"route": codedflow.RouteChoice,
		"id":    "74",
		"title": "Between 16 - 25",
	})
	c.AppendCall(map[string]any{"name": "answer_lead_q_47", "ok": true})
	c.SetSeq(0)

	_, ok := consumeNamedCall(c, "lead_q_48")
	assert.False(t, ok, "Education must not consume Age's call")
	assert.Equal(t, 0, c.Seq())

	rec, ok := consumeNamedCall(c, "lead_q_47")
	require.True(t, ok)
	assert.Equal(t, "74", asString(rec["id"]))
	assert.Equal(t, 1, c.Seq())

	drainLeadStepReplay(c, "lead_q_47") // answer marker still at seq 1
	assert.Equal(t, 2, c.Seq())
}

func TestReplayLeadRouteRequiresMatchingStep(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	c := codedflow.NewConv(nil, &handoffTestChat{session: session})
	c.AppendCall(map[string]any{
		"name":  "lead_q_47",
		"ok":    true,
		"route": codedflow.RouteChoice,
		"id":    "74",
		"title": "Between 16 - 25",
	})
	c.SetSeq(0)

	route, done, ok := replayLeadRoute(c, "lead_q_48")
	assert.False(t, done)
	assert.False(t, ok)
	assert.Equal(t, "", route.ID)
	assert.Equal(t, 0, c.Seq())

	route, done, ok = replayLeadRoute(c, "lead_q_47")
	assert.True(t, done)
	assert.True(t, ok)
	assert.Equal(t, "74", route.ID)
	assert.Equal(t, "Between 16 - 25", route.Title)
}

func TestAskLeadRouteListAcceptsListTapAfterPriorStepAnswered(t *testing.T) {
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindCriterion,
					"criterion_name": "Age",
					"answer":         "Between 16 - 25",
					"score":          100,
					"weight":         30,
				},
			},
		},
	}
	chat := &leadAskTestChat{
		session:  session,
		buttonID: "81",
		input:    "Post-graduate",
	}
	c := codedflow.NewConv(nil, chat)
	c.AppendCall(map[string]any{
		"name":  "lead_q_47",
		"ok":    true,
		"route": codedflow.RouteChoice,
		"id":    "74",
		"title": "Between 16 - 25",
	})
	c.AppendCall(map[string]any{"name": "answer_lead_q_47", "ok": true})
	c.SetSeq(0)

	drainLeadStepReplay(c, "lead_q_47")

	items := []any{
		map[string]any{"id": "78", "title": "10th Pass"},
		map[string]any{"id": "81", "title": "Post-graduate"},
	}
	route, ok := askLeadRouteList(c, "lead_q_48", items, codedflow.ListPrompt{
		Body:     "What is your highest educational qualification?",
		ItemsKey: "lead_q_48_items",
		IDField:  "id",
		Title:    "title",
		Step: codedflow.StepNote{
			Doing:  "Qualify the lead on Education",
			Expect: "One of the offered choices",
		},
	})
	require.True(t, ok)
	assert.Equal(t, "81", route.ID)
	assert.Equal(t, "Post-graduate", route.Title)
	assert.True(t, chat.consumed)

	stashCriterionAnswer(c, "lead_q_48", "Education", "What is your highest educational qualification?", route.Title, 75, 35)
	assert.True(t, stepAlreadyAnswered(c, answerKindCriterion, "Education"))
}

func TestAskLeadTextDoesNotTreatEmailAsHelp(t *testing.T) {
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindCriterion,
					"criterion_name": "Age",
					"answer":         "Between 16 - 25",
				},
				map[string]any{
					"kind":           answerKindField,
					"criterion_name": "Please share your email id for further communications.",
					"answer":         "",
				},
			},
		},
	}
	chat := &leadAskTestChat{
		session: session,
		input:   "aswindivakar100@gmail.com",
	}
	c := codedflow.NewConv(nil, chat)
	// Poisoned leftover from a skipped Age step — must not be consumed as email.
	c.AppendCall(map[string]any{
		"name":  "lead_q_47",
		"ok":    true,
		"id":    "74",
		"title": "Between 16 - 25",
	})
	c.SetSeq(0)

	text, ok := askLeadText(c, "lead_f_email", "Please share your email id for further communications.", codedflow.StepNote{
		Doing:  "Collect lead field email",
		Expect: "A free-text reply",
	})
	require.True(t, ok)
	assert.Equal(t, "aswindivakar100@gmail.com", text)
	assert.True(t, chat.consumed)
	assert.Equal(t, "aswindivakar100@gmail.com", asString(session.SessionData["lead_f_email"]))
	require.GreaterOrEqual(t, len(c.CallRecords()), 2)
	assert.Equal(t, "lead_q_47", asString(c.CallRecords()[0]["name"]), "Age call must remain unconsumed")
	assert.Equal(t, "lead_f_email", asString(c.CallRecords()[len(c.CallRecords())-1]["name"]))

	stashFieldAnswer(c, "lead_f_email", "Please share your email id for further communications.", "Please share your email id for further communications.", text)
	assert.True(t, stepAlreadyAnswered(c, answerKindField, "Please share your email id for further communications."))
}

type leadAskTestChat struct {
	session  *models.ChatbotSession
	buttonID string
	input    string
	consumed bool
}

func (c *leadAskTestChat) Session() *models.ChatbotSession          { return c.session }
func (c *leadAskTestChat) Account() *models.WhatsAppAccount        { return nil }
func (c *leadAskTestChat) Contact() *models.Contact                { return nil }
func (c *leadAskTestChat) UserInput() string                       { return c.input }
func (c *leadAskTestChat) SetUserInput(v string)                   { c.input = v }
func (c *leadAskTestChat) InboundMedia() codedflow.InboundMedia    { return codedflow.InboundMedia{} }
func (c *leadAskTestChat) ButtonID() string                        { return c.buttonID }
func (c *leadAskTestChat) SetButtonID(v string)                    { c.buttonID = v }
func (c *leadAskTestChat) FlowResponseData() map[string]any        { return nil }
func (c *leadAskTestChat) SetFlowResponseData(map[string]any)      {}
func (c *leadAskTestChat) QuotedMessage() *codedflow.QuotedMessage { return nil }
func (c *leadAskTestChat) Consumed() bool                          { return c.consumed }
func (c *leadAskTestChat) SetConsumed(v bool)                      { c.consumed = v }
func (c *leadAskTestChat) Capturing() bool                         { return true }
func (c *leadAskTestChat) LastTiqr() map[string]any                { return nil }
func (c *leadAskTestChat) SetLastTiqr(map[string]any)              {}
func (c *leadAskTestChat) LastTiqrErr() string                     { return "" }
func (c *leadAskTestChat) SetLastTiqrErr(string)                   {}
func (c *leadAskTestChat) LastTiqrStatus() int                     { return 0 }
func (c *leadAskTestChat) SetLastTiqrStatus(int)                   {}
func (c *leadAskTestChat) Preview() *codedflow.PreviewSink         { return nil }
func (c *leadAskTestChat) SetPreview(*codedflow.PreviewSink)       {}
func (c *leadAskTestChat) Archive() *codedflow.PreviewSink         { return nil }
func (c *leadAskTestChat) SetArchive(*codedflow.PreviewSink)       {}
func (c *leadAskTestChat) Native() any                             { return nil }

func TestHasCollectableAnswersIgnoresProfileName(t *testing.T) {
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindField,
					"criterion_name": "Name",
					"answer":         "Asha",
				},
			},
		},
	}
	assert.False(t, HasCollectableAnswers(session))

	session.SessionData[sessionAnswers] = []any{
		map[string]any{
			"kind":           answerKindField,
			"criterion_name": "Name",
			"answer":         "Asha",
		},
		map[string]any{
			"kind":           answerKindCriterion,
			"criterion_name": "Budget",
			"answer":         "Yes",
			"score":          100,
			"weight":         100,
		},
	}
	assert.True(t, HasCollectableAnswers(session))
}

func TestExpiryCreateLeadParams(t *testing.T) {
	session := &models.ChatbotSession{
		BaseModel:   models.BaseModel{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		PhoneNumber: "919999999999",
		SessionData: models.JSONB{
			"contact_name":  "Asha",
			sessionEvalMode: evalRules,
			sessionCriteria: map[string]any{"thank_you_text": "Thanks"},
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindCriterion,
					"criterion_name": "Budget",
					"answer":         "Yes",
					"score":          100,
					"weight":         50,
				},
			},
		},
	}
	params, ok := ExpiryCreateLeadParams(session)
	require.True(t, ok)
	assert.Equal(t, "false", params["qualified"])
	assert.Equal(t, completionSessionExpired, params["completion_action"])
	assert.Contains(t, params["answers_json"], "Budget")
	assert.Equal(t, "919999999999", params["phone_number"])
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", params["session_id"])
}

func TestSeedPartialLeadAnswers(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	c := codedflow.NewConv(nil, &handoffTestChat{session: session})
	seedPartialLeadAnswers(c, map[string]any{
		"id":       9,
		"name":     "Ravi",
		"score":    40,
		"category": "unclassified",
		"answers": []any{
			map[string]any{
				"kind":           answerKindCriterion,
				"criterion_name": "Budget",
				"answer":         "Yes",
			},
		},
	})
	assert.Equal(t, "1", asString(session.SessionData[sessionResumePartial]))
	assert.Equal(t, "Ravi", asString(session.SessionData["lead_f_name"]))
	assert.True(t, stepAlreadyAnswered(c, answerKindCriterion, "Budget"))
}
