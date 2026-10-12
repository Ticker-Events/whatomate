package geocode_test

import (
	"testing"

	"github.com/shridarpatil/whatomate/pkg/geocode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGoogleResponse_AddressParts(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"status": "OK",
		"results": [{
			"address_components": [
				{"long_name": "560001", "short_name": "560001", "types": ["postal_code"]},
				{"long_name": "Bengaluru", "short_name": "BLR", "types": ["locality", "political"]},
				{"long_name": "Karnataka", "short_name": "KA", "types": ["administrative_area_level_1", "political"]},
				{"long_name": "India", "short_name": "IN", "types": ["country", "political"]}
			]
		}]
	}`)
	addr, ok := geocode.ParseGoogleResponse(body)
	require.True(t, ok)
	assert.Equal(t, "560001", addr.Pincode)
	assert.Equal(t, "Bengaluru", addr.City)
	assert.Equal(t, "Karnataka", addr.State)
	assert.Equal(t, "India", addr.Country)
	assert.Equal(t, map[string]string{
		"pincode": "560001",
		"city":    "Bengaluru",
		"state":   "Karnataka",
		"country": "India",
	}, addr.Map())
}

func TestParseGoogleResponse_CityFallbackPostalTown(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"status": "OK",
		"results": [{
			"address_components": [
				{"long_name": "682042", "short_name": "682042", "types": ["postal_code"]},
				{"long_name": "Ernakulam", "short_name": "Ernakulam", "types": ["postal_town"]},
				{"long_name": "Kerala", "short_name": "KL", "types": ["administrative_area_level_1"]},
				{"long_name": "India", "short_name": "IN", "types": ["country"]}
			]
		}]
	}`)
	addr, ok := geocode.ParseGoogleResponse(body)
	require.True(t, ok)
	assert.Equal(t, "Ernakulam", addr.City)
}

func TestParseGoogleResponse_Failure(t *testing.T) {
	t.Parallel()
	_, ok := geocode.ParseGoogleResponse([]byte(`{"status":"ZERO_RESULTS","results":[]}`))
	assert.False(t, ok)
	_, ok = geocode.ParseGoogleResponse([]byte(`not-json`))
	assert.False(t, ok)
	_, ok = geocode.ParseGoogleResponse([]byte(`{"status":"OK","results":[{"address_components":[]}]}`))
	assert.False(t, ok)
}
