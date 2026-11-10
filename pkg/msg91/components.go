package msg91

import (
	"encoding/json"
	"fmt"
	"strings"
)

// mapTemplateComponents converts Meta-style template components
// (header/body/button parameter arrays) into MSG91's named component map:
// header_1, body_1, body_2, button_1, ...
func mapTemplateComponents(components []map[string]any) map[string]any {
	out := map[string]any{}
	if len(components) == 0 {
		return out
	}

	bodyIdx := 1
	buttonIdx := 1
	headerIdx := 1

	for _, comp := range components {
		typ, _ := comp["type"].(string)
		switch strings.ToLower(typ) {
		case "header":
			params, _ := comp["parameters"].([]any)
			if len(params) == 0 {
				continue
			}
			for _, p := range params {
				pm, ok := p.(map[string]any)
				if !ok {
					continue
				}
				key := fmt.Sprintf("header_%d", headerIdx)
				headerIdx++
				out[key] = mapHeaderParam(pm)
			}
		case "body":
			params, _ := comp["parameters"].([]any)
			for _, p := range params {
				pm, ok := p.(map[string]any)
				if !ok {
					continue
				}
				key := fmt.Sprintf("body_%d", bodyIdx)
				bodyIdx++
				out[key] = map[string]any{
					"type":  "text",
					"value": paramTextValue(pm),
				}
			}
		case "button":
			params, _ := comp["parameters"].([]any)
			subType, _ := comp["sub_type"].(string)
			for _, p := range params {
				pm, ok := p.(map[string]any)
				if !ok {
					continue
				}
				key := fmt.Sprintf("button_%d", buttonIdx)
				buttonIdx++
				entry := map[string]any{
					"type":  "text",
					"value": paramTextValue(pm),
				}
				if subType != "" {
					entry["subtype"] = strings.ToLower(subType)
				}
				out[key] = entry
			}
		}
	}
	return out
}

func mapHeaderParam(pm map[string]any) map[string]any {
	ptype, _ := pm["type"].(string)
	ptype = strings.ToLower(ptype)
	switch ptype {
	case "image":
		return map[string]any{"type": "image", "value": mediaLinkFromParam(pm, "image")}
	case "video":
		return map[string]any{"type": "video", "value": mediaLinkFromParam(pm, "video")}
	case "document":
		return map[string]any{"type": "document", "value": mediaLinkFromParam(pm, "document")}
	default:
		return map[string]any{"type": "text", "value": paramTextValue(pm)}
	}
}

func mediaLinkFromParam(pm map[string]any, key string) string {
	if m, ok := pm[key].(map[string]any); ok {
		if link, _ := m["link"].(string); link != "" {
			return link
		}
		if id, _ := m["id"].(string); id != "" {
			return id
		}
	}
	return paramTextValue(pm)
}

func paramTextValue(pm map[string]any) string {
	if t, ok := pm["text"].(string); ok {
		return t
	}
	if t, ok := pm["payload"].(string); ok {
		return t
	}
	if t, ok := pm["value"].(string); ok {
		return t
	}
	return ""
}

// parseSendID extracts a usable outbound message identifier from an MSG91 response.
// Prefers request_id (MSG91 async id); also accepts wamid / messages[0].id.
func parseSendID(body []byte) (string, error) {
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse msg91 send response: %w", err)
	}

	if id := findString(resp, "request_id", "requestId"); id != "" {
		return id, nil
	}
	if data, ok := resp["data"].(map[string]any); ok {
		if id := findString(data, "request_id", "requestId", "message_id", "id"); id != "" {
			return id, nil
		}
	}
	if msgs, ok := resp["messages"].([]any); ok && len(msgs) > 0 {
		if m, ok := msgs[0].(map[string]any); ok {
			if id := findString(m, "id", "message_id"); id != "" {
				return id, nil
			}
		}
	}
	if id := findString(resp, "message_id", "uuid", "id"); id != "" {
		return id, nil
	}
	if s, _ := resp["data"].(string); strings.TrimSpace(s) != "" {
		return "", fmt.Errorf("msg91 accepted request but returned no message id: %s", s)
	}
	return "", fmt.Errorf("no message id in msg91 response")
}

func findString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return t
				}
			case float64:
				return fmt.Sprintf("%.0f", t)
			}
		}
	}
	return ""
}
