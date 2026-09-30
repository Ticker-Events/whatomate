# TiQR Ecommerce coded flow

Code: `internal/handlers/codedflow_tiqr_ecommerce.go`. Key: `tiqr_ecommerce`.

This is the code-level map of that function: the customer path, where AI actually runs, and the gaps. The admin-facing page is `docs/src/content/docs/features/coded-flows.mdx`. AI role settings are in `docs/coded-flow-ai.md`.

The function is a script. It does not let a model browse the catalog or place the order. A button, list row, or carousel card that this step offered is accepted in code. AI runs only for free text, for translating authored lines, and for classifying a failed `create_order`.

## How a turn runs

`runCodedFlow` in `codedflow.go` starts the function from the top on every inbound message. Finished calls are replayed from `SessionData._coded_calls` in order. Call order is the identity of a step. Names can repeat inside the buy loop (`products`, `cart`, `quantity`) because the next unread record is what matters, not the name.

The keyword that started the flow is not an answer. `CurrentStep` is empty on that first turn, so the trigger text is cleared before `tiqrEcommerce` runs.

A call that returns false means stop this turn. The customer is waiting, a guide question was just sent, or the flow already transferred or ended. The session is persisted at the end of the turn.

## Customer path

```mermaid
flowchart TD
  kw[Keyword match] --> store[get_store]
  store -->|error| xfer[Agent transfer]
  store --> cols["list_collections limit 20"]
  cols -->|error or empty| xfer
  cols --> menu[Welcome menu]
  menu -->|Buy products| ful[Fulfillment]
  menu -->|named collection or product| ful
  menu -->|Check order status| look[MCP lookup_order_status]
  menu -->|Talk to staff| xfer
  look --> endStatus[Say status and end]
  ful --> list[Collection list]
  list -->|row or collection id| prod[list_products]
  list -->|product query| search[search_products]
  prod --> show[Carousel or one image]
  search --> show
  show --> opt{How many options}
  opt -->|none| skip[Say unavailable]
  opt -->|one| qty[Ask quantity]
  opt -->|many| olist[Option list]
  olist --> qty
  qty --> cart[Append tiqr_cart]
  skip --> next[Add more or Checkout]
  cart --> next
  next -->|Add more| list
  next -->|Checkout and cart has lines| flow[WhatsApp Flow by delivery mode]
  flow -->|pickup 1484028330223507| create[create_order]
  flow -->|delivery 1557965846018132| create
  create -->|success| done[Confirm, thanks, end]
  create -->|missing fields and retries left| fix[Ask each missing field]
  fix --> create
  create -->|give up or retries used| xfer
```

Checkout said in free text on the menu, the collection list, a product card, an option list, the quantity prompt, or the add-more prompt jumps to the same checkout, after an empty-cart message if `tiqr_cart` has no lines. That divert is not applied during fulfillment. See Gaps.

### 1. Load the store

`Store("store", "get_store")` then `StoreList("collections", "list_collections", limit 20)`. Either failure transfers with a fixed line (`tiqrEcommerceFailBusiness` or `tiqrEcommerceFailCollections`) and completes the session. An empty collection list is the same failure: `StoreList` treats zero rows as not ok.

`delivery_modes` on the store payload decides fulfillment later. Nothing else from the store is required to show the menu.

### 2. Welcome menu

Buttons, in order:

| Id | Title |
| --- | --- |
| `buy_products` | Buy products |
| `check_order_status` | Check order status |
| `talk_to_agent` | Talk to staff |

The body is `Welcome to {store name}.` plus `What would you like to do?` when the store has a name.

`askRouteButtons` is called with `AllowCatalog: true`. A tap on those three ids does not call AI. Free text can resolve to:

| Route | What the function does |
| --- | --- |
| `choice` / `buy_products` | `buyProducts` with an empty route, so the collection list is next |
| `choice` / `check_order_status` | `orderStatus` |
| `choice` / anything else, including `talk_to_agent` | `Transfer(codedAgentHandoff)` |
| `collection` | `buyProducts` with that collection id. The id must be one of the loaded collections |
| `product` | `buyProducts` with a search query. The model supplies words, not a product id |
| `checkout` | `afterCheckoutDivert`. Empty cart continues into browsing. A cart opens checkout |
| `handoff`, ungrounded JSON, or an AI error | transfer |

### 3. Fulfillment

`resolveFulfillment` runs once at the start of `buyProducts`, before the collection list. It reads `delivery_modes` already stored on the session.

| Store modes | Behaviour |
| --- | --- |
| Missing or empty | No prompt. `delivery_mode` is set to `PICKUP_FROM_STORE` inside `Once`, then browsing continues |
| Only `PICKUP_FROM_STORE` | One button, Proceed. Then pickup |
| Both | Store pickup or Delivery |
| Only `DELIVERY_TO_LOCATION` | No mode buttons. Goes straight to a location pin. `delivery_mode` is set inside `Once` |

