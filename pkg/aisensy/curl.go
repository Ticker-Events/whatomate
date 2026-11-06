package aisensy

import (
	"fmt"
	"strings"
)

const curlBodyLogLimit = 8 * 1024

// formatCurl builds a shell-reproducible curl for an AiSensy API call so the
// request can be forwarded to AiSensy support when debugging failures.
// Multipart bodies are omitted (binary); JSON/text bodies are truncated at ~8KB.
func formatCurl(method, url, authToken, contentType string, body []byte) string {
	var b strings.Builder
	b.WriteString("curl --request ")
	b.WriteString(shellQuote(method))
	b.WriteString(" --url ")
	b.WriteString(shellQuote(url))
	if authToken != "" {
		b.WriteString(" --header ")
		b.WriteString(shellQuote("Authorization: Bearer " + authToken))
	}
	if contentType != "" {
		b.WriteString(" --header ")
		b.WriteString(shellQuote("Content-Type: " + contentType))
	}
	if len(body) == 0 {
		return b.String()
	}
	if isMultipartContentType(contentType) {
		b.WriteString(" --data ")
		b.WriteString(shellQuote("<multipart body omitted>"))
		return b.String()
	}
	payload := string(body)
	if len(payload) > curlBodyLogLimit {
		payload = payload[:curlBodyLogLimit] + "...[truncated]"
	}
	b.WriteString(" --data ")
	b.WriteString(shellQuote(payload))
	return b.String()
}

func isMultipartContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func truncateForLog(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if len(body) <= curlBodyLogLimit {
		return string(body)
	}
	return string(body[:curlBodyLogLimit]) + "...[truncated]"
}

func (c *Client) logAPIFailure(method, url, authToken, contentType string, body []byte, statusCode int, respBody []byte, err error) {
	if c == nil {
		return
	}
	args := []any{
		"curl", formatCurl(method, url, authToken, contentType, body),
		"status", statusCode,
		"response", truncateForLog(respBody),
	}
	if err != nil {
		args = append(args, "error", err)
	}
	c.Log.Error(fmt.Sprintf("aisensy API request failed (%s %s)", method, url), args...)
}
