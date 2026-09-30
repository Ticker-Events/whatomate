package handlers

import (
	"math"
	"strings"
)

const (
	deliveryZoneFree         = "free"
	deliveryZonePaid         = "paid"
	deliveryZoneOutOfRange   = "out_of_range"
	deliveryZoneUnconfigured = "unconfigured"

	// earthRadiusKm matches ticker_events_backend.utils.location.distance.
	earthRadiusKm = 6373.0
)

// storeDeliveryEligibility is the local free / paid / out_of_range result
// computed from store details — no MCP call.
type storeDeliveryEligibility struct {
	Deliverable      bool
	Zone             string
	DistanceKm       float64
	HasDistance      bool
	ShippingFeePaise int64
}

// evaluateStoreDelivery mirrors service.utils.delivery_eligibility.evaluate_delivery
// using fields already present on the get_store payload. Flat StoreShippingFee is
// not available from store details, so paid-zone fee is left at 0.
func evaluateStoreDelivery(store map[string]any, latitude, longitude float64) storeDeliveryEligibility {
	if !storeGeoConfigured(store) {
		return storeDeliveryEligibility{
			Deliverable: true,
			Zone:        deliveryZoneUnconfigured,
		}
	}
	storeLat, okLat := anyToFloat64(store["latitude"])
	storeLng, okLng := anyToFloat64(store["longitude"])
	if !okLat || !okLng {
		return storeDeliveryEligibility{
			Deliverable: true,
			Zone:        deliveryZoneUnconfigured,
		}
	}
	doable, okDoable := anyToFloat64(store["delivery_radius"])
	if !okDoable || doable <= 0 {
		return storeDeliveryEligibility{
			Deliverable: true,
			Zone:        deliveryZoneUnconfigured,
		}
	}
	free := 0.0
	if v, ok := anyToFloat64(store["free_delivery_radius"]); ok && v > 0 {
		free = v
	}

	dist := haversineKm(storeLat, storeLng, latitude, longitude)
	dist = math.Round(dist*1000) / 1000

	if dist > doable {
		return storeDeliveryEligibility{
			Deliverable: false,
			Zone:        deliveryZoneOutOfRange,
			DistanceKm:  dist,
			HasDistance: true,
		}
	}
	if dist <= free {
		return storeDeliveryEligibility{
			Deliverable: true,
			Zone:        deliveryZoneFree,
			DistanceKm:  dist,
			HasDistance: true,
		}
	}
	return storeDeliveryEligibility{
		Deliverable: true,
		Zone:        deliveryZonePaid,
		DistanceKm:  dist,
		HasDistance: true,
	}
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
	case float64:
		return v != 0
	case string:
		s := strings.TrimSpace(v)
		return strings.EqualFold(s, "true") || s == "1"
	default:
		return false
	}
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	λ1 := lon1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	λ2 := lon2 * math.Pi / 180
	dφ := φ2 - φ1
	dλ := λ2 - λ1
	a := math.Sin(dφ/2)*math.Sin(dφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(dλ/2)*math.Sin(dλ/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}
