# TiQR Ecommerce V2

Code: `internal/handlers/tiqrecommercev2/`. Key: `tiqr_ecommerce_v2`. Engine: Google ADK (`google.golang.org/adk`) inside the coded-flow runner (`internal/handlers/codedflow/`).

V1 (`tiqr_ecommerce`) stays available. Accounts opt into V2 with their own keywords on the TiQR Flows screen. V2 serves the same shopping purpose through one Gemini agent that calls grounded store tools and **names a V1 waiting step**. Go builds that step’s WhatsApp shell (buttons, list, carousel, image, location pin, details Flow, or text question).

## How a turn runs

1. Keyword match starts or resumes the coded flow (`_coded_flow = tiqr_ecommerce_v2`).
2. If the WhatsApp account has no Google AI key, the turn transfers with a fixed “not ready” line.
3. Keyword-only first turn (empty message, no transcript): Go sends the **welcome** shell with no model call.
4. Otherwise one ADK agent run executes with domain tools and a `reply` tool that names a waiting step.
5. A tap or exact button title is **binding**: the agent must advance with that action (for example `confirm_items` → details Flow). Contradicting tools are rejected.
6. Cart, fulfillment, capture answers, waiting step, and a compact ADK transcript live on the WhatsApp session (`tiqr_cart`, `_v2_state`). There are no ADK AutoMigrate tables.
7. The turn stops after one accepted shell (optional deviation line first), a transfer, or `end_session`. Tool rounds are capped at 8. If no shell is accepted, Go resends the current waiting-step shell.

The v1 `_coded_calls` replay script is not used.

## Model

Binding setting `model` (Gemini model id). Default: `gemini-2.5-flash`. The API key is the WhatsApp account’s Google AI key. OpenAI/Anthropic and the four v1 AI roles are not used for V2.

## Waiting steps and shells

The agent calls `reply` with `step` set to one of:

| Step | Shell |
| --- | --- |
| `welcome` | Reply buttons: Buy products / Check order status / Talk to staff |
| `collections` | List (Show more when needed) |
| `products` | One product: image + Add to cart. Two or more: carousel |
| `options` | List of options |
| `quantity` / `capture` / `addons` / `cart_edit` | V1 text questions |
| `cart` | Buttons: Add more / Edit cart / Checkout |
| `fulfillment` | Store pickup / Delivery |
| `delivery_pin` | Location request |
| `delivery_fallback` | Store pickup / Share again (after out-of-range; max 3 pins) |
| `checkout_preview` | Confirm items / Edit cart / Add more |
| `checkout_preview_fail` | Try again / Edit cart |
| `details` | WhatsApp Flow (confirm opens this; `create_order` waits for submission) |
| `order_placed` | Text or Pay now CTA |
| `order_status` | Orders list |
| `order_status_summary` | Text summary, no pay link, then `end_session` |

Optional `deviation_line` is one text bubble before the shell (FAQ, clarify, or jump explanation). If the step did not change, the shell is resent after that bubble.

Non-English sessions may pass `chrome_*` translations. Product, collection, and option names must stay unchanged. Rejected chrome falls back to English V1 copy.

## Tools

### Reads (results are grounded)

| Tool | Purpose |
| --- | --- |
| `get_store` | Store profile and delivery modes (retries once on failure) |
| `get_store_info` | About us / policies |
| `list_collections` | Collections (retries once; empty list → transfer) |
| `list_products` | Products for a grounded collection id |
| `search_products` | Search; labeled as search results, not a collection |
| `get_product` | Product detail, options, add-ons |
| `list_faqs` | FAQs |
| `list_orders` / `lookup_order` | Order status via MCP |
| `checkout_preview` | Priced quote (only price the customer should see) |
| `check_delivery` / `set_delivery_pin` | Local radii check; pin message does not quote a fee |

### Writes

| Tool | Purpose |
| --- | --- |
| `add_cart_line` / `update_cart_line` / `remove_cart_line` | Cart by **line id** (option + capture answers) |
| `set_addons` | Session catalog add-ons |
| `set_fulfillment` | Pickup or delivery |
| `open_details_flow` | Same as `reply` step `details` |
| `create_order` | Place order after details Flow submission (gated) |
| `transfer_to_staff` | Commerce handoff (gated) |
| `end_session` | Finish after order placed or order-status summary |
| `reply` | Name a waiting step; Go sends the shell |

Button, list, and carousel ids must be grounded or fixed actions (`buy_products`, `check_order_status`, `talk_to_agent`, `add_more`, `checkout`, `edit_cart`, `confirm_items`, `pickup`, `delivery`, `share_again`, `try_again`).

## Confirmation gates

- `confirm_items` opens the **details** Flow. `create_order` runs only after that Flow is submitted.
- `transfer_to_staff` runs in the same turn when the customer already asked for staff.
- If the agent decides on its own, the tool refuses and asks; the next message can allow it.

## Welcome

Saved welcome plus the live pickup/delivery line is sent only when the first message has not said what the customer wants. A first message that already names a product, order check, or staff is acted on by the agent.

## Order status

Matches v1: summary text, no pay link, then the session ends. While choosing an order, a shopping request gets a reply instead of silence.

## Gaps closed vs V1

- Cart lines are option + capture answers; edit/remove/order target that line.
- “Talk to staff”, checkout, and product names are never stored as capture/edit/add-on/recovery values.
- Search results are labeled as search.
- Store/collection load retries once in-session.
- Language follows the latest message within store `supported_languages`.
- Out-of-range delivery: at most 3 pin attempts, then pickup or transfer.
- Checkout preview is the only price quote; `shipping_fee_paise` is not sent on `create_order`.
- Collection `ai_instructions` are included in the agent instruction when active.
- Failed handoff does not claim the request was saved when the draft was not.
- Tool rejections always produce a customer reply (shell resend when needed).
- Interactive shells match V1 widgets so the chat is not a free-text thread.
- Deviations from a previous step are handled by the agent naming a new step, without V1 intent mis-routes.

## Admin settings

Same TiQR Flows card as v1 for keywords, welcome, pickup/delivery Meta flow ids, plus **Model id** for V2 only.
