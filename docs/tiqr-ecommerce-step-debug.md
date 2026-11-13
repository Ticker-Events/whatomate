# TiQR Ecommerce step debug sheet

Use this when a WhatsApp reply did the wrong thing. The narrative map is [tiqr-ecommerce-coded-flow.md](./tiqr-ecommerce-coded-flow.md). This file is the per-step sheet: what the step sends, what it accepts, and what is sent back when the reply is not accepted.

Code: `internal/handlers/tiqrecommerce/`. Engine: `internal/handlers/codedflow/`. Flow key: `tiqr_ecommerce`.

Every inbound message starts the script from the top. Finished calls in `SessionData._coded_calls` are replayed. A call that returns false ends the turn: the customer is waiting, a guide question was just sent, or the session transferred or ended.

**Customer message** is WhatsApp text (or buttons) the shopper sees. **Model prompt** is text sent to the intent, guide, add-on, store-info, or recover role. Button taps and exact titles do not call a model.

---

## Shared invalid-reply ladder

Free text on a button, list, carousel, image, number, location, or details-form step runs `resolveFreeText` (`codedflow/intent.go`), unless the step says otherwise below.

1. **Local match** (`matchLocalIntent`). No model. Order: current-step id or exact title, then previous-step id or exact title, then welcome-menu id or exact title (`buy_products`, `check_order_status`, `talk_to_agent`). Logged as prompt `(local exact match)`.
2. **Intent model.** Prompt is [Intent prompt](#intent-prompt). Confidence must be at least **0.75** and the route must be grounded.
3. **Below 0.75 or route** `unclear`**.** Store-info model first ([Store-info prompt](#store-info-prompt)). Accepted only when `confident` is true, confidence is at least **0.9**, and `answer` is non-empty. That answer is sent and the step stays open.
4. **Still unresolved.** Guide model ([Guide prompt](#guide-prompt)). The question it returns is sent as-is. Up to **3** guide turns per step (`SessionData._coded_guide`). The 4th transfers.
5. **Ungrounded JSON, intent error, empty guide, or guide error.** Store-info first, unless the text asks for a person (`WantsPerson`). Then transfer.

Transfer lines:

| Case | Customer message |
| --- | --- |
| They asked for a person, or they tapped Talk to staff | `I'm connecting you with a team member who can help.` |
| Anything else the flow gives up on | `I'm not sure about {their message, max 160 characters}. I'm connecting you to a staff member who can help.` |
| Off-topic when route `refuse` is allowed | `I can't help you with that.` This flow does not enable `refuse`. |

Unsupported WhatsApp types, before the step’s own prompt is sent again:

| Case | Customer message |
| --- | --- |
| Voice note on an active ecommerce session | `I'm unable to handle voice messages right now. I'm connecting you with a team member who can help.` Then the commerce handoff path (enquiry; agent transfer only inside hours). |
| Live location, or a Maps link with no coordinates, while a pin is required | `Please tap Send location on the next message. I couldn’t read a location pin from that message.` Then the location request is sent again. |
| Any other unsupported type while a step is waiting | `That message type isn't supported here. Please reply again using the previous instructions.` Then the previous prompt is sent again. |

Welcome menu ids are stored for the whole session as `_coded_menu_choices` and are included in every later intent call. The immediately previous interactive step is `_coded_previous_step`.

Catalog routes (`collection`, `product`) are allowed on every `AskButtons`, `AskList`, `AskCarousel`, and `AskNumber` call, because `askChoice` and `AskNumber` pass `AllowCatalog: true`. They are **not** allowed on `AskLocation` or `AskFlow`. A grounded catalog or checkout route sets an in-memory reply route. The buy loop and checkout consume it. Order status does not. See [Gaps and issues](#gaps-and-issues). 

---

## Intent prompt

Built by `BuildIntentPrompt`. Placeholders in `{braces}` are filled per step. Collections and FAQs are the loaded rows (or `(none)`). FAQ and refuse paragraphs are omitted when that route is off.

```
You map one customer message onto one route. You do not reply to the customer. You do not invent ids, products, collections, prices, or stock. You do not write FAQ answers; code sends the stored answer for a faq_id.

Current question shown to the customer:
{question body}

What this step is doing:
{step Doing}

What a valid reply looks like:
{step Expect}

If this step accepts a free-text value (not a button id), the value must match this pattern after you normalize it:
{pattern or (none)}

Allowed choices for the current question, use only these ids. A typo or paraphrase of one of these titles is still route choice with that id:
{id (title) lines, or (none)}

Previous question choices (the customer may change their mind and answer that instead). A typo or paraphrase of one of these titles is route previous with that id:
{previous question and id (title) lines, or (none)}

Welcome menu choices available from any step. A typo or paraphrase of one of these titles is route choice with that id:
- buy_products (Buy products)
- check_order_status (Check order status)
- talk_to_agent (Talk to staff)

Collections already loaded, use only these ids. If this list is empty, do not use the collection route:
{name (id N) lines, or (none)}

Store FAQs already loaded, use only these ids. If this list is empty, do not use the faq route:
{question (id N) lines, or (none)}

When the customer says "buy …", "I want …", "I need …", "show me …", or a close variant, they are almost always naming a collection or a product. Do not use unclear just because that name is not the item currently on screen. If the name matches a loaded collection (including a typo, "buy <name>", or "<name> collection"), use route collection and set collection_id from the list above. If it does not match a loaded collection, use route product and set product_query to the short name taken from their words; code searches the catalog. Do not invent an id.

Prefer the current question when the reply clearly answers it. Prefer previous when they are changing an earlier choice. Prefer a welcome menu choice when they want to buy, check an order, or talk to staff from mid-flow. Do not use unclear only because the reply does not fit the current question.

route choice: they picked one of the current choices or a welcome menu choice (including a misspelled title). Set choice_id.
route previous: they answered the previous question instead, or changed that choice (including a misspelled title). Set choice_id from the previous list. Leave collection_id, product_query, and answer empty.
route collection: they want the items in one collection, or they ask what is available in that collection. Set collection_id from the list above. Do not use this for a single product name.
route product: they name a product or ask to buy a specific item. Set product_query to a short search string taken from their words. Leave both ids empty.
route answer: their reply is a valid answer to the current question but is not one of the choice ids. Set answer to the normalized value that satisfies the pattern and expected reply (for example "Two" becomes "2"). Leave both ids and product_query empty. Only use this when a pattern is listed above.
route checkout: they want to check out, place the order, pay, or finish shopping with what is already in the cart. Leave both ids, product_query, and answer empty. Do not use this for checking an existing order status.
route faq: they ask a question that one of the loaded FAQs already answers, including a paraphrase of that FAQ question. Set faq_id from the list above. Leave choice_id, collection_id, product_query, and answer empty. Prefer shopping routes when they are buying, browsing, checking out, checking an order, or asking for a person.
route handoff: they ask for a human, agent, or staff, they seem confused or stuck, or you would have to invent a product, collection, price, or id to help them. Leave both ids empty. Prefer talk_to_agent as route choice when that welcome menu id is listed.
route unclear: more than one shopping route fits, or none of them fit, and they are not asking for a person and do not seem stuck.

confidence is from 0 to 1. Use 0.9 or higher for an obvious route, including a clear request for a person, a clearly confused customer, a clear typo of an allowed choice, a clear previous-step change of mind, a clear checkout request, a clear FAQ match, a clear refuse case, and a clear normalized answer. Use a value below 0.75 when you are unsure. If you are unsure whether a catalog or FAQ id exists, use handoff instead of guessing an id.
{language line: open detection, or "language must be one of these codes only: … If the customer message is not clearly one of those languages, set language to en."}
reasoning explains why you chose this route, for an operator debugging the flow. At most 200 words. Do not address the customer.

Reply with JSON only:
{"language":"en","route":"unclear","choice_id":"","collection_id":"","product_query":"","answer":"","faq_id":"","confidence":0,"reasoning":""}

Customer message:
{their message}
```

Grounding after the model replies (`ValidateCodedIntent`):

| Route | Accepted when |
| --- | --- |
| `choice` | Id is on this step, or is a welcome-menu id |
| `previous` | Id is on the saved previous step |
| `collection` | Id is a loaded collection. Rejected when catalog is off |
| `product` | `product_query` is non-empty. Rejected when catalog is off |
| `answer` | Value matches the step pattern. Rejected when the step has no pattern |
| `faq` | Id is a loaded FAQ |
| `checkout`, `handoff` | No ids attached |
| anything else | Ungrounded |

A grounded `handoff` still goes through store-info, then transfer. It is not sent as the model’s own sentence.

## Guide prompt

Built by `buildGuidePrompt`. The returned question is the customer message. Empty or error transfers.

```
The customer has not chosen a path yet. Ask one short question so their next reply can be matched to a path.
You do not invent products, collections, prices, or ids. You may mention only these collection names: {names or (none)}.
If they named one of those collection names, treat it as that collection. Do not ask whether they want to buy that name as an unknown item.

Current question shown to the customer:
{question body}

What this step is doing:
{step Doing}

What a valid reply looks like:
{step Expect}

Paths: Buy products, Check order status, Checkout, Talk to staff.
If they named an item that is not one of the collection names above, ask whether they want to buy that item, see a collection, check out, check an order, or talk to staff. Do not say the item is available.
Use the context above to ask a valid clarifying question. Write one short question in {language name} using that language's native script only (never Manglish or Latin romanization). At most 1–2 sentences. Do not paste, summarize, or restate the context, prior messages, images, dates, or already-confirmed details in the reply — return only the simple question.

Customer message:
{their message}
```

## Store-info prompt

Built by `buildStoreInfoAnswerPrompt`. Facts are About us, privacy, terms, refund, and shipping from `get_store_info`. Empty facts skip the model.

```
Answer one customer question using ONLY the store information fields below.

Rules:
- Use only facts present in the fields. Do not invent products, prices, hours, policies, or services.
- Set confident=true only when the fields fully support a clear answer.
- confidence is from 0 to 1. Use 0.9 or higher only when the answer is clearly supported.
- answer must be short plain WhatsApp text (no markdown, no bullet markers).
- When unsure, set confident=false, confidence below 0.9, and answer to "".

Reply with JSON only:
{"confident":false,"confidence":0,"answer":""}

Store information:
- {field}: {value}

Customer question:
{their message}
```

## Recover prompt

Built by `BuildRecoverPrompt` after `create_order` fails. The customer does **not** see this JSON. `try_later` and `give_up` are ignored as customer text. The handoff cart message is sent instead. `missing_field` uses each `fields[].message`, or `Could you share your {label}?` when that message is empty. `AskText` stores the reply with no intent check.

Allowed fields, in ask order: `customer_name` (name), `customer_phone` (phone number), `customer_email` (email address), `address_line_one` (address line 1), `address_line_two` (address line 2), `city`, `state`, `country`, `pincode`, `customer_notes` (order notes).

```
You help a shopping chatbot recover from a store lookup or order failure.
You write only what the customer should see. Never mention APIs, URLs, HTTP, tokens, keys, payloads, curl, databases, servers, or internal ids.

Failure kind: create
Customer resource: order
HTTP status if known: {status or (unknown)}
Sanitized hint for you only (may list missing field names): {hint or (none)}

Allowed missing_field values when kind is create, in the order to ask them: customer_name (name), customer_phone (phone number), …

kind try_later: temporary fetch/create problem. Ask them to try again shortly. Leave field empty and fields empty.
kind missing_field: only when kind is create and the hint clearly names one or more missing customer details. Put every named detail in fields, using only allowed values, in the order listed above. Skip allowed values the hint does not name. Each fields item is {"field":"<allowed value>","message":"<one or two short sentences asking for that value only>"}. Also set field to the first fields item. Do not stop after one missing detail.
kind give_up: cannot recover. Apologize briefly and suggest trying later or messaging staff. Leave field empty and fields empty.

message must be one or two short sentences the customer can read on WhatsApp. For missing_field it can repeat the first question. confidence from 0 to 1.
reasoning explains why you chose this kind, for an operator debugging the flow. At most 100 words. The customer never sees it.

Return JSON only:
{"kind":"try_later","field":"","fields":[],"message":"","confidence":0,"reasoning":""}
```

If the TiQR hint contains `validation fields: …`, those names replace the model’s field list and the kind is forced to `missing_field`.

---

## 1. Load store

Call names: `store` (`get_store`), `collections` (`list_collections`, `limit=20`), `faqs` (`list_faqs`), `store_info` (`get_store_info`).

**What it is.** Loads the store before any menu. FAQ and store-info failures do not stop shopping. Store or collection failure ends the session.

**Sent to the customer.** Nothing on success. On `get_store` failure: `I was unable to fetch the business information.` plus the handoff connect line, then transfer. On collection failure or an empty collection list: `I was unable to fetch our collections.` plus the same connect line, then transfer.

**Valid input.** None. This step does not wait.

**Invalid.** Not a customer reply. There is no retry inside the session.

**Sent to TiQR.** `GET` buyer `get_store`, `list_collections?limit=20`, `list_faqs`, `get_store_info`. Reads may be served from the catalog cache. Orders and writes are not cached.

## 2. Welcome menu

Call name: `intent`.

**What it is.** First question after the store loads.

**Sent to the customer.** Buttons `buy_products` / Buy products, `check_order_status` / Check order status, `talk_to_agent` / Talk to staff.

Body, in order, separated by a blank line:

1. Saved welcome on the TiQR Ecommerce binding, with a trailing `What would you like to do?` stripped so it is not doubled. If the welcome is empty: `Welcome to {store name}.` When the store has no name, this part is omitted.
2. Fulfillment summary from `delivery_modes`, or nothing when modes are empty. Pickup only: `We offer store pickup.` Delivery only: `We offer delivery.` plus `Free delivery within N km` and `Delivery available up to M km` when those radii are set. Both: `We offer store pickup and delivery.` plus the same radius clauses.
3. `What would you like to do?`

**Valid input.**

| Input | Result |
| --- | --- |
| Button id or exact title Buy products | Collection list |
| Button id or exact title Check order status | Order status |
| Button id or exact title Talk to staff | Transfer with `I'm connecting you with a team member who can help.` |
| Free text that intent grounds as `choice` / `buy_products` | Collection list |
| `choice` / `check_order_status` | Order status |
| `choice` / `talk_to_agent` or any other choice id | Transfer |
| `collection` with a loaded collection id | That collection. `handoff_policy: after_capture` goes to early handoff |
| `product` with a non-empty query | Product search |
| `checkout` | Checkout when the session cart has lines. Empty cart sends the empty-cart line and opens the collection list |
| `faq` with a loaded FAQ id | Stored FAQ answer, then the menu stays open |

**Invalid input.** Shared ladder. Doing: `The customer is at the welcome menu.` Expect: `They may pick Buy products, Check order status, or Talk to staff, name a collection or a product, ask a store FAQ, or ask to check out.` Pattern: `(none)`. Catalog and FAQ routes are on.

## 3. Collection list

Call name: `collection`.

**What it is.** Shown when Buy products was chosen, or when a search found nothing and the buy loop continues.

**Sent to the customer.** List. Body: `Choose a collection below and we'll show you the items in it.` Header: `Our collections`. Button: `Browse`. Section: `Collections`. Footer: `Tap Browse to continue`. Row title `{{name}}`, description `{{description}}`. More than 10 rows: 9 rows plus **Show more**. Show more repeats `list_collections` / `search_collections` via `next` or `offset`.

**Valid input.** A row id or exact title from any loaded page, including rows not on the current page. That saves `collection_id` and `collection_name`. `handoff_policy: after_capture` starts early handoff and does not list products. Also: grounded `product` (search), `collection` (another loaded collection), `checkout` (same empty-cart rule as the menu), welcome-menu jumps, Show more.

**Invalid input.** Shared ladder. Doing: `The customer is looking at the collection list.` Expect: `They may pick a collection, name a product, or ask to check out.` Catalog on. FAQ route off (FAQs are not passed on this ask).

**Sent to TiQR.** None until a row or a product query is accepted. The rows already came from step 1.

## 4. Early handoff

Used when the chosen collection, or the product’s collection, has `handoff_policy: after_capture`. It does not write `tiqr_cart` and does not call `create_order`. Fulfillment-time slots are not asked.

### 4.1 Intro

**Sent.** `This is a custom {collection name} request — I’ll collect a few details and connect you with our team.` Name falls back to `order`. No wait.

### 4.2 Capture fields

Same questions as [step 9](#9-capture-fields), one per required field on that collection. Checkout phrases do **not** leave the question. A value that fails validation is not stored.

**Invalid customer message.** `Please provide a valid value.` plus the field prompt again. Photo/file fields: `Please send the photo again.` or `Please attach the requested file again.` plus the field prompt. No intent model.

### 4.3 Add-ons

Same as [step 10](#10-catalog-add-ons) when any loaded product has active catalog add-ons. Call prefix: `early_handoff_addons`. No add-ons: this step is skipped.

### 4.4 Fulfillment

Same as [step 12](#12-fulfillment-mode) and [step 13](#13-delivery-pin). A menu or catalog divert that is not “switch to pickup” leaves early handoff and returns to the buy loop.

### 4.5 Details form

Call name: `early_handoff_details`. Same Meta flow ids and prefill as [step 16](#16-details-form).

**Sent.** Header `Your details`, button `Enter details`. Pickup body: `Please share your name, email, and phone number so we can continue your request.` Delivery body: `Please share your name, phone number, and address so we can continue your request.`

**Valid input.** A WhatsApp Flow submission.

**Invalid input.** Shared ladder with catalog **off**. Doing: `Collecting customer details for a custom request before agent handoff.` Expect: `A WhatsApp Flow submission with contact details.` A product or collection route is ungrounded and transfers after store-info fails.

### 4.6 Finish

Stages a commerce draft (capture notes, address, a single option line when the product is known) and completes the handoff. In hours: agent transfer plus `handoff_message` (or the default specialist line) and `Your enquiry number is N.` when enquiry create succeeds. Outside hours: the same line through the closed-hours message, no agent transfer. Failure: `We saved your request, but could not connect an expert just now. Please try again shortly.`

## 5. Products

Call name: `product`.

**What it is.** After a collection row or a product search.

**Sent to TiQR.** Collection route: `list_products` with `category_id` (no limit on the first call). Product route: `search_products` with `search` and `limit` 20. Empty id or empty query, or a search with no rows: `I couldn't find that in our store.` then `Please choose a collection below, or tell me another item.` API error: `I was unable to fetch those products.` plus the handoff connect line, then transfer.

**Sent to the customer when rows exist.**

| Rows | Message |
| --- | --- |
| 1 | Image. Body: `*{name}* ({currency}{min_price})` then the description. Button title `Add to cart`. Missing image uses `FallbackMedia`. |
| 2 or more | Carousel. Body: `Here is what's available in *{collection_name}*.` then `Tap *Add to cart* on the item you'd like.` For a search, `collection_name` is the search text, not a collection. Card button `Add to cart`. More than 10: 9 cards plus Show more. |

**Valid input.** Add to cart on a shown card, including a card from an earlier page. That saves `product_id`, `product_name`, and `options`. A product whose collection is early-handoff goes to step 4 instead of options. Also grounded `collection`, `product`, `checkout`, and welcome-menu jumps.

**Invalid input.** Shared ladder. Catalog on. One product: Doing `One product is on screen.` Expect `They may accept it or name it.` Several: Doing `Several products are on screen.` Expect `They name one of those products.`

## 6. No options

**What it is.** The picked product has zero options, or the only option has no id.

**Sent.** `Sorry, this item isn't available right now.` then `Please choose another item from our collections.` Nothing is added. The function still returns success, so the cart-action step runs next. An empty cart then sends the empty-cart line and returns to collections. A cart that already has lines shows the cart buttons.

## 7. Option list

Call name: `option`. Skipped when the product has exactly one option; that option is taken.

**Sent.** List. Body: `This item comes in more than one option. Please choose the one you'd like.` Header: `Choose an option`. Button: `Choose`. Section: `Options`. Row title is the option name with its price.

**Valid input.** A row id or exact title. Saves `option_id` and `option_name`. Catalog diverts (another product, checkout, welcome menu) leave this step because `AskList` allows catalog routes.

**Invalid input.** Shared ladder. Doing: `The product has more than one option on screen.` Expect: `They pick one of those options.` Pattern: `(none)`.

## 8. Quantity

Call name: `quantity`.

**Sent.** `How many *{option name}* would you like?` then `Reply with a whole number of 1 or more, for example 1.`

**Valid input.** Text matching `^[1-9][0-9]*$` (1, 2, 10, not 0, not 01, not decimals). A number word is valid only when intent returns route `answer` whose `answer` matches that pattern (`two` must come back as `2`).

**Invalid input.** Non-matching text uses the shared ladder with pattern `^[1-9][0-9]*$`. Doing: `The customer is saying how many units to add.` Expect: `A whole number of 1 or more, as digits or as a number word such as two.` Catalog is on, so a product name can leave this step and search. If the model returns `answer` and the value still fails the pattern, **nothing is sent** on that turn. See issues.

Checkout phrases that are not a valid number set checkout divert and leave the step.

## 9. Capture fields

Call names: `capture_{index}_{key}`.

**What it is.** Each required field on the product’s collection (else the selected collection) that has a key, label, and type. Optional and incomplete fields are skipped. Asked before the cart line is written.

**Sent.** Label, then help text, then `Options: a, b` when the field has options. Image types also get `Please send a photo.` File types get `Please attach the requested file.` If the label is empty: `Please share {label}.`

**Valid input.**

| Type | Accepted |
| --- | --- |
| `number` | Parses as a float |
| `single_select` | One option, case-insensitive |
| `multi_select` | Comma-separated options, each must match |
| `image`, `images`, `file`, `document`, `media`, `attachment`, or a key containing both `reference` and `image` | A WhatsApp photo or document with a media URL. Preview paperclip stores metadata only |
| anything else | Any non-empty text |

On the buy path, these exact phrases leave the question and start checkout when they are **not** a valid value for the field: `checkout`, `check out`, `check-out`, `place order`, `place my order`, `ready to checkout`, `ready to check out`. Early handoff does not divert.

**Invalid customer message.** `Please provide a valid value.` plus the original field prompt. Attachment fields use the photo/file retry lines from step 4.2. No model.

**Bug.** A text field accepts `checkout` and `talk to staff` as the answer, because the checkout divert runs only when the value is invalid. See issues.

## 10. Catalog add-ons

Call names: `product_addons_1`, `product_addons_parse_1`, `product_addons_clarify_1` (attempt number goes up). Early handoff uses the prefix `early_handoff_addons`.

**What it is.** After capture, `get_product` loads active add-ons on the product and on the chosen option. No add-ons: skipped. The reply is **not** classified by the intent prompt. `AskText` accepts any non-empty text, then the guide role parses it.

**Sent.** 

```
This product has the following add-ons:
1. {name} — {price when price > 0}

Reply with the item number and how many you want, for example item 1 - 2.
You can list more than one item. Say Skip if you don't want any.
```

**Valid input.** Exact `skip`, `no`, `none`, `done`, or `continue` (any case): continue with nothing saved. A parse with intent `select`, confidence at least 0.75, and in-range indexes with quantities: save `{addon, quantity, name}` on `commerce_addons` and send `Added {name} x{qty}, …`. Intent `skip` is also accepted when confidence is 0 or at least 0.75.

**Invalid.**

| Outcome | Customer message | Model prompt |
| --- | --- | --- |
| Named item, quantity missing | `How many of item N ({name}) would you like? Reply with a whole number of 1 or more.` Several items are joined in one sentence | [Add-on parse prompt](#add-on-parse-prompt), with those indexes listed as waiting |
| Unclear, confidence under 0.75, or an index outside the list | The model’s `question` when it does not mention an id. Otherwise `I didn't catch that.` plus the hint, then the full list again | Same parse prompt |
| 4th unclear or missing-quantity turn | `I'm connecting you with a team member who can help.` | None. Transfer uses `AgentHandoff`, not `UnsureConnect` |

`checkout` and `talk to staff` are parsed as add-on text. They do not leave the step unless the model returns skip or the clarify cap transfers.

### Add-on parse prompt

```
You parse a customer's WhatsApp reply about optional product add-ons.
Return JSON only. Never invent, guess, or output an add-on id, product id, option id, or any other id.
Refer to listed items by their index number only.

Allowed items (index and name only):
1. {name}

Indexes waiting for a quantity (if any): {indexes or (none)}

Customer reply:
"{their message}"

intent select: the customer clearly chose one or more listed items.
intent skip: the customer does not want add-ons.
intent unclear: the reply does not clearly map onto the list.

Rules:
- items[].index must be a listed index from 1 to {n}. Never invent an index.
- Copy quantity only when the customer stated it (digits or a number word), including forms like "item 1 - 2", "1 x 2", or several lines.
- If they named an item and left the quantity out, put that index in missing_quantity and leave quantity null. Do not assume 1.
- question is one short WhatsApp question only for unclear or missing quantity. Name the list number and add-on name. Do not mention ids.
- confidence from 0 to 1. Use a low value when the mapping is uncertain.
- reasoning is for operators only, at most 100 words.

Return JSON only:
{"intent":"select","confidence":0,"items":[{"index":1,"quantity":2}],"missing_quantity":[],"question":"","reasoning":""}
```

## 11. Cart actions

Call name: `next` on the first add, then `next_2`, `next_3`, … after an edit.

**What it is.** One interactive message when the filled body fits in 1024 characters (less when a non-English option legend is appended). Otherwise the cart is plain text (split at 4096, on line boundaries) and the buttons use only the question.

**Sent after an add (fits in one message).**

```
*{option name}* has been added to your cart.

*Your cart:*
{numbered lines, capture answers under the line}

*Subtotal:* {session subtotal}

You now have *{unit count}* items in your cart.

Would you like to add anything else, edit your cart, or place your order?
```

Buttons: `add_more` / Add more items, `edit_cart` / Edit cart, `checkout` / Checkout.

After an edit, the “has been added” line and the unit-count line are omitted. The question stays.

**Valid input.** Those three ids or exact titles. Add more returns to the collection list. Checkout starts [step 12](#12-fulfillment-mode). Edit cart starts [step 11.1](#111-edit-cart). Also catalog, checkout, and welcome-menu diverts from the shared ladder.

**Invalid input.** Shared ladder. Doing: `The customer just added an item and can edit the cart or check out.` Expect: `They may add more, edit the cart, or check out.`

Empty cart at the start of this step sends `Your cart is empty.` then `Please choose a collection below to add items, then you can check out.` and returns to collections.

### 11.1 Edit cart

Call name: `cart_edit_text_{n}`. `AskText`: any non-empty reply is stored. **Intent is not called.** `talk to staff` is an edit instruction, not a transfer.

**Sent.** The cart summary, then:

```
Tell me what to change. For example:
- remove item 1
- remove Kunafa
- change item 2 to 1
- reduce Chocolate to 1
```

**Valid input.** `remove` or `delete` plus an item number or a name that matches one line. `change` / `update` / `set` / `reduce` / `make` plus a target and `to`, `x`, or `=` and a quantity from 1 to 9999. A bare item number or a unique name asks a follow-up.

**Follow-ups (also AskText or AskNumber, no intent except the quantity pattern).**

| Situation | Sent |
| --- | --- |
| Name or number with no action | `Should I remove that item, or change its quantity?` then `Reply like: remove, or change to 2.` |
| Several lines match the name, or the name matched nothing and a number is required | `Which item number did you mean?` plus the cart, then `Reply with a number, for example 1.` Pattern `^[1-9][0-9]*$` |
| Action known, target missing | `Which item should I update?` plus the cart |
| That reply is still ambiguous | `Please reply with the item number from the list.` plus the cart |
| Quantity missing on a set | `What quantity would you like for *{name}*?` then `Reply with a whole number of 1 or more.` |

**Invalid customer message.** Action not understood: `I couldn't understand that edit. Please try again, for example: remove item 1, or change Kunafa to 2.` The cart buttons are shown again. Item not in the cart: `I couldn't find that item in your cart.` plus the summary. A quantity that is not digits uses the shared ladder (catalog on). A bad `answer` that still fails the pattern sends nothing that turn.

**Success.** `Removed *{name}* from your cart.` plus the summary, or `Your cart is empty.` `Updated *{name}* to quantity {n}.` plus the summary.

Remove and set-quantity match the **option id** of the numbered line, not the line index. Two lines with the same option and different capture answers are both removed, or the first one is updated. `create_order` later merges those lines into one quantity. See issues.

## 12. Fulfillment mode

Call name: `fulfillment_mode`, or a `Once` key when there is nothing to ask.

**What it is.** Runs at the start of checkout, and inside early handoff, before checkout-preview.

| Store `delivery_modes` | Sent | Stored |
| --- | --- | --- |
| Missing or empty | Nothing | `delivery_mode` = `PICKUP_FROM_STORE` |
| Pickup only | Nothing | `PICKUP_FROM_STORE` |
| Pickup and delivery | `How would you like to receive your order?` Buttons `pickup` / Store pickup, `delivery` / Delivery | The chosen mode |
| Delivery only | Nothing yet | `DELIVERY_TO_LOCATION`, then the pin step |

**Valid input.** Those two ids or exact titles. Delivery continues to the pin. Also catalog and welcome-menu diverts. Repeating the previous choice resends this question (`route current`).

**Invalid input.** Shared ladder. Doing: `The customer is choosing pickup or delivery after confirming the cart.` Expect: `They pick Store pickup or Delivery.` Catalog on.

Saying checkout here does not open the details form. The buy loop starts checkout again, which is still this step or the pin step.

## 13. Delivery pin

Call names: `delivery_location_1`, then `_2`, … There is no attempt cap.

**Sent.** Location request:

```
Please tap Send location to share your delivery pin so we can check if we deliver to you.

Live locations are not supported. Please share a fixed location pin by tapping Send location, or use Attach > Location > Share your location.
```

**Valid input.**

| Input | Result |
| --- | --- |
| WhatsApp location pin | Latitude and longitude are stored. Distance is computed locally from the store pin and radii. No TiQR call |
| Google Maps link with coordinates (`@lat,lng`, `q=lat,lng`, `!3d…!4d…`) | Same as a pin |
| In range (`free`, `paid`, or `unconfigured`) | `We can deliver to that location.` Zone and distance are stored. Shipping text is left for checkout-preview. Checkout continues |
| Out of range, pickup offered | The out-of-range paragraph, then buttons |
| Out of range, delivery only | The out-of-range paragraph, then the pin request again |
| Previous choice `pickup`, and the store offers pickup | `Got it — we'll prepare this as a store pickup order.` Mode becomes pickup. The pin is not required |
| Previous choice `delivery` while the pin is already being asked | The location request is sent again |
| Menu Check order status | Leaves for order status |
| Menu Talk to staff | Transfer |
| Menu Buy products | The location request is sent again. Checkout is not cancelled |

**Out of range text.** `Sorry, we're currently unable to deliver to this location.` Then the store address and the free / max radius lines when the store has them. Pickup offered: `Please provide a location within our delivery radius, or choose Store Pickup as your preferred option.` Delivery only: `Please provide a location within our delivery radius.` Both end with `Thank you for your understanding.`

**Fallback buttons** (only when pickup exists). Call `delivery_fallback_{n}`. Body: `Would you like to pick up from the store instead, or share another location?` Buttons `pickup` / Store pickup, `share_again` / Share again. Share again asks for another pin. Invalid replies use the shared ladder. Doing: `Delivery is out of range. The customer can switch to pickup or try another pin.` Expect: `They pick Store pickup or Share again.`

**Invalid pin text.** A Maps link without coordinates uses the unreadable-pin line, then the location request again. No model on that path. Any other non-pin text uses the shared ladder with catalog **off**. Doing: `The customer is sharing a delivery location pin.` Expect: `A WhatsApp location pin with latitude and longitude.` A grounded `product` or `collection` route is ungrounded and transfers after store-info fails.

A place name is not accepted as a pin.

## 14. Checkout preview

Call names: `checkout_preview_1`, then `_2` after a retry or an edit. Confirm buttons: `checkout_preview_confirm_{n}`. Failure buttons: `checkout_preview_fail_{n}`.

**What it is.** Guest `POST /service/buyer/cart/checkout-preview/` after fulfillment is known. Not a placed order. Amounts arrive in paise and are converted once.

**Sent to TiQR.** `items` from the session cart (`product_option`, `quantity` only; same option id is merged), `delivery_mode` (empty becomes pickup), `addons` when any were chosen (`addon`, `quantity`), and for delivery `latitude` / `longitude` to 6 decimal places. No discount code.

**Sent to the customer on success.**

```
*Your order:*
{preview lines, with capture answers under the matching option}

*Subtotal:* {original_amount}
*Discount:* {label} −{amount}    (each applied discount)
*Shipping:* {original_shipping_fee, or Free}    (delivery only)
*Shipping discount:* −{amount}    (when present)
*Total:* {amount}

Please confirm these items before we continue checkout.

You can edit quantities or remove items first.
```

Buttons: `confirm_items` / Confirm items, `edit_cart` / Edit cart, `add_more` / Add more. Same 1024 split as the cart: full text, then the confirm question alone.

**Valid input.** Confirm items opens the details form. Add more returns to the collection list. Edit cart uses [step 11.1](#111-edit-cart), then preview is called again. Catalog and welcome-menu diverts also leave the buttons.

**Invalid input.** Shared ladder. Doing: `The customer is confirming the priced order before the details form.` Expect: `They confirm items, edit the cart, or add more products.`

**Preview failure.** `I couldn't load your order total just now.` then `Please try again, or edit your cart.` Buttons `try_again` / Try again, `edit_cart` / Edit cart. Doing: `Checkout preview failed. Offer try again or edit cart.` Expect: `They tap Try again or Edit cart.` A second failure transfers. Customer message starts `I couldn't load your order total just now.` then `Here is what was in the cart:` and the cart, add-ons, and address, then `So, I'm connecting you with a team member who can help.` The details form does not open. The session subtotal is not used as a fallback.

Empty cart before the call sends the empty-cart line and returns to browsing.

## 15. Session cart confirm (not on the live path)

`reviewCartBeforeOrder` still builds a session-priced confirm (`cart_review_{n}`) with the same three buttons as preview. Checkout does not call it. Confirm is the priced preview in step 14. Do not debug a missing pre-fulfillment total against this function.

## 16. Details form

Call name: `details`.

**Sent.** WhatsApp Flow. Header `Your details`, button `Enter details`. Pickup body: `Please share your name, email, and phone number so we can place your pickup order.` Delivery body: `Please share your name, phone number, and address so we can place your delivery order.`

Flow id: the account binding `pickup_flow_id` or `delivery_flow_id`. Defaults: pickup `1484028330223507` (name, email, phone, notes), delivery `1557965846018132` (those plus address). Prefill may include `customer_phone` and, for delivery, `pincode`, `city`, `state`, `country` from a reverse geocode of the pin. Unknown keys are dropped. A missing maps key still opens the form.

**Valid input.** A Flow submission. Fields are copied onto the session.

**Invalid input.** Any typed message uses the shared ladder with catalog **off**. Doing: `The customer is asked to submit the details form.` Expect: `A typed message is not the form.` Product and collection routes transfer after store-info fails. `AskFlow` ignores the route it gets back. A checkout route sets `Divert` and sends nothing; the next turn asks for the form again. A welcome-menu choice is recorded as an ok call on `details`. The next turn replays that call as a completed form and continues into `create_order`. See issues.

## 17. Place order

Call names: `order`, then `order_retry_1`, `order_retry_2`. Default is one try plus two retries (`codedflow.order_retries`, default 2). Missing fields are collected only while retries remain.

**Sent to TiQR.** `create_order` with:

| Param | Source |
| --- | --- |
| `email` | Form email if it looks like an email. Otherwise `support+{phone digits}@tiqr.events` |
| `phone_number` | Form phone when present |
| `items` | Cart lines reduced to `product_option` and `quantity`. Same option id is merged. Capture answers are not sent here |
| `addons` | `addon` and `quantity` when any were selected |
| `notes` | Plain customer note when nothing was captured. Otherwise `{product}({option})` then `- {label}: {value}`, blank line between products, and `Note: {customer note}` when the form had a note |
| `new_address` | Delivery only: name, address lines, city, state, country, pincode, email, phone, latitude, longitude |
| `delivery_mode` | Session value, or pickup if empty |
| `buyer_meta_data` | name, email, phone, notes. Delivery also has the pin |
| `shipping_fee_paise` | Not sent |

**Sent on success.** `Order placed!` plus `Your order number is {display_uid}.` when present. Delivery fee and total lines when the response has them. When a payment URL is present, that body is a **Pay now** button. If the button cannot be sent: the same body plus `Pay here: {url}`. The session then ends. The message does not say the order is already confirmed.

**Failure.** Recover model runs ([Recover prompt](#recover-prompt)). The canned `We couldn't place your order just now…` line is the fallback stored on the plan. It is not what the customer sees on give-up.

| Plan | Customer message |
| --- | --- |
| `missing_field` and retries left | One `AskText` per field: the plan’s `message`, or `Could you share your {label}?` Any non-empty reply is stored, including `talk to staff`. Then `create_order` runs again |
| `try_later`, `give_up`, bad JSON, or retries used | `Placing order for the following items failed.` then each line `name x qty`, capture answers, add-ons, address, then `So, I'm connecting you with a team member who can help.` Transfer. The model’s sentence is not sent |

## 18. Order status

Call names: `orders_page`, `pick_order`, `selected_order`.

**What it is.** Recent orders for this WhatsApp number. Not the cart.

**Sent to TiQR.** MCP `list_orders_by_phone` with `store_id`, the session phone, and `limit` 10. Then MCP `lookup_order_status` with `store_id`, phone, and `order_display_id` = the selected `display_uid`.

**Sent.**

| Case | Message |
| --- | --- |
| Tool error | `I couldn't look up your orders just now. Please try again in a moment.` Session ends |
| No orders | `I couldn't find a recent order for this phone number.` Session ends |
| Orders exist | List. Body: `Here are your recent orders. Pick one to see its status and summary.` Header: `Your orders`. Button: `Orders`. Title: `display_uid`. Description: placed date |
| A row is chosen | `Order *{id}*` / `Status: {status}` / `Items` / `Total` / `Fulfillment: Store pickup` or `Delivery` plus address. Session ends. No payment link |

**Valid input.** A row id or exact `display_uid`.

**Invalid input.** The list uses the shared ladder with catalog **on**. Doing: `The customer is choosing which order to check.` Expect: `They pick one of the listed orders.` A guide question or a transfer is sent as usual. A grounded product, collection, or checkout route is stored only in memory and **order status returns without sending anything**. The next customer message is a new turn. See issues.

---

## What each waiting step does with a bad reply

| Step | Model on a bad reply | Customer text when it is not a guide question |
| --- | --- | --- |
| Welcome, collections, products, options, cart buttons, fulfillment buttons, preview buttons | Intent, then store-info, then guide | Transfer line, FAQ answer, store-info answer, or `I can't help you with that` (refuse is off, so this last one is unused) |
| Quantity and cart-edit quantity | Same, with pattern `^[1-9][0-9]*$` | Same. A bad `answer` value sends nothing |
| Capture fields | None | `Please provide a valid value.` plus the question. Attachment retry lines for photo/file |
| Add-ons | Add-on parse prompt, not the intent prompt | Missing-quantity question, model question, or `I didn't catch that.` plus the hint and the list. 4th time: agent handoff line |
| Cart edit text | None | `I couldn't understand that edit…` or `I couldn't find that item in your cart.` |
| Delivery pin | Intent with catalog off, except an unreadable Maps link | Unreadable-pin line, then the location request. Out of range uses the radius paragraph |
| Details form | Intent with catalog off | Guide question, store-info answer, or transfer. No fixed “please use the form” line |
| Order recovery fields | None | The recover question is sent again only because the next `create_order` fails. A nonsense reply is stored |
| Order list | Intent with catalog on | Guide, store-info, or transfer. A shopping divert sends nothing |

---

## Gaps and issues

These are defects or sharp edges in the current code. The Gaps list in [tiqr-ecommerce-coded-flow.md](./tiqr-ecommerce-coded-flow.md) is older: cart edit exists now, and button steps allow catalog routes.

### Fix

1. **Cart edit and** `create_order` **collapse lines that are meant to stay separate.** Two cakes with the same option and different capture answers are two session lines. `RemoveTiqrCartLine` deletes every line with that option id. `SetTiqrCartLineQty` changes the first line with that option id, not the numbered line. `OrderItemsForAPI` adds the quantities together and sends one `product_option`. The notes can still list both messages. A “remove item 2” or a placed order can change the wrong cake or merge them.
2. **Order status drops shopping diverts.** `pick_order` allows catalog routes. `orderStatus` ignores the reply route and `Divert`. A confident “checkout” or product name while the order list is open consumes the message and sends nothing.
3. **A quantity** `answer` **that still fails the pattern sends nothing.** `AskNumber` sets an in-memory `skipIntent` flag and returns. The flag does not survive the turn. The customer sees silence instead of the quantity question.
4. **Capture and cart edit and order recovery swallow “talk to staff”.** Capture accepts any non-empty text. The checkout phrase list diverts only when the text is not a valid field value, so `checkout` is stored on a free-text field. Cart edit and `collectRecoverFields` use `AskText` and never call intent. Staff requests become the cake message, the edit instruction, or the email.
5. **Add-ons do not use the intent ladder.** `checkout` and `talk to staff` are parsed as add-on text. The fourth unclear reply transfers with the generic agent line, not `UnsureConnect`, and it does not try store-info.
6. **Details form treats a menu jump as a submitted form.** `AskFlow` ignores the route `resolveFreeText` returns. Catalog is off, so a product name transfers. A checkout route sets `Divert`, sends nothing, and the next turn asks for the form again. A welcome-menu choice (`check_order_status`, `talk_to_agent`, `buy_products`) is written as an ok `_coded_calls` row named `details`. The next inbound message replays that row and `checkout` goes on to `create_order` without a form submission.
7. **Delivery-only and out of range never ends.** `resolveDeliveryLocation` asks for another pin with no cap and no automatic transfer. A typed handoff can still transfer, because a non-pin message goes through intent.
8. **Checkout said during fulfillment does not open the form.** It re-enters checkout, which is still the mode question or the pin. The customer can think checkout was ignored.
9. **Second checkout-preview failure and non-field** `create_order` **failures hide the recover sentence.** The customer gets the cart handoff. `tiqrEcommerceFailed` (`We couldn't place your order just now…`) is only the plan’s stored fallback.
10. **Search labels the carousel with the query.** `productsForRoute` sets `collection_name` to the search string, so the body says the items are available in that name.
11. **Store or collection load failure ends the session.** There is no retry until the customer starts a new session with the keyword.
12. **Language sticks.** The first non-empty intent language is `customer_language` for the rest of the session. A later message in another language does not replace it. Button taps never set it.
13. **Order status is thinner than checkout.** No payment link, no “pay again”, and a failed lookup uses the same sentence as a tool error. An empty list has its own sentence.
14. **Pin success does not quote a fee.** In-range copy is only `We can deliver to that location.` The fee is on checkout-preview. `shipping_fee_paise` is stored for a paid zone and is not sent on `create_order`.
15. `reviewCartBeforeOrder` **is unused.** A session-priced confirm before fulfillment is dead code. Debug a missing total against checkout-preview, not this function.
16. **Narrative doc gap “no cart management” is stale.** Lines can be removed and quantities can be set from the cart buttons and from the preview. The bugs in item 1 are what is left of that gap.
17. **Narrative doc gap “naming a product on a product card transfers” is stale for cards, options, cart, and fulfillment buttons.** Those asks pass `AllowCatalog: true`. It is still true for the location pin and the details form.
18. **Collection** `ai_instructions` **are never added to a prompt.** Required capture fields are asked as their own questions. The store-authored instruction text is unused.
19. **Fallback product image and unpublished flow prefill.** A product with no image uses one hardcoded DigitalOcean URL. Prefill appears only after the published Meta flow declares the screen data keys. Published flows are immutable; a wrong id on the binding opens the wrong form.
20. **Early-handoff finish failure says the request was saved even when the draft step failed.** `CompleteCodedEarlyHandoff` returning false sends `We saved your request, but could not connect an expert just now.` The same sentence is used when account or contact is missing, before staging.