Delivery asks for a WhatsApp location pin (`AskLocation`). Distance is computed in `evaluateStoreDelivery` (`codedflow_delivery.go`) from the store latitude, longitude, `free_delivery_radius`, and `delivery_radius`. There is no MCP call.

| Zone | Next step |
| --- | --- |
| `free` | Say free delivery, store the zone and distance, continue |
| `paid` | Say a fee may apply. `ShippingFeePaise` from this function is always 0, so the rupee amount is not filled in |
| `unconfigured` | Treated as deliverable. The customer is told delivery is possible |
| `out_of_range` and pickup exists | Offer Store pickup or Share again |
| `out_of_range` and delivery only | Say the out-of-range line and ask for another pin. The loop has no attempt cap |

A deliverable pin stores `delivery_mode`, `delivery_zone`, `delivery_distance_km`, and `shipping_fee_paise` when the fee is greater than 0. The pin itself is stored by `AskLocation` as `delivery_latitude` and `delivery_longitude`.

### 4. Collections and products

The collection list uses header `Our collections`, button `Browse`, row title `{{name}}`, description `{{description}}`. A chosen row saves `collection_id` from `id` and `collection_name` from the rendered title.

`askRouteList` also has `AllowCatalog: true`, so free text on this step can name another loaded collection or a product.

`productsForRoute`:

| Route | Store call |
| --- | --- |
| `collection` or `choice` with an id | `list_products` with `category_id`. No limit or offset is passed |
| `product` with a query | `search_products` with `search` and `limit` 20. `collection_name` is overwritten with the query |
| empty query or empty id | "I couldn't find that" and the buy loop shows collections again |
| API error (`lastTiqrErr` set) | transfer with `tiqrEcommerceFailProducts` |
| any other kind | transfer |

WhatsApp shows at most 10 list rows and 10 carousel cards (`chatbot_graph_runner.go`). Collections were fetched with limit 20, so free text can still name a collection that is not on the list, as long as it was in that first page. Products past the first 10 cards cannot be tapped, and this step does not allow a catalog route (see AI).

One product is an image reply with Add to cart, because a carousel needs two cards. Two or more products are a carousel. A missing image uses the hardcoded fallback URL `tiqrEcommerceFallbackMedia`.

### 5. Option, quantity, cart

| Options on the selected product | What happens |
| --- | --- |
| 0 | `tiqrEcommerceUnavailable`. Nothing is added. The flow still asks add-more or checkout |
| 1 | That option is taken. No list |
| 2 or more | Option list. Title is `{{name}} (₹{{price}})` |

Quantity uses `AskNumber` with pattern `^[0-9]+$`. Digits are accepted in code, including `0`. A number word is accepted only when intent returns route `answer` with a value that matches the pattern.

`Once("cart")` appends `{product_option, quantity, option_name}` onto `tiqr_cart`. `cart_count` is the number of lines, not the sum of quantities. The same option added twice is two lines. Replay of that same call does not append again.

Then buttons `add_more` and `checkout`. Add more returns to the collection list. Checkout breaks the loop.

### 6. Checkout

`AskFlow` opens a WhatsApp Flow by delivery mode, button Enter details. Both flow ids are constants and must exist on the WhatsApp account under those Meta ids.

| `delivery_mode` | Meta flow id | Form fields | Body |
| --- | --- | --- | --- |
| `PICKUP_FROM_STORE` (or empty) | `1484028330223507` | `customer_name`, `customer_email`, `customer_phone`, `customer_notes` | Name, email, and phone. No address |
| `DELIVERY_TO_LOCATION` | `1557965846018132` | Same contact fields plus address lines | Name, phone, and address |

`pickupOrderParams` builds `create_order`:

| Param | Source |
| --- | --- |
| `email` | `customer_email` or `email` if it matches a simple email. Phone fields are skipped. Other session values are scanned for an email |
| `phone_number` | `customer_phone`, `phone`, or `phone_number` when present |
| `items` | `tiqr_cart` lines reduced to `product_option` and `quantity`. The TiQR client turns those strings into integers |
| `notes` | `customer_notes` or `notes` |
| `new_address` | Delivery only: name, address lines, city, state, country, pincode, email, phone, plus latitude and longitude to 6 decimal places. Omitted for pickup |
| `delivery_mode` | session value, or `PICKUP_FROM_STORE` if empty |
| `buyer_meta_data` | Pickup: `{name}` when a customer name exists. Delivery: latitude and longitude |

`shipping_fee_paise` is not sent.

