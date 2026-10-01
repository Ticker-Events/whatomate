package tiqrecommerce

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHaversineKm_SamePointIsZero(t *testing.T) {
	assert.InDelta(t, 0.0, haversineKm(12.97, 77.59, 12.97, 77.59), 1e-5)
}

func TestHaversineKm_KnownShortDistance(t *testing.T) {
	// ~1.1 km between nearby Bengaluru points (matches backend test).
	d := haversineKm(12.9716, 77.5946, 12.9816, 77.5946)
	assert.Greater(t, d, 1.0)
	assert.Less(t, d, 1.3)
}

func TestEvaluateStoreDelivery_Zones(t *testing.T) {
	store := map[string]any{
		"latitude":                "12.971600",
		"longitude":               "77.594600",
		"delivery_radius":         10,
		"free_delivery_radius":    3,
		"location_based_delivery": true,
	}

	free := evaluateStoreDelivery(store, 12.9816, 77.5946)
	assert.True(t, free.Deliverable)
	assert.Equal(t, deliveryZoneFree, free.Zone)
	assert.Equal(t, int64(0), free.ShippingFeePaise)

	paid := evaluateStoreDelivery(store, 13.0216, 77.5946)
	assert.True(t, paid.Deliverable)
	assert.Equal(t, deliveryZonePaid, paid.Zone)

	out := evaluateStoreDelivery(store, 13.1716, 77.5946)
	assert.False(t, out.Deliverable)
	assert.Equal(t, deliveryZoneOutOfRange, out.Zone)
}

func TestEvaluateStoreDelivery_UnconfiguredAllows(t *testing.T) {
	store := map[string]any{
		"latitude":                "12.971600",
		"longitude":               "77.594600",
		"delivery_radius":         10,
		"location_based_delivery": false,
	}
	result := evaluateStoreDelivery(store, 13.1716, 77.5946)
	assert.True(t, result.Deliverable)
	assert.Equal(t, deliveryZoneUnconfigured, result.Zone)
}

func TestEvaluateStoreDelivery_NullFreeRadiusIsPaidInsideDoable(t *testing.T) {
	store := map[string]any{
		"latitude":                12.9716,
		"longitude":               77.5946,
		"delivery_radius":         10,
		"location_based_delivery": true,
	}
	result := evaluateStoreDelivery(store, 12.9816, 77.5946)
	assert.True(t, result.Deliverable)
	assert.Equal(t, deliveryZonePaid, result.Zone)
}