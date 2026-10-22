package codedflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLocationPin_JSON(t *testing.T) {
	pin, ok := ParseLocationPin(`{"latitude":11.2,"longitude":75.8,"name":"Home"}`)
	require.True(t, ok)
	assert.InDelta(t, 11.2, pin.Latitude, 0.0001)
	assert.InDelta(t, 75.8, pin.Longitude, 0.0001)
	assert.Equal(t, "Home", pin.Name)
}

func TestParseLocationPin_MapsAtCoords(t *testing.T) {
	pin, ok := ParseLocationPin("https://www.google.com/maps/@12.9716,77.5946,17z")
	require.True(t, ok)
	assert.InDelta(t, 12.9716, pin.Latitude, 0.0001)
	assert.InDelta(t, 77.5946, pin.Longitude, 0.0001)
}

func TestParseLocationPin_MapsQueryCoords(t *testing.T) {
	pin, ok := ParseLocationPin("https://maps.google.com/?q=11.2,75.8")
	require.True(t, ok)
	assert.InDelta(t, 11.2, pin.Latitude, 0.0001)
	assert.InDelta(t, 75.8, pin.Longitude, 0.0001)
}

func TestParseLocationPin_MapsBangCoords(t *testing.T) {
	pin, ok := ParseLocationPin("https://www.google.com/maps/place/Demo/@0,0,17z/data=!3m1!4b1!4m6!3m5!1s0x0:0x0!8m2!3d12.97!4d77.59")
	require.True(t, ok)
	assert.InDelta(t, 12.97, pin.Latitude, 0.0001)
	assert.InDelta(t, 77.59, pin.Longitude, 0.0001)
}

func TestParseLocationPin_ShortMapsLinkHasNoCoords(t *testing.T) {
	_, ok := ParseLocationPin("https://maps.app.goo.gl/abc123")
	assert.False(t, ok)
	assert.True(t, LooksLikeMapsLink("https://maps.app.goo.gl/abc123"))
}

func TestLooksLikeMapsLink(t *testing.T) {
	assert.True(t, LooksLikeMapsLink("Check https://goo.gl/maps/xyz"))
	assert.False(t, LooksLikeMapsLink("please deliver to my house"))
}