Success sends the pickup or delivery confirmation, then `tiqrEcommerceThanks`, then `End`. There is no second confirmation.

Failure calls `createRecoverPlan`. If the plan is `missing_field` and retries remain, `collectRecoverFields` asks for every missing field in a fixed order, stores each reply on the session, and `create_order` runs again. Otherwise the customer is transferred with `formatFailedOrderHandoff`, which lists cart lines and the address. The recover model's own sentence is not sent on that path.

Retries come from `[codedflow] order_retries`, default 2. The loop is `attempt := 0; attempt <= retries`, so the default is one try plus two retries. Missing fields are collected only while `attempt < retries`.

### 7. Order status

`LookupOrder` calls TiQR MCP `lookup_order_status` for this WhatsApp number. It does not ask for an order id. Found orders are formatted by `formatDirectOrderStatus` (`Order {display_uid} is {status}.`). Missing or failed lookups say `tiqrEcommerceOrderMissing` and end. No payment link is attached. Payment retry stays on the separate commerce path.

## Where AI runs

Four roles, configured per chatbot AI settings. Default provider is Account AI. Intent may use TypeSafe Jev or AI Gateway System One. Translation, guide, and recover cannot use Jev, because Jev does not generate free text. Thresholds live in `codedIntentSettings`: confidence `0.75`, guide turns `3`, order retries `2`.

This file never calls a model itself. The calls are inside `Conv`.

### Intent

`resolveFreeText` in `codedflow_intent.go`. It runs when the reply is not an offered button id or title.

Steps in this flow that can reach it:

| Step | Call | Catalog routes allowed |
| --- | --- | --- |
| Welcome menu | `askRouteButtons` | yes: collection id and product query |
| Collection list | `askRouteList` | yes |
| Fulfillment buttons | `AskButtons` → `askChoice` | no |
| Product card or carousel | `AskImageButtons` / `AskCarousel` | no |
| Option list | `AskList` | no |
| Add more / checkout | `AskButtons` | no |
| Quantity, if the text is not digits | `AskNumber` | no, but route `answer` is allowed |
| Location pin, if the message is not a pin | `AskLocation` | no |
| Details form, if the message is not the flow submission | `AskFlow` | no |

The model must return one route and leave the other slots empty. Go then checks the allow-list (`validateCodedIntent`):

- `choice` id must be a button on this step
- `collection` id must be in the loaded collections, and only when catalog is allowed
- `product` is a non-empty query, only when catalog is allowed. No product id is trusted
- `answer` must match the step pattern
- `checkout` and `handoff` must not carry ids
- anything else, including a product route on a step that does not allow catalog, is ungrounded

At or above 0.75 and grounded: the route is applied in the same turn. `checkout` sets `c.divert` and does not record a shopping call, so the script can leave the step. Below 0.75, or route `unclear`: one guide question, staying on the same step.

Ungrounded output, a grounded `handoff`, or an intent error transfers immediately with `codedAgentHandoff`. The customer does not get a guide question in those cases.

Language from the first successful intent call is stored as `customer_language` only if the session does not already have one. Button taps never set it.

### Guide

`askGuide`. One short question, in the stored language (default `en`). Up to 3 questions per step, counted in `SessionData._coded_guide`. The 4th unclear reply transfers. An empty question or a guide error also transfers. A later confident route clears the counter.

### Translation

`Conv.text` in `codedflow_ai.go` runs on every authored line before send, including button titles (then truncated to 20 characters). It does not run when `customer_language` is empty, `en`, or `en-*`. English taps therefore stay in the authored English.

Catalog text is not translated: collection names, product names, option names, and prices stay as the store sent them. Placeholders, asterisks, numbers, prices, and line breaks are supposed to be kept. Results are cached on the session under `_translations`. A translation error sends the English source.

### Order recovery

`createRecoverPlan` → `runRecover` after `create_order` returns not ok. The model sees a sanitized hint, not the raw HTTP body. URLs, tokens, and curl text are stripped.

| Kind | What checkout does |
| --- | --- |
| `missing_field` | Ask the listed session fields, one `AskText` each, then retry |
| `try_later` or `give_up` | Ignore the model's sentence. Transfer with the cart handoff text |
| AI error or invalid JSON | Same transfer, using the canned failed-order fallback inside the plan only as a stored message |

If the TiQR error hint contains `validation fields: ...`, `expandRecoverAsks` replaces the model's field list with those names and forces `missing_field`. The model does not get to drop a field the API named. Field order is fixed: name, phone, email, address line 1, address line 2, city, state, country, pincode, notes.

`AskText` for those fields does not call intent. The `StepNote` is ignored (`_ = step`).

Fetch failures in this flow do not call `recoverFetchMessage`. Store, collection, and product errors transfer with a fixed sentence.

