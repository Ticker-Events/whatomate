package tiqrdining

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
)

const (
	tiqrDiningKey = "tiqr_dining"

	diningBookTable  = "book_table"
	diningMyBooking  = "my_booking"
	diningTalkStaff  = "talk_to_staff"
	diningCancelBook = "cancel_booking"
	diningKeepBook   = "keep_booking"
)

func init() {
	codedflow.Register(newTiqrDiningFlow())
}

// FlowKey is the coded-flow registry key for TiQR Dining.
const FlowKey = tiqrDiningKey

func newTiqrDiningFlow() codedflow.CodedFlow {
	return codedflow.NewFlow(
		tiqrDiningKey,
		"TiQR Dining",
		"Book a restaurant table with a rush-hour token queue. "+
			"Guests share their name and party size, get seated when a table fits, "+
			"or receive a waiting token with estimated wait.",
		[]codedflow.CodedStep{
			{Name: "load_store", Label: "Load store"},
			{Name: "intent", Label: "Book table, my booking, or agent"},
			{Name: "book", Label: "Collect name and party size"},
			{Name: "my_booking", Label: "Show or cancel booking"},
			{Name: "agent", Label: "Talk to staff"},
		},
		tiqrDining,
	)
}

func tiqrDining(c *codedflow.Conv) error {
	store, ok := c.Store("store", "get_store", nil)
	if !ok {
		return c.Transfer("I couldn't load the restaurant just now.\n\n" + codedflow.AgentHandoff)
	}
	storeName := strings.TrimSpace(asString(store["name"]))
	welcome := "Welcome"
	if storeName != "" {
		welcome = "Welcome to " + storeName
	}
	c.RememberMenuChoices(map[string]string{
		diningBookTable: "Book a table",
		diningMyBooking: "My booking",
		diningTalkStaff: "Talk to staff",
	})
	route, ok := c.AskRouteButtons("dining_intent", codedflow.ButtonPrompt{
		Body: welcome + ".\n\nWhat would you like to do?",
		Buttons: []codedflow.Button{
			{ID: diningBookTable, Title: "Book a table"},
			{ID: diningMyBooking, Title: "My booking"},
			{ID: diningTalkStaff, Title: "Talk to staff"},
		},
	}, codedflow.RouteOptions{})
	if !ok {
		return nil
	}
	switch route.Kind {
	case codedflow.RouteChoice:
		switch route.ID {
		case diningBookTable:
			return bookTable(c)
		case diningMyBooking:
			return myBooking(c)
		default:
			return c.Transfer(codedflow.AgentHandoff)
		}
	default:
		return c.Transfer(codedflow.AgentHandoff)
	}
}

func bookTable(c *codedflow.Conv) error {
	defaultName := strings.TrimSpace(asString(c.Session().SessionData["contact_name"]))
	namePrompt := "What name should we put on the booking?"
	if defaultName != "" {
		namePrompt = "What name should we put on the booking?\n\nReply with your name (WhatsApp shows *" + defaultName + "*)."
	}
	guestName, ok := c.AskText("dining_guest_name", namePrompt, codedflow.StepNote{})
	if !ok {
		return nil
	}
	guestName = strings.TrimSpace(guestName)
	if guestName == "" {
		guestName = defaultName
	}
	if guestName == "" {
		guestName = "Guest"
	}
	if looksLikeDiningCommand(guestName) {
		return divertDiningCommand(c, guestName)
	}

	partyText, ok := c.AskNumber("dining_party_size", codedflow.NumberPrompt{
		Body:    "How many people will be dining?",
		Pattern: `^[1-9][0-9]?$`,
	})
	if !ok {
		if c.HasReplyRoute() {
			return handleDiningDivert(c, c.TakeReplyRoute())
		}
		return nil
	}
	partySize, err := strconv.Atoi(strings.TrimSpace(partyText))
	if err != nil || partySize < 1 {
		c.Say("Please send a party size between 1 and 99.")
		return c.End()
	}

	phone := strings.TrimSpace(asString(c.Session().SessionData["phone_number"]))
	booking, ok := c.Store("dining_booking", "create_dining_booking", map[string]string{
		"guest_name":   guestName,
		"phone_number": phone,
		"party_size":   strconv.Itoa(partySize),
		"kind":         "token",
	})
	if !ok {
		return c.Transfer("I couldn't create your booking just now.\n\n" + codedflow.AgentHandoff)
	}
	c.Say(bookingConfirmationMessage(booking))
	return c.End()
}

func myBooking(c *codedflow.Conv) error {
	phone := strings.TrimSpace(asString(c.Session().SessionData["phone_number"]))
	booking, ok := c.Store("dining_current", "get_dining_booking", map[string]string{
		"phone_number": phone,
	})
	if !ok || booking == nil || asString(booking["id"]) == "" {
		c.Say("I couldn't find an active table booking for this number.\n\nYou can book a table any time from the menu.")
		return c.End()
	}
	c.Say(bookingStatusMessage(booking))

	c.RememberMenuChoices(map[string]string{
		diningCancelBook: "Cancel booking",
		diningKeepBook:   "Keep booking",
	})
	route, ok := c.AskRouteButtons("dining_manage", codedflow.ButtonPrompt{
		Body: "Would you like to cancel this booking?",
		Buttons: []codedflow.Button{
			{ID: diningCancelBook, Title: "Cancel booking"},
			{ID: diningKeepBook, Title: "Keep booking"},
		},
	}, codedflow.RouteOptions{})
	if !ok {
		return nil
	}
	if route.Kind == codedflow.RouteChoice && route.ID == diningCancelBook {
		cancelled, ok := c.Store("dining_cancelled", "cancel_dining_booking", map[string]string{
			"booking_id":   asString(booking["id"]),
			"phone_number": phone,
		})
		if !ok {
			return c.Transfer("I couldn't cancel that booking just now.\n\n" + codedflow.AgentHandoff)
		}
		c.Say(cancelConfirmationMessage(cancelled))
		return c.End()
	}
	c.Say("Okay, your booking is still active. We'll message you when your table is ready.")
	return c.End()
}

func looksLikeDiningCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	commands := []string{
		"cancel", "my booking", "book a table", "book table",
		"talk to staff", "talk to agent", "agent", "status",
	}
	for _, cmd := range commands {
		if lower == cmd || strings.Contains(lower, cmd) && len(lower) < 40 {
			return true
		}
	}
	return false
}

func divertDiningCommand(c *codedflow.Conv, text string) error {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "cancel") || strings.Contains(lower, "my booking") || strings.Contains(lower, "status"):
		return myBooking(c)
	case strings.Contains(lower, "staff") || strings.Contains(lower, "agent"):
		return c.Transfer(codedflow.AgentHandoff)
	default:
		c.Say("Please reply with the guest name for this booking.")
		return c.End()
	}
}

func handleDiningDivert(c *codedflow.Conv, route codedflow.Route) error {
	switch route.Kind {
	case codedflow.RouteChoice:
		switch route.ID {
		case diningMyBooking:
			return myBooking(c)
		case diningTalkStaff:
			return c.Transfer(codedflow.AgentHandoff)
		case diningBookTable:
			return bookTable(c)
		}
	case codedflow.RouteHandoff:
		return c.Transfer(codedflow.AgentHandoff)
	}
	return c.Transfer(codedflow.AgentHandoff)
}

func bookingConfirmationMessage(booking map[string]any) string {
	status := asString(booking["status"])
	token := asString(booking["token_number"])
	name := asString(booking["guest_name"])
	if name == "" {
		name = "there"
	}
	tables := tableTitles(booking)
	switch status {
	case "seated":
		msg := fmt.Sprintf("Hi %s, your table is ready", name)
		if tables != "" {
			msg += ": *" + tables + "*"
		}
		if token != "" {
			msg += fmt.Sprintf(". Token *#%s*.", token)
		} else {
			msg += "."
		}
		return msg
	default:
		msg := fmt.Sprintf("Hi %s, you are on the waitlist", name)
		if token != "" {
			msg += fmt.Sprintf(" with token *#%s*", token)
		}
		msg += "."
		if wait := asString(booking["average_wait_minutes"]); wait != "" && wait != "0" {
			msg += fmt.Sprintf(" Average wait is about *%s minutes*.", wait)
		}
		if eta := formatETA(booking["estimated_seat_at"]); eta != "" {
			msg += fmt.Sprintf(" Estimated seating around *%s*.", eta)
		}
		msg += " We will message you before your table is ready."
		return msg
	}
}

func bookingStatusMessage(booking map[string]any) string {
	status := asString(booking["status"])
	token := asString(booking["token_number"])
	party := asString(booking["party_size"])
	tables := tableTitles(booking)
	switch status {
	case "seated":
		msg := "Your party is seated"
		if tables != "" {
			msg += " at *" + tables + "*"
		}
		if token != "" {
			msg += fmt.Sprintf(" (token #%s)", token)
		}
		if party != "" {
			msg += fmt.Sprintf(" for %s guests", party)
		}
		return msg + "."
	default:
		msg := "You have an active waitlist booking"
		if token != "" {
			msg += fmt.Sprintf(" — token *#%s*", token)
		}
		if party != "" {
			msg += fmt.Sprintf(" for %s guests", party)
		}
		msg += "."
		if eta := formatETA(booking["estimated_seat_at"]); eta != "" {
			msg += fmt.Sprintf(" Estimated seating around *%s*.", eta)
		}
		return msg
	}
}

func cancelConfirmationMessage(booking map[string]any) string {
	token := asString(booking["token_number"])
	if token != "" {
		return fmt.Sprintf("Your dining booking (token #%s) has been cancelled.", token)
	}
	return "Your dining booking has been cancelled."
}

func tableTitles(booking map[string]any) string {
	raw, ok := booking["tables"]
	if !ok || raw == nil {
		return ""
	}
	var titles []string
	switch list := raw.(type) {
	case []any:
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				if title := asString(m["title"]); title != "" {
					titles = append(titles, title)
				}
			}
		}
	case []map[string]any:
		for _, m := range list {
			if title := asString(m["title"]); title != "" {
				titles = append(titles, title)
			}
		}
	}
	return strings.Join(titles, ", ")
}

func formatETA(raw any) string {
	s := asString(raw)
	if s == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Local().Format("3:04 PM")
		}
	}
	return ""
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case jsonNumber:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

// jsonNumber mirrors encoding/json.Number without importing for the type switch.
type jsonNumber interface {
	String() string
}
