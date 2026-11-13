package handlers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMSG91IncomingMessageText(t *testing.T) {
	payload := MSG91WebhookPayload{
		CustomerName: "Manas",
		ContentType:  "text",
		Text:         "Hi there",
		Messages:     "",
	}
	msg, name := buildMSG91IncomingMessage(payload, "917748847990", "wamid.ABC123_hello")
	assert.Equal(t, "Manas", name)
	assert.Equal(t, "917748847990", msg.From)
	assert.Equal(t, "wamid.ABC123", msg.ID)
	require.NotNil(t, msg.Text)
	assert.Equal(t, "Hi there", msg.Text.Body)
	assert.Equal(t, "text", msg.Type)
}

func TestBuildMSG91IncomingMessageNested(t *testing.T) {
	nested, _ := json.Marshal([]map[string]any{
		{
			"from":      "917748847990",
			"id":        "wamid.NESTED",
			"timestamp": "1756115753",
			"type":      "text",
			"text":      map[string]string{"body": "Hi"},
		},
	})
	payload := MSG91WebhookPayload{Messages: string(nested), CustomerName: "Manas"}
	msg, name := buildMSG91IncomingMessage(payload, "917748847990", "")
	assert.Equal(t, "Manas", name)
	assert.Equal(t, "wamid.NESTED", msg.ID)
	require.NotNil(t, msg.Text)
	assert.Equal(t, "Hi", msg.Text.Body)
}

func TestBuildMSG91IncomingMessageButton(t *testing.T) {
	payload := MSG91WebhookPayload{
		Button: `{"payload":"Stop","text":"Stop"}`,
	}
	msg, _ := buildMSG91IncomingMessage(payload, "919999999999", "wamid.BTN")
	assert.Equal(t, "button", msg.Type)
	require.NotNil(t, msg.Button)
	assert.Equal(t, "Stop", msg.Button.Payload)
}

func TestMapMSG91EventToStatus(t *testing.T) {
	assert.Equal(t, "sent", mapMSG91EventToStatus("Sent"))
	assert.Equal(t, "delivered", mapMSG91EventToStatus("delivered"))
	assert.Equal(t, "read", mapMSG91EventToStatus("read"))
	assert.Equal(t, "failed", mapMSG91EventToStatus("Failed"))
	assert.Equal(t, "", mapMSG91EventToStatus("url_click"))
}

func TestStripMSG91UUIDSuffix(t *testing.T) {
	assert.Equal(t, "wamid.ABC", stripMSG91UUIDSuffix("wamid.ABC_hello"))
	assert.Equal(t, "req-1", stripMSG91UUIDSuffix("req-1"))
}

func TestMSG91WebhookSecretHeaderConstant(t *testing.T) {
	assert.Equal(t, "X-MSG91-Secret", msg91WebhookSecretHeader)
}
