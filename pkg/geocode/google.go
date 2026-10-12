// Package geocode reverse-geocodes lat/lng via Google Geocoding API.
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const googleGeocodeURL = "https://maps.googleapis.com/maps/api/geocode/json"

// Address holds reverse-geocode address parts used by ecommerce flow prefill.
type Address struct {
	Pincode string
	City    string
	State   string
	Country string
}

// Empty reports whether no useful address fields were parsed.
func (a Address) Empty() bool {
	return a.Pincode == "" && a.City == "" && a.State == "" && a.Country == ""
}

// Map returns non-empty fields keyed for WhatsApp Flow prefill.
func (a Address) Map() map[string]string {
	out := map[string]string{}
	if a.Pincode != "" {
		out["pincode"] = a.Pincode
	}
	if a.City != "" {
		out["city"] = a.City
	}
	if a.State != "" {
		out["state"] = a.State
	}
	if a.Country != "" {
		out["country"] = a.Country
	}
	return out
}

type googleGeocodeResponse struct {
	Status  string `json:"status"`
	Results []struct {
		AddressComponents []struct {
			LongName  string   `json:"long_name"`
			ShortName string   `json:"short_name"`
			Types     []string `json:"types"`
		} `json:"address_components"`
	} `json:"results"`
}

// ParseGoogleResponse extracts pincode/city/state/country from a Geocoding API body.
// Returns false when status is not OK or no useful components are present.
func ParseGoogleResponse(body []byte) (Address, bool) {
	var payload googleGeocodeResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return Address{}, false
	}
	if !strings.EqualFold(payload.Status, "OK") || len(payload.Results) == 0 {
		return Address{}, false
	}
	addr := addressFromComponents(payload.Results[0].AddressComponents)
	if addr.Empty() {
		return Address{}, false
	}
	return addr, true
}

func addressFromComponents(components []struct {
	LongName  string   `json:"long_name"`
	ShortName string   `json:"short_name"`
	Types     []string `json:"types"`
}) Address {
	byType := map[string]string{}
	for _, c := range components {
		name := strings.TrimSpace(c.LongName)
		if name == "" {
			continue
		}
		for _, t := range c.Types {
			if _, exists := byType[t]; !exists {
				byType[t] = name
			}
		}
	}
	city := firstNonEmpty(
		byType["locality"],
		byType["postal_town"],
		byType["administrative_area_level_2"],
	)
	return Address{
		Pincode: strings.TrimSpace(byType["postal_code"]),
		City:    city,
		State:   strings.TrimSpace(byType["administrative_area_level_1"]),
		Country: strings.TrimSpace(byType["country"]),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// ReverseGoogle looks up address parts for lat/lng. apiKey must be non-empty.
func ReverseGoogle(ctx context.Context, client *http.Client, apiKey string, lat, lng float64) (Address, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return Address{}, fmt.Errorf("google maps api key is empty")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	q := url.Values{}
	q.Set("latlng", fmt.Sprintf("%f,%f", lat, lng))
	q.Set("key", apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleGeocodeURL+"?"+q.Encode(), nil)
	if err != nil {
		return Address{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Address{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Address{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Address{}, fmt.Errorf("google geocode http %d", resp.StatusCode)
	}
	addr, ok := ParseGoogleResponse(body)
	if !ok {
		return Address{}, fmt.Errorf("google geocode returned no address")
	}
	return addr, nil
}
