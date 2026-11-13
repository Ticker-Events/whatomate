package tiqrecommercev2

import (
	"math"
	"strconv"
	"strings"
)

const (
	zoneFree         = "free"
	zonePaid         = "paid"
	zoneOutOfRange   = "out_of_range"
	zoneUnconfigured = "unconfigured"
	earthRadiusKm    = 6373.0
)

type deliveryEligibility struct {
	Deliverable bool
	Zone        string
	DistanceKm  float64
	HasDistance bool
}

func evaluateStoreDelivery(store map[string]any, latitude, longitude float64) deliveryEligibility {
	if !storeGeoConfigured(store) {
		return deliveryEligibility{Deliverable: true, Zone: zoneUnconfigured}
	}
	storeLat, okLat := anyToFloat64(store["latitude"])
	storeLng, okLng := anyToFloat64(store["longitude"])
	if !okLat || !okLng {
		return deliveryEligibility{Deliverable: true, Zone: zoneUnconfigured}
	}
	doable, okDoable := anyToFloat64(store["delivery_radius"])
	if !okDoable || doable <= 0 {
		return deliveryEligibility{Deliverable: true, Zone: zoneUnconfigured}
	}
	free := 0.0
	if v, ok := anyToFloat64(store["free_delivery_radius"]); ok && v > 0 {
		free = v
	}
	dist := haversineKm(storeLat, storeLng, latitude, longitude)
	dist = math.Round(dist*1000) / 1000
	if dist > doable {
		return deliveryEligibility{Deliverable: false, Zone: zoneOutOfRange, DistanceKm: dist, HasDistance: true}
	}
	if dist <= free {
		return deliveryEligibility{Deliverable: true, Zone: zoneFree, DistanceKm: dist, HasDistance: true}
	}
	return deliveryEligibility{Deliverable: true, Zone: zonePaid, DistanceKm: dist, HasDistance: true}
}

func storeGeoConfigured(store map[string]any) bool {
	if store == nil {
		return false
	}
	if !storeLocationBasedDelivery(store) {
		return false
	}
	if _, ok := anyToFloat64(store["latitude"]); !ok {
		return false
	}
	if _, ok := anyToFloat64(store["longitude"]); !ok {
		return false
	}
	radius, ok := anyToFloat64(store["delivery_radius"])
	return ok && radius > 0
}

func storeLocationBasedDelivery(store map[string]any) bool {
	switch v := store["location_based_delivery"].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || v == "1"
	default:
		// When the field is absent, radii still apply if present.
		return true
	}
}

func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusKm * math.Asin(math.Min(1, math.Sqrt(a)))
}

func anyToFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