## What is not AI

- Loading the store, collections, products, and search results
- Distance and delivery zone
- Appending the cart
- Building the `create_order` body
- Order-status lookup and the status sentence
- Exact taps on an offered id or title
- The decision to transfer after an ungrounded route

Collection `AIInstructions` and required capture fields used by the commerce chatbot are not read here.

## Session values this flow writes

| Key | When |
| --- | --- |
| `store` | `get_store` |
| `collections` | `list_collections` rows |
| `delivery_mode` | Fulfillment. `PICKUP_FROM_STORE` or `DELIVERY_TO_LOCATION` |
| `delivery_latitude`, `delivery_longitude` | Accepted pin |
| `delivery_zone`, `delivery_distance_km`, `shipping_fee_paise` | Deliverable pin. Fee stays unset while the calculator returns 0 |
| `collection_id`, `collection_name` | Collection choice, or the search query for a product route |
| `products`, `product_id`, `options`, `product_selected` | Product choice |
| `option_id`, `option_name`, `quantity` | Option and quantity |
| `tiqr_cart`, `cart_count` | Each append |
| `customer_language` | First free-text intent, if unset |
| form fields | WhatsApp Flow submission, plus any field recover asks for |
| `order` | Successful `create_order` |

`phone_number` and `contact_name` are written by `runCodedFlow` on every turn, before this function runs.

## Gaps

1. **Checkout during fulfillment is dropped.** `resolveFulfillment` and `resolveDeliveryLocation` return false when the reply is not the expected button or pin. `buyProducts` then returns without `applyCheckoutDivert`. Intent can still set `c.divert` to checkout, but this turn sends nothing and does not open the form. The same step is waiting on the next message. Checkout from the menu, collection list, product, option, quantity, and add-more steps does call `applyCheckoutDivert`.

2. **Naming a product on a product card transfers.** Product, option, fulfillment, quantity, location, and the details form call `resolveFreeText` with catalog routes off. A grounded `product` or `collection` route is rejected and the customer is transferred. Only the menu and the collection list can search or jump to a collection.

3. **Delivery fee is never priced or charged.** `evaluateStoreDelivery` documents that the flat store shipping fee is not on the `get_store` payload, so `ShippingFeePaise` stays 0. The paid-zone sentence falls through to "an additional delivery fee may apply." `pickupOrderParams` does not send `shipping_fee_paise` even if it were set.

4. **Delivery-only and out of range does not end.** `resolveDeliveryLocation` asks for another pin forever when pickup is not allowed. There is no attempt limit and no automatic transfer. A typed handoff can still transfer, because a non-pin message goes through intent.

5. **Recovery replies are literal.** While collecting a missing email or address, "talk to staff" is stored as that field. Intent is not run. A bad value is sent again on the next `create_order`.

6. **Non-field order failures hide the recover sentence.** `try_later` and `give_up` do not `Say` the model message or `tiqrEcommerceFailed`. The customer gets the cart-and-address handoff and a transfer. Thanks is sent only after a successful create.

7. **No cart management.** Lines are append-only. There is no view, edit, remove, or merge of the same option. `cart_count` is the line count, and the added-to-cart sentence reads as if it were a unit count. Quantity `0` matches `^[0-9]+$`. Stock is not checked before add.

8. **Catalog window is one page.** Collections: 20 from the API, 10 on the WhatsApp list. Products: whatever the API default page is, then 10 cards. No offset, no "more" button. A product the model was not allowed to search for on the card step cannot be reached except by going back to collections and searching again, and only if search returns it in the first 20.

9. **Search labels the carousel with the query.** `productsForRoute` sets `collection_name` to the search string, and the carousel body says the items are available in that name.

10. **Empty store data is a transfer, not a retry inside the session.** `get_store` or `list_collections` failure completes the session. A later keyword can start a new session and call TiQR again. Within one session those calls are not retried, because the flow has already ended.

11. **Hardcoded Meta flow ids and fallback image.** Pickup uses `tiqrEcommercePickupFlowID` (`1484028330223507`). Delivery uses `tiqrEcommerceFlowID` (`1557965846018132`). The fallback product photo is one DigitalOcean Spaces URL. A store whose form id differs, or a dead image URL, fails open at checkout or shows the wrong photo.

12. **Language is sticky.** The first non-empty intent language wins for the session. A later message in another language does not replace it, so translation keeps using the first label.

13. **Order status is thinner than commerce checkout.** No payment link, no retry payment, no order id prompt. A failed lookup and a customer with no orders share one sentence.

14. **Collection AI instructions are ignored.** The commerce chatbot loads per-collection instructions and required capture fields. This flow never reads them, so a collection that needs an extra question (for example a cake message) is not asked before the cart line.
