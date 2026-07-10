package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

// generateAIResponseWithCommerce runs a tool-calling loop when commerce is configured;
// otherwise falls through to the existing single-shot providers.
func (a *App) generateAIResponse(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage string) (string, error) {
	contextData := a.buildAIContext(settings.OrganizationID, session, userMessage)
	rt := a.newCommerceRuntime(settings, session)

	if rt != nil {
		switch settings.AI.Provider {
		case models.AIProviderOpenAI:
			return a.generateOpenAIWithTools(settings, session, userMessage, contextData, rt)
		case models.AIProviderAnthropic:
			return a.generateAnthropicWithTools(settings, session, userMessage, contextData, rt)
		case models.AIProviderGoogle:
			return a.generateGoogleWithTools(settings, session, userMessage, contextData, rt)
		}
		a.Log.Warn("commerce enabled but provider unsupported for tools; falling back to text-only",
			"provider", settings.AI.Provider)
	} else if settings.AI.CommerceEnabled {
		a.Log.Warn("AI commerce enabled but base URL or store ID missing; using text-only AI")
	}

	switch settings.AI.Provider {
	case models.AIProviderOpenAI:
		return a.generateOpenAIResponse(settings, session, userMessage, contextData)
	case models.AIProviderAnthropic:
		return a.generateAnthropicResponse(settings, session, userMessage, contextData)
	case models.AIProviderGoogle:
		return a.generateGoogleResponse(settings, session, userMessage, contextData)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", settings.AI.Provider)
	}
}

func (a *App) generateOpenAIWithTools(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string, rt *commerceRuntime) (string, error) {
	messages := make([]map[string]any, 0, 16)
	systemPrompt := buildCommerceSystemPrompt(settings.AI.SystemPrompt, contextData)
	if systemPrompt != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})
	}
	if settings.AI.IncludeHistory && session != nil {
		for _, msg := range a.getSessionHistoryForAI(session.ID, effectiveAIHistoryLimit(settings), userMessage) {
			role := "user"
			if msg.Direction == models.DirectionOutgoing {
				role = "assistant"
			}
			messages = append(messages, map[string]any{"role": role, "content": msg.Message})
		}
	}
	messages = append(messages, map[string]any{"role": "user", "content": userMessage})

	maxTokens := settings.AI.MaxTokens
	if maxTokens < 500 {
		maxTokens = 500
	}

	for round := 0; round < commerceMaxToolRounds; round++ {
		payload := map[string]any{
			"model":      settings.AI.Model,
			"messages":   messages,
			"max_tokens": maxTokens,
			"tools":      commerceToolDefs(),
		}
		if settings.AI.Temperature > 0 {
			payload["temperature"] = settings.AI.Temperature
		}

		body, err := a.doOpenAIRequest(settings.AI.APIKey, payload)
		if err != nil {
			return "", err
		}

		var result struct {
			Choices []struct {
				Message struct {
					Role      string `json:"role"`
					Content   string `json:"content"`
					ToolCalls []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("failed to parse OpenAI response: %w", err)
		}
		if len(result.Choices) == 0 {
			return "", fmt.Errorf("no response from OpenAI")
		}

		msg := result.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			return strings.TrimSpace(msg.Content), nil
		}

		// Append assistant tool-call message, then tool results.
		var content any
		if strings.TrimSpace(msg.Content) != "" {
			content = msg.Content
		}
		assistantMsg := map[string]any{
			"role":       "assistant",
			"content":    content,
			"tool_calls": msg.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		for _, tc := range msg.ToolCalls {
			toolOut := a.executeCommerceTool(rt, tc.Function.Name, tc.Function.Arguments)
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      toolOut,
			})
		}
	}

	return "", fmt.Errorf("commerce tool loop exceeded max rounds")
}

func (a *App) doOpenAIRequest(apiKey string, payload map[string]any) ([]byte, error) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &errResp)
		msg := errResp.Error.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("OpenAI API error: %s", msg)
	}
	return body, nil
}

