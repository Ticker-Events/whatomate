package tiqrecommerce

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
)

var (
	simpleEmailRE  = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	relativeTimeRE = regexp.MustCompile(`(?i)^\s*(?:in|after)\s+(\d+)\s*(m|min|mins|minute|minutes|h|hr|hrs|hour|hours)\s*$`)
	asapTimeRE     = regexp.MustCompile(`(?i)^\s*(asap|earliest|soonest|now|as soon as possible)\s*$`)
	absoluteTimeRE = regexp.MustCompile(`(?i)^\s*(today|tomorrow)?\s*(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*$`)
)

func promptCaptureField(field map[string]any) string {
	label := asString(field["label"])
	if help := asString(field["help_text"]); help != "" {
		label += "\n" + help
	}
	if options, ok := field["options"].([]any); ok && len(options) > 0 {
		values := make([]string, 0, len(options))
		for _, option := range options {
			values = append(values, fmt.Sprint(option))
		}
		label += "\nOptions: " + strings.Join(values, ", ")
	} else if options, ok := field["options"].([]string); ok && len(options) > 0 {
		label += "\nOptions: " + strings.Join(options, ", ")
	}
	return label
}

func validCaptureValue(field map[string]any, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if asString(field["type"]) == "number" {
		_, err := strconv.ParseFloat(value, 64)
		return err == nil
	}
	var options []string
	switch raw := field["options"].(type) {
	case []string:
		options = raw
	case []any:
		for _, option := range raw {
			options = append(options, fmt.Sprint(option))
		}
	}
	if len(options) > 0 {
		values := []string{value}
		if asString(field["type"]) == "multi_select" {
			values = strings.Split(value, ",")
		} else if asString(field["type"]) != "single_select" {
			return true
		}
		for _, candidate := range values {
			found := false
			for _, option := range options {
				if strings.EqualFold(strings.TrimSpace(option), strings.TrimSpace(candidate)) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return true
}

func normalizedCaptureValue(field map[string]any, value string) any {
	if asString(field["type"]) != "multi_select" {
		return strings.TrimSpace(value)
	}
	raw := strings.Split(value, ",")
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func checkoutSlotLabel(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return codedflow.TruncateRunes(value, 20)
	}
	return parsed.Format("02 Jan 3:04 PM")
}

func parseFulfillmentTimeText(text string, now time.Time, loc *time.Location, earliestAt string) (time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, errors.New("empty time")
	}
	if asapTimeRE.MatchString(text) {
		if earliestAt != "" {
			if parsed, err := time.Parse(time.RFC3339, earliestAt); err == nil {
				return parsed.In(loc), nil
			}
		}
		return now.Add(time.Minute).Truncate(time.Minute), nil
	}
	if m := relativeTimeRE.FindStringSubmatch(text); len(m) == 3 {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return time.Time{}, errors.New("invalid relative amount")
		}
		unit := strings.ToLower(m[2])
		switch unit {
		case "h", "hr", "hrs", "hour", "hours":
			return now.Add(time.Duration(n) * time.Hour).Truncate(time.Minute), nil
		default:
			return now.Add(time.Duration(n) * time.Minute).Truncate(time.Minute), nil
		}
	}
	if m := absoluteTimeRE.FindStringSubmatch(text); len(m) == 5 {
		dayOffset := 0
		switch strings.ToLower(m[1]) {
		case "tomorrow":
			dayOffset = 1
		}
		hour, err := strconv.Atoi(m[2])
		if err != nil {
			return time.Time{}, err
		}
		minute := 0
		if m[3] != "" {
			minute, err = strconv.Atoi(m[3])
			if err != nil || minute > 59 {
				return time.Time{}, errors.New("invalid minutes")
			}
		}
		ampm := strings.ToLower(m[4])
		if ampm == "pm" || ampm == "am" {
			if hour < 1 || hour > 12 {
				return time.Time{}, errors.New("invalid hour")
			}
			if ampm == "pm" && hour < 12 {
				hour += 12
			}
			if ampm == "am" && hour == 12 {
				hour = 0
			}
		} else if hour > 23 {
			return time.Time{}, errors.New("invalid hour")
		}
		base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, dayOffset)
		candidate := time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, loc)
		// Bare clock times that already passed today roll to tomorrow.
		if dayOffset == 0 && m[1] == "" && !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate, nil
	}
	return time.Time{}, errors.New("unrecognized time")
}
