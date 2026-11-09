package codedflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnsureConnectFormatsTopic(t *testing.T) {
	assert.Equal(t,
		"I'm not sure about what courses are available. I'm connecting you to a staff member who can help.",
		UnsureConnect("What courses are available?"),
	)
	assert.Equal(t,
		"I'm not sure about that. I'm connecting you to a staff member who can help.",
		UnsureConnect(""),
	)
	assert.Equal(t,
		"I'm not sure about that. I'm connecting you to a staff member who can help.",
		UnsureConnect("   ???  "),
	)
	assert.Equal(t,
		"I'm not sure about pg courses available. I'm connecting you to a staff member who can help.",
		UnsureConnect("  Pg   courses   available!  "),
	)
}

func TestUnsureConnectCapsTopic(t *testing.T) {
	long := strings.Repeat("a", unsureConnectMaxTopic+40)
	got := UnsureConnect(long)
	assert.Contains(t, got, "I'm not sure about ")
	assert.Contains(t, got, ". I'm connecting you to a staff member who can help.")
	topic := strings.TrimPrefix(got, "I'm not sure about ")
	topic = strings.TrimSuffix(topic, ". I'm connecting you to a staff member who can help.")
	assert.LessOrEqual(t, len([]rune(topic)), unsureConnectMaxTopic)
}

func TestWantsPerson(t *testing.T) {
	assert.True(t, WantsPerson("Can I talk to an agent?"))
	assert.True(t, WantsPerson("connect me to staff"))
	assert.True(t, WantsPerson("agent"))
	assert.False(t, WantsPerson("What courses are available?"))
	assert.False(t, WantsPerson(""))
}

func TestHandoffMessage(t *testing.T) {
	assert.Equal(t, AgentHandoff, HandoffMessage("I want to talk to a person"))
	assert.Equal(t,
		UnsureConnect("What courses are available?"),
		HandoffMessage("What courses are available?"),
	)
}
