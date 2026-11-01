# TiQR Dining coded flow

WhatsApp coded flow for restaurant table token booking. Key: `tiqr_dining`.

## Registration

Package `internal/handlers/tiqrdining` registers the flow in `init()`. `chatbot_processor.go` blank-imports the package so the binary always links it.

Per-account enablement, default flow, and keywords use the existing `coded_flow_bindings` API and TiQR Flows admin UI — same as `tiqr_ecommerce`.

## Conversation

1. Load store (`get_store`).
2. Route buttons: **Book a table**, **My booking**, **Talk to staff**.
3. **Book a table**
   - Ask guest name (`AskText`). Command-like replies (cancel / my booking / staff) divert.
   - Ask party size (`AskNumber`, pattern `1–99`) with the shared intent / guide path.
   - Call `create_dining_booking` with name, party size, and session phone.
   - Reply with seated table or waitlist token + average wait + ETA.
4. **My booking**
   - Call `get_dining_booking` by phone.
   - Offer cancel via `cancel_dining_booking`.
5. **Talk to staff** — generic coded-flow transfer.

Intent identification, translation (`Conv.text`), and guided questions come from the coded-flow engine unchanged.

## Store API operations (REST)

| Operation | Buyer path |
| --- | --- |
| `create_dining_booking` | `POST /service/buyer/store/{id}/dining/bookings/` |
| `get_dining_booking` | `GET /service/buyer/store/{id}/dining/bookings/current/?phone_number=` |
| `cancel_dining_booking` | `POST /service/buyer/store/{id}/dining/bookings/{id}/cancel/` |

Implemented in `pkg/ticker/client.go` and `invokeTiqrStoreRESTOperation`.

## Slot booking

Not implemented in this flow. Backend booking `kind` / settings `booking_mode` reserve room for a future slot path.