func (a *App) generateAnthropicWithTools(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string, rt *commerceRuntime) (string, error) {
	messages := make([]map[string]any, 0, 16)
	if settings.AI.IncludeHistory && session != nil {
		for _, msg := range a.getSessionHistoryForAI(session.ID, effectiveAIHistoryLimit(settings), userMessage) {
			role := "user"
			if msg.Direction == models.DirectionOutgoing {
				role = "assistant"
			}
			messages = append(messages, map[string]any{
				"role":    role,
				"content": msg.Message,
			})
		}
	}
	messages = append(messages, map[string]any{"role": "user", "content": userMessage})

	systemPrompt := buildCommerceSystemPrompt(settings.AI.SystemPrompt, contextData)
	maxTokens := settings.AI.MaxTokens
	if maxTokens < 500 {
		maxTokens = 500
	}

	for round := 0; round < commerceMaxToolRounds; round++ {
		payload := map[string]any{
			"model":      settings.AI.Model,
			"messages":   messages,
			"max_tokens": maxTokens,
			"tools":      anthropicToolDefs(),
		}
		if systemPrompt != "" {
			payload["system"] = systemPrompt
		}
		if settings.AI.Temperature > 0 {
			payload["temperature"] = settings.AI.Temperature
		}

		body, err := a.doAnthropicRequest(settings.AI.APIKey, payload)
		if err != nil {
			return "", err
		}

		var result struct {
			Content []struct {
				Type  string         `json:"type"`
				Text  string         `json:"text,omitempty"`
				ID    string         `json:"id,omitempty"`
				Name  string         `json:"name,omitempty"`
				Input map[string]any `json:"input,omitempty"`
			} `json:"content"`
			StopReason string `json:"stop_reason"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("failed to parse Anthropic response: %w", err)
		}

		var textParts []string
		var toolUses []map[string]any
		for _, block := range result.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					textParts = append(textParts, block.Text)
				}
			case "tool_use":
				toolUses = append(toolUses, map[string]any{
					"type":  "tool_use",
					"id":    block.ID,
					"name":  block.Name,
					"input": block.Input,
				})
			}
		}

		if len(toolUses) == 0 {
			return strings.TrimSpace(strings.Join(textParts, "\n")), nil
		}

		// Assistant content must include the tool_use blocks.
		assistantContent := make([]any, 0, len(result.Content))
		for _, block := range result.Content {
			switch block.Type {
			case "text":
				assistantContent = append(assistantContent, map[string]any{"type": "text", "text": block.Text})
			case "tool_use":
				assistantContent = append(assistantContent, map[string]any{
					"type":  "tool_use",
					"id":    block.ID,
					"name":  block.Name,
					"input": block.Input,
				})
			}
		}
		messages = append(messages, map[string]any{"role": "assistant", "content": assistantContent})

		toolResults := make([]any, 0, len(toolUses))
		for _, tu := range toolUses {
			name, _ := tu["name"].(string)
			id, _ := tu["id"].(string)
			input, _ := tu["input"].(map[string]any)
			argsBytes, _ := json.Marshal(input)
			toolOut := a.executeCommerceTool(rt, name, string(argsBytes))
			toolResults = append(toolResults, map[string]any{
				"type":        "tool_result",
				"tool_use_id": id,
				"content":     toolOut,
			})
		}
		messages = append(messages, map[string]any{"role": "user", "content": toolResults})
	}

	return "", fmt.Errorf("commerce tool loop exceeded max rounds")
}

func (a *App) doAnthropicRequest(apiKey string, payload map[string]any) ([]byte, error) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &errResp)
		msg := errResp.Error.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("anthropic API error: %s", msg)
	}
	return body, nil
}

func (a *App) generateGoogleWithTools(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string, rt *commerceRuntime) (string, error) {
	contents := make([]map[string]any, 0, 16)
	if settings.AI.IncludeHistory && session != nil {
		for _, msg := range a.getSessionHistoryForAI(session.ID, effectiveAIHistoryLimit(settings), userMessage) {
			role := "user"
			if msg.Direction == models.DirectionOutgoing {
				role = "model"
			}
			contents = appendGeminiTurn(contents, role, msg.Message)
		}
	}
	contents = appendGeminiTurn(contents, "user", userMessage)

	systemPrompt := buildCommerceSystemPrompt(settings.AI.SystemPrompt, contextData)
	maxTokens := settings.AI.MaxTokens
	if maxTokens < 500 {
		maxTokens = 500
	}

	forceCatalogTools := false
	for round := 0; round < commerceMaxToolRounds; round++ {
		fcMode := map[string]any{"mode": "AUTO"}
		if forceCatalogTools {
			// Gemini often answers catalog questions in plain text; force a tool call once.
			fcMode = map[string]any{
				"mode":                 "ANY",
				"allowedFunctionNames": []string{"search_products", "get_product", "get_order_status"},
			}
		}
		payload := map[string]any{
			"contents": contents,
			"tools":    googleToolDefs(),
			"toolConfig": map[string]any{
				"functionCallingConfig": fcMode,
			},
			"generationConfig": map[string]any{
				"maxOutputTokens": maxTokens,
			},
		}
		if systemPrompt != "" {
			payload["systemInstruction"] = map[string]any{
				"parts": []map[string]any{{"text": systemPrompt}},
			}
		}
		if settings.AI.Temperature > 0 {
			payload["generationConfig"].(map[string]any)["temperature"] = settings.AI.Temperature
		}

		body, err := a.doGoogleRequest(settings.AI.Model, settings.AI.APIKey, payload)
		if err != nil {
			return "", err
		}

		var result struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text         string `json:"text,omitempty"`
						FunctionCall *struct {
							Name string         `json:"name"`
							Args map[string]any `json:"args"`
						} `json:"functionCall,omitempty"`
					} `json:"parts"`
					Role string `json:"role"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("failed to parse Google response: %w", err)
		}
		if len(result.Candidates) == 0 {
			return "", fmt.Errorf("no response from Google AI")
		}

		parts := result.Candidates[0].Content.Parts
		var textParts []string
		var functionCalls []map[string]any
		modelParts := make([]map[string]any, 0, len(parts))

		for _, p := range parts {
			if p.FunctionCall != nil {
				fc := map[string]any{
					"functionCall": map[string]any{
						"name": p.FunctionCall.Name,
						"args": p.FunctionCall.Args,
					},
				}
				functionCalls = append(functionCalls, fc)
				modelParts = append(modelParts, fc)
			} else if p.Text != "" {
				textParts = append(textParts, p.Text)
				modelParts = append(modelParts, map[string]any{"text": p.Text})
			}
		}

		if len(functionCalls) == 0 {
			text := strings.TrimSpace(strings.Join(textParts, "\n"))
			if !forceCatalogTools && round == 0 && looksLikeCommerceCatalogQuery(userMessage) {
				a.Log.Info("commerce google skipped tools on catalog query; retrying with forced tool mode",
					"response_preview", truncateForLog(text, 120),
				)
				forceCatalogTools = true
				continue
			}
			a.Log.Info("commerce google round returned text without tool calls",
				"round", round,
				"response_length", len(text),
			)
			return text, nil
		}
		forceCatalogTools = false

		contents = append(contents, map[string]any{
			"role":  "model",
			"parts": modelParts,
		})

		responseParts := make([]map[string]any, 0, len(functionCalls))
		for _, fcWrap := range functionCalls {
			fc, _ := fcWrap["functionCall"].(map[string]any)
			name, _ := fc["name"].(string)
			args, _ := fc["args"].(map[string]any)
			argsBytes, _ := json.Marshal(args)
			toolOut := a.executeCommerceTool(rt, name, string(argsBytes))
			responseParts = append(responseParts, map[string]any{
				"functionResponse": map[string]any{
					"name":     name,
					"response": googleFunctionResponse(toolOut),
				},
			})
		}
		contents = append(contents, map[string]any{
			"role":  "user",
			"parts": responseParts,
		})
	}

	return "", fmt.Errorf("commerce tool loop exceeded max rounds")
}

// googleFunctionResponse ensures Gemini functionResponse.response is a JSON object.
// Arrays/primitives are wrapped under "result" (Gemini rejects non-object responses).
func googleFunctionResponse(toolOut string) map[string]any {
	var parsed any
	if err := json.Unmarshal([]byte(toolOut), &parsed); err != nil {
		return map[string]any{"error": toolOut}
	}
	if m, ok := parsed.(map[string]any); ok {
		return m
	}
	return map[string]any{"result": parsed}
}

func looksLikeCommerceCatalogQuery(msg string) bool {
	m := strings.ToLower(strings.TrimSpace(msg))
	if m == "" {
		return false
	}
	keywords := []string{
		"product", "products", "catalog", "price", "prices", "stock",
		"buy", "order", "sell", "selling", "available", "availability",
		"option", "sku", "what do you have", "what all", "menu", "item", "items",
	}
	for _, k := range keywords {
		if strings.Contains(m, k) {
			return true
		}
	}
	return false
}

func truncateForLog(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (a *App) doGoogleRequest(model, apiKey string, payload map[string]any) ([]byte, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &errResp)
		msg := errResp.Error.Message
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("google AI API error: %s", msg)
	}
	return body, nil
}
