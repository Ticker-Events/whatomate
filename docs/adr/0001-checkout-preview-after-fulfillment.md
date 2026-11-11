# 0001. Checkout preview after fulfillment

## Status

Accepted

## Context

WhatsApp ecommerce held a session-priced confirm step (`qty × catalog price`) before the customer chose pickup or delivery. Shipping and automatic discounts are only known once `delivery_mode` (and for delivery, the pin) are set. Calling `checkout-preview` without `delivery_mode` is treated as delivery on the store API and can quote shipping too early. Quoting a delivery fee on the pin-success message also duplicated pricing that belongs on the preview.

## Decision

1. Skip the pre-fulfillment session confirm. Saying checkout or tapping Checkout with a non-empty session cart goes straight into fulfillment.
2. After fulfillment is known, call guest `POST /service/buyer/cart/checkout-preview/` with `items` from the session cart (buy-now mode), `delivery_mode`, and pin coordinates for delivery. Do not send a discount code; only API-applied discounts appear.
3. Show Confirm items / Edit cart / Add more on that priced summary before the details WhatsApp Flow.
4. Pin success copy is only “We can deliver to that location.” Fees and free-delivery sentences belong on the preview.
5. If preview fails, offer Try again / Edit cart. A second failure hands off with the cart and copy that the total could not be loaded. The details form does not open on failure. The session subtotal is not used as a fallback, because it omits discounts and shipping.

## Consequences

- Add-to-cart and edit prompts still render session prices; only the post-fulfillment summary is API-priced.
- Replay records each preview attempt as its own Store call (`checkout_preview_1`, then `_2` after retry or edit). Confirm replays the successful payload.
- Long preview (and cart) button bodies split into a full text message plus a short button message instead of trimming rows.
