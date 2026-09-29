package handlers

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTruthyEnv(t *testing.T) {
	assert.True(t, parseTruthyEnv("", true))
	assert.False(t, parseTruthyEnv("", false))
	assert.False(t, parseTruthyEnv("0", true))
	assert.False(t, parseTruthyEnv("false", true))
	assert.False(t, parseTruthyEnv("OFF", true))
	assert.True(t, parseTruthyEnv("1", true))
	assert.True(t, parseTruthyEnv("true", false))
	assert.True(t, parseTruthyEnv("yes", false))
}

func TestCodedFlowTraceEnabled_EnvDefaultOn(t *testing.T) {
	t.Setenv("WHATOMATE_CODEDFLOW_TRACE", "")
	os.Unsetenv("WHATOMATE_CODEDFLOW_TRACE")
	app := &App{}
	assert.True(t, app.codedFlowTraceEnabled())
}

func TestCodedFlowTraceEnabled_EnvOff(t *testing.T) {
	t.Setenv("WHATOMATE_CODEDFLOW_TRACE", "false")
	app := &App{}
	assert.False(t, app.codedFlowTraceEnabled())
}

func TestFormatHTTPCurl(t *testing.T) {
	curl := formatHTTPCurl("GET", "https://api.example.com/service/buyer/store/42/", map[string]string{
		"Accept": "application/json",
	}, "")
	require.Contains(t, curl, "curl -sS -X GET")
	require.Contains(t, curl, "https://api.example.com/service/buyer/store/42/")
	require.Contains(t, curl, "Accept: application/json")
	require.NotContains(t, curl, "--data-raw")

	post := formatHTTPCurl("POST", "https://api.example.com/service/buyer/order/", map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}, `{"store":1}`)
	require.Contains(t, post, "--data-raw")
	require.Contains(t, post, `{"store":1}`)
}
