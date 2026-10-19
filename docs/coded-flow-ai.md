# TiQR Flows AI

TiQR Flows use four AI roles. Each role can use Account AI, TypeSafe Jev, or Vercel AI Gateway.

## Roles

| Role | Purpose | Settings field |
| --- | --- | --- |
| Intent | Map free text onto a route | `ai_intent_provider` |
| Translation | Translate authored lines into the customer language | `ai_translate_provider` |
| Guide | Ask one clarifying question when intent is unclear | `ai_guide_provider` |
| Order recovery | Classify create_order failures and missing fields | `ai_recover_provider` |

Default for every role is `generic` (Account AI). Account AI is the WhatsApp account provider and model on the chatbot AI tab (OpenAI, Anthropic, or Google).

## Engines

| Value | Host | Typical model | Key field |
| --- | --- | --- | --- |
| `generic` (default) | Account LLM chat completions | Account `ai_model` | `ai_api_key` |
| `jev` | `https://api.typesafe.ai/v1/systemone` | `jev-latest` | `ai_typesafe_api_key` |
| `gateway` | AI Gateway System One or chat completions | Picklist below | `ai_gateway_api_key` |

TypeSafe Jev is a System One evaluator. It fits **intent** (structured route Choice + Noul judges). It cannot generate free text, so **translation**, **guide**, and **order recovery** set to `jev` fail at runtime until those roles use Account AI or a generative Gateway model.

AI Gateway models (fixed picklist):

| Model | Use |
| --- | --- |
| `typesafe-ai/jev` (default) | Structured intent via System One |
| `google/gemini-2.5-flash` | Free-text roles (translation, guide, recovery) via chat completions |

Keys are encrypted at rest and never returned by GET; the API exposes `ai_typesafe_api_key_set` and `ai_gateway_api_key_set` instead. One TypeSafe key and one Gateway key are shared across all roles that need them.

Language models such as Gemini may answer through structured output without native confidence. When probabilities are empty, intent takes the unclear path instead of acting on a zero confidence.

Structured intent (`jev` / `gateway` with System One) does not require “Enable AI responses”. Guide, translation, and recovery on Account AI do. Gateway chat completions for those roles use the Gateway key and model only.

Account AI free-text roles (translation, guide, recovery) send the role prompt with recent text history for context, but omit media attachments and the store system prompt. That context is for a valid reply only; the customer-facing output is a simple translation or short question and must not restate it. Outbound replies use the language’s native script (Manglish detection maps to Malayalam script).

## Jev request shape (intent)

Intent uses one or two System One requests so FAQ and catalog lists are not pasted into every turn.

**Route request** (always for free text that is not an exact local match):

- A route **Choice** for the routes this step can return (includes `faq` only when `AllowFAQ` is set and FAQs are loaded)
- Speculative **Choice** fields for choice id, collection id (only when collections are not already the on-screen choices), and language
- **Noul** judges where two routes can both be true: `names_collection` / `names_product` on catalog steps that still need a separate collection list, `names_product` alone when collection rows are already choices, `asks_for_person` when choices are shown, `states_quantity` on number steps
- Criteria labels are a single string each (no duplicated `what` + `title` / `question` objects)
- Product search strings are derived in code (shopping prefixes stripped). There is no `product_span` question. Product records stay out of the prompt

**FAQ follow-up** (only when the route is `faq` and confidence is usable):

- At most **8** FAQ questions, ranked by token overlap with the customer message
- A `faq_id` **Choice** with a `none` option
- An exact match on a FAQ question skips this call

Exact matches on an on-screen choice title, a loaded collection name, or a FAQ question skip System One entirely.

Code composes `codedIntentResult`. Number words such as “two” are normalized to digits in code. A grounded `faq` route uses the loaded FAQ id so code can send the stored answer without inventing text. The existing grounding check and **0.75** confidence threshold still decide whether to act or ask a guide question.

Language labels for Jev (open detection): `en`, `hi`, `ml`, `ta`, `te`, `kn`, `ar`, `es`, `manglish`, `other`.

For `tiqr_ecommerce`, detection is limited by the store's `supported_languages` field. Empty means open detection. A non-empty list restricts Account AI and Jev to those codes. Other coded flows use open detection. Jev also gets an internal `other` choice that is never stored. Labels outside the list, empty labels, and `other` become `en`, so the conversation continues in English.
