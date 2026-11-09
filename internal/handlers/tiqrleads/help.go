package tiqrleads

import (
	"fmt"
	"strings"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
)

func bindOpeningKind(c *codedflow.Conv) {
	c.Once("leads_classify_opening", func() {
		text := asString(c.Session().SessionData[sessionOpeningText])
		if text == "" && c.ChatCtx() != nil {
			text = strings.TrimSpace(c.ChatCtx().UserInput())
			if text != "" {
				c.Session().SessionData[sessionOpeningText] = text
			}
		}
		c.Session().SessionData[sessionOpeningKind] = classifyOpeningKind(text)
	})
}

func stashDefaultNameAnswer(c *codedflow.Conv) {
	c.Once("leads_stash_name", func() {
		name := strings.TrimSpace(asString(c.Session().SessionData["contact_name"]))
		if name == "" {
			return
		}
		c.Session().SessionData["lead_f_name"] = name
		rows := answersFromSessionData(c.Session().SessionData)
		for _, item := range rows {
			row, ok := asStringMap(item)
			if !ok {
				continue
			}
			if strings.EqualFold(asString(row["criterion_name"]), "Name") && asString(row["kind"]) == answerKindField {
				return
			}
		}
		rows = append([]any{map[string]any{
			"kind":           answerKindField,
			"criterion_name": "Name",
			"question":       "Name",
			"answer":         name,
			"score":          0,
			"weight":         0,
			"position":       0,
		}}, rows...)
		for i := range rows {
			if row, ok := asStringMap(rows[i]); ok {
				row["position"] = i
				rows[i] = row
			}
		}
		c.Session().SessionData[sessionAnswers] = rows
	})
}

func welcomeWithName(c *codedflow.Conv) {
	c.Once("leads_say_welcome", func() {
		if asString(c.Session().SessionData[sessionResumePartial]) == "1" {
			c.Say(continueLeadMessage)
			return
		}
		welcome := strings.TrimSpace(asString(c.Session().SessionData[sessionWelcome]))
		name := strings.TrimSpace(asString(c.Session().SessionData["contact_name"]))
		switch {
		case name != "" && welcome != "":
			c.Say("Hi " + name + ",\n" + welcome)
		case name != "":
			c.Say("Hi " + name + ",")
		case welcome != "":
			c.Say(welcome)
		}
	})
}

func runHelpPath(c *codedflow.Conv, reentry bool) error {
	bindOpeningKind(c)
	c.Once("leads_help_mode", func() {
		if reentry {
			c.Session().SessionData[sessionHelpMode] = helpModeReentry
		} else {
			c.Session().SessionData[sessionHelpMode] = helpModePostQualify
		}
		if c.Session().SessionData[sessionHelpTurn] == nil {
			c.Session().SessionData[sessionHelpTurn] = 0
		}
	})

	kind := asString(c.Session().SessionData[sessionOpeningKind])
	if kind == openingKindQuestion {
		if err := answerOpeningQuestion(c); err != nil {
			return err
		}
		if c.Stop {
			return nil
		}
	}

	return helpFollowUpLoop(c, kind == openingKindGeneric || kind == "")
}

func answerOpeningQuestion(c *codedflow.Conv) error {
	c.Once("leads_open_classify", func() {
		text := asString(c.Session().SessionData[sessionOpeningText])
		outcome := classifyHelpQuestion(c, text)
		c.Session().SessionData[sessionOpenOutcome] = outcome.Kind
		c.Session().SessionData[sessionOpenAnswer] = outcome.Answer
		if outcome.Kind == helpOutcomeMiss || outcome.Kind == helpOutcomeStaff {
			c.Session().SessionData[sessionPendingQuestion] = text
		}
	})

	outcome := asString(c.Session().SessionData[sessionOpenOutcome])
	switch outcome {
	case helpOutcomeFAQ, helpOutcomeStoreInfo:
		answer := asString(c.Session().SessionData[sessionOpenAnswer])
		if answer != "" {
			c.Say(answer)
		}
		return nil
	case helpOutcomeRefuse:
		c.Say(codedflow.RefuseMessage)
		return nil
	case helpOutcomeStaff:
		return c.Transfer(codedflow.AgentHandoff)
	default:
		pending := asString(c.Session().SessionData[sessionPendingQuestion])
		return c.Transfer(codedflow.UnsureConnect(pending))
	}
}

func deliverHelpLine(c *codedflow.Conv, step, message string) {
	if c == nil || c.App() == nil || c.ChatCtx() == nil {
		return
	}
	text := c.Text(message)
	if err := c.App().DeliverCodedText(c.ChatCtx(), step, text); err != nil {
		c.App().LogWarn("tiqr leads help deliver failed", "error", err, "step", step)
	}
}

func helpFollowUpLoop(c *codedflow.Conv, startWithHowCanIHelp bool) error {
	for {
		if c.Stop {
			return nil
		}
		turn := asInt(c.Session().SessionData[sessionHelpTurn])
		step := fmt.Sprintf("leads_help_%d", turn)
		prompt := anythingElseMessage
		if turn == 0 && startWithHowCanIHelp {
			prompt = howCanIHelpMessage
		}

		text, ok := c.AskText(step, prompt, codedflow.StepNote{
			Doing:  "Answer follow-up questions from the store FAQ, then store information",
			Expect: "A store question, a decline, or a request for staff",
		})
		if !ok {
			return nil
		}

		actionKey := "leads_help_action_" + step
		proc := "leads_help_proc_" + step
		c.Once(proc, func() {
			if isDeclineHelp(text) {
				c.Session().SessionData[actionKey] = "decline"
				return
			}
			outcome := classifyHelpQuestion(c, text)
			c.Session().SessionData[actionKey] = outcome.Kind
			if outcome.Kind == helpOutcomeMiss || outcome.Kind == helpOutcomeStaff {
				c.Session().SessionData[sessionPendingQuestion] = text
			}
			switch outcome.Kind {
			case helpOutcomeFAQ, helpOutcomeStoreInfo:
				deliverHelpLine(c, step+"_ans", outcome.Answer)
				c.Session().SessionData[sessionHelpTurn] = turn + 1
			case helpOutcomeRefuse:
				deliverHelpLine(c, step+"_ans", codedflow.RefuseMessage)
				c.Session().SessionData[sessionHelpTurn] = turn + 1
			}
		})

		action := asString(c.Session().SessionData[actionKey])
		switch action {
		case "decline":
			return finishAfterHelp(c)
		case helpOutcomeFAQ, helpOutcomeStoreInfo, helpOutcomeRefuse:
			startWithHowCanIHelp = false
			continue
		case helpOutcomeStaff:
			return c.Transfer(codedflow.AgentHandoff)
		default:
			pending := asString(c.Session().SessionData[sessionPendingQuestion])
			return c.Transfer(codedflow.UnsureConnect(pending))
		}
	}
}

func finishAfterHelp(c *codedflow.Conv) error {
	mode := asString(c.Session().SessionData[sessionHelpMode])
	if mode == helpModeReentry {
		return c.End()
	}
	completion := asString(c.Session().SessionData[sessionCompletion])
	category := asString(c.Session().SessionData[sessionCategory])
	switch completion {
	case completionAlwaysHandoff:
		return c.Transfer(codedflow.AgentHandoff)
	case completionHandoffHigh:
		if category == "high" {
			return c.Transfer(codedflow.AgentHandoff)
		}
		return c.End()
	default:
		return c.End()
	}
}
